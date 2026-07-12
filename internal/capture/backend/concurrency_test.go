package backend

import (
	"context"
	"sync"
	"testing"
)

func TestConcurrentStartsHaveOneWinner(t *testing.T) {
	backend, sessionRoot := newSyntheticBackend(t, 200)
	req := StartSegmentRequest{SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "synthetic-default"}
	results := make(chan error, 2)
	var gate sync.WaitGroup
	gate.Add(1)
	for range 2 {
		go func() {
			gate.Wait()
			_, err := backend.StartSegment(context.Background(), req)
			results <- err
		}()
	}
	gate.Done()
	winners, conflicts := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			winners++
		case CodeOf(err) == ErrorOwnershipConflict || CodeOf(err) == ErrorSegmentConflict:
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
}

func TestConcurrentFinalizeAndAbortRace(t *testing.T) {
	backend, sessionRoot := newSyntheticBackend(t, 200)
	active := startSegment(t, backend, sessionRoot, 1)
	results := make(chan error, 2)
	var gate sync.WaitGroup
	gate.Add(1)
	go func() {
		gate.Wait()
		_, err := backend.FinalizeSegment(context.Background(), active)
		results <- err
	}()
	go func() {
		gate.Wait()
		_, err := backend.AbortSegment(context.Background(), active)
		results <- err
	}()
	gate.Done()
	success, rejected := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			success++
		case CodeOf(err) == ErrorInvalidRequest:
			rejected++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	// Exactly one of finalize/abort claims the single active recording.
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
}
