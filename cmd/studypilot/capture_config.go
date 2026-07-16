package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/application"
	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/capture/backend"
	"github.com/Arameair/studypilot/internal/workspace"
)

type captureRuntimeConfig struct {
	Backend, Executable, Driver, Device string
	StopTimeout                         time.Duration
	Available                           bool
	Issues                              []capture.CapabilityIssue
}

func loadGUICaptureConfig() (captureRuntimeConfig, error) {
	selection := strings.TrimSpace(os.Getenv("STUDYPILOT_CAPTURE_BACKEND"))
	config := captureRuntimeConfig{Backend: selection, StopTimeout: 3 * time.Second}
	if raw := strings.TrimSpace(os.Getenv("STUDYPILOT_CAPTURE_STOP_TIMEOUT")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 || value > 30*time.Second {
			return captureRuntimeConfig{}, fmt.Errorf("invalid local capture stop timeout")
		}
		config.StopTimeout = value
	}
	switch selection {
	case "":
		config.Issues = []capture.CapabilityIssue{{Code: "capture_not_configured", Message: "local capture is not configured"}}
	case "synthetic":
		config.Available = true
		config.Driver, config.Device = "synthetic", "synthetic-default"
	case "local":
		config.Executable = strings.TrimSpace(os.Getenv("STUDYPILOT_CAPTURE_EXECUTABLE"))
		config.Driver = strings.TrimSpace(os.Getenv("STUDYPILOT_CAPTURE_DRIVER"))
		config.Device = os.Getenv("STUDYPILOT_CAPTURE_DEVICE")
		config.Issues = backend.LocalConfigurationIssues(config.Executable, config.Driver, config.Device)
		config.Available = len(config.Issues) == 0
	default:
		return captureRuntimeConfig{}, fmt.Errorf("unsupported capture backend")
	}
	return config, nil
}

func configuredCaptureFactory(config captureRuntimeConfig, allowSynthetic bool) application.CaptureServiceFactory {
	return func(paths workspace.Paths, name string, resolve func(string) (string, error)) (application.CaptureService, error) {
		var raw backend.Backend
		var err error
		switch name {
		case "synthetic":
			if !allowSynthetic && config.Backend != "synthetic" {
				return nil, capture.NewError(capture.ErrorUnavailable, capture.OpStart, false, capture.OutcomeNotStarted, "synthetic capture requires explicit configuration", nil)
			}
			raw, err = backend.NewSyntheticBackend(backend.SyntheticConfig{Paths: paths})
		case "local":
			if config.Backend != "local" {
				return nil, capture.NewError(capture.ErrorUnavailable, capture.OpStart, false, capture.OutcomeNotStarted, "local capture is not configured", nil)
			}
			raw, err = backend.NewLocalBackend(backend.LocalConfig{Paths: paths, Executable: config.Executable, Driver: config.Driver, Device: config.Device, StopTimeout: config.StopTimeout})
		default:
			return nil, capture.NewError(capture.ErrorUnavailable, capture.OpStart, false, capture.OutcomeNotStarted, "capture backend is unavailable", nil)
		}
		if err != nil {
			return nil, err
		}
		return backend.NewBackendService(raw, resolve)
	}
}
