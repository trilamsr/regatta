package selfimprove

import (
	"context"
	"testing"
	"time"
)

// TestDetector_AcceptsFuncSource pins post-Wave-E Detector.Source as a func value (EventSource interface deleted).
func TestDetector_AcceptsFuncSource(t *testing.T) {
	t.Helper()
	d := &Detector{
		Source: func(_ context.Context, _ time.Time, _ []string) ([]Event, error) {
			return nil, nil
		},
		Clock: time.Now,
	}
	if d.Source == nil {
		t.Fatal("Source nil")
	}
}
