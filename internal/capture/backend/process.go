package backend

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// maxStderrBytes bounds how much of a recorder's stderr is retained for
// diagnostics so a chatty process cannot exhaust memory. Raw stderr is never
// exposed in public error messages.
const maxStderrBytes = 4096

// ProcessSpec describes a recorder invocation. Args are fixed and passed
// directly to the executable; no shell is ever involved and no string is
// concatenated into a command line.
type ProcessSpec struct {
	Executable  string
	Args        []string
	OutputPath  string
	StopTimeout time.Duration
}

// ProcessRunner launches recorder processes. It is injected so unit tests use a
// fake runner and never spawn a real process.
type ProcessRunner interface {
	// Lookup resolves an executable name to a path, or reports it missing.
	Lookup(name string) (string, error)
	// Start launches the process writing a WAV to spec.OutputPath. It does not
	// wait; the returned handle controls termination, reaping, and diagnostics.
	Start(ctx context.Context, spec ProcessSpec) (ProcessHandle, error)
}

// ProcessHandle controls one running recorder process.
type ProcessHandle interface {
	// Terminate gracefully stops the process, waits, and reaps it.
	Terminate(ctx context.Context) error
	// Kill force-terminates and reaps the process.
	Kill() error
	// Exited reports whether the process has already exited, its exit error if
	// any, and bounded stderr.
	Exited() (bool, error, string)
}

// execRunner is the production ProcessRunner using os/exec with no shell.
type execRunner struct{}

// NewExecRunner returns the production process runner.
func NewExecRunner() ProcessRunner { return execRunner{} }

func (execRunner) Lookup(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", newError(ErrorExecutableMissing, "process", "recorder executable not found", err)
	}
	return path, nil
}

func (execRunner) Start(ctx context.Context, spec ProcessSpec) (ProcessHandle, error) {
	if spec.Executable == "" {
		return nil, newError(ErrorInvalidRequest, "process", "empty executable", nil)
	}
	if spec.OutputPath == "" || len(spec.Args) == 0 || spec.Args[len(spec.Args)-1] != spec.OutputPath {
		return nil, newError(ErrorInvalidRequest, "process", "recorder output argument is not authoritative", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, newError(ErrorCancelled, "process", "recording start was cancelled", err)
	}
	cmd := exec.Command(spec.Executable, spec.Args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	stderr := &boundedBuffer{limit: maxStderrBytes}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, newError(ErrorExecutableMissing, "process", "recorder executable not found", err)
		}
		if errors.Is(err, fs.ErrPermission) {
			return nil, newError(ErrorPermissionDenied, "process", "recorder executable not permitted", err)
		}
		return nil, newError(ErrorInternal, "process", "start recorder process", err)
	}
	stopTimeout := spec.StopTimeout
	if stopTimeout <= 0 {
		stopTimeout = 2 * time.Second
	}
	handle := &execHandle{cmd: cmd, stderr: stderr, done: make(chan struct{}), stopTimeout: stopTimeout}
	go func() {
		handle.waitErr = cmd.Wait()
		close(handle.done)
	}()
	if err := ctx.Err(); err != nil {
		_ = handle.Kill()
		return nil, newError(ErrorCancelled, "process", "recording start was cancelled", err)
	}
	return handle, nil
}

type execHandle struct {
	cmd         *exec.Cmd
	stderr      *boundedBuffer
	done        chan struct{}
	once        sync.Once
	waitErr     error
	stopTimeout time.Duration
	expected    atomic.Bool
}

func (h *execHandle) Terminate(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	h.once.Do(func() {
		h.expected.Store(true)
		_ = terminateProcess(h.cmd)
	})
	timer := time.NewTimer(h.stopTimeout)
	defer timer.Stop()
	select {
	case <-h.done:
		return h.exitError()
	case <-ctx.Done():
		_ = h.cmd.Process.Kill()
		<-h.done
		code := ErrorCancelled
		message := "recording stop was cancelled"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = ErrorTimeout
			message = "recording stop deadline expired"
		}
		return newError(code, "process", message, ctx.Err())
	case <-timer.C:
		_ = h.cmd.Process.Kill()
		<-h.done
		return newError(ErrorTimeout, "process", "recorder did not stop before the configured timeout", nil)
	}
}

func (h *execHandle) Kill() error {
	h.expected.Store(true)
	_ = h.cmd.Process.Kill()
	<-h.done
	return h.exitError()
}

func (h *execHandle) Exited() (bool, error, string) {
	select {
	case <-h.done:
		return true, h.exitError(), h.stderr.String()
	default:
		return false, nil, h.stderr.String()
	}
}

func (h *execHandle) exitError() error {
	if h.waitErr == nil {
		return nil
	}
	// A signalled exit after our own termination is expected, not a failure.
	var exitErr *exec.ExitError
	if h.expected.Load() && errors.As(h.waitErr, &exitErr) && !exitErr.Success() {
		return nil
	}
	return h.waitErr
}

// boundedBuffer captures at most limit bytes, discarding the rest.
type boundedBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if remaining := b.limit - b.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			b.buf.Write(p[:remaining])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
