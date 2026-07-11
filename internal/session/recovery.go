package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/Arameair/studypilot/internal/filesystem"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

type RecoveryStatus string

const (
	RecoveryClean          RecoveryStatus = "clean"
	RecoveryIncomplete     RecoveryStatus = "incomplete"
	RecoveryUncertain      RecoveryStatus = "uncertain"
	RecoveryMalformed      RecoveryStatus = "malformed"
	RecoveryMissingRuntime RecoveryStatus = "missing_runtime"
)

type Inspection struct {
	Root     string
	Status   RecoveryStatus
	Record   *Record
	Terminal bool
	Cause    error
}

// Inspect optionally accepts an old and intended runtime hash. When supplied,
// it reconciles a previously uncertain replacement without writing.
func (r *Repository) Inspect(ctx context.Context, sessionRoot string, expectedHashes ...string) Inspection {
	inspection := Inspection{Root: filepath.Clean(sessionRoot)}
	record, err := r.Load(ctx, sessionRoot)
	if err != nil {
		inspection.Cause = err
		switch {
		case errors.Is(err, ErrSessionNotFound):
			if _, metadataErr := os.Lstat(filepath.Join(sessionRoot, sessionMetadataName)); metadataErr == nil {
				inspection.Status = RecoveryMissingRuntime
			} else {
				inspection.Status = RecoveryMalformed
			}
		case errors.Is(err, ErrMalformedState), errors.Is(err, ErrIdentityMismatch), errors.Is(err, ErrInvalidMetadata):
			inspection.Status = RecoveryMalformed
		default:
			inspection.Status = RecoveryMalformed
		}
		return inspection
	}
	inspection.Record = &record
	inspection.Terminal = record.Runtime.Snapshot.IsSessionTerminal()
	switch record.Runtime.Snapshot.SessionStatus {
	case studyruntime.SessionStatusActive, studyruntime.SessionStatusInterrupted, studyruntime.SessionStatusRecovering:
		inspection.Status = RecoveryIncomplete
	default:
		inspection.Status = RecoveryClean
	}
	if record.DurabilityWarning {
		inspection.Status = RecoveryUncertain
	}
	if len(expectedHashes) == 2 {
		switch record.RuntimeHash {
		case expectedHashes[0]:
			// The original state remains authoritative; retain its normal classification.
		case expectedHashes[1]:
			inspection.Status = RecoveryUncertain
		default:
			inspection.Status = RecoveryUncertain
			inspection.Cause = ErrSessionConflict
		}
	}
	return inspection
}

type Discovery struct {
	Records []Record
	Issues  []Inspection
}

func (r *Repository) DiscoverIncomplete(ctx context.Context, courseID, moduleID string) (Discovery, error) {
	_, module, err := r.resolveModule(courseID, moduleID)
	if err != nil {
		return Discovery{}, err
	}
	sessionsRoot := filepath.Join(module.Path, "Sessions")
	entries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		return Discovery{}, err
	}
	result := Discovery{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return Discovery{}, err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			result.Issues = append(result.Issues, Inspection{Root: filepath.Join(sessionsRoot, entry.Name()), Status: RecoveryMalformed, Cause: filesystem.ErrUnsafePath})
			continue
		}
		if !entry.IsDir() {
			continue
		}
		inspection := r.Inspect(ctx, filepath.Join(sessionsRoot, entry.Name()))
		if inspection.Record == nil {
			result.Issues = append(result.Issues, inspection)
			continue
		}
		switch inspection.Record.Runtime.Snapshot.SessionStatus {
		case studyruntime.SessionStatusPlanned, studyruntime.SessionStatusActive, studyruntime.SessionStatusInterrupted, studyruntime.SessionStatusRecovering:
			result.Records = append(result.Records, *inspection.Record)
		}
	}
	sort.Slice(result.Records, func(i, j int) bool {
		if result.Records[i].Metadata.Number != result.Records[j].Metadata.Number {
			return result.Records[i].Metadata.Number < result.Records[j].Metadata.Number
		}
		return result.Records[i].Metadata.ID < result.Records[j].Metadata.ID
	})
	sort.Slice(result.Issues, func(i, j int) bool { return result.Issues[i].Root < result.Issues[j].Root })
	return result, nil
}
