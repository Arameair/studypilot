package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Arameair/studypilot/internal/application"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

const sessionUsage = `StudyPilot session commands

Usage:
  studypilot session create   --course REF --module REF --title TITLE [--idempotency-key KEY] [--root PATH] [--json]
  studypilot session get      --course REF --module REF --session REF [--root PATH] [--json]
  studypilot session start    --course REF --module REF --session REF --revision N [--root PATH] [--json]
  studypilot session interrupt --course REF --module REF --session REF --revision N [--reason TEXT] [--root PATH] [--json]
  studypilot session recover  --course REF --module REF --session REF --revision N [--root PATH] [--json]
  studypilot session resume   --course REF --module REF --session REF --revision N [--root PATH] [--json]
  studypilot session complete --course REF --module REF --session REF --revision N [--root PATH] [--json]
  studypilot session abandon  --course REF --module REF --session REF --revision N [--reason TEXT] [--root PATH] [--json]
  studypilot session list     [--course REF] [--module REF] [--status STATUS] [--root PATH] [--json]
  studypilot session inspect  --course REF --module REF (--session REF | --all) [--root PATH] [--json]

A reference (REF) may be an immutable ID, a session number, or an exact title.
Mutation commands require the current --revision; obtain it from get, list,
inspect, or create. Interruption and abandonment reasons are private and are
never persisted or echoed.
`

func runSession(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return sessionUsageError(stderr, "session requires a subcommand")
	}
	rest := args[1:]
	switch args[0] {
	case "help", "--help", "-h":
		return writeSessionUsage(stdout)
	case "create":
		return runSessionCreate(rest, stdout, stderr)
	case "get":
		return runSessionGet(rest, stdout, stderr)
	case "start", "resume", "recover", "complete":
		return runSessionMutation(args[0], rest, false, stdout, stderr)
	case "interrupt", "abandon":
		return runSessionMutation(args[0], rest, true, stdout, stderr)
	case "list":
		return runSessionList(rest, stdout, stderr)
	case "inspect":
		return runSessionInspect(rest, stdout, stderr)
	default:
		return sessionUsageError(stderr, fmt.Sprintf("unknown session subcommand %q", args[0]))
	}
}

func newSessionFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	return flags
}

func runSessionCreate(args []string, stdout, stderr io.Writer) int {
	flags := newSessionFlagSet("session create", stderr)
	course := stringFlag{name: "--course"}
	module := stringFlag{name: "--module"}
	title := stringFlag{name: "--title"}
	key := stringFlag{name: "--idempotency-key"}
	var root rootFlag
	asJSON := boolFlag{name: "--json"}
	flags.Var(&course, "course", "course reference")
	flags.Var(&module, "module", "module reference")
	flags.Var(&title, "title", "session title")
	flags.Var(&key, "idempotency-key", "optional idempotency key for safe retries")
	flags.Var(&root, "root", "workspace root path")
	flags.Var(&asJSON, "json", "emit JSON output")
	if code, ok := parseSessionFlags(flags, args, stderr); !ok {
		return code
	}
	if msg := requireRefs(map[string]stringFlag{"--course": course, "--module": module, "--title": title}); msg != "" {
		return sessionUsageError(stderr, msg)
	}
	service, code := newService(stderr)
	if service == nil {
		return code
	}
	req := application.CreateSessionRequest{Root: root.value, CourseRef: course.value, ModuleRef: module.value, Title: title.value, IdempotencyKey: key.value}
	result, err := service.CreateSession(context.Background(), req)
	return renderSessionResult(result, err, asJSON.value, false, stdout, stderr)
}

func runSessionGet(args []string, stdout, stderr io.Writer) int {
	flags := newSessionFlagSet("session get", stderr)
	course := stringFlag{name: "--course"}
	module := stringFlag{name: "--module"}
	session := stringFlag{name: "--session"}
	var root rootFlag
	asJSON := boolFlag{name: "--json"}
	flags.Var(&course, "course", "course reference")
	flags.Var(&module, "module", "module reference")
	flags.Var(&session, "session", "session reference")
	flags.Var(&root, "root", "workspace root path")
	flags.Var(&asJSON, "json", "emit JSON output")
	if code, ok := parseSessionFlags(flags, args, stderr); !ok {
		return code
	}
	if msg := requireRefs(map[string]stringFlag{"--course": course, "--module": module, "--session": session}); msg != "" {
		return sessionUsageError(stderr, msg)
	}
	service, code := newService(stderr)
	if service == nil {
		return code
	}
	req := application.SessionReferenceRequest{Root: root.value, CourseRef: course.value, ModuleRef: module.value, SessionRef: session.value}
	result, err := service.GetSession(context.Background(), req)
	return renderSessionResult(result, err, asJSON.value, false, stdout, stderr)
}

// runSessionMutation handles the state-changing commands. All require the
// current --revision; interrupt and abandon additionally accept a private
// --reason. The command dispatches to the matching application use case and
// never retries on conflict.
func runSessionMutation(op string, args []string, withReason bool, stdout, stderr io.Writer) int {
	flags := newSessionFlagSet("session "+op, stderr)
	course := stringFlag{name: "--course"}
	module := stringFlag{name: "--module"}
	session := stringFlag{name: "--session"}
	reason := stringFlag{name: "--reason"}
	revision := intFlag{name: "--revision"}
	var root rootFlag
	asJSON := boolFlag{name: "--json"}
	flags.Var(&course, "course", "course reference")
	flags.Var(&module, "module", "module reference")
	flags.Var(&session, "session", "session reference")
	flags.Var(&revision, "revision", "expected current revision")
	flags.Var(&root, "root", "workspace root path")
	flags.Var(&asJSON, "json", "emit JSON output")
	if withReason {
		flags.Var(&reason, "reason", "private reason; not persisted or echoed")
	}
	if code, ok := parseSessionFlags(flags, args, stderr); !ok {
		return code
	}
	if msg := requireRefs(map[string]stringFlag{"--course": course, "--module": module, "--session": session}); msg != "" {
		return sessionUsageError(stderr, msg)
	}
	if !revision.set || revision.value <= 0 {
		return sessionUsageError(stderr, "session "+op+" requires a positive --revision")
	}
	service, code := newService(stderr)
	if service == nil {
		return code
	}
	update := application.UpdateSessionRequest{Root: root.value, CourseRef: course.value, ModuleRef: module.value, SessionRef: session.value, ExpectedRevision: uint64(revision.value)}
	ctx := context.Background()
	var result application.SessionResult
	var err error
	switch op {
	case "start":
		result, err = service.StartSession(ctx, update)
	case "resume":
		result, err = service.ResumeSession(ctx, update)
	case "recover":
		result, err = service.BeginSessionRecovery(ctx, update)
	case "complete":
		result, err = service.CompleteSession(ctx, application.CompleteSessionRequest{UpdateSessionRequest: update})
	case "interrupt":
		result, err = service.InterruptSession(ctx, application.InterruptSessionRequest{UpdateSessionRequest: update, Reason: reason.value})
	case "abandon":
		result, err = service.AbandonSession(ctx, application.AbandonSessionRequest{UpdateSessionRequest: update, Reason: reason.value})
	default:
		return sessionUsageError(stderr, fmt.Sprintf("unknown session mutation %q", op))
	}
	return renderSessionResult(result, err, asJSON.value, true, stdout, stderr)
}

func runSessionList(args []string, stdout, stderr io.Writer) int {
	flags := newSessionFlagSet("session list", stderr)
	course := stringFlag{name: "--course"}
	module := stringFlag{name: "--module"}
	status := stringFlag{name: "--status"}
	var root rootFlag
	asJSON := boolFlag{name: "--json"}
	flags.Var(&course, "course", "course reference filter")
	flags.Var(&module, "module", "module reference filter")
	flags.Var(&status, "status", "status filter: planned, active, interrupted, recovering")
	flags.Var(&root, "root", "workspace root path")
	flags.Var(&asJSON, "json", "emit JSON output")
	if code, ok := parseSessionFlags(flags, args, stderr); !ok {
		return code
	}
	if strings.TrimSpace(module.value) != "" && strings.TrimSpace(course.value) == "" {
		return sessionUsageError(stderr, "session list --module requires --course")
	}
	var filter studyruntime.SessionStatus
	if status.set {
		parsed, ok := incompleteStatus(status.value)
		if !ok {
			return sessionUsageError(stderr, "session list --status must be one of planned, active, interrupted, recovering")
		}
		filter = parsed
	}
	service, code := newService(stderr)
	if service == nil {
		return code
	}
	req := application.ListIncompleteSessionsRequest{Root: root.value, CourseRef: course.value, ModuleRef: module.value}
	summaries, err := service.ListIncompleteSessions(context.Background(), req)
	if err == nil && status.set {
		summaries = filterByStatus(summaries, filter)
	}
	return renderSessionList(summaries, err, asJSON.value, stdout, stderr)
}

func runSessionInspect(args []string, stdout, stderr io.Writer) int {
	flags := newSessionFlagSet("session inspect", stderr)
	course := stringFlag{name: "--course"}
	module := stringFlag{name: "--module"}
	session := stringFlag{name: "--session"}
	var root rootFlag
	all := boolFlag{name: "--all"}
	asJSON := boolFlag{name: "--json"}
	flags.Var(&course, "course", "course reference")
	flags.Var(&module, "module", "module reference")
	flags.Var(&session, "session", "session reference")
	flags.Var(&root, "root", "workspace root path")
	flags.Var(&all, "all", "inspect every session in the module")
	flags.Var(&asJSON, "json", "emit JSON output")
	if code, ok := parseSessionFlags(flags, args, stderr); !ok {
		return code
	}
	if msg := requireRefs(map[string]stringFlag{"--course": course, "--module": module}); msg != "" {
		return sessionUsageError(stderr, msg)
	}
	hasSession := strings.TrimSpace(session.value) != ""
	if all.value == hasSession {
		return sessionUsageError(stderr, "session inspect requires exactly one of --session or --all")
	}
	service, code := newService(stderr)
	if service == nil {
		return code
	}
	ctx := context.Background()
	if all.value {
		scan, err := service.InspectModuleSessions(ctx, application.InspectModuleRequest{Root: root.value, CourseRef: course.value, ModuleRef: module.value})
		return renderSessionScan(scan, err, asJSON.value, stdout, stderr)
	}
	req := application.SessionReferenceRequest{Root: root.value, CourseRef: course.value, ModuleRef: module.value, SessionRef: session.value}
	result, err := service.InspectSession(ctx, req)
	return renderSessionInspection(result, err, asJSON.value, stdout, stderr)
}

// parseSessionFlags parses a session flag set and enforces no positional
// arguments. It returns (exit code, ok=false) when the caller should stop.
func parseSessionFlags(flags *flag.FlagSet, args []string, stderr io.Writer) (int, bool) {
	if err := flags.Parse(args); err != nil {
		return 2, false
	}
	if flags.NArg() != 0 {
		return sessionUsageError(stderr, fmt.Sprintf("unexpected argument %q", flags.Arg(0))), false
	}
	return 0, true
}

// requireRefs returns a usage message for the first missing required reference,
// or "" when all are present.
func requireRefs(refs map[string]stringFlag) string {
	for _, name := range []string{"--course", "--module", "--session", "--title"} {
		flag, ok := refs[name]
		if !ok {
			continue
		}
		if !flag.set || strings.TrimSpace(flag.value) == "" {
			return "session command requires " + name
		}
	}
	return ""
}

func incompleteStatus(value string) (studyruntime.SessionStatus, bool) {
	switch studyruntime.SessionStatus(value) {
	case studyruntime.SessionStatusPlanned:
		return studyruntime.SessionStatusPlanned, true
	case studyruntime.SessionStatusActive:
		return studyruntime.SessionStatusActive, true
	case studyruntime.SessionStatusInterrupted:
		return studyruntime.SessionStatusInterrupted, true
	case studyruntime.SessionStatusRecovering:
		return studyruntime.SessionStatusRecovering, true
	default:
		return "", false
	}
}

func filterByStatus(summaries []application.SessionSummary, status studyruntime.SessionStatus) []application.SessionSummary {
	filtered := make([]application.SessionSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.SessionStatus == status {
			filtered = append(filtered, summary)
		}
	}
	return filtered
}

func writeSessionUsage(writer io.Writer) int {
	if _, err := io.WriteString(writer, sessionUsage); err != nil {
		return 1
	}
	return 0
}

func sessionUsageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "Error: %s\n\n", strings.TrimSpace(message))
	writeSessionUsage(stderr)
	return 2
}
