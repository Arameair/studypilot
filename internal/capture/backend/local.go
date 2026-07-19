package backend

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/workspace"
)

const (
	maxLocalDeviceBytes      = 512
	defaultLocalStartupGrace = 200 * time.Millisecond
)

// LocalConfig describes the narrow platform-aware ffmpeg capture boundary.
// Executable and Device are private operational inputs and are never copied to
// public capabilities or manifests.
type LocalConfig struct {
	Paths        workspace.Paths
	Runner       ProcessRunner
	Executable   string
	Driver       string
	Device       string
	Format       AudioFormat
	StopTimeout  time.Duration
	StartupGrace time.Duration
	Clock        func() time.Time
	NewSegmentID func() (string, error)
	Liveness     LivenessChecker
}

// NewLocalBackend builds an explicitly configured ffmpeg recorder. Invalid or
// incomplete operational configuration returns an unavailable backend with
// safe deterministic issues so the GUI can remain usable without capture.
func NewLocalBackend(cfg LocalConfig) (Backend, error) {
	if err := cfg.Paths.Validate(); err != nil {
		return nil, newError(ErrorInvalidRequest, OpNameCapabilities, "invalid workspace paths", err)
	}
	format := cfg.Format
	if (format == AudioFormat{}) {
		format = DefaultFormat()
	}
	if format != DefaultFormat() {
		return nil, newError(ErrorInvalidRequest, OpNameCapabilities, "local capture requires 16 kHz mono 16-bit PCM", nil)
	}
	runner := cfg.Runner
	if runner == nil {
		runner = NewExecRunner()
	}
	issues := localConfigurationIssues(cfg)
	capabilities := Capabilities{BackendName: "local", Status: capture.CapabilityUnavailable, Format: format, Issues: issues}
	var eng engine = &unavailableEngine{message: "local capture is not configured or available"}
	if len(issues) == 0 {
		capabilities.Status = capture.CapabilityReady
		capabilities.Devices = []capture.Device{{ID: "configured", Name: "Configured local input", Kind: capture.DeviceKindAudioInput, Default: true, Available: true}}
		capabilities.DefaultDeviceID = "configured"
		startupGrace := cfg.StartupGrace
		if startupGrace <= 0 {
			startupGrace = defaultLocalStartupGrace
		}
		eng = &processEngine{runner: runner, label: "local", executable: cfg.Executable, stopTimeout: cfg.StopTimeout, startupGrace: startupGrace, buildArgs: func(outputPath string, format AudioFormat) []string {
			return localFFmpegArgs(cfg.Driver, cfg.Device, outputPath)
		}}
	}
	recorder := newRecorder(cfg.Paths, eng, capabilities)
	if cfg.Clock != nil {
		recorder.clock = cfg.Clock
	}
	if cfg.NewSegmentID != nil {
		recorder.newSegmentID = cfg.NewSegmentID
	}
	if cfg.Liveness != nil {
		recorder.liveness = cfg.Liveness
	}
	return recorder, nil
}

func localConfigurationIssues(cfg LocalConfig) []capture.CapabilityIssue {
	issues := []capture.CapabilityIssue{}
	add := func(code, message string) {
		issues = append(issues, capture.CapabilityIssue{Code: code, Message: message})
	}
	if strings.TrimSpace(cfg.Executable) == "" {
		add("capture_not_configured", "a local capture executable is not configured")
	} else if code, message := localExecutableIssue(cfg.Executable); code != "" {
		add(code, message)
	}
	if code, message := localDriverIssue(cfg.Driver); code != "" {
		add(code, message)
	}
	if !validLocalDevice(cfg.Device) {
		add("capture_device_missing", "a safe local capture device identifier is required")
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Code < issues[j].Code })
	return issues
}

// LocalConfigurationIssues returns safe deterministic readiness issues without
// starting a process or probing/recording from the configured device.
func LocalConfigurationIssues(executable, driver, device string) []capture.CapabilityIssue {
	return append([]capture.CapabilityIssue(nil), localConfigurationIssues(LocalConfig{Executable: executable, Driver: driver, Device: device})...)
}

func validLocalDevice(value string) bool {
	return value != "" && len(value) <= maxLocalDeviceBytes && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n") && validPlatformDevice(value)
}
