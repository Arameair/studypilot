package studyartifact

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

const indexRelativePath = ".studypilot/artifacts.json"

type Store struct {
	context  Context
	clock    func() time.Time
	generate IDGenerator
	saveHook func(string, []byte) error
}

func NewStore(ctx Context, clock func() time.Time, generate IDGenerator) (*Store, error) {
	if clock == nil || generate == nil || ctx.CourseID == "" || ctx.ModuleID == "" || !filepath.IsAbs(ctx.ModuleRoot) {
		return nil, fmt.Errorf("%w: store configuration", ErrInvalid)
	}
	root := filepath.Clean(ctx.ModuleRoot)
	sessionIDs := map[string]bool{}
	sessionRoots := map[string]bool{}
	for _, s := range ctx.Sessions {
		cleanSessionRoot := filepath.Clean(s.Root)
		if s.ID == "" || sessionIDs[s.ID] || sessionRoots[cleanSessionRoot] || !within(filepath.Join(root, "Sessions"), cleanSessionRoot) {
			return nil, fmt.Errorf("%w: session authority", ErrInvalid)
		}
		sessionIDs[s.ID] = true
		sessionRoots[cleanSessionRoot] = true
	}
	ctx.ModuleRoot = root
	return &Store{context: ctx, clock: clock, generate: generate}, nil
}
func (s *Store) IndexPath() string {
	return filepath.Join(s.context.ModuleRoot, filepath.FromSlash(indexRelativePath))
}
func (s *Store) Load(ctx context.Context) (Index, error) {
	if err := ctx.Err(); err != nil {
		return Index{}, err
	}
	path := s.IndexPath()
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return EmptyIndex(), nil
	}
	if err != nil {
		return Index{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Index{}, fmt.Errorf("%w: unsafe index", ErrInvalid)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Index{}, err
	}
	var index Index
	if err = json.Unmarshal(data, &index); err != nil {
		return Index{}, fmt.Errorf("%w: malformed index", ErrInvalid)
	}
	if err = index.Validate(); err != nil {
		return Index{}, err
	}
	SortRecords(index.Artifacts)
	return index.Clone(), nil
}
func (s *Store) CreateModuleNotes(ctx context.Context, title string, expected uint64) (Record, Index, error) {
	return s.createNotes(ctx, title, expected, nil)
}
func (s *Store) CreateSessionNotes(ctx context.Context, sessionID, title string, expected uint64) (Record, Index, error) {
	sc, ok := s.session(sessionID)
	if !ok {
		return Record{}, Index{}, ErrNotFound
	}
	return s.createNotes(ctx, title, expected, &sc)
}
func (s *Store) createNotes(ctx context.Context, title string, expected uint64, session *SessionContext) (Record, Index, error) {
	if !safeText(title, 160) {
		return Record{}, Index{}, ErrInvalid
	}
	index, err := s.Load(ctx)
	if err != nil {
		return Record{}, Index{}, err
	}
	if index.Revision != expected {
		return Record{}, index, ErrRevisionConflict
	}
	id, err := s.generate()
	if err != nil {
		return Record{}, index, err
	}
	if id.Validate() != nil {
		return Record{}, index, ErrInvalid
	}
	now := s.clock().UTC()
	scope := Scope{Kind: ScopeModule, CourseID: s.context.CourseID, ModuleID: s.context.ModuleID}
	relative := "Notes/module-notes.md"
	if session != nil {
		scope.Kind = ScopeSession
		scope.SessionID = session.ID
		relative = filepath.ToSlash(filepath.Join("Sessions", filepath.Base(session.Root), "Notes", "session-notes.md"))
	}
	abs := filepath.Join(s.context.ModuleRoot, filepath.FromSlash(relative))
	body := noteDocument(id, scope, title, now, session != nil)
	if err = writeExclusiveAtomic(abs, []byte(body), 0o640); err != nil {
		return Record{}, index, err
	}
	record, err := fileRecord(id, TypeNote, scope, title, relative, "text/markdown", "", "generated_template", true, now, abs)
	if err != nil {
		return Record{}, index, err
	}
	next, err := s.appendAndSave(ctx, index, record)
	if err != nil {
		return record, index, fmt.Errorf("%w: %v", ErrPersistenceUncertain, err)
	}
	return record, next, nil
}
func noteDocument(id ID, scope Scope, title string, now time.Time, session bool) string {
	kind := "Module"
	headings := []string{"Module Summary", "Key Concepts", "Questions", "Exercises", "References"}
	if session {
		kind = "Session"
		headings = []string{"Session Objective", "Key Points", "Questions", "Practice", "Follow-up"}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\nschema_version: 1\nartifact_id: %s\nscope: %s\ncourse_id: %s\nmodule_id: %s\n", id, scope.Kind, scope.CourseID, scope.ModuleID)
	if scope.SessionID != "" {
		fmt.Fprintf(&b, "session_id: %s\n", scope.SessionID)
	}
	fmt.Fprintf(&b, "created_at: %s\nupdated_at: %s\n---\n\n# %s\n", now.Format(time.RFC3339), now.Format(time.RFC3339), title)
	for _, h := range headings {
		fmt.Fprintf(&b, "\n## %s\n", h)
	}
	_ = kind
	return b.String()
}

func (s *Store) RegisterModuleAsset(ctx context.Context, source, title, category string, expected uint64) (Record, Index, error) {
	return s.registerAsset(ctx, source, title, category, expected, nil)
}
func (s *Store) RegisterSessionAsset(ctx context.Context, sessionID, source, title, category string, expected uint64) (Record, Index, error) {
	sc, ok := s.session(sessionID)
	if !ok {
		return Record{}, Index{}, ErrNotFound
	}
	return s.registerAsset(ctx, source, title, category, expected, &sc)
}
func (s *Store) registerAsset(ctx context.Context, source, title, category string, expected uint64, session *SessionContext) (Record, Index, error) {
	if !safeText(title, 160) || !validCategory(category) {
		return Record{}, Index{}, ErrInvalid
	}
	info, err := os.Lstat(source)
	if err != nil {
		return Record{}, Index{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > MaxAssetSize {
		return Record{}, Index{}, ErrInvalid
	}
	index, err := s.Load(ctx)
	if err != nil {
		return Record{}, Index{}, err
	}
	if index.Revision != expected {
		return Record{}, index, ErrRevisionConflict
	}
	id, err := s.generate()
	if err != nil {
		return Record{}, index, err
	}
	name, err := safeAssetName(filepath.Base(source))
	if err != nil {
		return Record{}, index, err
	}
	scope := Scope{Kind: ScopeModule, CourseID: s.context.CourseID, ModuleID: s.context.ModuleID}
	base := "Assets"
	if session != nil {
		scope.Kind = ScopeSession
		scope.SessionID = session.ID
		base = filepath.ToSlash(filepath.Join("Sessions", filepath.Base(session.Root), "Assets"))
	}
	relative := base + "/" + id.String() + "-" + name
	destination := filepath.Join(s.context.ModuleRoot, filepath.FromSlash(relative))
	if err = copyExclusiveAtomic(source, destination, 0o640); err != nil {
		return Record{}, index, err
	}
	media := mediaType(category, name)
	now := s.clock().UTC()
	record, err := fileRecord(id, TypeAsset, scope, title, relative, media, category, "import", false, now, destination)
	if err != nil {
		return Record{}, index, err
	}
	record.OriginalFilename = name
	next, err := s.appendAndSave(ctx, index, record)
	if err != nil {
		return record, index, fmt.Errorf("%w: %v", ErrPersistenceUncertain, err)
	}
	return record, next, nil
}
func (s *Store) appendAndSave(ctx context.Context, index Index, record Record) (Index, error) {
	for _, r := range index.Artifacts {
		if r.ID == record.ID || r.RelativePath == record.RelativePath {
			return index, ErrConflict
		}
	}
	next := index.Clone()
	next.Revision++
	next.UpdatedAt = s.clock().UTC()
	next.Artifacts = append(next.Artifacts, record.Clone())
	SortRecords(next.Artifacts)
	if err := s.save(ctx, index.Revision, next); err != nil {
		return index, err
	}
	return next, nil
}
func (s *Store) save(ctx context.Context, expected uint64, index Index) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := s.Load(ctx)
	if err != nil {
		return err
	}
	if current.Revision != expected {
		return ErrRevisionConflict
	}
	if err := index.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if s.saveHook != nil {
		return s.saveHook(s.IndexPath(), data)
	}
	return replaceAtomic(s.IndexPath(), data, 0o640)
}

func (s *Store) Refresh(ctx context.Context, expected uint64) (Index, []Issue, error) {
	current, err := s.Load(ctx)
	if err != nil {
		return Index{}, nil, err
	}
	if current.Revision != expected {
		return current, nil, ErrRevisionConflict
	}
	records, issues := s.discover(ctx, current, true)
	next := Index{SchemaVersion: SchemaVersion, Revision: current.Revision + 1, Artifacts: records, UpdatedAt: s.clock().UTC()}
	SortRecords(next.Artifacts)
	if err = s.save(ctx, current.Revision, next); err != nil {
		return current, issues, err
	}
	return next, issues, nil
}
func (s *Store) Inspect(ctx context.Context) (Inspection, error) {
	index, err := s.Load(ctx)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			return Inspection{Artifacts: []Record{}, Issues: []Issue{issue("malformed_index", "error", false, "", indexRelativePath, "artifact index is malformed or unsupported")}}, nil
		}
		return Inspection{}, err
	}
	discovered, issues := s.discover(ctx, index, false)
	byPath := map[string]Record{}
	for _, r := range discovered {
		byPath[r.RelativePath] = r
	}
	for _, r := range index.Artifacts {
		d, ok := byPath[r.RelativePath]
		if !ok {
			issues = append(issues, issue("indexed_file_missing", "error", true, r.ID, r.RelativePath, "indexed artifact file is missing"))
			continue
		}
		if r.SizeBytes != d.SizeBytes {
			issues = append(issues, issue("artifact_size_mismatch", "warning", true, r.ID, r.RelativePath, "artifact size differs from the index"))
		}
		if r.SHA256 != d.SHA256 {
			issues = append(issues, issue("artifact_hash_mismatch", "warning", true, r.ID, r.RelativePath, "artifact hash differs from the index"))
		}
		delete(byPath, r.RelativePath)
	}
	for _, r := range byPath {
		issues = append(issues, issue("unindexed_managed_file", "warning", true, r.ID, r.RelativePath, "managed artifact is absent from the index"))
	}
	sortIssues(issues)
	return Inspection{Revision: index.Revision, Artifacts: index.Artifacts, Issues: issues}, nil
}

func (s *Store) discover(ctx context.Context, current Index, assignIdentities bool) ([]Record, []Issue) {
	preserved := map[string]Record{}
	for _, r := range current.Artifacts {
		preserved[string(r.Type)+"\x00"+r.RelativePath] = r
	}
	var records []Record
	var issues []Issue
	addFile := func(t Type, scope Scope, title, relative, media, category, source string, mutable bool, transcript *TranscriptReference) {
		abs := filepath.Join(s.context.ModuleRoot, filepath.FromSlash(relative))
		info, err := os.Lstat(abs)
		if err != nil {
			return
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			issues = append(issues, issue("asset_symlink", "error", false, "", relative, "managed artifact is not a regular file"))
			return
		}
		if hasMultipleLinks(info) {
			issues = append(issues, issue("asset_hardlink_conflict", "error", false, "", relative, "managed artifact has multiple filesystem links"))
			return
		}
		previous := preserved[string(t)+"\x00"+relative]
		id := previous.ID
		if t == TypeNote {
			noteID, valid := parseNoteMetadata(abs, scope)
			if !valid {
				issues = append(issues, issue("note_metadata_mismatch", "error", true, id, relative, "note metadata does not match its managed scope"))
				return
			}
			if id == "" {
				id = noteID
			}
			if id != noteID {
				issues = append(issues, issue("note_metadata_mismatch", "error", true, id, relative, "note identity differs from the index"))
				return
			}
		}
		if id == "" && t == TypeAsset {
			id = parseAssetID(filepath.Base(abs))
		}
		if id == "" && assignIdentities {
			id, _ = s.generate()
		}
		if id == "" {
			return
		}
		now := s.clock().UTC()
		created := now
		if !previous.CreatedAt.IsZero() {
			created = previous.CreatedAt
			title = previous.Title
			category = previous.Category
			media = previous.MediaType
			source = previous.Source
		}
		r, err := fileRecord(id, t, scope, title, relative, media, category, source, mutable, created, abs)
		if err != nil {
			issues = append(issues, issue("invalid_artifact_scope", "error", false, id, relative, "managed artifact metadata is invalid"))
			return
		}
		r.UpdatedAt = now
		r.Transcript = transcript
		if t == TypeAsset {
			r.OriginalFilename = assetTitle(filepath.Base(relative))
		}
		records = append(records, r)
	}
	moduleScope := Scope{Kind: ScopeModule, CourseID: s.context.CourseID, ModuleID: s.context.ModuleID}
	addFile(TypeNote, moduleScope, "Module Notes", "Notes/module-notes.md", "text/markdown", "", "generated_template", true, nil)
	s.discoverAssets(filepath.Join(s.context.ModuleRoot, "Assets"), "Assets", moduleScope, addFile, &issues)
	for _, sc := range s.context.Sessions {
		if err := ctx.Err(); err != nil {
			break
		}
		sessionBase := filepath.ToSlash(filepath.Join("Sessions", filepath.Base(sc.Root)))
		sessionScope := Scope{Kind: ScopeSession, CourseID: s.context.CourseID, ModuleID: s.context.ModuleID, SessionID: sc.ID}
		addFile(TypeNote, sessionScope, "Session Notes", sessionBase+"/Notes/session-notes.md", "text/markdown", "", "generated_template", true, nil)
		s.discoverAssets(filepath.Join(sc.Root, "Assets"), sessionBase+"/Assets", sessionScope, addFile, &issues)
		completedTranscriptPaths := map[string]bool{}
		for _, state := range sc.Snapshot.Transcriptions {
			if state.JobStatus != "completed" {
				continue
			}
			if state.TranscriptJSONRelativePath != "" {
				completedTranscriptPaths[moduleRelative(s.context.ModuleRoot, sc.Root, state.TranscriptJSONRelativePath)] = true
			}
			r, is := s.discoverTranscript(sc, state, preserved, assignIdentities)
			issues = append(issues, is...)
			if r.ID != "" {
				records = append(records, r)
			}
		}
		s.discoverOrphanTranscripts(sc, completedTranscriptPaths, &issues)
	}
	transcripts := map[string][]ID{}
	for _, r := range records {
		if r.Type == TypeTranscript {
			transcripts[r.Scope.SessionID] = append(transcripts[r.Scope.SessionID], r.ID)
		}
	}
	for i := range records {
		if records[i].Type == TypeNote && records[i].Scope.Kind == ScopeSession {
			ids := append([]ID(nil), transcripts[records[i].Scope.SessionID]...)
			sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
			records[i].RelatedTranscriptArtifactIDs = ids
		}
	}
	SortRecords(records)
	sortIssues(issues)
	return records, issues
}

type addFileFunc func(Type, Scope, string, string, string, string, string, bool, *TranscriptReference)

func (s *Store) discoverAssets(dir, relativeBase string, scope Scope, add addFileFunc, issues *[]Issue) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		rel := relativeBase + "/" + e.Name()
		info, infoErr := e.Info()
		if e.Type()&os.ModeSymlink != 0 || infoErr != nil {
			*issues = append(*issues, issue("asset_symlink", "error", false, "", rel, "managed asset is a symlink"))
			continue
		}
		if hasMultipleLinks(info) {
			*issues = append(*issues, issue("asset_hardlink_conflict", "error", false, "", rel, "managed asset has multiple filesystem links"))
			continue
		}
		id := parseAssetID(e.Name())
		if id == "" {
			*issues = append(*issues, issue("unmanaged_file", "warning", true, "", rel, "asset filename is not managed"))
			continue
		}
		add(TypeAsset, scope, assetTitle(e.Name()), rel, mediaType("other", e.Name()), "other", "import", false, nil)
	}
}

func (s *Store) discoverOrphanTranscripts(session SessionContext, completed map[string]bool, issues *[]Issue) {
	entries, err := os.ReadDir(filepath.Join(session.Root, "Transcripts"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "-transcript.json") {
			continue
		}
		relative := moduleRelative(s.context.ModuleRoot, session.Root, filepath.ToSlash(filepath.Join("Transcripts", entry.Name())))
		if completed[relative] {
			continue
		}
		*issues = append(*issues, issue("transcript_without_completed_job", "error", false, "", relative, "transcript file has no matching completed runtime job"))
	}
}

type transcriptDoc struct {
	SchemaVersion int    `json:"schema_version"`
	JobID         string `json:"job_id"`
	SessionID     string `json:"session_id"`
	CaptureID     string `json:"capture_id"`
	SegmentID     string `json:"segment_id"`
	SegmentNumber int    `json:"segment_number"`
	Transcript    struct {
		Language       string `json:"language"`
		DurationMillis int64  `json:"duration_millis"`
		Text           string `json:"text"`
	} `json:"transcript"`
	ProvenanceRelativePath string `json:"provenance_relative_path"`
}
type provenanceDoc struct {
	SchemaVersion int `json:"schema_version"`
	Provenance    struct {
		JobID             string `json:"job_id"`
		SessionID         string `json:"session_id"`
		CaptureID         string `json:"capture_id"`
		SegmentID         string `json:"segment_id"`
		InputRelativePath string `json:"input_relative_path"`
		InputSHA256       string `json:"input_sha256"`
	} `json:"provenance"`
}
type jobDoc struct {
	SchemaVersion int       `json:"schema_version"`
	JobID         string    `json:"job_id"`
	SessionID     string    `json:"session_id"`
	CaptureID     string    `json:"capture_id"`
	SegmentID     string    `json:"segment_id"`
	Status        string    `json:"status"`
	SegmentNumber int       `json:"segment_number"`
	CompletedAt   time.Time `json:"completed_at"`
}

func (s *Store) discoverTranscript(sc SessionContext, state studyruntime.SegmentTranscriptionState, preserved map[string]Record, assignIdentity bool) (Record, []Issue) {
	relative := moduleRelative(s.context.ModuleRoot, sc.Root, state.TranscriptJSONRelativePath)
	paths := []string{state.TranscriptJSONRelativePath, state.TranscriptTextRelativePath, state.ProvenanceRelativePath, state.JobMetadataRelativePath}
	for _, p := range paths {
		if !safeRelative(p) {
			return Record{}, []Issue{issue("transcript_artifact_set_incomplete", "error", true, "", relative, "completed transcript artifact set is incomplete")}
		}
	}
	var td transcriptDoc
	var pd provenanceDoc
	var jd jobDoc
	for _, artifactPath := range paths {
		if !regularUnlinkedFile(filepath.Join(sc.Root, filepath.FromSlash(artifactPath))) {
			return Record{}, []Issue{issue("transcript_artifact_set_incomplete", "error", true, "", relative, "completed transcript artifact set is invalid")}
		}
	}
	if readJSON(filepath.Join(sc.Root, filepath.FromSlash(paths[0])), &td) != nil || readJSON(filepath.Join(sc.Root, filepath.FromSlash(paths[2])), &pd) != nil || readJSON(filepath.Join(sc.Root, filepath.FromSlash(paths[3])), &jd) != nil {
		return Record{}, []Issue{issue("transcript_artifact_set_incomplete", "error", true, "", relative, "completed transcript artifact set is invalid")}
	}
	segmentAudioPath := ""
	for _, segment := range sc.Snapshot.Segments {
		if segment.ID == state.SegmentID && segment.Number == state.SegmentNumber {
			segmentAudioPath = segment.AudioPath
			break
		}
	}
	identitiesMatch := td.JobID == state.JobID && pd.Provenance.JobID == state.JobID && jd.JobID == state.JobID &&
		td.SessionID == sc.ID && pd.Provenance.SessionID == sc.ID && jd.SessionID == sc.ID &&
		td.CaptureID != "" && td.CaptureID == pd.Provenance.CaptureID && td.CaptureID == jd.CaptureID &&
		td.SegmentID == state.SegmentID && pd.Provenance.SegmentID == state.SegmentID && jd.SegmentID == state.SegmentID
	pathsMatch := td.ProvenanceRelativePath == state.ProvenanceRelativePath && safeRelative(pd.Provenance.InputRelativePath) &&
		pd.Provenance.InputRelativePath == state.InputRelativePath && pd.Provenance.InputRelativePath == segmentAudioPath
	if td.SchemaVersion != 1 || pd.SchemaVersion != 1 || jd.SchemaVersion != 1 || jd.Status != "completed" || !identitiesMatch || !pathsMatch || pd.Provenance.InputSHA256 == "" {
		return Record{}, []Issue{issue("transcript_without_completed_job", "error", false, "", relative, "transcript evidence does not match completed runtime")}
	}
	sourceAbs := filepath.Join(sc.Root, filepath.FromSlash(pd.Provenance.InputRelativePath))
	if !regularUnlinkedFile(sourceAbs) {
		return Record{}, []Issue{issue("artifact_hash_mismatch", "error", false, "", relative, "transcript source is not a safe regular file")}
	}
	digest, _, err := hashFile(sourceAbs)
	if err != nil || digest != pd.Provenance.InputSHA256 {
		return Record{}, []Issue{issue("artifact_hash_mismatch", "error", false, "", relative, "transcript source hash does not match provenance")}
	}
	previous := preserved[string(TypeTranscript)+"\x00"+relative]
	id := previous.ID
	if id == "" && assignIdentity {
		id, _ = s.generate()
	}
	if id == "" {
		return Record{}, []Issue{issue("completed_job_without_transcript_record", "warning", true, "", relative, "completed runtime job has no transcript index record")}
	}
	created := previous.CreatedAt
	if created.IsZero() {
		created = jd.CompletedAt
	}
	record, err := fileRecord(id, TypeTranscript, Scope{Kind: ScopeSegment, CourseID: s.context.CourseID, ModuleID: s.context.ModuleID, SessionID: sc.ID, SegmentID: state.SegmentID}, fmt.Sprintf("Transcript %03d", state.SegmentNumber), relative, "application/json", "", "transcription", false, created, filepath.Join(sc.Root, filepath.FromSlash(paths[0])))
	if err != nil {
		return Record{}, []Issue{issue("transcript_artifact_set_incomplete", "error", false, id, relative, "transcript metadata is invalid")}
	}
	record.UpdatedAt = s.clock().UTC()
	record.Transcript = &TranscriptReference{TextRelativePath: moduleRelative(s.context.ModuleRoot, sc.Root, paths[1]), ProvenanceRelativePath: moduleRelative(s.context.ModuleRoot, sc.Root, paths[2]), JobMetadataRelativePath: moduleRelative(s.context.ModuleRoot, sc.Root, paths[3]), JobID: state.JobID, SourceAudioRelativePath: moduleRelative(s.context.ModuleRoot, sc.Root, pd.Provenance.InputRelativePath), SourceAudioSHA256: pd.Provenance.InputSHA256, Language: td.Transcript.Language, DurationMillis: td.Transcript.DurationMillis, CompletedAt: jd.CompletedAt}
	if record.Validate() != nil {
		return Record{}, []Issue{issue("transcript_artifact_set_incomplete", "error", false, id, relative, "transcript record is invalid")}
	}
	return record, nil
}

func fileRecord(id ID, t Type, scope Scope, title, relative, media, category, source string, mutable bool, created time.Time, abs string) (Record, error) {
	digest, size, err := hashFile(abs)
	if err != nil {
		return Record{}, err
	}
	return Record{ID: id, Type: t, Scope: scope, Title: title, RelativePath: relative, MediaType: media, Category: category, Source: source, CreatedAt: created, UpdatedAt: created, SizeBytes: size, SHA256: digest, Mutable: mutable}, nil
}
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), n, nil
}
func (s *Store) session(id string) (SessionContext, bool) {
	for _, sc := range s.context.Sessions {
		if sc.ID == id {
			return sc, true
		}
	}
	return SessionContext{}, false
}
func moduleRelative(moduleRoot, sessionRoot, relative string) string {
	value, _ := filepath.Rel(moduleRoot, filepath.Join(sessionRoot, filepath.FromSlash(relative)))
	return filepath.ToSlash(value)
}
func within(parent, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func readJSON(path string, out any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || hasMultipleLinks(info) {
		return ErrInvalid
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func regularUnlinkedFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular() && !hasMultipleLinks(info)
}
func writeExclusiveAtomic(path string, data []byte, mode fs.FileMode) error {
	if _, err := os.Lstat(path); err == nil {
		return ErrConflict
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".studypilot-artifact-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Link(name, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrConflict
		}
		return err
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("%w: note directory sync", ErrPersistenceUncertain)
	}
	return nil
}
func replaceAtomic(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".studypilot-index-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("%w: index directory sync", ErrPersistenceUncertain)
	}
	return nil
}
func copyExclusiveAtomic(source, dest string, mode fs.FileMode) error {
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 || !sourceInfo.Mode().IsRegular() || sourceInfo.Size() > MaxAssetSize {
		return ErrInvalid
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	openedInfo, err := in.Stat()
	if err != nil {
		return err
	}
	latestInfo, err := os.Lstat(source)
	if err != nil || latestInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(openedInfo, latestInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Size() > MaxAssetSize {
		return ErrInvalid
	}
	if err = os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	if _, err = os.Lstat(dest); err == nil {
		return ErrConflict
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".studypilot-asset-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(mode); err == nil {
		var copied int64
		copied, err = io.CopyN(tmp, in, MaxAssetSize+1)
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if copied > MaxAssetSize {
			err = ErrInvalid
		}
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Link(name, dest); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrConflict
		}
		return err
	}
	if err = syncDirectory(filepath.Dir(dest)); err != nil {
		return fmt.Errorf("%w: asset directory sync", ErrPersistenceUncertain)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func safeAssetName(name string) (string, error) {
	if name == "" || name == "." || name == ".." || !utf8Valid(name) {
		return "", ErrInvalid
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	var b strings.Builder
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) || r == '.' {
			b.WriteByte('-')
		}
	}
	clean := strings.Trim(b.String(), "-_")
	if clean == "" {
		clean = "asset"
	}
	ext = strings.ToLower(ext)
	if len(ext) > 16 || strings.ContainsAny(ext, "/\\") {
		ext = ""
	}
	value := clean + ext
	if len(value) > 100 {
		value = value[:100-len(ext)] + ext
	}
	return value, nil
}
func utf8Valid(v string) bool {
	for _, r := range v {
		if r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}
func validCategory(v string) bool {
	return v == "document" || v == "image" || v == "code" || v == "archive" || v == "other"
}
func mediaType(category, name string) string {
	if category == "image" {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".png":
			return "image/png"
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".gif":
			return "image/gif"
		case ".webp":
			return "image/webp"
		case ".svg":
			return "image/svg+xml"
		}
	}
	if category == "code" {
		return "text/plain"
	}
	return "application/octet-stream"
}

var assetIDPrefix = regexp.MustCompile(`^(study-artifact-[0-9a-f]{32})-`)

func parseAssetID(name string) ID {
	match := assetIDPrefix.FindStringSubmatch(name)
	if len(match) != 2 {
		return ""
	}
	id, _ := NewID(match[1])
	return id
}
func assetTitle(name string) string {
	if m := assetIDPrefix.FindStringIndex(name); m != nil {
		return strings.TrimSpace(name[m[1]:])
	}
	return name
}
func parseNoteMetadata(path string, scope Scope) (ID, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 || lines[0] != "---" {
		return "", false
	}
	values := map[string]string{}
	closed := false
	for _, line := range lines[1:] {
		if line == "---" {
			closed = true
			break
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) == 2 {
			if _, duplicate := values[parts[0]]; duplicate {
				return "", false
			}
			values[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	if !closed {
		return "", false
	}
	id, err := NewID(values["artifact_id"])
	if err != nil {
		return "", false
	}
	valid := values["schema_version"] == "1" && values["scope"] == string(scope.Kind) && values["course_id"] == scope.CourseID && values["module_id"] == scope.ModuleID && values["session_id"] == scope.SessionID
	return id, valid
}
func issue(code IssueCode, severity string, recoverable bool, id ID, path, message string) Issue {
	return Issue{Code: code, Severity: severity, Recoverable: recoverable, ArtifactID: id, RelativePath: path, Message: message}
}
func sortIssues(issues []Issue) {
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		if issues[i].RelativePath != issues[j].RelativePath {
			return issues[i].RelativePath < issues[j].RelativePath
		}
		return issues[i].ArtifactID < issues[j].ArtifactID
	})
}
