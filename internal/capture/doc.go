// Package capture defines the UI-neutral contracts for future recording and
// media-segment capture: capability discovery, device abstraction, capture and
// segment identity, explicit start/pause/resume/stop operations, failure
// classification, and pure runtime-snapshot mapping helpers.
//
// This package models contracts only. Nothing in it records audio or video,
// probes hardware, or writes media files. It never mutates session status,
// never persists state, and never performs I/O: internal/runtime owns the
// state contracts, internal/session owns persistence, and the application
// layer will later coordinate session and capture state explicitly.
package capture
