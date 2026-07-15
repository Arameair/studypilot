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
	"github.com/Arameair/studypilot/internal/course"
)

const transcriptionUsage = `StudyPilot transcription commands

Usage:
  studypilot transcription execute --course REF --module REF --session REF --segment ID --backend synthetic|local --model MODEL --revision N [--language CODE] [--max-attempts N] [--idempotency-key KEY] [--root PATH] [--json]
  studypilot transcription inspect --course REF --module REF --session REF [--root PATH] [--json]
  studypilot transcription capabilities [--backend synthetic|local] [--model MODEL] [--json]

Execution is explicit, synchronous, and limited to one job. The combined
execute command keeps enqueue and run in one process because the queue is
in-memory and is not durable across separate CLI invocations.
`

func runTranscription(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return transcriptionUsageError(stderr, "transcription requires a subcommand")
	}
	switch args[0] {
	case "help", "--help", "-h":
		_, _ = io.WriteString(stdout, transcriptionUsage)
		return 0
	case "execute":
		return runTranscriptionExecute(ctx, args[1:], stdout, stderr)
	case "inspect":
		return runTranscriptionInspect(ctx, args[1:], stdout, stderr)
	case "capabilities":
		return runTranscriptionCapabilities(ctx, args[1:], stdout, stderr)
	default:
		return transcriptionUsageError(stderr, "unknown transcription subcommand")
	}
}

func transcriptionFlags(name string, stderr io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	return flags
}

func runTranscriptionExecute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := transcriptionFlags("transcription execute", stderr)
	courseRef := stringFlag{name: "--course"}
	moduleRef := stringFlag{name: "--module"}
	sessionRef := stringFlag{name: "--session"}
	segment := stringFlag{name: "--segment"}
	backendName := stringFlag{name: "--backend"}
	model := stringFlag{name: "--model"}
	language := stringFlag{name: "--language", value: "en"}
	key := stringFlag{name: "--idempotency-key"}
	revision := intFlag{name: "--revision"}
	maxAttempts := intFlag{name: "--max-attempts", value: 3}
	var root rootFlag
	asJSON := boolFlag{name: "--json"}
	flags.Var(&courseRef, "course", "course reference")
	flags.Var(&moduleRef, "module", "module reference")
	flags.Var(&sessionRef, "session", "session reference")
	flags.Var(&segment, "segment", "finalized segment identity")
	flags.Var(&backendName, "backend", "explicit backend: synthetic or local")
	flags.Var(&model, "model", "configured model identity")
	flags.Var(&language, "language", "requested language")
	flags.Var(&key, "idempotency-key", "optional process-local idempotency key")
	flags.Var(&revision, "revision", "expected runtime revision")
	flags.Var(&maxAttempts, "max-attempts", "maximum attempts")
	flags.Var(&root, "root", "workspace root")
	flags.Var(&asJSON, "json", "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return transcriptionUsageError(stderr, "invalid transcription execute arguments")
	}
	for name, value := range map[string]string{"--course": courseRef.value, "--module": moduleRef.value, "--session": sessionRef.value, "--segment": segment.value, "--backend": backendName.value, "--model": model.value} {
		if strings.TrimSpace(value) == "" {
			return transcriptionUsageError(stderr, "transcription execute requires "+name)
		}
	}
	if !revision.set || revision.value <= 0 || maxAttempts.value <= 0 {
		return transcriptionUsageError(stderr, "transcription execute requires positive --revision and --max-attempts")
	}
	if backendName.value != "synthetic" && backendName.value != "local" {
		return transcriptionUsageError(stderr, "transcription execute --backend must be synthetic or local")
	}
	config, err := loadTranscriptionConfig(backendName.value, model.value)
	if err != nil {
		return reportTranscriptionError(err, asJSON.value, stderr)
	}
	service, code := newTranscriptionService(config, stderr)
	if service == nil {
		return code
	}
	request := application.ExecuteTranscriptionRequest{Root: root.value, CourseRef: courseRef.value, ModuleRef: moduleRef.value, SessionRef: sessionRef.value, SegmentID: segment.value, Backend: config.BackendName, Model: config.ModelID, Language: language.value, IdempotencyKey: key.value, MaxAttempts: maxAttempts.value, ExpectedRevision: uint64(revision.value)}
	result, err := service.ExecuteTranscription(ctx, request)
	return renderTranscriptionExecution(result, err, asJSON.value, stdout, stderr)
}

func runTranscriptionInspect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := transcriptionFlags("transcription inspect", stderr)
	courseRef := stringFlag{name: "--course"}
	moduleRef := stringFlag{name: "--module"}
	sessionRef := stringFlag{name: "--session"}
	var root rootFlag
	asJSON := boolFlag{name: "--json"}
	flags.Var(&courseRef, "course", "course reference")
	flags.Var(&moduleRef, "module", "module reference")
	flags.Var(&sessionRef, "session", "session reference")
	flags.Var(&root, "root", "workspace root")
	flags.Var(&asJSON, "json", "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return transcriptionUsageError(stderr, "invalid transcription inspect arguments")
	}
	if strings.TrimSpace(courseRef.value) == "" || strings.TrimSpace(moduleRef.value) == "" || strings.TrimSpace(sessionRef.value) == "" {
		return transcriptionUsageError(stderr, "transcription inspect requires --course, --module, and --session")
	}
	config, _ := loadTranscriptionConfig(os.Getenv("STUDYPILOT_TRANSCRIPTION_BACKEND"), "configured")
	service, code := newTranscriptionService(config, stderr)
	if service == nil {
		return code
	}
	result, err := service.InspectTranscription(ctx, application.InspectTranscriptionRequest{Root: root.value, CourseRef: courseRef.value, ModuleRef: moduleRef.value, SessionRef: sessionRef.value})
	return renderTranscriptionInspection(result, err, asJSON.value, stdout, stderr)
}

func runTranscriptionCapabilities(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := transcriptionFlags("transcription capabilities", stderr)
	backendName := stringFlag{name: "--backend", value: os.Getenv("STUDYPILOT_TRANSCRIPTION_BACKEND")}
	model := stringFlag{name: "--model", value: "configured"}
	asJSON := boolFlag{name: "--json"}
	flags.Var(&backendName, "backend", "backend: synthetic or local")
	flags.Var(&model, "model", "configured model identity")
	flags.Var(&asJSON, "json", "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return transcriptionUsageError(stderr, "invalid transcription capabilities arguments")
	}
	if strings.TrimSpace(backendName.value) != "" && backendName.value != "synthetic" && backendName.value != "local" {
		return transcriptionUsageError(stderr, "transcription capabilities --backend must be synthetic or local")
	}
	config, err := loadTranscriptionConfig(backendName.value, model.value)
	if err != nil && strings.TrimSpace(backendName.value) != "" {
		return reportTranscriptionError(err, asJSON.value, stderr)
	}
	service, code := newTranscriptionService(config, stderr)
	if service == nil {
		return code
	}
	capability, err := service.TranscriptionCapabilities(ctx)
	return renderTranscriptionCapabilities(capability, err, asJSON.value, stdout, stderr)
}

func loadTranscriptionConfig(selection, model string) (application.TranscriptionExecutionConfig, error) {
	selection = strings.TrimSpace(selection)
	model = strings.TrimSpace(model)
	timeout := 30 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 || parsed > 24*time.Hour {
			return application.TranscriptionExecutionConfig{}, fmt.Errorf("invalid transcription timeout configuration")
		}
		timeout = parsed
	}
	switch selection {
	case "":
		return application.TranscriptionExecutionConfig{}, nil
	case "synthetic":
		if model != "synthetic/deterministic" && model != "deterministic" {
			return application.TranscriptionExecutionConfig{}, fmt.Errorf("synthetic backend requires model deterministic")
		}
		return application.TranscriptionExecutionConfig{BackendName: "synthetic", ModelID: "synthetic/deterministic", Timeout: timeout}, nil
	case "local":
		if !strings.HasPrefix(model, "faster-whisper/") {
			model = "faster-whisper/" + model
		}
		config := application.TranscriptionExecutionConfig{BackendName: "faster-whisper", ModelID: model, PythonExecutable: strings.TrimSpace(os.Getenv("STUDYPILOT_PYTHON")), WorkerScript: strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_WORKER")), ModelPath: strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_MODEL")), Device: envDefault("STUDYPILOT_TRANSCRIPTION_DEVICE", "cpu"), ComputeType: envDefault("STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE", "int8"), Timeout: timeout}
		if config.PythonExecutable == "" || config.WorkerScript == "" || config.ModelPath == "" {
			return config, fmt.Errorf("local transcription configuration is incomplete")
		}
		return config, nil
	default:
		return application.TranscriptionExecutionConfig{}, fmt.Errorf("unsupported transcription backend")
	}
}

func envDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func newTranscriptionService(config application.TranscriptionExecutionConfig, stderr io.Writer) (*application.Service, int) {
	service, err := application.NewService(application.Dependencies{Now: now, GenerateID: course.DefaultIDGenerator, TranscriptionExecution: config})
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize transcription application service")
		return nil, 1
	}
	return service, 0
}

func transcriptionUsageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "Error: %s\n\n%s", strings.TrimSpace(message), transcriptionUsage)
	return 2
}
