// Command studypilot is the command-line entry point for StudyPilot. It is a
// thin adapter: it parses flags, builds application requests, calls the shared
// application service, and renders the results. All orchestration and domain
// logic lives in internal/application and the domain packages it calls.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/application"
	localconfig "github.com/Arameair/studypilot/internal/config"
	"github.com/Arameair/studypilot/internal/course"
)

var version = "dev"

// now is the CLI's clock seam. It is injected into the application service as
// the domain clock rather than being read for timestamps here; tests replace it
// to make course and module creation deterministic.
var now = time.Now

const usage = `StudyPilot

Usage:
  studypilot version
  studypilot init [--dry-run] [--root PATH]
  studypilot setup --root PATH [--dry-run]
  studypilot course create --name NAME [--dry-run] [--root PATH]
  studypilot module create --course NAME --number NUMBER --name NAME [--dry-run] [--root PATH]
  studypilot session <subcommand> ...   (run 'studypilot session help' for details)
  studypilot capture <subcommand> ...   (run 'studypilot capture help' for details)
  studypilot transcription <subcommand> ...   (run 'studypilot transcription help' for details)
  studypilot artifacts <subcommand> ...   (run 'studypilot artifacts help' for details)
  studypilot notes <subcommand> ...
  studypilot assets <subcommand> ...
  studypilot gui [--address 127.0.0.1:8765] [--root PATH]
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return writeUsage(stdout)
	}

	switch args[0] {
	case "help", "--help", "-h":
		if len(args) != 1 {
			return usageError(stderr, "help does not accept arguments")
		}
		return writeUsage(stdout)
	case "version":
		if len(args) != 1 {
			return usageError(stderr, "version does not accept arguments")
		}
		if _, err := fmt.Fprintf(stdout, "StudyPilot %s\n", version); err != nil {
			return 1
		}
		return 0
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "setup":
		return runSetup(args[1:], stdout, stderr)
	case "course":
		return runCourse(args[1:], stdout, stderr)
	case "module":
		return runModule(args[1:], stdout, stderr)
	case "session":
		return runSession(args[1:], stdout, stderr)
	case "capture":
		return runCapture(args[1:], stdout, stderr)
	case "transcription":
		return runTranscription(ctx, args[1:], stdout, stderr)
	case "artifacts", "notes", "assets":
		return runStudyArtifacts(ctx, args[0], args[1:], stdout, stderr)
	case "gui":
		return runGUI(ctx, args[1:], stdout, stderr)
	default:
		return usageError(stderr, fmt.Sprintf("unknown command %q", args[0]))
	}
}

var newLocalConfigStore = localconfig.DefaultStore

func runSetup(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	var root rootFlag
	var dryRun boolFlag
	flags.Var(&root, "root", "workspace root path")
	flags.Var(&dryRun, "dry-run", "validate without initializing or saving")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return usageError(stderr, "setup requires --root PATH and accepts only --dry-run")
	}
	if !root.set || strings.TrimSpace(root.value) == "" {
		return usageError(stderr, "setup requires --root PATH")
	}
	service, code := newService(stderr)
	if service == nil {
		return code
	}
	store, err := newLocalConfigStore()
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize local configuration.")
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "Error: determine current user configuration.")
		return 1
	}
	setup, err := application.NewSetupService(service, application.SetupOptions{ConfigStore: store, UserHome: home, SourceRoot: executableSourceRoot()})
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize workspace setup.")
		return 1
	}
	ctx := context.Background()
	if dryRun.value {
		state, validateErr := setup.ValidateSetup(ctx, application.SetupRequest{Root: root.value})
		return renderSetupState(state, validateErr, true, stdout, stderr)
	}
	state, setupErr := setup.InitializeSetup(ctx, application.SetupRequest{Root: root.value, Confirm: true})
	return renderSetupState(state, setupErr, false, stdout, stderr)
}

func executableSourceRoot() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	directory := filepath.Dir(executable)
	if strings.EqualFold(filepath.Base(directory), "bin") {
		return filepath.Dir(directory)
	}
	return directory
}

func newCaptureService(stderr io.Writer) (*application.Service, int) {
	config, err := loadGUICaptureConfig()
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize capture configuration")
		return nil, 1
	}
	service, err := application.NewService(application.Dependencies{Now: now, GenerateID: course.DefaultIDGenerator, CaptureServices: configuredCaptureFactory(config, true)})
	if err != nil {
		fmt.Fprintf(stderr, "Error: initialize application service\n")
		return nil, 1
	}
	return service, 0
}

func runInit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}

	var dryRun boolFlag
	var root rootFlag
	flags.Var(&dryRun, "dry-run", "print the initialization plan without writing")
	flags.Var(&root, "root", "workspace root path")
	if err := flags.Parse(args); err != nil {
		writeUsage(stderr)
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, fmt.Sprintf("unexpected init argument %q", flags.Arg(0)))
	}

	service, code := newService(stderr)
	if service == nil {
		return code
	}
	req := application.WorkspaceRequest{Root: root.value}
	ctx := context.Background()
	if dryRun.value {
		result, err := service.PlanWorkspaceInitialization(ctx, req)
		return renderPlan(result, err, root.value, stdout, stderr)
	}
	result, err := service.InitializeWorkspace(ctx, req)
	return renderExecution(result, err, "Initialization", root.value, stdout, stderr)
}

func runCourse(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "create" {
		return usageError(stderr, "course requires the create subcommand")
	}
	flags := flag.NewFlagSet("course create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	name := stringFlag{name: "--name"}
	var root rootFlag
	var dryRun boolFlag
	flags.Var(&name, "name", "course display name")
	flags.Var(&root, "root", "workspace root path")
	flags.Var(&dryRun, "dry-run", "print the course plan without writing")
	if err := flags.Parse(args[1:]); err != nil {
		writeUsage(stderr)
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, fmt.Sprintf("unexpected course argument %q", flags.Arg(0)))
	}
	if !name.set || strings.TrimSpace(name.value) == "" {
		return usageError(stderr, "course create requires --name")
	}

	service, code := newService(stderr)
	if service == nil {
		return code
	}
	req := application.CourseCreateRequest{Root: root.value, Name: name.value}
	ctx := context.Background()
	if dryRun.value {
		result, err := service.PlanCourseCreation(ctx, req)
		return renderPlan(result, err, root.value, stdout, stderr)
	}
	result, err := service.CreateCourse(ctx, req)
	return renderExecution(result, err, "Course creation", root.value, stdout, stderr)
}

func runModule(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "create" {
		return usageError(stderr, "module requires the create subcommand")
	}
	flags := flag.NewFlagSet("module create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	courseName := stringFlag{name: "--course"}
	moduleName := stringFlag{name: "--name"}
	number := intFlag{name: "--number"}
	var root rootFlag
	var dryRun boolFlag
	flags.Var(&courseName, "course", "existing course display name")
	flags.Var(&number, "number", "positive module number")
	flags.Var(&moduleName, "name", "module display name")
	flags.Var(&root, "root", "workspace root path")
	flags.Var(&dryRun, "dry-run", "print the module plan without writing")
	if err := flags.Parse(args[1:]); err != nil {
		writeUsage(stderr)
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, fmt.Sprintf("unexpected module argument %q", flags.Arg(0)))
	}
	if !courseName.set || strings.TrimSpace(courseName.value) == "" {
		return usageError(stderr, "module create requires --course")
	}
	if !number.set || number.value <= 0 {
		return usageError(stderr, "module create requires a positive --number")
	}
	if !moduleName.set || strings.TrimSpace(moduleName.value) == "" {
		return usageError(stderr, "module create requires --name")
	}

	service, code := newService(stderr)
	if service == nil {
		return code
	}
	req := application.ModuleCreateRequest{
		Root:      root.value,
		CourseRef: courseName.value,
		Number:    number.value,
		Name:      moduleName.value,
	}
	ctx := context.Background()
	if dryRun.value {
		result, err := service.PlanModuleCreation(ctx, req)
		return renderPlan(result, err, root.value, stdout, stderr)
	}
	result, err := service.CreateModule(ctx, req)
	return renderExecution(result, err, "Module creation", root.value, stdout, stderr)
}

// newService wires the production application service using the CLI clock seam
// and StudyPilot's secure ID generator. On construction failure it reports the
// error and returns a nil service with the exit code to use.
func newService(stderr io.Writer) (*application.Service, int) {
	service, err := application.NewService(application.Dependencies{
		Now:        now,
		GenerateID: course.DefaultIDGenerator,
	})
	if err != nil {
		fmt.Fprintf(stderr, "Error: initialize application service: %v\n", err)
		return nil, 1
	}
	return service, 0
}

type rootFlag struct {
	value string
	set   bool
}

type boolFlag struct {
	name  string
	value bool
	set   bool
}

func (f *boolFlag) String() string { return strconv.FormatBool(f.value) }

func (f *boolFlag) Set(value string) error {
	if f.set {
		name := f.name
		if name == "" {
			name = "flag"
		}
		return fmt.Errorf("%s may only be specified once", name)
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid boolean value %q", value)
	}
	f.value = parsed
	f.set = true
	return nil
}

func (*boolFlag) IsBoolFlag() bool { return true }

func (f *rootFlag) String() string { return f.value }

func (f *rootFlag) Set(value string) error {
	if f.set {
		return errors.New("--root may only be specified once")
	}
	f.value = value
	f.set = true
	return nil
}

type stringFlag struct {
	name  string
	value string
	set   bool
}

func (f *stringFlag) String() string { return f.value }

func (f *stringFlag) Set(value string) error {
	if f.set {
		return fmt.Errorf("%s may only be specified once", f.name)
	}
	f.value = value
	f.set = true
	return nil
}

type intFlag struct {
	name  string
	value int
	set   bool
}

func (f *intFlag) String() string { return strconv.Itoa(f.value) }

func (f *intFlag) Set(value string) error {
	if f.set {
		return fmt.Errorf("%s may only be specified once", f.name)
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s must be an integer", f.name)
	}
	f.value = parsed
	f.set = true
	return nil
}

func writeUsage(writer io.Writer) int {
	if _, err := io.WriteString(writer, usage); err != nil {
		return 1
	}
	return 0
}

func usageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "Error: %s\n\n", strings.TrimSpace(message))
	writeUsage(stderr)
	return 2
}
