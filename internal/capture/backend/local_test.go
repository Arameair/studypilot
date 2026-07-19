package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func localExecutable(t *testing.T) string {
	t.Helper()
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name = "ffmpeg.exe"
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("synthetic executable fixture\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	return path
}

func localTestDriver() string {
	if runtime.GOOS == "windows" {
		return "dshow"
	}
	return "pulse"
}

func newLocalBackend(t *testing.T, runner ProcessRunner, mutate func(*LocalConfig)) (Backend, string) {
	t.Helper()
	paths, sessionRoot := newSession(t)
	config := LocalConfig{Paths: paths, Runner: runner, Executable: localExecutable(t), Driver: localTestDriver(), Device: "purpose-created-test-input", StopTimeout: 50 * time.Millisecond, StartupGrace: time.Nanosecond, Clock: fixedClock(), NewSegmentID: sequentialSegmentIDs(), Liveness: deadLiveness}
	if mutate != nil {
		mutate(&config)
	}
	value, err := NewLocalBackend(config)
	if err != nil {
		t.Fatal(err)
	}
	return value, sessionRoot
}

func TestLocalBackendUsesFixedShellFreeFFmpegRequest(t *testing.T) {
	runner := &fakeRunner{frames: 320, writeWAV: true}
	value, sessionRoot := newLocalBackend(t, runner, nil)
	capabilities, err := value.Capabilities(context.Background())
	if err != nil || capabilities.Status != capture.CapabilityReady || capabilities.DefaultDeviceID != "configured" || len(capabilities.Issues) != 0 {
		t.Fatalf("capabilities=%+v err=%v", capabilities, err)
	}
	active, err := value.StartSegment(context.Background(), StartSegmentRequest{SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "configured"})
	if err != nil {
		t.Fatal(err)
	}
	want := localFFmpegArgs(localTestDriver(), "purpose-created-test-input", runner.lastSpec.OutputPath)
	if !reflect.DeepEqual(runner.lastSpec.Args, want) || runner.lastSpec.Executable == "" || runner.lastSpec.StopTimeout != 50*time.Millisecond {
		t.Fatalf("process spec=%+v", runner.lastSpec)
	}
	for _, argument := range runner.lastSpec.Args {
		if strings.ContainsAny(argument, "\x00\r\n") {
			t.Fatal("unsafe process argument")
		}
	}
	finalized, err := value.FinalizeSegment(context.Background(), active)
	if err != nil || finalized.Manifest.SampleRate != 16000 || finalized.Manifest.Channels != 1 || finalized.Manifest.BitDepth != 16 || finalized.Manifest.Backend != "local" {
		t.Fatalf("finalized=%+v err=%v", finalized, err)
	}
}

func TestLocalBackendConstructsEachAllowedDriverAsOneArgument(t *testing.T) {
	drivers := []string{"pulse", "alsa"}
	if runtime.GOOS == "windows" {
		drivers = []string{"dshow"}
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			runner := &fakeRunner{frames: 160, writeWAV: true}
			value, sessionRoot := newLocalBackend(t, runner, func(config *LocalConfig) {
				config.Driver = driver
				config.Device = "configured input with punctuation !@#$%"
			})
			active, err := value.StartSegment(context.Background(), StartSegmentRequest{SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "configured"})
			if err != nil {
				t.Fatal(err)
			}
			wantInput := "configured input with punctuation !@#$%"
			if runtime.GOOS == "windows" {
				wantInput = "audio=" + wantInput
			}
			if argumentAfter(runner.lastSpec.Args, "-f") != driver || argumentAfter(runner.lastSpec.Args, "-i") != wantInput {
				t.Fatalf("driver/device argument construction=%q", runner.lastSpec.Args)
			}
			if _, err := value.AbortSegment(context.Background(), active); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalFailedStartEvidenceAndOwnership(t *testing.T) {
	tests := []struct {
		name         string
		writeOutput  func(ProcessSpec) error
		frames       int
		writeWAV     bool
		wantCode     ErrorCode
		wantPartial  bool
		wantManifest bool
	}{
		{name: "no output", wantCode: ErrorProcessExited},
		{name: "empty output", writeOutput: func(spec ProcessSpec) error { return os.WriteFile(spec.OutputPath, nil, 0o640) }, wantCode: ErrorProcessExited},
		{name: "header only", writeWAV: true, frames: 0, wantCode: ErrorProcessExited},
		{name: "meaningful audio", writeWAV: true, frames: 160, wantCode: ErrorPartialOutput, wantPartial: true, wantManifest: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{exited: true, stderr: "private device and ffmpeg diagnostics", writeOutput: test.writeOutput, writeWAV: test.writeWAV, frames: test.frames}
			value, sessionRoot := newLocalBackend(t, runner, nil)
			_, err := value.StartSegment(context.Background(), StartSegmentRequest{SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "configured"})
			if CodeOf(err) != test.wantCode {
				t.Fatalf("code=%s err=%v", CodeOf(err), err)
			}
			segments := segmentsPath(sessionRoot)
			if _, present, ownershipErr := readOwnership(segments); ownershipErr != nil || present {
				t.Fatalf("ownership present=%t err=%v", present, ownershipErr)
			}
			_, partialErr := os.Stat(filepath.Join(segments, partialName(1)))
			if (partialErr == nil) != test.wantPartial {
				t.Fatalf("partial present=%t err=%v", partialErr == nil, partialErr)
			}
			manifestPath := filepath.Join(segments, manifestName(1))
			_, manifestErr := os.Stat(manifestPath)
			if (manifestErr == nil) != test.wantManifest {
				t.Fatalf("manifest present=%t err=%v", manifestErr == nil, manifestErr)
			}
			if test.wantManifest {
				manifest, readErr := readManifest(manifestPath)
				if readErr != nil || !manifest.Partial || !manifest.Recoverable || manifest.BytesWritten <= 0 || manifest.AudioFile != partialName(1) {
					t.Fatalf("manifest=%+v err=%v", manifest, readErr)
				}
			}
		})
	}
}

func TestLocalStartupGraceDetectsDelayedExitAndAcceptsLiveProcess(t *testing.T) {
	t.Run("delayed early exit", func(t *testing.T) {
		runner := &fakeRunner{writeWAV: true, frames: 160, exitAfter: 5 * time.Millisecond}
		value, sessionRoot := newLocalBackend(t, runner, func(config *LocalConfig) { config.StartupGrace = 15 * time.Millisecond })
		_, err := value.StartSegment(context.Background(), StartSegmentRequest{SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "configured"})
		if CodeOf(err) != ErrorPartialOutput {
			t.Fatalf("delayed exit err=%v", err)
		}
	})
	t.Run("alive after grace", func(t *testing.T) {
		runner := &fakeRunner{writeWAV: true, frames: 160}
		value, sessionRoot := newLocalBackend(t, runner, func(config *LocalConfig) { config.StartupGrace = time.Millisecond })
		active, err := value.StartSegment(context.Background(), StartSegmentRequest{SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "configured"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := value.AbortSegment(context.Background(), active); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLocalUncertainStartPreservesOwnershipAndEvidence(t *testing.T) {
	privateDetail := "private recorder termination detail"
	runner := &fakeRunner{
		writeWAV: true,
		frames:   160,
		startAfterWriteErr: newProcessStartFailure(
			newError(ErrorTimeout, OpNameStart, "recorder startup termination is uncertain", nil),
			false,
			privateDetail,
		),
	}
	value, sessionRoot := newLocalBackend(t, runner, nil)
	_, err := value.StartSegment(context.Background(), StartSegmentRequest{SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "configured"})
	if CodeOf(err) != ErrorTimeout {
		t.Fatalf("err=%v", err)
	}
	if _, present, ownershipErr := readOwnership(segmentsPath(sessionRoot)); ownershipErr != nil || !present {
		t.Fatalf("ownership present=%t err=%v", present, ownershipErr)
	}
	if _, statErr := os.Stat(filepath.Join(segmentsPath(sessionRoot), partialName(1))); statErr != nil {
		t.Fatalf("partial evidence missing: %v", statErr)
	}
	if strings.Contains(err.Error(), privateDetail) {
		t.Fatal("internal diagnostics escaped through the backend error")
	}
}

func TestResolvedEarlyExitHasSafePublicErrorAndNoActiveCapture(t *testing.T) {
	privateDetail := "private device identifier and ffmpeg stderr"
	runner := &fakeRunner{exited: true, stderr: privateDetail}
	value, sessionRoot := newLocalBackend(t, runner, nil)
	service, err := NewBackendService(value, func(string) (string, error) { return sessionRoot, nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Start(context.Background(), capture.StartRequest{SessionID: testSessionID, ExpectedStatus: studyruntime.CaptureStatusReady, DeviceID: "configured", SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"})
	var captureErr *capture.Error
	if !errors.As(err, &captureErr) || captureErr.Code != capture.ErrorStartFailed || captureErr.Outcome != capture.OutcomeNotStarted || captureErr.Message != "The configured local capture process exited during startup." {
		t.Fatalf("public error=%+v err=%v", captureErr, err)
	}
	if strings.Contains(captureErr.Error(), privateDetail) || strings.Contains(captureErr.Message, privateDetail) {
		t.Fatal("public capture error exposed recorder stderr")
	}
	service.mu.Lock()
	activeInstances := len(service.instances)
	service.mu.Unlock()
	if activeInstances != 0 {
		t.Fatalf("resolved failed start persisted %d recording instance(s)", activeInstances)
	}
	if shutdownErr := service.Shutdown(context.Background()); shutdownErr != nil {
		t.Fatalf("shutdown after resolved failed start: %v", shutdownErr)
	}
	if runner.lastHandle == nil || runner.lastHandle.killed || runner.lastHandle.terminated {
		t.Fatalf("resolved exited process was treated as active: %+v", runner.lastHandle)
	}
}

func TestLocalCapabilityDiscoveryFailsClosed(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(*LocalConfig)
	}{
		{"not configured", "capture_not_configured", func(c *LocalConfig) { c.Executable = "" }},
		{"missing executable", "capture_executable_missing", func(c *LocalConfig) {
			name := "ffmpeg"
			if runtime.GOOS == "windows" {
				name = "ffmpeg.exe"
			}
			c.Executable = filepath.Join(t.TempDir(), name)
		}},
		{"unsafe executable", "capture_executable_unsafe", func(c *LocalConfig) { c.Executable = "/bin/sh" }},
		{"unsupported driver", "capture_driver_unsupported", func(c *LocalConfig) { c.Driver = "network" }},
		{"missing device", "capture_device_missing", func(c *LocalConfig) { c.Device = "" }},
		{"device newline", "capture_device_missing", func(c *LocalConfig) { c.Device = "unsafe\ndevice" }},
		{"device too long", "capture_device_missing", func(c *LocalConfig) { c.Device = strings.Repeat("x", maxLocalDeviceBytes+1) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, _ := newLocalBackend(t, &fakeRunner{}, test.mutate)
			capabilities, err := value.Capabilities(context.Background())
			if err != nil || capabilities.Status != capture.CapabilityUnavailable || !hasCapabilityIssue(capabilities.Issues, test.code) {
				t.Fatalf("capabilities=%+v err=%v", capabilities, err)
			}
		})
	}
}

func TestLocalBackendRejectsInvalidOrEmptyWAV(t *testing.T) {
	for _, frames := range []int{0} {
		runner := &fakeRunner{frames: frames, writeWAV: true}
		value, sessionRoot := newLocalBackend(t, runner, nil)
		active, err := value.StartSegment(context.Background(), StartSegmentRequest{SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "configured"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = value.FinalizeSegment(context.Background(), active); CodeOf(err) != ErrorFinalizationFailed {
			t.Fatalf("empty wav err=%v", err)
		}
	}
}

func TestLocalBackendDoesNotExposePrivateConfigurationInCapabilities(t *testing.T) {
	device := "private device identifier with punctuation !@#$%"
	executable := localExecutable(t)
	paths, _ := newSession(t)
	value, err := NewLocalBackend(LocalConfig{Paths: paths, Runner: &fakeRunner{}, Executable: executable, Driver: localTestDriver(), Device: device})
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := value.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	serialized := capabilities.BackendName + capabilities.DefaultDeviceID
	for _, item := range capabilities.Devices {
		serialized += item.ID + item.Name
	}
	if strings.Contains(serialized, device) || strings.Contains(serialized, executable) {
		t.Fatal("capabilities exposed private configuration")
	}
}

func argumentAfter(arguments []string, name string) string {
	for i := 0; i+1 < len(arguments); i++ {
		if arguments[i] == name {
			return arguments[i+1]
		}
	}
	return ""
}

func TestBackendServiceShutdownKillsProcessAndPreservesPartialEvidence(t *testing.T) {
	runner := &fakeRunner{frames: 320, writeWAV: true}
	value, sessionRoot := newLocalBackend(t, runner, nil)
	service, err := NewBackendService(value, func(sessionID string) (string, error) { return sessionRoot, nil })
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(context.Background(), capture.StartRequest{SessionID: testSessionID, ExpectedStatus: studyruntime.CaptureStatusReady, DeviceID: "configured", SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.lastHandle == nil || !runner.lastHandle.killed {
		t.Fatal("capture process was not killed during shutdown")
	}
	if _, err := os.Stat(filepath.Join(segmentsPath(sessionRoot), partialName(1))); err != nil {
		t.Fatalf("partial evidence missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(segmentsPath(sessionRoot), manifestName(1))); err != nil {
		t.Fatalf("partial manifest missing: %v", err)
	}
	if _, present, err := readOwnership(segmentsPath(sessionRoot)); err != nil || present {
		t.Fatalf("ownership after shutdown: present=%t err=%v", present, err)
	}
	if started.Status != studyruntime.CaptureStatusRecording {
		t.Fatalf("start status changed: %s", started.Status)
	}
}

func hasCapabilityIssue(issues []capture.CapabilityIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
