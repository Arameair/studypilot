package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ownershipFileName is the narrowly scoped managed lock proving one active
// recording owns a session's Segments directory.
const ownershipFileName = ".studypilot-capture.lock"

// ownershipSchemaVersion is the only supported ownership record version.
const ownershipSchemaVersion = 1

// LivenessChecker reports whether the process identified by pid on the recorded
// host appears to be alive. It is injectable so tests never depend on real
// process IDs. A checker must not block.
type LivenessChecker func(pid int, host string) bool

// Ownership records who holds the active-recording lock. It carries no
// sensitive data: only capture/segment identity, the owning process ID and
// host, and the start time.
type Ownership struct {
	SchemaVersion int       `json:"schema_version"`
	CaptureID     string    `json:"capture_id"`
	SegmentID     string    `json:"segment_id"`
	Number        int       `json:"number"`
	ProcessID     int       `json:"process_id"`
	Host          string    `json:"host"`
	StartedAt     time.Time `json:"started_at"`
}

func (o Ownership) validate() error {
	if o.SchemaVersion != ownershipSchemaVersion || o.CaptureID == "" || o.SegmentID == "" || o.Number <= 0 || o.StartedAt.IsZero() {
		return newError(ErrorInternal, "ownership", "invalid ownership record", nil)
	}
	return nil
}

// createOwnership creates the lock exclusively. An existing lock is an
// ownership conflict — it is never silently overwritten or deleted.
func createOwnership(authority SegmentAuthority, ownership Ownership) error {
	if err := ownership.validate(); err != nil {
		return err
	}
	path, err := authority.Resolve(ownershipFileName)
	if err != nil {
		return err
	}
	content, err := json.MarshalIndent(ownership, "", "  ")
	if err != nil {
		return newError(ErrorInternal, "ownership", "encode ownership", err)
	}
	handle, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		if os.IsExist(err) {
			return newError(ErrorOwnershipConflict, "ownership", "an active recording already owns this session", nil)
		}
		return newError(ErrorInternal, "ownership", "create ownership lock", err)
	}
	if _, err := handle.Write(append(content, '\n')); err != nil {
		_ = handle.Close()
		return newError(ErrorInternal, "ownership", "write ownership lock", err)
	}
	if err := handle.Sync(); err != nil {
		_ = handle.Close()
		return newError(ErrorInternal, "ownership", "sync ownership lock", err)
	}
	if err := handle.Close(); err != nil {
		return newError(ErrorInternal, "ownership", "close ownership lock", err)
	}
	if err := syncDir(authority.SegmentsDir()); err != nil {
		return newError(ErrorDurabilityUncertain, "ownership", "sync ownership directory", err)
	}
	return nil
}

// readOwnership decodes the lock, returning (false, nil) when absent.
func readOwnership(segmentsDir string) (Ownership, bool, error) {
	path := filepath.Join(segmentsDir, ownershipFileName)
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Ownership{}, false, nil
		}
		return Ownership{}, false, newError(ErrorInternal, "ownership", "inspect ownership lock", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Ownership{}, true, newError(ErrorUnsafePath, "ownership", "ownership lock is a symlink", nil)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Ownership{}, true, newError(ErrorInternal, "ownership", "read ownership lock", err)
	}
	var ownership Ownership
	if err := json.Unmarshal(content, &ownership); err != nil {
		return Ownership{}, true, newError(ErrorInternal, "ownership", "malformed ownership lock", err)
	}
	return ownership, true, nil
}

// removeOwnership deletes the lock after successful finalization and syncs the
// directory. A failure to remove is reported, never ignored.
func removeOwnership(segmentsDir string) error {
	path := filepath.Join(segmentsDir, ownershipFileName)
	if err := os.Remove(path); err != nil {
		return newError(ErrorInternal, "ownership", "remove ownership lock", err)
	}
	if err := syncDir(segmentsDir); err != nil {
		return newError(ErrorDurabilityUncertain, "ownership", "sync ownership directory after removal", err)
	}
	return nil
}

// currentOwnership builds an ownership record for this process.
func currentOwnership(captureID, segmentID string, number int, startedAt time.Time) Ownership {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "unknown"
	}
	return Ownership{
		SchemaVersion: ownershipSchemaVersion,
		CaptureID:     captureID,
		SegmentID:     segmentID,
		Number:        number,
		ProcessID:     os.Getpid(),
		Host:          host,
		StartedAt:     startedAt,
	}
}
