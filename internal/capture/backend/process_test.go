package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

// fakeRunner is a deterministic ProcessRunner that never spawns a real process.
type fakeRunner struct {
	available          map[string]string
	frames             int
	startErr           error
	earlyExitErr       error
	terminateErr       error
	killErr            error
	stderr             string
	writeWAV           bool
	writeOutput        func(ProcessSpec) error
	startAfterWriteErr error
	exited             bool
	exitAfter          time.Duration
	started            int
	lastSpec           ProcessSpec
	lastHandle         *fakeHandle
}

func (r *fakeRunner) Lookup(name string) (string, error) {
	if path, ok := r.available[name]; ok {
		return path, nil
	}
	return "", newError(ErrorExecutableMissing, "process", "recorder executable not found", nil)
}

func (r *fakeRunner) Start(ctx context.Context, spec ProcessSpec) (ProcessHandle, error) {
	r.started++
	r.lastSpec = spec
	if r.startErr != nil {
		return nil, r.startErr
	}
	if r.writeWAV {
		if err := writeValidWAV(spec.OutputPath, DefaultFormat(), r.frames); err != nil {
			return nil, err
		}
	}
	if r.writeOutput != nil {
		if err := r.writeOutput(spec); err != nil {
			return nil, err
		}
	}
	if r.startAfterWriteErr != nil {
		return nil, r.startAfterWriteErr
	}
	handle := &fakeHandle{earlyExitErr: r.earlyExitErr, exited: r.exited, exitAfter: r.exitAfter, startedAt: time.Now(), terminateErr: r.terminateErr, killErr: r.killErr, stderr: r.stderr}
	r.lastHandle = handle
	return handle, nil
}

type fakeHandle struct {
	earlyExitErr error
	exited       bool
	exitAfter    time.Duration
	startedAt    time.Time
	terminateErr error
	killErr      error
	stderr       string
	terminated   bool
	killed       bool
}

func (h *fakeHandle) Terminate(ctx context.Context) error { h.terminated = true; return h.terminateErr }
func (h *fakeHandle) Kill() error                         { h.killed = true; return h.killErr }
func (h *fakeHandle) Exited() (bool, error, string) {
	if h.exited || h.earlyExitErr != nil || (h.exitAfter > 0 && time.Since(h.startedAt) >= h.exitAfter) {
		return true, h.earlyExitErr, h.stderr
	}
	return false, nil, h.stderr
}

func writeValidWAV(path string, format AudioFormat, frames int) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	dataLen := frames * format.blockAlign()
	if err := writeWAVHeader(file, format, uint32(dataLen)); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(make([]byte, dataLen)); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func newLinuxBackend(t *testing.T, runner ProcessRunner) (Backend, string) {
	t.Helper()
	paths, sessionRoot := newSession(t)
	backend, err := NewLinuxBackend(LinuxConfig{
		Paths: paths, Runner: runner, Clock: fixedClock(),
		NewSegmentID: sequentialSegmentIDs(), Liveness: deadLiveness,
	})
	if err != nil {
		t.Fatal(err)
	}
	return backend, sessionRoot
}

func TestLinuxBackendRecordsThroughFakeProcess(t *testing.T) {
	runner := &fakeRunner{available: map[string]string{"arecord": "/usr/bin/arecord"}, frames: 300, writeWAV: true, stderr: "some diagnostics"}
	backend, sessionRoot := newLinuxBackend(t, runner)

	active := startSegment(t, backend, sessionRoot, 1)
	// The fixed recorder args were used with no shell.
	if runner.lastSpec.Executable != "/usr/bin/arecord" {
		t.Fatalf("unexpected executable: %s", runner.lastSpec.Executable)
	}
	for _, arg := range runner.lastSpec.Args {
		if strings.Contains(arg, "&&") || strings.Contains(arg, ";") || strings.Contains(arg, "|") {
			t.Fatalf("shell metacharacter in args: %q", runner.lastSpec.Args)
		}
	}
	finalized, err := backend.FinalizeSegment(context.Background(), active)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Segment.Status != studyruntime.SegmentStatusStopped {
		t.Fatalf("finalized = %+v", finalized.Segment)
	}
	if _, err := ParseWAVFile(filepath.Join(segmentsPath(sessionRoot), audioName(1))); err != nil {
		t.Fatalf("process-produced audio invalid: %v", err)
	}
}

func TestLinuxBackendUnavailableWhenNoRecorder(t *testing.T) {
	runner := &fakeRunner{available: map[string]string{}}
	backend, sessionRoot := newLinuxBackend(t, runner)
	caps, err := backend.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if caps.Status != capture.CapabilityUnavailable || len(caps.Issues) == 0 {
		t.Fatalf("capabilities = %+v", caps)
	}
	_, err = backend.StartSegment(context.Background(), StartSegmentRequest{
		SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "default",
	})
	if CodeOf(err) != ErrorUnavailable {
		t.Fatalf("start with no recorder = %v", err)
	}
}

func TestLinuxBackendReportsProcessExit(t *testing.T) {
	runner := &fakeRunner{available: map[string]string{"arecord": "/usr/bin/arecord"}, frames: 100, writeWAV: true, earlyExitErr: errors.New("exit status 1")}
	backend, sessionRoot := newLinuxBackend(t, runner)
	if _, err := backend.StartSegment(context.Background(), StartSegmentRequest{SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "default"}); CodeOf(err) != ErrorPartialOutput {
		t.Fatalf("start after early exit = %v", err)
	}
	if _, present, err := readOwnership(segmentsPath(sessionRoot)); err != nil || present {
		t.Fatalf("resolved failed start retained ownership: present=%t err=%v", present, err)
	}
}

func TestProcessFinalizationPreservesTimeoutAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name string
		code ErrorCode
	}{
		{"timeout", ErrorTimeout},
		{"cancelled", ErrorCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{available: map[string]string{"arecord": "/usr/bin/arecord"}, frames: 100, writeWAV: true, terminateErr: newError(test.code, "process", "safe injected process failure", nil)}
			backend, sessionRoot := newLinuxBackend(t, runner)
			active := startSegment(t, backend, sessionRoot, 1)
			if _, err := backend.FinalizeSegment(context.Background(), active); CodeOf(err) != test.code {
				t.Fatalf("finalize error=%v", err)
			}
			if _, err := os.Stat(filepath.Join(segmentsPath(sessionRoot), partialName(1))); err != nil {
				t.Fatalf("partial evidence missing: %v", err)
			}
			if _, present, err := readOwnership(segmentsPath(sessionRoot)); err != nil || !present {
				t.Fatalf("ownership evidence present=%t err=%v", present, err)
			}
		})
	}
}

func TestAbortFailurePreservesOwnershipAndReturnsSafeError(t *testing.T) {
	runner := &fakeRunner{available: map[string]string{"arecord": "/usr/bin/arecord"}, frames: 100, writeWAV: true, killErr: errors.New("private raw process detail")}
	backend, sessionRoot := newLinuxBackend(t, runner)
	active := startSegment(t, backend, sessionRoot, 1)
	partial, err := backend.AbortSegment(context.Background(), active)
	if CodeOf(err) != ErrorPartialOutput || partial.Number != 1 {
		t.Fatalf("partial=%+v err=%v", partial, err)
	}
	if _, present, inspectErr := readOwnership(segmentsPath(sessionRoot)); inspectErr != nil || !present {
		t.Fatalf("ownership evidence present=%t err=%v", present, inspectErr)
	}
	if strings.Contains(err.(*Error).Message, "private raw") {
		t.Fatal("safe public backend message included raw process detail")
	}
}

func TestLinuxBackendAbortKillsAndKeepsPartial(t *testing.T) {
	runner := &fakeRunner{available: map[string]string{"arecord": "/usr/bin/arecord"}, frames: 100, writeWAV: true}
	backend, sessionRoot := newLinuxBackend(t, runner)
	active := startSegment(t, backend, sessionRoot, 1)
	partial, err := backend.AbortSegment(context.Background(), active)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Number != 1 || partial.RelativePath != partialName(1) {
		t.Fatalf("partial = %+v", partial)
	}
	if _, present, _ := readOwnership(segmentsPath(sessionRoot)); present {
		t.Fatal("ownership held after abort")
	}
}

func TestBoundedBufferCapsStderr(t *testing.T) {
	buf := &boundedBuffer{limit: 8}
	buf.Write([]byte("abcdefghijklmnop"))
	buf.Write([]byte("more"))
	if got := buf.String(); len(got) != 8 || got != "abcdefgh" {
		t.Fatalf("bounded buffer = %q", got)
	}
}

func TestExecRunnerClassifiesMissingExecutable(t *testing.T) {
	runner := NewExecRunner()
	if _, err := runner.Lookup("studypilot-nonexistent-recorder-xyz"); CodeOf(err) != ErrorExecutableMissing {
		t.Fatalf("lookup = %v", err)
	}
	// Starting a bogus executable fails fast without spawning a real recorder.
	output := filepath.Join(t.TempDir(), "out.wav")
	_, err := runner.Start(context.Background(), ProcessSpec{Executable: "/nonexistent/studypilot-recorder-xyz", Args: []string{output}, OutputPath: output})
	if CodeOf(err) != ErrorExecutableMissing {
		t.Fatalf("start bogus = %v", err)
	}
}
