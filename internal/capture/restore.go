package capture

import (
	"context"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

type RestoreRequest struct {
	CaptureID           CaptureID
	SessionID, DeviceID string
	Status              studyruntime.CaptureStatus
	Active              *Segment
	Finalized           []Segment
}
type RestorableService interface {
	Restore(context.Context, RestoreRequest) error
}
