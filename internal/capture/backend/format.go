package backend

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// wavHeaderSize is the byte length of a canonical 44-byte PCM WAV header
// (RIFF + fmt + data chunk headers) preceding the sample data.
const wavHeaderSize = 44

// AudioFormat describes the PCM encoding of a segment. Only signed
// little-endian PCM WAV is produced in this milestone.
type AudioFormat struct {
	SampleRate int
	Channels   int
	BitDepth   int
}

// DefaultFormat is the recommended capture format: 16 kHz, mono, 16-bit PCM.
func DefaultFormat() AudioFormat {
	return AudioFormat{SampleRate: 16000, Channels: 1, BitDepth: 16}
}

// Name is the stable format identifier stored in manifests.
func (f AudioFormat) Name() string { return "pcm_s16le" }

func (f AudioFormat) Validate() error {
	if f.SampleRate <= 0 || f.SampleRate > 384000 {
		return newError(ErrorInvalidRequest, "", "invalid sample rate", nil)
	}
	if f.Channels != 1 && f.Channels != 2 {
		return newError(ErrorInvalidRequest, "", "invalid channel count", nil)
	}
	if f.BitDepth != 16 {
		return newError(ErrorInvalidRequest, "", "only 16-bit PCM is supported", nil)
	}
	return nil
}

func (f AudioFormat) blockAlign() int { return f.Channels * f.BitDepth / 8 }
func (f AudioFormat) byteRate() int   { return f.SampleRate * f.blockAlign() }

// DurationOf returns the playback duration of dataLen sample bytes in
// milliseconds, truncated. A zero byte rate yields zero.
func (f AudioFormat) DurationMillis(dataLen int64) int64 {
	rate := int64(f.byteRate())
	if rate <= 0 {
		return 0
	}
	return dataLen * 1000 / rate
}

// writeWAVHeader writes a 44-byte PCM header. When dataLen is unknown at write
// time it may be zero and patched later with patchWAVHeader.
func writeWAVHeader(w io.Writer, format AudioFormat, dataLen uint32) error {
	header := make([]byte, wavHeaderSize)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataLen)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:24], uint16(format.Channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(format.SampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(format.byteRate()))
	binary.LittleEndian.PutUint16(header[32:34], uint16(format.blockAlign()))
	binary.LittleEndian.PutUint16(header[34:36], uint16(format.BitDepth))
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataLen)
	_, err := w.Write(header)
	return err
}

// patchWAVHeader rewrites the RIFF and data chunk sizes for a file whose sample
// data has already been written after a placeholder header.
func patchWAVHeader(file *os.File, dataLen uint32) error {
	var field [4]byte
	binary.LittleEndian.PutUint32(field[:], 36+dataLen)
	if _, err := file.WriteAt(field[:], 4); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(field[:], dataLen)
	if _, err := file.WriteAt(field[:], 40); err != nil {
		return err
	}
	return nil
}

// WAVInfo is the validated result of parsing a PCM WAV file.
type WAVInfo struct {
	Format   AudioFormat
	DataLen  int64
	TotalLen int64
}

// ParseWAV validates a PCM WAV byte stream. It accepts standard ancillary RIFF
// chunks produced by local recorders while requiring one PCM fmt chunk, one
// bounded data chunk, internally consistent format fields, and an exact RIFF
// length.
func ParseWAV(data []byte) (WAVInfo, error) {
	fail := func(message string) (WAVInfo, error) {
		return WAVInfo{}, newError(ErrorInternal, "parse_wav", message, nil)
	}
	if len(data) < wavHeaderSize {
		return fail("wav shorter than header")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return fail("missing RIFF/WAVE identifiers")
	}
	riffLen := int64(binary.LittleEndian.Uint32(data[4:8]))
	if riffLen != int64(len(data))-8 {
		return fail("riff chunk size disagrees with file length")
	}
	var format AudioFormat
	foundFormat, foundData := false, false
	dataLen := int64(0)
	offset := 12
	for offset+8 <= len(data) {
		chunkID := string(data[offset : offset+4])
		chunkLen := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start := offset + 8
		end := start + chunkLen
		if chunkLen < 0 || end < start || end > len(data) {
			return fail("wav chunk exceeds file length")
		}
		switch chunkID {
		case "fmt ":
			if foundFormat || chunkLen < 16 || binary.LittleEndian.Uint16(data[start:start+2]) != 1 {
				return fail("format is not canonical PCM")
			}
			format = AudioFormat{Channels: int(binary.LittleEndian.Uint16(data[start+2 : start+4])), SampleRate: int(binary.LittleEndian.Uint32(data[start+4 : start+8])), BitDepth: int(binary.LittleEndian.Uint16(data[start+14 : start+16]))}
			if err := format.Validate(); err != nil {
				return WAVInfo{}, err
			}
			if int(binary.LittleEndian.Uint32(data[start+8:start+12])) != format.byteRate() || int(binary.LittleEndian.Uint16(data[start+12:start+14])) != format.blockAlign() {
				return fail("pcm format rates are inconsistent")
			}
			foundFormat = true
		case "data":
			if foundData {
				return fail("multiple data chunks")
			}
			dataLen, foundData = int64(chunkLen), true
		}
		offset = end
		if chunkLen%2 != 0 {
			offset++
		}
		if offset > len(data) {
			return fail("wav chunk padding exceeds file length")
		}
	}
	if offset != len(data) {
		return fail("wav contains a truncated trailing chunk")
	}
	if !foundFormat || !foundData {
		return fail("wav requires fmt and data chunks")
	}
	if dataLen%int64(format.blockAlign()) != 0 {
		return fail("data length is not a whole number of frames")
	}
	return WAVInfo{Format: format, DataLen: dataLen, TotalLen: int64(len(data))}, nil
}

// ParseWAVFile reads and validates a WAV file at path.
func ParseWAVFile(path string) (WAVInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return WAVInfo{}, fmt.Errorf("read wav: %w", err)
	}
	return ParseWAV(data)
}
