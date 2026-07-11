package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Arameair/studypilot/internal/schema"
	"github.com/Arameair/studypilot/internal/workspace"
)

type PlanStatus string

const (
	StatusCurrent PlanStatus = "current"
	StatusUpgrade PlanStatus = "upgrade_required"
	StatusRepair  PlanStatus = "repair_available"
	StatusManual  PlanStatus = "manual_conflict"
)

// Plan contains no exported document bytes; summaries are safe for UI adapters.
type Plan struct {
	Path                   string
	DocumentType           schema.DocumentType
	FromVersion, ToVersion schema.Version
	Status                 PlanStatus
	Safety                 SafetyClass
	Changes                []Change
	BeforeHash, AfterHash  string
	input, output          []byte
}

func (p Plan) String() string {
	return fmt.Sprintf("migration plan path=%q type=%s status=%s from=v%d to=v%d safety=%s changes=%d before=%s after=%s", p.Path, p.DocumentType, p.Status, p.FromVersion, p.ToVersion, p.Safety, len(p.Changes), p.BeforeHash, p.AfterHash)
}

type WorkspacePlan struct {
	Documents []Plan
	Unmanaged []string
}

type Planner struct {
	schemas    *schema.Registry
	migrations *Registry
}

func NewPlanner(schemas *schema.Registry, migrations *Registry) (*Planner, error) {
	if schemas == nil || migrations == nil || migrations.schemas != schemas {
		return nil, ErrInvalidMigration
	}
	return &Planner{schemas: schemas, migrations: migrations}, nil
}

func (p *Planner) PlanDocument(ctx context.Context, path string, content []byte) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	documentType, _, err := schema.DetectType(path)
	if err != nil {
		return Plan{}, err
	}
	definition, ok := p.schemas.Definition(documentType)
	if !ok {
		return Plan{}, schema.ErrUnknownDocument
	}
	document, err := schema.ParseDocument(path, content, definition)
	if err != nil {
		return Plan{}, err
	}
	before := hash(content)
	plan := Plan{Path: filepath.Clean(path), DocumentType: document.Type, FromVersion: document.Version, ToVersion: document.Version, Status: StatusCurrent, Safety: SafetyAutomatic, BeforeHash: before, AfterHash: before, input: append([]byte(nil), content...), output: append([]byte(nil), content...)}
	for document.Version < definition.CurrentVersion {
		if err := ctx.Err(); err != nil {
			return Plan{}, err
		}
		step, exists := p.migrations.migration(document.Type, document.Version)
		if !exists {
			return Plan{}, schema.ErrMissingMigrationPath
		}
		changes, output, err := step.Plan(ctx, document)
		if err != nil {
			return Plan{}, err
		}
		if changes.DocumentType != document.Type || changes.FromVersion != document.Version || changes.ToVersion != document.Version+1 {
			return Plan{}, ErrInvalidMigration
		}
		if changes.BeforeHash != hash(document.Content) || changes.AfterHash != hash(output) {
			return Plan{}, fmt.Errorf("%w: migration hashes do not match content", ErrInvalidMigration)
		}
		document, err = schema.ParseDocument(path, output, definition)
		if err != nil {
			return Plan{}, fmt.Errorf("validate planned result: %w", err)
		}
		plan.Changes = append(plan.Changes, changes.Changes...)
		plan.output = append([]byte(nil), output...)
		plan.ToVersion = document.Version
	}
	if plan.ToVersion != plan.FromVersion {
		plan.Status = StatusUpgrade
		plan.AfterHash = hash(plan.output)
		plan.Safety = maximumSafety(plan.Changes)
	}
	return plan, nil
}

func (p *Planner) PlanWorkspace(ctx context.Context, paths workspace.Paths) (WorkspacePlan, error) {
	if err := paths.Validate(); err != nil {
		return WorkspacePlan{}, err
	}
	var candidates []string
	for _, root := range []string{paths.Private, paths.Portfolio} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if isRawMediaDirectory(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if _, _, err := schema.DetectType(path); err == nil {
				candidates = append(candidates, path)
			} else if strings.HasPrefix(entry.Name(), ".studypilot-") {
				resultPath := filepath.Clean(path)
				candidates = append(candidates, resultPath+"\x00")
			}
			return nil
		})
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return WorkspacePlan{}, err
		}
	}
	sort.Strings(candidates)
	result := WorkspacePlan{}
	for _, path := range candidates {
		if strings.HasSuffix(path, "\x00") {
			result.Unmanaged = append(result.Unmanaged, strings.TrimSuffix(path, "\x00"))
			continue
		}
		if err := ctx.Err(); err != nil {
			return WorkspacePlan{}, err
		}
		documentType, _, _ := schema.DetectType(path)
		if within(paths.Portfolio, path) && (documentType == schema.DocumentTranscript || documentType == schema.DocumentSessionMetadata || documentType == schema.DocumentSessionNote) {
			result.Documents = append(result.Documents, Plan{Path: path, DocumentType: documentType, Status: StatusManual, Safety: SafetyManual})
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return WorkspacePlan{}, err
		}
		plan, err := p.PlanDocument(ctx, path, content)
		if err != nil {
			result.Documents = append(result.Documents, Plan{Path: path, DocumentType: documentType, Status: StatusManual, Safety: SafetyManual})
			continue
		}
		if within(paths.Portfolio, path) && plan.Safety == SafetyAutomatic && visibleChanges(plan.Changes) {
			plan.Safety = SafetyReview
		}
		result.Documents = append(result.Documents, plan)
	}
	return result, nil
}

func hash(content []byte) string { return fmt.Sprintf("%x", sha256.Sum256(content)) }
func maximumSafety(changes []Change) SafetyClass {
	result := SafetyAutomatic
	for _, change := range changes {
		if change.Safety == SafetyManual {
			return SafetyManual
		}
		if change.Safety == SafetyReview {
			result = SafetyReview
		}
	}
	return result
}
func visibleChanges(changes []Change) bool {
	for _, c := range changes {
		if c.Kind == ChangeManagedRegionAdd || c.Kind == ChangeManagedRegionUpdate || c.Kind == ChangeFileMove {
			return true
		}
	}
	return false
}
func isRawMediaDirectory(name string) bool {
	switch strings.ToLower(name) {
	case "audio", "recording", "recordings", "video", "videos", "assets":
		return true
	}
	return false
}
func within(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
