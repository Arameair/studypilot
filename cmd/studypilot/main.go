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

	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/workspace"
)

var version = "dev"

const usage = `StudyPilot

Usage:
  studypilot version
  studypilot init [--root PATH]
  studypilot init --dry-run [--root PATH]
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
	return executeInit(plan, stdout, stderr)
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

func executeInit(plan filesystem.Plan, stdout, stderr io.Writer) int {
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

	if _, err := fmt.Fprintf(stdout, "Initialization complete:\n  Created: %d\n  Skipped: %d\n  Conflicts: %d\n",
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
