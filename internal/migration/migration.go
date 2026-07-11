// Package migration plans and applies narrowly scoped schema upgrades.
package migration

import (
	"context"
	"errors"
	"fmt"

	"github.com/Arameair/studypilot/internal/schema"
)

type ApplyError struct {
	Stage    string
	Replaced bool
	Cause    error
}

func (e *ApplyError) Error() string {
	return fmt.Sprintf("migration apply %s failed (replaced=%t): %v", e.Stage, e.Replaced, e.Cause)
}
func (e *ApplyError) Unwrap() error { return e.Cause }

type SafetyClass string

const (
	SafetyAutomatic SafetyClass = "automatic"
	SafetyReview    SafetyClass = "review"
	SafetyManual    SafetyClass = "manual"
)

type ChangeKind string

const (
	ChangeFrontmatterAdd      ChangeKind = "frontmatter_add"
	ChangeFrontmatterUpdate   ChangeKind = "frontmatter_update"
	ChangeFrontmatterRename   ChangeKind = "frontmatter_rename"
	ChangeManagedRegionAdd    ChangeKind = "managed_region_add"
	ChangeManagedRegionUpdate ChangeKind = "managed_region_update"
	ChangeFileMove            ChangeKind = "file_move"
	ChangeDirectoryAdd        ChangeKind = "directory_add"
)

type Change struct {
	Kind                     ChangeKind
	Path, Field, Description string
	Safety                   SafetyClass
}
type ChangeSet struct {
	DocumentType           schema.DocumentType
	FromVersion, ToVersion schema.Version
	Changes                []Change
	BeforeHash, AfterHash  string
}

type Migration interface {
	DocumentType() schema.DocumentType
	FromVersion() schema.Version
	ToVersion() schema.Version
	Plan(context.Context, schema.Document) (ChangeSet, []byte, error)
}

var (
	ErrInvalidMigration = errors.New("invalid migration")
	ErrManualMigration  = errors.New("migration requires manual intervention")
	ErrStalePlan        = errors.New("migration source changed after planning")
	ErrPublicMigration  = errors.New("public portfolio migration is not authorized")
)
