package transcription

import (
	"errors"
	"math"
	"time"
)

type RetryPolicy struct {
	MaxAttempts            int
	InitialDelay, MaxDelay time.Duration
	Multiplier             float64
}

func (p RetryPolicy) Validate() error {
	if p.MaxAttempts < 1 || p.InitialDelay < 0 || p.MaxDelay < 0 || p.MaxDelay < p.InitialDelay || math.IsNaN(p.Multiplier) || math.IsInf(p.Multiplier, 0) || p.Multiplier < 1 {
		return newError(ErrorInvalidInput, "validate_retry_policy", false, "invalid retry policy", nil, "")
	}
	return nil
}

type RetryDecision struct {
	Allowed     bool
	NextAttempt int
	RetryAt     time.Time
	Delay       time.Duration
	Reason      string
	Exhausted   bool
}

func IsRetryable(err error) bool {
	var target *Error
	if !errors.As(err, &target) {
		return false
	}
	switch target.Code {
	case ErrorUnavailable:
		return target.Recoverable
	case ErrorInputNotFinalized, ErrorTimeout, ErrorPartialOutput:
		return true
	case ErrorInternal:
		return target.Recoverable
	default:
		return false
	}
}
func NextRetry(entry QueueEntry, policy RetryPolicy, now time.Time) (RetryDecision, error) {
	if err := policy.Validate(); err != nil {
		return RetryDecision{}, err
	}
	if now.IsZero() {
		return RetryDecision{}, newError(ErrorInvalidInput, "next_retry", false, "retry decision requires current time", nil, entry.Job.ID)
	}
	limit := policy.MaxAttempts
	if entry.MaxAttempts < limit {
		limit = entry.MaxAttempts
	}
	next := entry.Attempt + 1
	if entry.Job.Status == JobCompleted {
		return RetryDecision{Reason: "completed jobs cannot retry"}, nil
	}
	if entry.Job.Status == JobCancelled || entry.QueueStatus == QueueCancelled {
		return RetryDecision{Reason: "cancelled jobs cannot retry"}, nil
	}
	if entry.Job.LastError == nil || entry.Job.LastError.Code == ErrorUncertain {
		return RetryDecision{Reason: "inspection required before retry"}, nil
	}
	if !IsRetryable(entry.Job.LastError) {
		return RetryDecision{Reason: "failure is not retryable"}, nil
	}
	if next > limit {
		return RetryDecision{NextAttempt: next, Reason: "retry attempts exhausted", Exhausted: true}, nil
	}
	delay := boundedBackoff(policy, entry.Attempt)
	return RetryDecision{Allowed: true, NextAttempt: next, RetryAt: now.Add(delay), Delay: delay, Reason: "retry permitted"}, nil
}
func boundedBackoff(policy RetryPolicy, attempt int) time.Duration {
	delay := float64(policy.InitialDelay)
	for i := 1; i < attempt; i++ {
		if delay >= float64(policy.MaxDelay)/policy.Multiplier {
			return policy.MaxDelay
		}
		delay *= policy.Multiplier
		if math.IsInf(delay, 0) || delay > float64(math.MaxInt64) {
			return policy.MaxDelay
		}
	}
	if delay > float64(policy.MaxDelay) {
		return policy.MaxDelay
	}
	return time.Duration(delay)
}
