// Package studyartifact owns StudyPilot's private transcript, note, and asset
// inventory. It does not create transcripts or interpret note/asset contents.
package studyartifact

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

const (
	SchemaVersion = 1
	MaxAssetSize  = int64(64 << 20)
	MaxNoteBytes  = 256 << 10
)

var (
	ErrInvalid              = errors.New("invalid study artifact")
	ErrConflict             = errors.New("study artifact conflict")
	ErrNotFound             = errors.New("study artifact not found")
	ErrRevisionConflict     = errors.New("study artifact revision conflict")
	ErrPersistenceUncertain = errors.New("study artifact persistence is uncertain")
)

type ID string

var idPattern = regexp.MustCompile(`^study-artifact-[0-9a-f]{32}$`)

func NewID(value string) (ID, error) {
	id := ID(value)
	if !idPattern.MatchString(value) {
		return "", fmt.Errorf("%w: artifact identity", ErrInvalid)
	}
	return id, nil
}
func (id ID) String() string  { return string(id) }
func (id ID) Validate() error { _, err := NewID(string(id)); return err }
func DefaultIDGenerator() (ID, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return NewID("study-artifact-" + hex.EncodeToString(value[:]))
}

type IDGenerator func() (ID, error)

type Type string

const (
	TypeTranscript Type = "transcript"
	TypeNote       Type = "note"
	TypeAsset      Type = "asset"
)

func (t Type) Valid() bool { return t == TypeTranscript || t == TypeNote || t == TypeAsset }

type ScopeKind string

const (
	ScopeModule  ScopeKind = "module"
	ScopeSession ScopeKind = "session"
	ScopeSegment ScopeKind = "segment"
)

type Scope struct {
	Kind      ScopeKind `json:"kind"`
	CourseID  string    `json:"course_id"`
	ModuleID  string    `json:"module_id"`
	SessionID string    `json:"session_id,omitempty"`
	SegmentID string    `json:"segment_id,omitempty"`
}

func (s Scope) Validate() error {
	if !safeText(s.CourseID, 128) || !safeText(s.ModuleID, 128) || (s.SessionID != "" && !safeText(s.SessionID, 128)) || (s.SegmentID != "" && !safeText(s.SegmentID, 128)) {
		return fmt.Errorf("%w: incomplete scope", ErrInvalid)
	}
	switch s.Kind {
	case ScopeModule:
		if s.SessionID != "" || s.SegmentID != "" {
			return fmt.Errorf("%w: contradictory module scope", ErrInvalid)
		}
	case ScopeSession:
		if s.SessionID == "" || s.SegmentID != "" {
			return fmt.Errorf("%w: contradictory session scope", ErrInvalid)
		}
	case ScopeSegment:
		if s.SessionID == "" || s.SegmentID == "" {
			return fmt.Errorf("%w: incomplete segment scope", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown scope", ErrInvalid)
	}
	return nil
}

type Record struct {
	ID                           ID                   `json:"id"`
	Type                         Type                 `json:"type"`
	Scope                        Scope                `json:"scope"`
	Title                        string               `json:"title"`
	RelativePath                 string               `json:"relative_path"`
	OriginalFilename             string               `json:"original_filename,omitempty"`
	MediaType                    string               `json:"media_type"`
	Category                     string               `json:"category,omitempty"`
	Source                       string               `json:"source"`
	CreatedAt                    time.Time            `json:"created_at"`
	UpdatedAt                    time.Time            `json:"updated_at"`
	SizeBytes                    int64                `json:"size_bytes"`
	SHA256                       string               `json:"sha256"`
	Mutable                      bool                 `json:"mutable"`
	Transcript                   *TranscriptReference `json:"transcript,omitempty"`
	RelatedTranscriptArtifactIDs []ID                 `json:"related_transcript_artifact_ids,omitempty"`
}

// NoteDocument is the path-free authoritative note payload returned to the
// application boundary. Revision is the enclosing artifact-index revision used
// for optimistic concurrency.
type NoteDocument struct {
	Artifact Record
	Content  string
	Revision uint64
}

type TranscriptReference struct {
	TextRelativePath        string    `json:"text_relative_path"`
	ProvenanceRelativePath  string    `json:"provenance_relative_path"`
	JobMetadataRelativePath string    `json:"job_metadata_relative_path"`
	JobID                   string    `json:"job_id"`
	SourceAudioRelativePath string    `json:"source_audio_relative_path"`
	SourceAudioSHA256       string    `json:"source_audio_sha256"`
	Language                string    `json:"language"`
	DurationMillis          int64     `json:"duration_millis"`
	CompletedAt             time.Time `json:"completed_at"`
}

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var mediaPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.+-]{0,63}/[a-z0-9][a-z0-9.+-]{0,63}$`)

func (r Record) Validate() error {
	if r.ID.Validate() != nil || !r.Type.Valid() || r.Scope.Validate() != nil || !safeText(r.Title, 160) || !safeRelative(r.RelativePath) || !mediaPattern.MatchString(r.MediaType) || r.SizeBytes < 0 || !hashPattern.MatchString(r.SHA256) || r.CreatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		return fmt.Errorf("%w: record", ErrInvalid)
	}
	if r.Source != "transcription" && r.Source != "user" && r.Source != "import" && r.Source != "generated_template" {
		return fmt.Errorf("%w: source", ErrInvalid)
	}
	switch r.Type {
	case TypeTranscript:
		if r.Scope.Kind != ScopeSegment || r.Mutable || r.Source != "transcription" || r.Transcript == nil {
			return fmt.Errorf("%w: transcript record", ErrInvalid)
		}
		tr := r.Transcript
		for _, p := range []string{tr.TextRelativePath, tr.ProvenanceRelativePath, tr.JobMetadataRelativePath, tr.SourceAudioRelativePath} {
			if !safeRelative(p) {
				return fmt.Errorf("%w: transcript path", ErrInvalid)
			}
		}
		if tr.JobID == "" || !hashPattern.MatchString(tr.SourceAudioSHA256) || tr.Language == "" || tr.DurationMillis < 0 || tr.CompletedAt.IsZero() {
			return fmt.Errorf("%w: transcript metadata", ErrInvalid)
		}
		if r.Category != "" || r.OriginalFilename != "" || len(r.RelatedTranscriptArtifactIDs) != 0 {
			return fmt.Errorf("%w: transcript classification", ErrInvalid)
		}
	case TypeNote:
		if r.Transcript != nil || r.Category != "" || r.OriginalFilename != "" || !r.Mutable || (r.Source != "user" && r.Source != "generated_template") || r.Scope.Kind == ScopeSegment {
			return fmt.Errorf("%w: note record", ErrInvalid)
		}
	case TypeAsset:
		safeName, err := safeAssetName(r.OriginalFilename)
		if r.Transcript != nil || safeName != r.OriginalFilename || err != nil || !validCategory(r.Category) || r.Mutable || r.Source != "import" || len(r.RelatedTranscriptArtifactIDs) != 0 || r.Scope.Kind == ScopeSegment {
			return fmt.Errorf("%w: asset record", ErrInvalid)
		}
	}
	for _, id := range r.RelatedTranscriptArtifactIDs {
		if id.Validate() != nil {
			return fmt.Errorf("%w: related identity", ErrInvalid)
		}
	}
	return nil
}
func (r Record) Clone() Record {
	out := r
	if r.Transcript != nil {
		x := *r.Transcript
		out.Transcript = &x
	}
	out.RelatedTranscriptArtifactIDs = append([]ID(nil), r.RelatedTranscriptArtifactIDs...)
	return out
}

type Index struct {
	SchemaVersion int       `json:"schema_version"`
	Revision      uint64    `json:"revision"`
	Artifacts     []Record  `json:"artifacts"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func EmptyIndex() Index { return Index{SchemaVersion: SchemaVersion, Artifacts: []Record{}} }
func (i Index) Clone() Index {
	out := i
	out.Artifacts = make([]Record, len(i.Artifacts))
	for n := range i.Artifacts {
		out.Artifacts[n] = i.Artifacts[n].Clone()
	}
	return out
}
func (i Index) Validate() error {
	if i.SchemaVersion != SchemaVersion || (i.Revision > 0 && i.UpdatedAt.IsZero()) {
		return fmt.Errorf("%w: index schema", ErrInvalid)
	}
	ids, paths := map[ID]bool{}, map[string]bool{}
	for _, r := range i.Artifacts {
		if r.Validate() != nil || ids[r.ID] || paths[r.RelativePath] {
			return fmt.Errorf("%w: duplicate or invalid index record", ErrInvalid)
		}
		ids[r.ID] = true
		paths[r.RelativePath] = true
	}
	return nil
}
func SortRecords(records []Record) {
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		av := []string{a.Scope.CourseID, a.Scope.ModuleID, a.Scope.SessionID, a.Scope.SegmentID, string(a.Type), a.RelativePath, a.ID.String()}
		bv := []string{b.Scope.CourseID, b.Scope.ModuleID, b.Scope.SessionID, b.Scope.SegmentID, string(b.Type), b.RelativePath, b.ID.String()}
		for n := range av {
			if av[n] != bv[n] {
				return av[n] < bv[n]
			}
		}
		return false
	})
}

type SessionContext struct {
	ID, Root string
	Snapshot studyruntime.Snapshot
}
type Context struct {
	CourseID, ModuleID, ModuleRoot string
	Sessions                       []SessionContext
}

type IssueCode string
type Issue struct {
	Code         IssueCode `json:"code"`
	Severity     string    `json:"severity"`
	Recoverable  bool      `json:"recoverable"`
	ArtifactID   ID        `json:"artifact_id,omitempty"`
	RelativePath string    `json:"relative_path,omitempty"`
	Message      string    `json:"message"`
}
type Inspection struct {
	Revision  uint64
	Artifacts []Record
	Issues    []Issue
}

func safeRelative(value string) bool {
	if value == "" || strings.Contains(value, `\`) || path.IsAbs(value) {
		return false
	}
	clean := path.Clean(value)
	return clean == value && clean != ".." && !strings.HasPrefix(clean, "../")
}
func safeText(value string, max int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= max && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}
