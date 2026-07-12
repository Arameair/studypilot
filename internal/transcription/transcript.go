package transcription

type TranscriptSegment struct {
	Index                  int
	StartMillis, EndMillis int64
	Text                   string
	Confidence             *float64
}
type Word struct {
	Index                  int
	StartMillis, EndMillis int64
	Text                   string
	Confidence             *float64
}
type Transcript struct {
	Text, Language string
	DurationMillis int64
	Segments       []TranscriptSegment
	Words          []Word
	Partial        bool
}
type PartialUpdate struct {
	JobID                         JobID
	Transcript                    Transcript
	Sequence, StableThroughMillis int64
}

func cloneConfidence(v *float64) *float64 {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func (s TranscriptSegment) Clone() TranscriptSegment {
	s.Confidence = cloneConfidence(s.Confidence)
	return s
}
func (w Word) Clone() Word { w.Confidence = cloneConfidence(w.Confidence); return w }
func (t Transcript) Clone() Transcript {
	out := t
	out.Segments = make([]TranscriptSegment, len(t.Segments))
	for i := range t.Segments {
		out.Segments[i] = t.Segments[i].Clone()
	}
	out.Words = make([]Word, len(t.Words))
	for i := range t.Words {
		out.Words[i] = t.Words[i].Clone()
	}
	return out
}
func validConfidence(v *float64) bool { return v == nil || (*v >= 0 && *v <= 1) }
func (t Transcript) Validate() error {
	if t.DurationMillis < 0 {
		return newError(ErrorInvalidInput, "validate_transcript", false, "invalid transcript duration", nil, "")
	}
	lastEnd := int64(0)
	for i, s := range t.Segments {
		if s.Index != i || s.StartMillis < 0 || s.EndMillis < s.StartMillis || s.StartMillis < lastEnd || !validConfidence(s.Confidence) {
			return newError(ErrorMalformedOutput, "validate_transcript", false, "invalid transcript segment metadata", nil, "")
		}
		lastEnd = s.EndMillis
	}
	wordEnd := int64(0)
	for i, w := range t.Words {
		if w.Index != i || w.StartMillis < 0 || w.EndMillis < w.StartMillis || w.StartMillis < wordEnd || !validConfidence(w.Confidence) {
			return newError(ErrorMalformedOutput, "validate_transcript", false, "invalid transcript word metadata", nil, "")
		}
		wordEnd = w.EndMillis
	}
	if lastEnd > t.DurationMillis || wordEnd > t.DurationMillis {
		return newError(ErrorMalformedOutput, "validate_transcript", false, "transcript timing exceeds duration", nil, "")
	}
	return nil
}
func (p PartialUpdate) Clone() PartialUpdate { p.Transcript = p.Transcript.Clone(); return p }
func (p PartialUpdate) Validate() error {
	if err := p.JobID.Validate(); err != nil {
		return err
	}
	if p.Sequence < 1 || p.StableThroughMillis < 0 {
		return newError(ErrorInvalidInput, "validate_partial", false, "invalid partial update sequence", nil, p.JobID)
	}
	if !p.Transcript.Partial {
		return newError(ErrorInvalidInput, "validate_partial", false, "partial update requires a partial transcript", nil, p.JobID)
	}
	if err := p.Transcript.Validate(); err != nil {
		return err
	}
	if p.StableThroughMillis > p.Transcript.DurationMillis {
		return newError(ErrorInvalidInput, "validate_partial", false, "stable-through exceeds transcript duration", nil, p.JobID)
	}
	return nil
}
