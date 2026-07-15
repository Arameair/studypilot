package backend

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"time"
)

const maxDiagnosticBytes = 4096

type ProcessRequest struct {
	Executable string
	Args       []string
	Stdin      []byte
	MaxStdout  int
}
type ProcessResult struct {
	Stdout      []byte
	StderrBytes int
}
type ProcessRunner interface {
	Lookup(string) (string, error)
	Run(context.Context, ProcessRequest) (ProcessResult, error)
}

type ExecRunner struct{}

func NewExecRunner() ProcessRunner { return ExecRunner{} }
func (ExecRunner) Lookup(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", newError(ErrorPythonMissing, "process_lookup", false, "configured Python executable is unavailable", err)
	}
	return path, nil
}
func (ExecRunner) Run(ctx context.Context, request ProcessRequest) (ProcessResult, error) {
	if request.Executable == "" || request.MaxStdout <= 0 || request.MaxStdout > maxWorkerOutput {
		return ProcessResult{}, newError(ErrorInvalidRequest, "process_run", false, "invalid process request", nil)
	}
	if err := contextError(ctx, "process_run"); err != nil {
		return ProcessResult{}, err
	}
	cmd := exec.CommandContext(ctx, request.Executable, request.Args...)
	cmd.Cancel = func() error {
		err := cmd.Process.Signal(os.Interrupt)
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = bytes.NewReader(request.Stdin)
	stdout := &limitedBuffer{limit: request.MaxStdout}
	stderr := &limitedBuffer{limit: maxDiagnosticBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return ProcessResult{}, contextError(ctx, "process_run")
	}
	if stdout.overflow {
		return ProcessResult{}, newError(ErrorOutputTooLarge, "process_run", false, "worker protocol output exceeded its limit", nil)
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return ProcessResult{}, newError(ErrorPythonMissing, "process_run", false, "configured Python executable is unavailable", err)
		}
		return ProcessResult{}, newError(ErrorProcessFailed, "process_run", true, "local transcription worker failed", err)
	}
	return ProcessResult{Stdout: append([]byte(nil), stdout.buf.Bytes()...), StderrBytes: stderr.total}, nil
}

type limitedBuffer struct {
	buf          bytes.Buffer
	limit, total int
	overflow     bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.total += len(p)
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			b.buf.Write(p[:remaining])
		} else {
			b.buf.Write(p)
		}
	}
	if b.total > b.limit {
		b.overflow = true
	}
	return len(p), nil
}

var _ io.Writer = (*limitedBuffer)(nil)
