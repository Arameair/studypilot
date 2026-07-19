package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/application"
	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/httpapi"
)

const guiUsage = `StudyPilot local GUI

Usage:
  studypilot gui [--address 127.0.0.1:8765] [--root PATH]

The GUI is served only on IPv4 loopback. It does not provide remote access,
daemonization, browser microphone capture, or automatic browser opening.
`

var guiListen = httpapi.Listen

func runGUI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("gui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	address := stringFlag{name: "--address", value: httpapi.DefaultAddress}
	var root rootFlag
	flags.Var(&address, "address", "loopback listen address")
	flags.Var(&root, "root", "workspace root")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "Error: invalid GUI arguments.")
		fmt.Fprint(stderr, guiUsage)
		return 2
	}
	if err := httpapi.ValidateAddress(address.value); err != nil {
		fmt.Fprintln(stderr, "Error: GUI address must use 127.0.0.1 or localhost.")
		return 2
	}
	captureConfig, err := loadGUICaptureConfig()
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize StudyPilot GUI capture configuration.")
		return 1
	}
	transcriptionConfig, err := guiTranscriptionConfig()
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize StudyPilot GUI transcription configuration.")
		return 1
	}
	service, err := newGUIApplication(captureConfig, transcriptionConfig)
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize StudyPilot GUI application.")
		return 1
	}
	handler, err := httpapi.New(service, httpapi.Config{
		Root:                 root.value,
		CaptureBackend:       captureConfig.Backend,
		CaptureDriver:        safeCaptureDriver(captureConfig),
		CaptureDevice:        safeCaptureDevice(captureConfig),
		CaptureAvailable:     captureConfig.Available,
		CaptureIssues:        append([]capture.CapabilityIssue(nil), captureConfig.Issues...),
		TranscriptionBackend: transcriptionConfig.BackendName,
		TranscriptionModel:   transcriptionConfig.ModelID,
	})
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize StudyPilot GUI server.")
		return 1
	}
	listener, err := guiListen(address.value)
	if err != nil {
		fmt.Fprintln(stderr, "Error: start StudyPilot GUI listener.")
		return 1
	}
	defer listener.Close()
	fmt.Fprintf(stdout, "StudyPilot GUI listening at http://%s\nPress Ctrl+C to stop.\n", listener.Addr().String())
	serveErr := httpapi.Serve(ctx, listener, handler)
	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownErr := service.ShutdownCapture(shutdownContext)
	cancel()
	if serveErr != nil {
		fmt.Fprintln(stderr, "Error: StudyPilot GUI server stopped unexpectedly.")
		return 1
	}
	if shutdownErr != nil {
		fmt.Fprintln(stderr, "Error: StudyPilot capture shutdown requires recovery inspection.")
		return 1
	}
	return 0
}

func safeCaptureDevice(config captureRuntimeConfig) string {
	if !config.Available {
		return ""
	}
	if config.Backend == "local" {
		return "configured"
	}
	return config.Device
}

func safeCaptureDriver(config captureRuntimeConfig) string {
	if config.Backend == "synthetic" {
		return "synthetic"
	}
	if config.Driver == "pulse" || config.Driver == "alsa" || config.Driver == "dshow" {
		return config.Driver
	}
	return ""
}

func newGUIApplication(captureConfig captureRuntimeConfig, transcriptionConfig application.TranscriptionExecutionConfig) (*application.Service, error) {
	return application.NewService(application.Dependencies{Now: now, GenerateID: course.DefaultIDGenerator, CaptureServices: configuredCaptureFactory(captureConfig, false), TranscriptionExecution: transcriptionConfig})
}

func guiTranscriptionConfig() (application.TranscriptionExecutionConfig, error) {
	selection := strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_BACKEND"))
	model := strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_MODEL_ID"))
	if selection == "" {
		return application.TranscriptionExecutionConfig{}, nil
	}
	if selection == "local" && (strings.TrimSpace(os.Getenv("STUDYPILOT_PYTHON")) == "" || strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_WORKER")) == "" || strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_MODEL")) == "") {
		// An unavailable local worker is a UI capability state, not a reason to
		// prevent the loopback GUI from starting.
		return application.TranscriptionExecutionConfig{}, nil
	}
	if model == "" {
		model = "configured"
	}
	return loadTranscriptionConfig(selection, model)
}
