package backend

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func localExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(path, []byte("synthetic executable fixture\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	return path
}

func newLocalBackend(t *testing.T, runner ProcessRunner, mutate func(*LocalConfig)) (Backend, string) {
	t.Helper()
	paths, sessionRoot := newSession(t)
	config := LocalConfig{Paths: paths, Runner: runner, Executable: localExecutable(t), Driver: "pulse", Device: "purpose-created-test-input", StopTimeout: 50 * time.Millisecond, Clock: fixedClock(), NewSegmentID: sequentialSegmentIDs(), Liveness: deadLiveness}
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
	want := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-f", "pulse", "-i", "purpose-created-test-input", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-map_metadata", "-1", "-fflags", "+bitexact", "-flags:a", "+bitexact", "-y", runner.lastSpec.OutputPath}
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
	for _, driver := range []string{"pulse", "alsa"} {
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
			if runner.lastSpec.Args[5] != driver || runner.lastSpec.Args[7] != "configured input with punctuation !@#$%" {
				t.Fatalf("driver/device argument construction=%q", runner.lastSpec.Args)
			}
			if _, err := value.AbortSegment(context.Background(), active); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalCapabilityDiscoveryFailsClosed(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(*LocalConfig)
	}{
		{"not configured", "capture_not_configured", func(c *LocalConfig) { c.Executable = "" }},
		{"missing executable", "capture_executable_missing", func(c *LocalConfig) { c.Executable = filepath.Join(t.TempDir(), "ffmpeg") }},
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
	value, err := NewLocalBackend(LocalConfig{Paths: paths, Runner: &fakeRunner{}, Executable: executable, Driver: "alsa", Device: device})
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
