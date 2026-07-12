package backend

import (
	"context"
	"os"
	"regexp"
	"sort"
	"strconv"
)

// RecoveryIssueKind classifies a problem found while inspecting a session's
// segment files. Kinds are stable, UI-neutral strings.
type RecoveryIssueKind string

const (
	IssueStaleOwnership      RecoveryIssueKind = "stale_ownership"
	IssueActiveOwnership     RecoveryIssueKind = "active_ownership"
	IssuePartialAudio        RecoveryIssueKind = "partial_audio"
	IssueMissingManifest     RecoveryIssueKind = "missing_manifest"
	IssueMissingAudio        RecoveryIssueKind = "missing_audio"
	IssueConflictingFiles    RecoveryIssueKind = "conflicting_files"
	IssueMalformedManifest   RecoveryIssueKind = "malformed_manifest"
	IssueUnsupportedManifest RecoveryIssueKind = "unsupported_manifest"
)

// FinalizedRecord is a healthy finalized segment discovered on disk. Paths are
// relative to the session's Segments directory.
type FinalizedRecord struct {
	Number         int
	SegmentID      string
	AudioFile      string
	ManifestFile   string
	DurationMillis int64
	BytesWritten   int64
}

// PartialRecord is an unfinalized segment whose partial audio remains for
// inspection.
type PartialRecord struct {
	Number    int
	AudioFile string
}

// RecoveryIssue names one problem. Number is 0 when it cannot be attributed to
// a segment number. OwnerAlive is meaningful only for ownership issues.
type RecoveryIssue struct {
	Kind       RecoveryIssueKind
	Number     int
	File       string
	Message    string
	OwnerAlive bool
}

// Inspection is the read-only view of a session's recording state. It never
// mutates or deletes anything, never follows symlinks, and exposes no file
// contents or absolute paths.
type Inspection struct {
	Finalized []FinalizedRecord
	Partial   []PartialRecord
	Issues    []RecoveryIssue
	HasOwner  bool
}

var (
	audioPattern    = regexp.MustCompile(`^(\d{3})-audio\.wav$`)
	partialPattern  = regexp.MustCompile(`^(\d{3})-audio\.wav\.partial$`)
	manifestPattern = regexp.MustCompile(`^(\d{3})-segment\.json$`)
)

func (r *recorder) Inspect(ctx context.Context, sessionRoot string) (Inspection, error) {
	if err := checkContext(ctx, OpNameInspect); err != nil {
		return Inspection{}, err
	}
	authority, err := NewSegmentAuthority(r.paths, sessionRoot)
	if err != nil {
		return Inspection{}, err
	}
	segmentsDir := authority.SegmentsDir()
	if info, statErr := os.Lstat(segmentsDir); statErr != nil {
		if os.IsNotExist(statErr) {
			return Inspection{}, nil
		}
		return Inspection{}, newError(ErrorInternal, OpNameInspect, "inspect Segments directory", statErr)
	} else if info.Mode()&os.ModeSymlink != 0 {
		return Inspection{}, newError(ErrorUnsafePath, OpNameInspect, "Segments directory is a symlink", nil)
	}
	entries, err := os.ReadDir(segmentsDir)
	if err != nil {
		return Inspection{}, newError(ErrorInternal, OpNameInspect, "read Segments directory", err)
	}

	finalAudio := map[int]string{}
	partialAudio := map[int]string{}
	manifests := map[int]string{}
	for _, entry := range entries {
		// Never follow symlinks; skip them rather than reading through.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		switch {
		case audioPattern.MatchString(name):
			finalAudio[numberOf(audioPattern, name)] = name
		case partialPattern.MatchString(name):
			partialAudio[numberOf(partialPattern, name)] = name
		case manifestPattern.MatchString(name):
			manifests[numberOf(manifestPattern, name)] = name
		}
	}

	inspection := Inspection{}
	numbers := sortedUnion(finalAudio, partialAudio, manifests)
	for _, number := range numbers {
		audio, hasAudio := finalAudio[number]
		partial, hasPartial := partialAudio[number]
		manifest, hasManifest := manifests[number]

		if hasAudio && hasPartial {
			inspection.Issues = append(inspection.Issues, RecoveryIssue{Kind: IssueConflictingFiles, Number: number, File: partial, Message: "finalized and partial audio both exist"})
		}
		switch {
		case hasAudio && hasManifest:
			record, issue := r.classifyFinalized(segmentsDir, number, audio, manifest)
			if issue != nil {
				inspection.Issues = append(inspection.Issues, *issue)
			} else {
				inspection.Finalized = append(inspection.Finalized, record)
			}
		case hasAudio && !hasManifest:
			inspection.Issues = append(inspection.Issues, RecoveryIssue{Kind: IssueMissingManifest, Number: number, File: audio, Message: "finalized audio has no manifest"})
		case hasManifest && !hasAudio && !hasPartial:
			inspection.Issues = append(inspection.Issues, RecoveryIssue{Kind: IssueMissingAudio, Number: number, File: manifest, Message: "manifest has no audio file"})
		}
		if hasPartial && !hasAudio {
			inspection.Partial = append(inspection.Partial, PartialRecord{Number: number, AudioFile: partial})
			inspection.Issues = append(inspection.Issues, RecoveryIssue{Kind: IssuePartialAudio, Number: number, File: partial, Message: "partial audio was not finalized"})
		}
	}

	if ownership, present, err := readOwnership(segmentsDir); err != nil {
		inspection.HasOwner = present
		inspection.Issues = append(inspection.Issues, RecoveryIssue{Kind: IssueStaleOwnership, Message: "ownership lock could not be read safely"})
	} else if present {
		inspection.HasOwner = true
		alive := r.liveness(ownership.ProcessID, ownership.Host)
		kind := IssueStaleOwnership
		message := "ownership lock exists but its process is not alive"
		if alive {
			kind = IssueActiveOwnership
			message = "ownership lock exists and its process appears alive"
		}
		inspection.Issues = append(inspection.Issues, RecoveryIssue{Kind: kind, Number: ownership.Number, Message: message, OwnerAlive: alive})
	}

	sort.SliceStable(inspection.Issues, func(i, j int) bool {
		if inspection.Issues[i].Number != inspection.Issues[j].Number {
			return inspection.Issues[i].Number < inspection.Issues[j].Number
		}
		return inspection.Issues[i].Kind < inspection.Issues[j].Kind
	})
	return inspection, nil
}

// classifyFinalized validates a finalized manifest, distinguishing malformed
// from unsupported. It reads the manifest but exposes no contents in results.
func (r *recorder) classifyFinalized(segmentsDir string, number int, audio, manifest string) (FinalizedRecord, *RecoveryIssue) {
	manifestPath := segmentsDir + string(os.PathSeparator) + manifest
	parsed, err := readManifest(manifestPath)
	if err != nil {
		return FinalizedRecord{}, &RecoveryIssue{Kind: IssueMalformedManifest, Number: number, File: manifest, Message: "manifest is malformed"}
	}
	if parsed.SchemaVersion != ManifestSchemaVersion {
		return FinalizedRecord{}, &RecoveryIssue{Kind: IssueUnsupportedManifest, Number: number, File: manifest, Message: "manifest schema version is unsupported"}
	}
	if err := parsed.Validate(); err != nil || parsed.Partial || parsed.AudioFile != audio {
		return FinalizedRecord{}, &RecoveryIssue{Kind: IssueMalformedManifest, Number: number, File: manifest, Message: "manifest is invalid for a finalized segment"}
	}
	return FinalizedRecord{
		Number:         number,
		SegmentID:      parsed.SegmentID,
		AudioFile:      audio,
		ManifestFile:   manifest,
		DurationMillis: parsed.DurationMillis,
		BytesWritten:   parsed.BytesWritten,
	}, nil
}

func numberOf(pattern *regexp.Regexp, name string) int {
	match := pattern.FindStringSubmatch(name)
	if len(match) != 2 {
		return 0
	}
	value, _ := strconv.Atoi(match[1])
	return value
}

func sortedUnion(maps ...map[int]string) []int {
	seen := map[int]bool{}
	for _, m := range maps {
		for number := range m {
			seen[number] = true
		}
	}
	numbers := make([]int, 0, len(seen))
	for number := range seen {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	return numbers
}
