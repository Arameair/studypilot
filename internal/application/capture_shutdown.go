package application

import (
	"context"
	"errors"

	"github.com/Arameair/studypilot/internal/capture"
)

// ShutdownCapture asks process-backed capture services to terminate and reap
// active producers. It does not persist runtime transitions, so an interrupted
// recording remains explicitly recoverable during the next inspection.
func (s *Service) ShutdownCapture(ctx context.Context) error {
	s.sessionMu.Lock()
	services := make([]capture.Service, 0, len(s.captureByRoot))
	for _, service := range s.captureByRoot {
		services = append(services, service)
	}
	s.sessionMu.Unlock()

	var failures []error
	for _, service := range services {
		if shutdown, ok := service.(capture.ShutdownService); ok {
			if err := shutdown.Shutdown(nonNilContext(ctx)); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}
