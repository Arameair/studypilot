package backend

import (
	"context"
	"encoding/binary"
	"io"
)

// SourceResult reports how much PCM data a source produced and whether it
// completed. A cancelled or failed write reports Complete=false with the bytes
// already written so callers can treat the output as partial.
type SourceResult struct {
	BytesWritten int64
	Frames       int64
	Complete     bool
}

// Source produces raw PCM sample data (no WAV header) into a writer for a given
// format. Implementations must honor cancellation, leave no goroutine running,
// and — for the synthetic source — be fully deterministic.
type Source interface {
	WriteAudio(ctx context.Context, writer io.Writer, format AudioFormat) (SourceResult, error)
}

// SyntheticSource generates a deterministic repeating 16-bit sample pattern. It
// requires no microphone, no external process, and (by default) no real-time
// delay, so tests run fast and reproducibly.
type SyntheticSource struct {
	// Frames is the total number of audio frames to write.
	Frames int
	// FailAfterBytes, when positive, makes WriteAudio return an error once that
	// many data bytes have been written, modeling a mid-write backend failure.
	FailAfterBytes int64
	// chunkFrames bounds how many frames are written between cancellation checks.
	chunkFrames int
}

// syntheticPattern is a fixed, deterministic 16-bit sample sequence. It is not
// random; the same frame index always yields the same bytes.
func syntheticSample(frame int) int16 {
	// A small triangle-ish pattern keeps values bounded and deterministic.
	v := frame % 64
	if v >= 32 {
		v = 64 - v
	}
	return int16((v - 16) * 512)
}

func (s SyntheticSource) WriteAudio(ctx context.Context, writer io.Writer, format AudioFormat) (SourceResult, error) {
	if err := format.Validate(); err != nil {
		return SourceResult{}, err
	}
	if s.Frames < 0 {
		return SourceResult{}, newError(ErrorInvalidRequest, "source", "negative frame count", nil)
	}
	chunk := s.chunkFrames
	if chunk <= 0 {
		chunk = 256
	}
	block := format.blockAlign()
	buffer := make([]byte, 0, chunk*block)
	var result SourceResult
	for frame := 0; frame < s.Frames; frame++ {
		if frame%chunk == 0 {
			if err := ctx.Err(); err != nil {
				return result, contextError("source", err)
			}
		}
		sample := syntheticSample(frame)
		for ch := 0; ch < format.Channels; ch++ {
			var field [2]byte
			binary.LittleEndian.PutUint16(field[:], uint16(sample))
			buffer = append(buffer, field[:]...)
		}
		if len(buffer) >= chunk*block || frame == s.Frames-1 {
			if s.FailAfterBytes > 0 && result.BytesWritten+int64(len(buffer)) > s.FailAfterBytes {
				// Write up to the failure boundary, then fail with a partial result.
				allowed := s.FailAfterBytes - result.BytesWritten
				allowed -= allowed % int64(block)
				if allowed > 0 {
					n, err := writer.Write(buffer[:allowed])
					result.BytesWritten += int64(n)
					result.Frames += int64(n / block)
					if err != nil {
						return result, newError(ErrorPartialOutput, "source", "audio source write failed", err)
					}
				}
				return result, newError(ErrorPartialOutput, "source", "synthetic source failed mid-write", nil)
			}
			n, err := writer.Write(buffer)
			result.BytesWritten += int64(n)
			result.Frames += int64(n / block)
			if err != nil {
				return result, newError(ErrorPartialOutput, "source", "audio source write failed", err)
			}
			buffer = buffer[:0]
		}
	}
	result.Complete = true
	return result, nil
}

// contextError classifies a done context into a backend error, distinguishing
// deadline expiry from cancellation.
func contextError(op string, err error) *Error {
	if err == context.DeadlineExceeded {
		return newError(ErrorTimeout, op, "operation deadline exceeded", err)
	}
	return newError(ErrorCancelled, op, "operation cancelled", err)
}
