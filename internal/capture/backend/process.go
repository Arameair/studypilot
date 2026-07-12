package backend

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"sync"
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
	Executable string
	Args       []string
	OutputPath string
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
	cmd := exec.CommandContext(ctx, spec.Executable, spec.Args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	stderr := &boundedBuffer{limit: maxStderrBytes}
	cmd.Stderr = stderr
	// Graceful cancellation: send a termination signal, then force-kill after a
	// short grace period, so a cancelled context never orphans the process.
	cmd.Cancel = func() error { return terminateProcess(cmd) }
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, newError(ErrorExecutableMissing, "process", "recorder executable not found", err)
		}
		if errors.Is(err, fs.ErrPermission) {
			return nil, newError(ErrorPermissionDenied, "process", "recorder executable not permitted", err)
		}
		return nil, newError(ErrorInternal, "process", "start recorder process", err)
	}
	handle := &execHandle{cmd: cmd, stderr: stderr, done: make(chan struct{})}
	go func() {
		handle.waitErr = cmd.Wait()
		close(handle.done)
	}()
	return handle, nil
}

type execHandle struct {
	cmd     *exec.Cmd
	stderr  *boundedBuffer
	done    chan struct{}
	once    sync.Once
	waitErr error
}

func (h *execHandle) Terminate(ctx context.Context) error {
	h.once.Do(func() { _ = terminateProcess(h.cmd) })
	select {
	case <-h.done:
	case <-ctx.Done():
		_ = h.cmd.Process.Kill()
		<-h.done
	case <-time.After(2 * time.Second):
		_ = h.cmd.Process.Kill()
		<-h.done
	}
	return h.exitError()
}

func (h *execHandle) Kill() error {
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
	if errors.As(h.waitErr, &exitErr) && !exitErr.Success() {
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
