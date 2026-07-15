package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Arameair/studypilot/internal/transcription"
)

type RecoveryIssue struct {
	Code, Severity, Message, RelativePath string
	SegmentNumber                         int
	Recoverable                           bool
}
type ArtifactRecord struct {
	JobID         string
	SegmentNumber int
	Artifacts     transcription.TranscriptArtifacts
	InputSHA256   string
}
type RecoveryInspection struct {
	Completed []ArtifactRecord
	Issues    []RecoveryIssue
}

var artifactName = regexp.MustCompile(`^(\d{3})-(transcript\.json|transcript\.txt|transcription-job\.json|provenance\.json)(\.partial)?$`)

func (s *ArtifactStore) Inspect(ctx context.Context, runtimeJobs ...transcription.JobID) (RecoveryInspection, error) {
	if err := contextError(ctx, "artifact_inspect"); err != nil {
		return RecoveryInspection{}, err
	}
	dir := s.authority.TranscriptsDir()
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return RecoveryInspection{}, nil
		}
		return RecoveryInspection{}, newError(ErrorInternal, "artifact_inspect", false, "inspect Transcripts directory", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return RecoveryInspection{}, newError(ErrorUnsafePath, "artifact_inspect", false, "Transcripts directory is unsafe", nil)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return RecoveryInspection{}, newError(ErrorInternal, "artifact_inspect", false, "read Transcripts directory", err)
	}
	byNumber := map[int]map[string]string{}
	inspection := RecoveryInspection{}
	expected := map[string]bool{}
	for _, id := range runtimeJobs {
		expected[id.String()] = true
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		match := artifactName.FindStringSubmatch(entry.Name())
		if len(match) == 0 {
			continue
		}
		number, _ := strconv.Atoi(match[1])
		if byNumber[number] == nil {
			byNumber[number] = map[string]string{}
		}
		kind := match[2]
		if match[3] != "" {
			kind += ".partial"
			inspection.Issues = append(inspection.Issues, RecoveryIssue{"partial_transcript", "warning", "partial transcription evidence exists", filepath.ToSlash(filepath.Join("Transcripts", entry.Name())), number, true})
		}
		byNumber[number][kind] = entry.Name()
	}
	for _, number := range sortedNumbers(byNumber) {
		files := byNumber[number]
		jsonName, jok := files["transcript.json"]
		textName, tok := files["transcript.txt"]
		jobName, mok := files["transcription-job.json"]
		provName, pok := files["provenance.json"]
		add := func(code, message, file string) {
			inspection.Issues = append(inspection.Issues, RecoveryIssue{code, "error", message, relativeArtifact(file), number, true})
		}
		if !jok {
			add("missing_transcript_json", "transcript JSON is missing", jobName)
		}
		if !tok {
			add("missing_transcript_text", "transcript text is missing", jobName)
		}
		if !mok {
			add("missing_job_metadata", "completion metadata is missing", firstName(jsonName, textName, provName))
		}
		if !pok {
			add("missing_provenance", "provenance is missing", jobName)
		}
		if !jok || !tok || !mok || !pok {
			if jok || tok || pok {
				add("uncertain_completion", "artifact set is incomplete", firstName(jsonName, textName, provName))
			}
			continue
		}
		transcriptDoc := TranscriptDocument{}
		if code := decodeArtifact(filepath.Join(dir, jsonName), &transcriptDoc); code != "" {
			add("malformed_transcript", "transcript artifact is malformed", jsonName)
			continue
		}
		if transcriptDoc.SchemaVersion != ArtifactSchemaVersion {
			add("unsupported_schema", "transcript schema is unsupported", jsonName)
			continue
		}
		if err := transcriptDoc.Transcript.Validate(); err != nil {
			add("malformed_transcript", "transcript artifact is invalid", jsonName)
			continue
		}
		textContent, textErr := os.ReadFile(filepath.Join(dir, textName))
		expectedText := transcriptDoc.Transcript.Text
		if !strings.HasSuffix(expectedText, "\n") {
			expectedText += "\n"
		}
		if textErr != nil || !utf8.Valid(textContent) || string(textContent) != expectedText {
			add("malformed_transcript", "text transcript does not match transcript JSON", textName)
			continue
		}
		jobDoc := JobDocument{}
		if code := decodeArtifact(filepath.Join(dir, jobName), &jobDoc); code != "" {
			add("malformed_job_metadata", "job metadata is malformed", jobName)
			continue
		}
		if jobDoc.SchemaVersion != ArtifactSchemaVersion {
			add("unsupported_schema", "job metadata schema is unsupported", jobName)
			continue
		}
		provDoc := ProvenanceDocument{}
		if code := decodeArtifact(filepath.Join(dir, provName), &provDoc); code != "" {
			add("malformed_provenance", "provenance is malformed", provName)
			continue
		}
		if provDoc.SchemaVersion != ArtifactSchemaVersion {
			add("unsupported_schema", "provenance schema is unsupported", provName)
			continue
		}
		if provDoc.Provenance.Validate() != nil {
			add("malformed_provenance", "provenance is invalid", provName)
			continue
		}
		input := filepath.Join(s.authority.SessionRoot(), filepath.FromSlash(provDoc.Provenance.InputRelativePath))
		content, readErr := os.ReadFile(input)
		if readErr != nil {
			add("input_audio_missing", "source audio is unavailable", provName)
			continue
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(content))
		if digest != provDoc.Provenance.InputSHA256 {
			add("input_hash_mismatch", "source audio hash differs from provenance", provName)
			continue
		}
		if transcriptDoc.JobID != jobDoc.JobID || jobDoc.JobID != provDoc.Provenance.JobID.String() || transcriptDoc.SegmentNumber != number || jobDoc.SegmentNumber != number {
			add("artifact_conflict", "artifact identities disagree", jobName)
			continue
		}
		inspection.Completed = append(inspection.Completed, ArtifactRecord{jobDoc.JobID, number, jobDoc.Artifacts, provDoc.Provenance.InputSHA256})
		if len(expected) != 0 && !expected[jobDoc.JobID] {
			add("artifact_without_runtime_job", "completed artifacts have no supplied runtime job", jobName)
		}
	}
	sort.Slice(inspection.Issues, func(i, j int) bool {
		a, b := inspection.Issues[i], inspection.Issues[j]
		if a.SegmentNumber != b.SegmentNumber {
			return a.SegmentNumber < b.SegmentNumber
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.RelativePath < b.RelativePath
	})
	return inspection, nil
}
func decodeArtifact(path string, target any) string {
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || info.Size() > maxWorkerOutput {
		return "malformed_artifact"
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "malformed_artifact"
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return "malformed_artifact"
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return "malformed_artifact"
	}
	return ""
}
func sortedNumbers(values map[int]map[string]string) []int {
	out := make([]int, 0, len(values))
	for n := range values {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}
func relativeArtifact(name string) string {
	if name == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Join("Transcripts", name))
}
func firstName(names ...string) string {
	for _, n := range names {
		if n != "" {
			return n
		}
	}
	return ""
}
