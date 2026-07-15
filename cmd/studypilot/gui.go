package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Arameair/studypilot/internal/application"
	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/capture/backend"
	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/httpapi"
	"github.com/Arameair/studypilot/internal/workspace"
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
	service, err := newGUIApplication()
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize StudyPilot GUI application.")
		return 1
	}
	transcriptionConfig, err := guiTranscriptionConfig()
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize StudyPilot GUI transcription configuration.")
		return 1
	}
	handler, err := httpapi.New(service, httpapi.Config{Root: root.value, CaptureBackend: "synthetic", TranscriptionBackend: transcriptionConfig.BackendName, TranscriptionModel: transcriptionConfig.ModelID})
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
	if err = httpapi.Serve(ctx, listener, handler); err != nil {
		fmt.Fprintln(stderr, "Error: StudyPilot GUI server stopped unexpectedly.")
		return 1
	}
	return 0
}

func newGUIApplication() (*application.Service, error) {
	captureFactory := func(paths workspace.Paths, name string, resolve func(string) (string, error)) (application.CaptureService, error) {
		if name != "synthetic" {
			return nil, capture.NewError(capture.ErrorUnavailable, capture.OpStart, false, capture.OutcomeNotStarted, "capture backend is unavailable", nil)
		}
		raw, err := backend.NewSyntheticBackend(backend.SyntheticConfig{Paths: paths})
		if err != nil {
			return nil, err
		}
		return backend.NewBackendService(raw, resolve)
	}
	transcriptionConfig, err := guiTranscriptionConfig()
	if err != nil {
		return nil, err
	}
	return application.NewService(application.Dependencies{Now: now, GenerateID: course.DefaultIDGenerator, CaptureServices: captureFactory, TranscriptionExecution: transcriptionConfig})
}

func guiTranscriptionConfig() (application.TranscriptionExecutionConfig, error) {
	selection := strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_BACKEND"))
	model := strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_MODEL_ID"))
	if selection == "" {
		selection, model = "synthetic", "synthetic/deterministic"
	}
	if model == "" {
		model = "configured"
	}
	return loadTranscriptionConfig(selection, model)
}
