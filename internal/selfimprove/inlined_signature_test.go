package selfimprove

import (
	"context"
	"testing"
	"time"
)

// TestLLMScanner_AcceptsFuncClient pins post-Wave-E LLMScanner.Client as a func value (LLMClient interface deleted).
func TestLLMScanner_AcceptsFuncClient(t *testing.T) {
	t.Helper()
	called := false
	s := &LLMScanner{
		Client: func(_ context.Context, _ string, _ int) (string, error) {
			called = true
			return "", nil
		},
		OutputDir: t.TempDir(),
		Clock:     func() time.Time { return time.Unix(0, 0).UTC() },
	}
	if _, err := s.Scan(context.Background(), "digest", "tmpl"); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !called {
		t.Fatal("Client func not invoked")
	}
}

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
