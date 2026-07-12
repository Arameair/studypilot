package backend

import (
	"bytes"
	"context"
	"testing"
)

func TestSyntheticSourceIsDeterministic(t *testing.T) {
	format := DefaultFormat()
	source := SyntheticSource{Frames: 1000}
	var first, second bytes.Buffer
	r1, err := source.WriteAudio(context.Background(), &first, format)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := source.WriteAudio(context.Background(), &second, format)
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Complete || r1 != r2 || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("synthetic source is not deterministic")
	}
	if r1.BytesWritten != int64(1000*format.blockAlign()) {
		t.Fatalf("bytes = %d", r1.BytesWritten)
	}
}

func TestSyntheticSourceCancellationProducesPartial(t *testing.T) {
	format := DefaultFormat()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := SyntheticSource{Frames: 100000}
	var buf bytes.Buffer
	result, err := source.WriteAudio(ctx, &buf, format)
	if CodeOf(err) != ErrorCancelled {
		t.Fatalf("cancelled source = %v", err)
	}
	if result.Complete {
		t.Fatal("cancelled source reported complete")
	}
}

func TestSyntheticSourceInjectedFailure(t *testing.T) {
	format := DefaultFormat()
	source := SyntheticSource{Frames: 100000, FailAfterBytes: 512}
	var buf bytes.Buffer
	result, err := source.WriteAudio(context.Background(), &buf, format)
	if CodeOf(err) != ErrorPartialOutput {
		t.Fatalf("injected failure = %v", err)
	}
	if result.Complete || result.BytesWritten > 512 {
		t.Fatalf("result = %+v", result)
	}
	if int64(buf.Len()) != result.BytesWritten {
		t.Fatalf("buffer %d disagrees with reported %d", buf.Len(), result.BytesWritten)
	}
}
