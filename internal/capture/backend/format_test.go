package backend

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndParseWAVRoundTrip(t *testing.T) {
	format := DefaultFormat()
	frames := 500
	dataLen := frames * format.blockAlign()
	var buf bytes.Buffer
	if err := writeWAVHeader(&buf, format, uint32(dataLen)); err != nil {
		t.Fatal(err)
	}
	buf.Write(make([]byte, dataLen))
	info, err := ParseWAV(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != format || info.DataLen != int64(dataLen) {
		t.Fatalf("info = %+v", info)
	}
	if info.Format.DurationMillis(info.DataLen) <= 0 {
		t.Fatal("duration should be positive")
	}
}

func TestPatchWAVHeaderProducesValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audio.wav")
	format := DefaultFormat()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeWAVHeader(file, format, 0); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 200*format.blockAlign())
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := patchWAVHeader(file, uint32(len(data))); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := ParseWAVFile(path)
	if err != nil {
		t.Fatalf("patched file invalid: %v", err)
	}
	if info.DataLen != int64(len(data)) {
		t.Fatalf("data len = %d", info.DataLen)
	}
}

func TestParseWAVRejectsMalformed(t *testing.T) {
	valid := func() []byte {
		var buf bytes.Buffer
		_ = writeWAVHeader(&buf, DefaultFormat(), 4)
		buf.Write(make([]byte, 4))
		return buf.Bytes()
	}
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"too short", func([]byte) []byte { return []byte("RIFF") }},
		{"bad riff", func(b []byte) []byte { copy(b[0:4], "XXXX"); return b }},
		{"bad wave", func(b []byte) []byte { copy(b[8:12], "XXXX"); return b }},
		{"bad data tag", func(b []byte) []byte { copy(b[36:40], "XXXX"); return b }},
		{"truncated data", func(b []byte) []byte { return b[:len(b)-2] }},
		{"trailing garbage", func(b []byte) []byte { return append(b, 0, 0) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseWAV(test.mutate(valid())); err == nil {
				t.Fatal("expected parse failure")
			}
		})
	}
}
