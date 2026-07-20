// Package config persists small, machine-local StudyPilot settings outside
// every workspace. It contains no secrets and performs no writes while loading.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Arameair/studypilot/internal/platformfs"
)

const (
	SchemaVersion = 1
	MaxFileSize   = 64 << 10
)

var (
	ErrMissing     = errors.New("configuration is missing")
	ErrInvalid     = errors.New("configuration is invalid")
	ErrUnsafe      = errors.New("configuration path is unsafe")
	ErrUnsupported = errors.New("configuration schema is unsupported")
)

// Config is the versioned machine-local settings document.
type Config struct {
	SchemaVersion int    `json:"schema_version"`
	WorkspaceRoot string `json:"workspace_root"`
}

// Store owns one explicit configuration path. Tests should inject a path under
// t.TempDir rather than using DefaultStore.
type Store struct {
	path    string
	replace func(string, string) error
}

// DefaultStore resolves the current operating system's per-user configuration
// directory without creating it.
func DefaultStore() (*Store, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("determine user configuration directory: %w", err)
	}
	return NewStore(filepath.Join(root, "StudyPilot", "config.json"))
}

// NewStore constructs a store for an injected absolute path.
func NewStore(path string) (*Store, error) {
	clean := filepath.Clean(path)
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(clean) {
		return nil, fmt.Errorf("%w: configuration path must be absolute", ErrInvalid)
	}
	return &Store{path: clean, replace: platformfs.Replace}, nil
}

// Path returns the authoritative configuration file path.
func (s *Store) Path() string { return s.path }

// Load reads and strictly validates a bounded configuration document.
func (s *Store) Load() (Config, error) {
	if s == nil || s.path == "" {
		return Config{}, fmt.Errorf("%w: store is unavailable", ErrInvalid)
	}
	if err := inspectSafePath(s.path, true); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, ErrMissing
		}
		return Config{}, err
	}
	before, err := os.Lstat(s.path)
	if err != nil {
		return Config{}, fmt.Errorf("inspect configuration before open: %w", err)
	}
	file, err := os.Open(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, ErrMissing
	}
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Config{}, fmt.Errorf("inspect configuration: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxFileSize {
		return Config{}, fmt.Errorf("%w: configuration size or type is invalid", ErrInvalid)
	}
	afterOpen, err := os.Lstat(s.path)
	if err != nil || afterOpen.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, info) || !os.SameFile(info, afterOpen) {
		return Config{}, fmt.Errorf("%w: configuration identity changed during open", ErrUnsafe)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err != nil {
		return Config{}, fmt.Errorf("read configuration: %w", err)
	}
	if len(data) > MaxFileSize {
		return Config{}, fmt.Errorf("%w: configuration is too large", ErrInvalid)
	}
	afterRead, err := os.Lstat(s.path)
	if err != nil || afterRead.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, afterRead) {
		return Config{}, fmt.Errorf("%w: configuration identity changed during read", ErrUnsafe)
	}
	if unsafe, err := platformfs.PathHasReparsePoint(s.path); err != nil || unsafe {
		return Config{}, fmt.Errorf("%w: configuration path changed during read", ErrUnsafe)
	}
	var value Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Config{}, fmt.Errorf("%w: malformed JSON", ErrInvalid)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%w: configuration must contain one object", ErrInvalid)
	}
	if err := Validate(value); err != nil {
		return Config{}, err
	}
	return value, nil
}

// Save atomically replaces the configuration after an explicit caller action.
func (s *Store) Save(value Config) (returnErr error) {
	if s == nil || s.path == "" {
		return fmt.Errorf("%w: store is unavailable", ErrInvalid)
	}
	if err := Validate(value); err != nil {
		return err
	}
	parent := filepath.Dir(s.path)
	if unsafe, err := platformfs.PathHasReparsePoint(parent); err != nil {
		return fmt.Errorf("%w: inspect configuration directory", ErrUnsafe)
	} else if unsafe {
		return fmt.Errorf("%w: configuration directory contains a reparse point", ErrUnsafe)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	if unsafe, err := platformfs.PathHasReparsePoint(parent); err != nil || unsafe {
		return fmt.Errorf("%w: configuration directory is unsafe", ErrUnsafe)
	}
	if err := inspectSafePath(s.path, false); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(parent, ".studypilot-config-*")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if err := temporary.Close(); returnErr == nil && err != nil {
				returnErr = err
			}
		}
		if err := os.Remove(temporaryPath); returnErr == nil && err != nil && !errors.Is(err, fs.ErrNotExist) {
			returnErr = err
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary configuration: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	closed = true
	if err := s.replace(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace configuration: %w", err)
	}
	if err := platformfs.SyncDir(parent); err != nil {
		return fmt.Errorf("sync configuration directory: %w", err)
	}
	return nil
}

// Validate rejects ambiguous or non-canonical configuration values.
func Validate(value Config) error {
	if value.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schema version %d", ErrUnsupported, value.SchemaVersion)
	}
	root := value.WorkspaceRoot
	if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || containsParentTraversal(root) {
		return fmt.Errorf("%w: workspace_root must be a cleaned absolute path", ErrInvalid)
	}
	return nil
}

func inspectSafePath(path string, required bool) error {
	unsafe, err := platformfs.PathHasReparsePoint(path)
	if err != nil {
		return fmt.Errorf("%w: inspect configuration path", ErrUnsafe)
	}
	if unsafe {
		return fmt.Errorf("%w: configuration path contains a reparse point", ErrUnsafe)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if required {
			return fs.ErrNotExist
		}
		return fs.ErrNotExist
	}
	if err != nil {
		return fmt.Errorf("inspect configuration: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: configuration is not a regular file", ErrUnsafe)
	}
	multiple, err := platformfs.HasMultipleHardLinks(path)
	if err != nil || multiple {
		return fmt.Errorf("%w: configuration link state is unsafe", ErrUnsafe)
	}
	return nil
}

func containsParentTraversal(path string) bool {
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return true
		}
	}
	return false
}
