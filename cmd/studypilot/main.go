// Command studypilot is the command-line entry point for StudyPilot.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/workspace"
)

var version = "dev"
var now = time.Now

const usage = `StudyPilot

Usage:
  studypilot version
  studypilot init [--dry-run] [--root PATH]
  studypilot course create --name NAME [--dry-run] [--root PATH]
  studypilot module create --course NAME --number NUMBER --name NAME [--dry-run] [--root PATH]
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
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
			return runtimeError(stderr, "write version", err)
		}
		return 0
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "course":
		return runCourse(args[1:], stdout, stderr)
	case "module":
		return runModule(args[1:], stdout, stderr)
	default:
		return usageError(stderr, fmt.Sprintf("unknown command %q", args[0]))
	}
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
	paths, err := resolvePaths(root)
	if err != nil {
		return runtimeError(stderr, "resolve workspace paths", err)
	}
	plan, err := filesystem.NewPlan(paths)
	if err != nil {
		return runtimeError(stderr, "construct filesystem plan", err)
	}
	if err := plan.Validate(); err != nil {
		return runtimeError(stderr, "validate filesystem plan", err)
	}

	if dryRun.value {
		return printDryRun(plan, stdout, stderr)
	}
	return executeCreation(plan, "Initialization", stdout, stderr)
}

func printDryRun(plan filesystem.Plan, stdout, stderr io.Writer) int {
	for _, line := range plan.Lines() {
		if _, err := fmt.Fprintln(stdout, line); err != nil {
			return runtimeError(stderr, "write dry-run plan", err)
		}
	}
	if _, err := fmt.Fprintf(stdout, "Dry run complete: %d operations planned.\n", len(plan.Operations)); err != nil {
		return runtimeError(stderr, "write dry-run summary", err)
	}
	if _, err := fmt.Fprintln(stdout, "No files or directories were created."); err != nil {
		return runtimeError(stderr, "write dry-run summary", err)
	}
	return 0
}

func executeCreation(plan filesystem.Plan, label string, stdout, stderr io.Writer) int {
	report, executionErr := filesystem.Execute(plan)
	for _, result := range report.Results {
		writer := stdout
		if result.Status == filesystem.ResultConflict {
			writer = stderr
		}
		if _, err := fmt.Fprintf(writer, "%-10s%s", strings.ToUpper(string(result.Status)), result.Operation.Path); err != nil {
			return runtimeError(stderr, "write initialization result", err)
		}
		if result.Status == filesystem.ResultConflict && result.Message != "" {
			if _, err := fmt.Fprintf(writer, ": %s", result.Message); err != nil {
				return runtimeError(stderr, "write initialization result", err)
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return runtimeError(stderr, "write initialization result", err)
		}
	}

	if _, err := fmt.Fprintf(stdout, "%s complete:\n  Created: %d\n  Skipped: %d\n  Conflicts: %d\n", label,
		report.CreatedCount(), report.SkippedCount(), report.ConflictCount()); err != nil {
		return runtimeError(stderr, "write initialization summary", err)
	}
	if executionErr != nil {
		return runtimeError(stderr, "execute filesystem plan", executionErr)
	}
	if report.HasConflicts() {
		return 1
	}
	return 0
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

	paths, err := resolvePaths(root)
	if err != nil {
		return runtimeError(stderr, "resolve workspace paths", err)
	}
	plan, err := course.NewCoursePlan(paths, name.value, now())
	if err != nil {
		if errors.Is(err, course.ErrInvalidName) {
			return usageError(stderr, err.Error())
		}
		return runtimeError(stderr, "construct course plan", err)
	}
	if dryRun.value {
		return printDryRun(plan, stdout, stderr)
	}
	return executeCreation(plan, "Course creation", stdout, stderr)
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

	paths, err := resolvePaths(root)
	if err != nil {
		return runtimeError(stderr, "resolve workspace paths", err)
	}
	plan, err := course.NewModulePlan(paths, courseName.value, number.value, moduleName.value, now())
	if err != nil {
		if errors.Is(err, course.ErrInvalidName) || errors.Is(err, course.ErrInvalidModuleNumber) {
			return usageError(stderr, err.Error())
		}
		return runtimeError(stderr, "construct module plan", err)
	}
	if dryRun.value {
		return printDryRun(plan, stdout, stderr)
	}
	return executeCreation(plan, "Module creation", stdout, stderr)
}

func resolvePaths(root rootFlag) (workspace.Paths, error) {
	if root.set {
		return workspace.PathsFromRoot(root.value)
	}
	return workspace.DefaultPaths()
}

type rootFlag struct {
	value string
	set   bool
}

type boolFlag struct {
	value bool
	set   bool
}

func (f *boolFlag) String() string { return strconv.FormatBool(f.value) }

func (f *boolFlag) Set(value string) error {
	if f.set {
		return errors.New("--dry-run may only be specified once")
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

func runtimeError(stderr io.Writer, action string, err error) int {
	fmt.Fprintf(stderr, "Error: %s: %v\n", action, err)
	return 1
}
