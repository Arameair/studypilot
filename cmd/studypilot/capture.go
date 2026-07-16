package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Arameair/studypilot/internal/application"
)

const captureUsage = `StudyPilot capture commands

Usage:
  studypilot capture start   --course REF --module REF --session REF --revision N --backend synthetic [--device ID] [--root PATH] [--json]
  studypilot capture pause   --course REF --module REF --session REF --revision N [--root PATH] [--json]
  studypilot capture resume  --course REF --module REF --session REF --revision N [--device ID] [--root PATH] [--json]
  studypilot capture stop    --course REF --module REF --session REF --revision N [--root PATH] [--json]
  studypilot capture inspect --course REF --module REF --session REF [--backend synthetic|local] [--root PATH] [--json]
`

func runCapture(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return captureUsageError(stderr, "capture requires a subcommand")
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, _ = io.WriteString(stdout, captureUsage)
		return 0
	}
	switch args[0] {
	case "start", "pause", "resume", "stop", "inspect":
		return runCaptureOperation(args[0], args[1:], stdout, stderr)
	default:
		return captureUsageError(stderr, "unknown capture subcommand")
	}
}

func runCaptureOperation(op string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("capture "+op, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	course := stringFlag{name: "--course"}
	module := stringFlag{name: "--module"}
	session := stringFlag{name: "--session"}
	backendName := stringFlag{name: "--backend"}
	device := stringFlag{name: "--device"}
	revision := intFlag{name: "--revision"}
	var root rootFlag
	asJSON := boolFlag{name: "--json"}
	flags.Var(&course, "course", "course reference")
	flags.Var(&module, "module", "module reference")
	flags.Var(&session, "session", "session reference")
	flags.Var(&root, "root", "workspace root")
	flags.Var(&asJSON, "json", "emit JSON")
	if op == "start" || op == "inspect" {
		flags.Var(&backendName, "backend", "capture backend")
	}
	if op == "start" || op == "resume" {
		flags.Var(&device, "device", "device id")
	}
	if op != "inspect" {
		flags.Var(&revision, "revision", "expected revision")
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return captureUsageError(stderr, "capture command accepts no positional arguments")
	}
	if msg := requireRefs(map[string]stringFlag{"--course": course, "--module": module, "--session": session}); msg != "" {
		return captureUsageError(stderr, msg)
	}
	if op != "inspect" && (!revision.set || revision.value <= 0) {
		return captureUsageError(stderr, "capture "+op+" requires a positive --revision")
	}
	if op == "start" && (!backendName.set || backendName.value != "synthetic") {
		return captureUsageError(stderr, "capture start requires --backend synthetic; operational local recording uses 'studypilot gui'")
	}
	if op == "inspect" && backendName.set && backendName.value != "synthetic" && backendName.value != "local" {
		return captureUsageError(stderr, "capture inspect supports only --backend synthetic or --backend local")
	}
	service, code := newCaptureService(stderr)
	if service == nil {
		return code
	}
	base := application.CaptureRequest{Root: root.value, CourseRef: course.value, ModuleRef: module.value, SessionRef: session.value, ExpectedRevision: uint64(revision.value)}
	ctx := context.Background()
	if op == "inspect" {
		result, err := service.InspectCapture(ctx, application.InspectCaptureRequest{Root: root.value, CourseRef: course.value, ModuleRef: module.value, SessionRef: session.value, Backend: backendName.value})
		return renderCaptureInspection(result, err, asJSON.value, stdout, stderr)
	}
	var result application.CaptureResult
	var err error
	switch op {
	case "start":
		result, err = service.StartCapture(ctx, application.StartCaptureRequest{CaptureRequest: base, Backend: backendName.value, DeviceID: device.value})
	case "pause":
		result, err = service.PauseCapture(ctx, base)
	case "resume":
		result, err = service.ResumeCapture(ctx, application.ResumeCaptureRequest{CaptureRequest: base, DeviceID: device.value})
	case "stop":
		result, err = service.StopCapture(ctx, base)
	}
	return renderCaptureResult(result, err, asJSON.value, stdout, stderr)
}
func captureUsageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "Error: %s\n\n%s", strings.TrimSpace(message), captureUsage)
	return 2
}
