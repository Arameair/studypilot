// Package backend implements StudyPilot's first real recording backends behind
// the contracts in internal/capture. It creates actual segment files under a
// validated session's Segments directory, with a mandatory deterministic
// synthetic audio backend and a Linux process backend boundary.
//
// The backend produces capture results; it never completes sessions, mutates
// session status, writes .studypilot-runtime.json, transcribes, publishes, or
// touches the public vault. Persisting results into session runtime state is a
// later application milestone. Tests use only temporary directories, a synthetic
// source, a fake process runner, and injected clock, IDs, and liveness checks.
package backend
