package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Arameair/studypilot/internal/application"
	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/studyartifact"
)

const artifactUsage = `StudyPilot study artifact commands

Usage:
  studypilot artifacts list --course REF --module REF [--session REF] [--type transcript|note|asset] [--category CATEGORY] [--root PATH] [--json]
  studypilot artifacts inspect --course REF --module REF [--root PATH] [--json]
  studypilot artifacts refresh --course REF --module REF --expected-artifact-revision N [--root PATH] [--json]
  studypilot notes create-module --course REF --module REF --title TITLE --expected-artifact-revision N [--root PATH] [--json]
  studypilot notes create-session --course REF --module REF --session REF --title TITLE --expected-artifact-revision N [--root PATH] [--json]
  studypilot assets add-module --course REF --module REF --file PATH --title TITLE --category document|image|code|archive|other --expected-artifact-revision N [--root PATH] [--json]
  studypilot assets add-session --course REF --module REF --session REF --file PATH --title TITLE --category document|image|code|archive|other --expected-artifact-revision N [--root PATH] [--json]

Notes are empty user-editable templates. Assets are copied into private managed
storage. External source paths and file contents are never printed or indexed.
`

type artifactFlags struct {
	course, module, session, title, file, category, kind stringFlag
	revision                                             intFlag
	root                                                 rootFlag
	json                                                 boolFlag
}

func newArtifactFlags(name string, stderr io.Writer) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(stderr)
	f.Usage = func() {}
	return f
}
func (a *artifactFlags) common(f *flag.FlagSet) {
	f.Var(&a.course, "course", "course reference")
	f.Var(&a.module, "module", "module reference")
	f.Var(&a.root, "root", "workspace root")
	f.Var(&a.json, "json", "emit JSON")
}
func (a *artifactFlags) requireModule(stderr io.Writer) bool {
	if strings.TrimSpace(a.course.value) == "" || strings.TrimSpace(a.module.value) == "" {
		artifactUsageError(stderr, "--course and --module are required")
		return false
	}
	return true
}
func (a artifactFlags) moduleRequest() application.StudyArtifactModuleRequest {
	return application.StudyArtifactModuleRequest{Root: a.root.value, CourseRef: a.course.value, ModuleRef: a.module.value}
}

func runStudyArtifacts(ctx context.Context, group string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return artifactUsageError(stderr, group+" requires a subcommand")
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		io.WriteString(stdout, artifactUsage)
		return 0
	}
	switch group + " " + args[0] {
	case "artifacts list":
		return runArtifactList(ctx, args[1:], stdout, stderr)
	case "artifacts inspect":
		return runArtifactInspect(ctx, args[1:], stdout, stderr)
	case "artifacts refresh":
		return runArtifactRefresh(ctx, args[1:], stdout, stderr)
	case "notes create-module":
		return runNoteCreate(ctx, false, args[1:], stdout, stderr)
	case "notes create-session":
		return runNoteCreate(ctx, true, args[1:], stdout, stderr)
	case "assets add-module":
		return runAssetAdd(ctx, false, args[1:], stdout, stderr)
	case "assets add-session":
		return runAssetAdd(ctx, true, args[1:], stdout, stderr)
	default:
		return artifactUsageError(stderr, "unknown "+group+" subcommand")
	}
}
func artifactService(stderr io.Writer) (*application.Service, int) {
	service, err := application.NewService(application.Dependencies{Now: now, GenerateID: course.DefaultIDGenerator})
	if err != nil {
		fmt.Fprintln(stderr, "Error: initialize study artifact service")
		return nil, 1
	}
	return service, 0
}
func runArtifactList(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var a artifactFlags
	f := newArtifactFlags("artifacts list", stderr)
	a.common(f)
	f.Var(&a.session, "session", "session filter")
	f.Var(&a.kind, "type", "artifact type")
	f.Var(&a.category, "category", "asset category")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return artifactUsageError(stderr, "invalid artifacts list arguments")
	}
	if !a.requireModule(stderr) {
		return 2
	}
	if a.kind.value != "" && a.kind.value != "transcript" && a.kind.value != "note" && a.kind.value != "asset" {
		return artifactUsageError(stderr, "invalid artifact type")
	}
	if a.category.value != "" && !validArtifactCategory(a.category.value) {
		return artifactUsageError(stderr, "invalid artifact category")
	}
	service, code := artifactService(stderr)
	if service == nil {
		return code
	}
	result, err := service.ListStudyArtifacts(ctx, application.ListStudyArtifactsRequest{StudyArtifactModuleRequest: a.moduleRequest(), SessionRef: a.session.value, Type: a.kind.value, Category: a.category.value})
	return renderArtifactList(result, err, a.json.value, stdout, stderr)
}
func runArtifactInspect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var a artifactFlags
	f := newArtifactFlags("artifacts inspect", stderr)
	a.common(f)
	if f.Parse(args) != nil || f.NArg() != 0 {
		return artifactUsageError(stderr, "invalid artifacts inspect arguments")
	}
	if !a.requireModule(stderr) {
		return 2
	}
	service, code := artifactService(stderr)
	if service == nil {
		return code
	}
	result, err := service.InspectStudyArtifacts(ctx, application.InspectStudyArtifactsRequest{StudyArtifactModuleRequest: a.moduleRequest()})
	return renderArtifactInspection(result, err, a.json.value, stdout, stderr)
}
func runArtifactRefresh(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var a artifactFlags
	f := newArtifactFlags("artifacts refresh", stderr)
	a.common(f)
	f.Var(&a.revision, "expected-artifact-revision", "expected artifact index revision")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return artifactUsageError(stderr, "invalid artifacts refresh arguments")
	}
	if !a.requireModule(stderr) || !a.revision.set || a.revision.value < 0 {
		return artifactUsageError(stderr, "refresh requires a non-negative --expected-artifact-revision")
	}
	service, code := artifactService(stderr)
	if service == nil {
		return code
	}
	result, err := service.RefreshStudyArtifactIndex(ctx, application.RefreshStudyArtifactIndexRequest{StudyArtifactModuleRequest: a.moduleRequest(), ExpectedArtifactRevision: uint64(a.revision.value)})
	return renderArtifactRefresh(result, err, a.json.value, stdout, stderr)
}
func runNoteCreate(ctx context.Context, session bool, args []string, stdout, stderr io.Writer) int {
	var a artifactFlags
	f := newArtifactFlags("notes create", stderr)
	a.common(f)
	f.Var(&a.title, "title", "note title")
	f.Var(&a.revision, "expected-artifact-revision", "expected artifact index revision")
	if session {
		f.Var(&a.session, "session", "session reference")
	}
	if f.Parse(args) != nil || f.NArg() != 0 {
		return artifactUsageError(stderr, "invalid notes arguments")
	}
	if !a.requireModule(stderr) || strings.TrimSpace(a.title.value) == "" || !a.revision.set || a.revision.value < 0 || (session && strings.TrimSpace(a.session.value) == "") {
		return artifactUsageError(stderr, "notes require title, scope, and non-negative expected revision")
	}
	service, code := artifactService(stderr)
	if service == nil {
		return code
	}
	var result application.StudyArtifactMutationResult
	var err error
	if session {
		result, err = service.CreateSessionNotes(ctx, application.CreateSessionNotesRequest{StudyArtifactModuleRequest: a.moduleRequest(), SessionRef: a.session.value, Title: a.title.value, ExpectedArtifactRevision: uint64(a.revision.value)})
	} else {
		result, err = service.CreateModuleNotes(ctx, application.CreateModuleNotesRequest{StudyArtifactModuleRequest: a.moduleRequest(), Title: a.title.value, ExpectedArtifactRevision: uint64(a.revision.value)})
	}
	return renderArtifactMutation(result, err, a.json.value, stdout, stderr)
}
func runAssetAdd(ctx context.Context, session bool, args []string, stdout, stderr io.Writer) int {
	var a artifactFlags
	f := newArtifactFlags("assets add", stderr)
	a.common(f)
	f.Var(&a.file, "file", "external source file")
	f.Var(&a.title, "title", "asset title")
	f.Var(&a.category, "category", "asset category")
	f.Var(&a.revision, "expected-artifact-revision", "expected artifact index revision")
	if session {
		f.Var(&a.session, "session", "session reference")
	}
	if f.Parse(args) != nil || f.NArg() != 0 {
		return artifactUsageError(stderr, "invalid assets arguments")
	}
	if !a.requireModule(stderr) || a.file.value == "" || a.title.value == "" || !validArtifactCategory(a.category.value) || !a.revision.set || a.revision.value < 0 || (session && a.session.value == "") {
		return artifactUsageError(stderr, "assets require file, title, category, scope, and non-negative expected revision")
	}
	service, code := artifactService(stderr)
	if service == nil {
		return code
	}
	var result application.StudyArtifactMutationResult
	var err error
	if session {
		result, err = service.RegisterSessionAsset(ctx, application.RegisterSessionAssetRequest{StudyArtifactModuleRequest: a.moduleRequest(), SessionRef: a.session.value, SourcePath: a.file.value, Title: a.title.value, Category: a.category.value, ExpectedArtifactRevision: uint64(a.revision.value)})
	} else {
		result, err = service.RegisterModuleAsset(ctx, application.RegisterModuleAssetRequest{StudyArtifactModuleRequest: a.moduleRequest(), SourcePath: a.file.value, Title: a.title.value, Category: a.category.value, ExpectedArtifactRevision: uint64(a.revision.value)})
	}
	return renderArtifactMutation(result, err, a.json.value, stdout, stderr)
}

func validArtifactCategory(value string) bool {
	switch value {
	case "document", "image", "code", "archive", "other":
		return true
	default:
		return false
	}
}

func artifactJSON(r studyartifact.Record) map[string]any {
	return map[string]any{"artifact_id": r.ID, "type": r.Type, "title": r.Title, "scope": r.Scope, "relative_path": r.RelativePath, "original_filename": r.OriginalFilename, "media_type": r.MediaType, "category": r.Category, "size_bytes": r.SizeBytes, "sha256": r.SHA256, "mutable": r.Mutable, "related_transcript_artifact_ids": r.RelatedTranscriptArtifactIDs}
}
func renderArtifactMutation(result application.StudyArtifactMutationResult, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportArtifactError(err, asJSON, stderr)
	}
	if asJSON {
		value := artifactJSON(result.Artifact)
		value["revision"] = result.Revision
		return writeJSON(value, stdout, stderr)
	}
	fmt.Fprintf(stdout, "Study artifact created\nID: %s\nType: %s\nScope: %s\nPath: %s\nRevision: %d\n", result.Artifact.ID, result.Artifact.Type, result.Artifact.Scope.Kind, result.Artifact.RelativePath, result.Revision)
	return 0
}
func renderArtifactList(result application.StudyArtifactListResult, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportArtifactError(err, asJSON, stderr)
	}
	if asJSON {
		items := make([]map[string]any, 0, len(result.Artifacts))
		for _, r := range result.Artifacts {
			items = append(items, artifactJSON(r))
		}
		return writeJSON(map[string]any{"revision": result.Revision, "artifacts": items}, stdout, stderr)
	}
	fmt.Fprintf(stdout, "Study artifacts\nRevision: %d\nCount: %d\n", result.Revision, len(result.Artifacts))
	for _, r := range result.Artifacts {
		hashSummary := r.SHA256
		if len(hashSummary) > 12 {
			hashSummary = hashSummary[:12]
		}
		fmt.Fprintf(stdout, "ARTIFACT %s type=%s title=%q scope=%s path=%s size=%d sha256=%s\n", r.ID, r.Type, r.Title, r.Scope.Kind, r.RelativePath, r.SizeBytes, hashSummary)
	}
	return 0
}
func renderArtifactInspection(result application.StudyArtifactInspectionResult, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportArtifactError(err, asJSON, stderr)
	}
	if asJSON {
		artifacts, issues := result.Artifacts, result.Issues
		if artifacts == nil {
			artifacts = []studyartifact.Record{}
		}
		if issues == nil {
			issues = []studyartifact.Issue{}
		}
		return writeJSON(map[string]any{"revision": result.Revision, "artifacts": artifacts, "issues": issues}, stdout, stderr)
	}
	fmt.Fprintf(stdout, "Study artifact inspection\nRevision: %d\nArtifacts: %d\nIssues: %d\n", result.Revision, len(result.Artifacts), len(result.Issues))
	for _, i := range result.Issues {
		fmt.Fprintf(stdout, "ISSUE %s path=%s: %s\n", i.Code, i.RelativePath, i.Message)
	}
	return 0
}
func renderArtifactRefresh(result application.StudyArtifactRefreshResult, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportArtifactError(err, asJSON, stderr)
	}
	if asJSON {
		artifacts, issues := result.Artifacts, result.Issues
		if artifacts == nil {
			artifacts = []studyartifact.Record{}
		}
		if issues == nil {
			issues = []studyartifact.Issue{}
		}
		return writeJSON(map[string]any{"revision": result.Revision, "artifacts": artifacts, "issues": issues}, stdout, stderr)
	}
	fmt.Fprintf(stdout, "Study artifact index refreshed\nRevision: %d\nArtifacts: %d\nIssues: %d\n", result.Revision, len(result.Artifacts), len(result.Issues))
	for _, i := range result.Issues {
		fmt.Fprintf(stdout, "ISSUE %s path=%s: %s\n", i.Code, i.RelativePath, i.Message)
	}
	return 0
}
func reportArtifactError(err error, asJSON bool, stderr io.Writer) int {
	kind := application.Classify(err)
	message := "study artifact command failed; run 'studypilot artifacts inspect'"
	if asJSON {
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": map[string]string{"kind": string(kind), "message": message}})
	} else {
		fmt.Fprintf(stderr, "Error: %s (%s).\n", message, kind)
	}
	if kind == application.ErrorInvalidInput {
		return 2
	}
	return 1
}
func artifactUsageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "Error: %s\n\n%s", message, artifactUsage)
	return 2
}
