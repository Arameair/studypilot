// Command studypilot is the command-line entry point for StudyPilot.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/workspace"
)

var version = "dev"

const usage = `StudyPilot

Usage:
  studypilot version
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

	var dryRun bool
	var root rootFlag
	flags.BoolVar(&dryRun, "dry-run", false, "print the initialization plan without writing")
	flags.Var(&root, "root", "workspace root path")
	if err := flags.Parse(args); err != nil {
		writeUsage(stderr)
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, fmt.Sprintf("unexpected init argument %q", flags.Arg(0)))
	}
	if !dryRun {
		return usageError(stderr, "Initialization execution is not enabled yet. Use --dry-run to inspect the plan.")
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
