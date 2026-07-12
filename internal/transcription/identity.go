package transcription

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

const jobIDPrefix = "transcription-job-"

type JobID string
type JobIDGenerator func() (JobID, error)

func NewJobID(suffix string) (JobID, error) { return ParseJobID(jobIDPrefix + suffix) }

func ParseJobID(value string) (JobID, error) {
	if !strings.HasPrefix(value, jobIDPrefix) {
		return "", newError(ErrorInvalidInput, "parse_job_id", false, "invalid transcription job ID", nil, "")
	}
	suffix := strings.TrimPrefix(value, jobIDPrefix)
	if len(suffix) != 32 {
		return "", newError(ErrorInvalidInput, "parse_job_id", false, "invalid transcription job ID", nil, "")
	}
	if _, err := hex.DecodeString(suffix); err != nil || strings.ToLower(suffix) != suffix {
		return "", newError(ErrorInvalidInput, "parse_job_id", false, "invalid transcription job ID", nil, "")
	}
	return JobID(value), nil
}

func (id JobID) String() string  { return string(id) }
func (id JobID) Validate() error { _, err := ParseJobID(string(id)); return err }

func DefaultJobIDGenerator() (JobID, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", newError(ErrorInternal, "generate_job_id", false, "generate transcription job ID", err, "")
	}
	id, err := NewJobID(hex.EncodeToString(raw[:]))
	if err != nil {
		return "", fmt.Errorf("validate generated transcription job ID: %w", err)
	}
	return id, nil
}
