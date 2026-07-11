package migration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/schema"
	"github.com/Arameair/studypilot/internal/workspace"
)

type jsonMigration struct {
	documentType schema.DocumentType
	from, to     schema.Version
	field        string
	safety       SafetyClass
}

func (m jsonMigration) DocumentType() schema.DocumentType { return m.documentType }
func (m jsonMigration) FromVersion() schema.Version       { return m.from }
func (m jsonMigration) ToVersion() schema.Version         { return m.to }
func (m jsonMigration) Plan(ctx context.Context, input schema.Document) (ChangeSet, []byte, error) {
	if err := ctx.Err(); err != nil {
		return ChangeSet{}, nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(input.Content, &value); err != nil {
		return ChangeSet{}, nil, err
	}
	value["schema_version"] = int(m.to)
	if m.field != "" {
		value[m.field] = true
	}
	output, err := json.Marshal(value)
	if err != nil {
		return ChangeSet{}, nil, err
	}
	output = append(output, '\n')
	change := Change{Kind: ChangeFrontmatterAdd, Field: m.field, Description: "add synthetic managed field", Safety: m.safety}
	return ChangeSet{DocumentType: m.documentType, FromVersion: m.from, ToVersion: m.to, Changes: []Change{change}, BeforeHash: hash(input.Content), AfterHash: hash(output)}, output, nil
}

func syntheticPlanner(t *testing.T, current schema.Version, migrations ...Migration) (*Planner, *schema.Registry) {
	t.Helper()
	schemas, err := schema.NewRegistry(schema.Definition{Type: schema.DocumentCourseMetadata, MinimumVersion: 1, CurrentVersion: current, ManagedFields: []string{"schema_version", "added_v2", "added_v3"}})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(schemas, migrations...)
	if err != nil {
		t.Fatal(err)
	}
	planner, err := NewPlanner(schemas, registry)
	if err != nil {
		t.Fatal(err)
	}
	return planner, schemas
}

func TestMigrationRegistryRejectsDuplicateMissingAndInvalidEdges(t *testing.T) {
	schemas, _ := schema.NewRegistry(schema.Definition{Type: schema.DocumentCourseMetadata, MinimumVersion: 1, CurrentVersion: 3})
	v1v2 := jsonMigration{schema.DocumentCourseMetadata, 1, 2, "v2", SafetyAutomatic}
	if _, err := NewRegistry(schemas, v1v2); !errors.Is(err, schema.ErrMissingMigrationPath) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := NewRegistry(schemas, v1v2, v1v2); !errors.Is(err, schema.ErrDuplicateMigration) {
		t.Fatalf("duplicate: %v", err)
	}
	invalid := jsonMigration{schema.DocumentCourseMetadata, 2, 1, "", SafetyAutomatic}
	if _, err := NewRegistry(schemas, v1v2, invalid); !errors.Is(err, schema.ErrMigrationCycle) {
		t.Fatalf("invalid/cycle: %v", err)
	}
}

func TestPlanDocumentNoOpSequentialAndCancellation(t *testing.T) {
	planner, _ := syntheticPlanner(t, 3,
		jsonMigration{schema.DocumentCourseMetadata, 1, 2, "added_v2", SafetyAutomatic},
		jsonMigration{schema.DocumentCourseMetadata, 2, 3, "added_v3", SafetyReview},
	)
	plan, err := planner.PlanDocument(context.Background(), ".studypilot-course.json", []byte("{\"schema_version\":1,\"custom\":\"preserve\"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.FromVersion != 1 || plan.ToVersion != 3 || plan.Status != StatusUpgrade || plan.Safety != SafetyReview || len(plan.Changes) != 2 {
		t.Fatalf("plan=%+v", plan)
	}
	var output map[string]any
	if err := json.Unmarshal(plan.output, &output); err != nil || output["custom"] != "preserve" {
		t.Fatalf("custom field lost: %v %v", output, err)
	}
	again, err := planner.PlanDocument(context.Background(), plan.Path, plan.output)
	if err != nil || again.Status != StatusCurrent || len(again.Changes) != 0 {
		t.Fatalf("rerun=%+v error=%v", again, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := planner.PlanDocument(cancelled, plan.Path, plan.input); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestTranscriptPlanningPreservesSourceAndAnnotations(t *testing.T) {
	schemas, err := schema.NewRegistry(schema.Definition{Type: schema.DocumentTranscript, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: []string{"schema_version", "visibility"}})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(schemas)
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := NewPlanner(schemas, registry)
	content := []byte("---\nschema_version: 1\nvisibility: private\ncustom_tag: preserve\n---\nRaw transcript words.\n\nUser annotation: keep exactly.\n")
	plan, err := planner.PlanDocument(context.Background(), "Transcript.md", content)
	if err != nil || plan.Status != StatusCurrent || string(plan.output) != string(content) {
		t.Fatalf("plan=%+v error=%v", plan, err)
	}
}

func TestPlanWorkspaceStableDryRunAndPublicProtection(t *testing.T) {
	planner, _ := syntheticPlanner(t, 1)
	paths, _ := workspace.PathsFromRoot(filepath.Join(t.TempDir(), "workspace"))
	privateCourse := filepath.Join(paths.Private, "01 Courses", "course")
	public := filepath.Join(paths.Portfolio, "03 Troubleshooting")
	asset := filepath.Join(paths.Private, "assets", "raw.bin")
	for _, directory := range []string{privateCourse, public, filepath.Dir(asset)} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	privateMetadata := filepath.Join(privateCourse, ".studypilot-course.json")
	if err := os.WriteFile(privateMetadata, []byte("{\"schema_version\":1}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	secret := "PRIVATE TRANSCRIPT CONTENT"
	if err := os.WriteFile(filepath.Join(public, "Transcript.md"), []byte(secret), 0o640); err != nil {
		t.Fatal(err)
	}
	assetBytes := []byte{0, 1, 2, 3}
	if err := os.WriteFile(asset, assetBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(privateMetadata)
	plan, err := planner.PlanWorkspace(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(privateMetadata)
	assetAfter, _ := os.ReadFile(asset)
	if string(before) != string(after) || string(assetBytes) != string(assetAfter) {
		t.Fatal("dry run modified files")
	}
	if len(plan.Documents) != 2 || plan.Documents[0].Status != StatusManual || len(plan.Documents[0].input) != 0 || len(plan.Documents[0].output) != 0 || plan.Documents[1].Path != privateMetadata {
		t.Fatalf("workspace plan=%+v", plan.Documents)
	}
}

func TestApplyMigrationUsesBackupAtomicMutationAndIsIdempotent(t *testing.T) {
	planner, schemas := syntheticPlanner(t, 2, jsonMigration{schema.DocumentCourseMetadata, 1, 2, "added_v2", SafetyAutomatic})
	paths, _ := workspace.PathsFromRoot(filepath.Join(t.TempDir(), "workspace"))
	courseRoot := filepath.Join(paths.Private, "01 Courses", "course")
	if err := os.MkdirAll(courseRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(courseRoot, ".studypilot-course.json")
	original := []byte("{\"schema_version\":1,\"custom\":\"handwritten-value\"}\n")
	if err := os.WriteFile(target, original, 0o640); err != nil {
		t.Fatal(err)
	}
	plan, err := planner.PlanDocument(context.Background(), target, original)
	if err != nil {
		t.Fatal(err)
	}
	executor, _ := NewExecutor(schemas)
	result, err := executor.Apply(context.Background(), paths, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.BackupPath == "" || result.Record.BeforeHash != plan.BeforeHash {
		t.Fatalf("result=%+v", result)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil || string(backup) != string(original) {
		t.Fatalf("backup=%q error=%v", backup, err)
	}
	installed, _ := os.ReadFile(target)
	if hash(installed) != plan.AfterHash {
		t.Fatalf("installed hash mismatch")
	}
	noOp, err := planner.PlanDocument(context.Background(), target, installed)
	if err != nil || noOp.Status != StatusCurrent {
		t.Fatalf("no-op=%+v %v", noOp, err)
	}
	second, err := executor.Apply(context.Background(), paths, noOp, false)
	if err != nil || second.Applied {
		t.Fatalf("second=%+v %v", second, err)
	}
}

func TestApplyRejectsStaleAndPublicPlans(t *testing.T) {
	planner, schemas := syntheticPlanner(t, 2, jsonMigration{schema.DocumentCourseMetadata, 1, 2, "added_v2", SafetyAutomatic})
	paths, _ := workspace.PathsFromRoot(filepath.Join(t.TempDir(), "workspace"))
	courseRoot := filepath.Join(paths.Private, "01 Courses", "course")
	if err := os.MkdirAll(courseRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(courseRoot, ".studypilot-course.json")
	original := []byte("{\"schema_version\":1}\n")
	if err := os.WriteFile(target, original, 0o640); err != nil {
		t.Fatal(err)
	}
	plan, _ := planner.PlanDocument(context.Background(), target, original)
	if err := os.WriteFile(target, []byte("{\"schema_version\":1,\"changed\":true}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	executor, _ := NewExecutor(schemas)
	if _, err := executor.Apply(context.Background(), paths, plan, false); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("stale: %v", err)
	}
	plan.Path = filepath.Join(paths.Portfolio, ".studypilot-course.json")
	if _, err := executor.Apply(context.Background(), paths, plan, false); !errors.Is(err, ErrPublicMigration) {
		t.Fatalf("public: %v", err)
	}
}

type uncertainMutationEngine struct {
	reads []filesystem.ManagedFile
	state filesystem.ManagedFileState
}

func (e *uncertainMutationEngine) Read(context.Context, filesystem.MutationAuthority, string) (filesystem.ManagedFile, error) {
	result := e.reads[0]
	e.reads = e.reads[1:]
	return result, nil
}
func (e *uncertainMutationEngine) Apply(context.Context, filesystem.Mutation) (filesystem.MutationResult, error) {
	result := filesystem.MutationResult{Path: e.state.Path, CurrentHash: e.state.SHA256, BytesWritten: e.state.Size, Mode: 0o640}
	return result, &filesystem.MutationError{Stage: filesystem.MutationStageDirectorySync, Replaced: true, Cause: errors.New("synthetic directory sync failure")}
}
func (e *uncertainMutationEngine) Inspect(context.Context, filesystem.MutationAuthority, string) (filesystem.ManagedFileState, error) {
	return e.state, nil
}

func TestApplyReconcilesReplacementAfterDirectorySyncFailure(t *testing.T) {
	planner, schemas := syntheticPlanner(t, 2, jsonMigration{schema.DocumentCourseMetadata, 1, 2, "added_v2", SafetyAutomatic})
	paths, _ := workspace.PathsFromRoot(filepath.Join(t.TempDir(), "workspace"))
	courseRoot := filepath.Join(paths.Private, "01 Courses", "course")
	if err := os.MkdirAll(courseRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(courseRoot, ".studypilot-course.json")
	original := []byte("{\"schema_version\":1}\n")
	if err := os.WriteFile(target, original, 0o640); err != nil {
		t.Fatal(err)
	}
	plan, _ := planner.PlanDocument(context.Background(), target, original)
	executor, _ := NewExecutor(schemas)
	executor.mutations = &uncertainMutationEngine{
		reads: []filesystem.ManagedFile{
			{Path: target, Content: original, SHA256: plan.BeforeHash, Mode: 0o640, Size: int64(len(original))},
			{Path: target, Content: plan.output, SHA256: plan.AfterHash, Mode: 0o640, Size: int64(len(plan.output))},
		},
		state: filesystem.ManagedFileState{Path: target, SHA256: plan.AfterHash, Mode: 0o640, Size: int64(len(plan.output))},
	}
	result, err := executor.Apply(context.Background(), paths, plan, false)
	if !errors.Is(err, ErrDurabilityUncertain) || !result.Applied || result.Record.Result != "applied_directory_sync_uncertain" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
