package web

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriteInternalError_DoesNotLeakErrToClient asserts the response body is the fixed generic message, not err.Error (R-MEGA-3 LIVE-5).
func TestWriteInternalError_DoesNotLeakErrToClient(t *testing.T) {
	rec := httptest.NewRecorder()
	secret := "secret-db-dsn=postgres://user:pw@host/db" //nolint:gosec // synthetic fixture for leak-detection test, not a real credential
	writeInternalError(rec, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), "op.test", errors.New(secret))
	if rec.Code != 500 {
		t.Fatalf("status=%d want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Errorf("body leaked err.Error: %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), internalErrorMessage) {
		t.Errorf("body missing generic message; got %q", rec.Body.String())
	}
}

// TestWriteInternalError_LogsFullError asserts the logger receives the full err detail (R-MEGA-3 LIVE-5).
func TestWriteInternalError_LogsFullError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := httptest.NewRecorder()
	secret := "row-not-found id=42" //nolint:gosec // not a credential; gosec mistakes the substring "id="
	writeInternalError(rec, logger, "op.lookup", errors.New(secret))
	if !strings.Contains(buf.String(), secret) {
		t.Errorf("log missing err detail; got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "op.lookup") {
		t.Errorf("log missing op; got %q", buf.String())
	}
}

// TestWriteInternalError_NilLoggerUsesDefault asserts passing nil logger does not panic and falls back to slog.Default.
func TestWriteInternalError_NilLoggerUsesDefault(t *testing.T) {
	rec := httptest.NewRecorder()
	writeInternalError(rec, nil, "op.x", errors.New("boom"))
	if rec.Code != 500 {
		t.Fatalf("status=%d want 500", rec.Code)
	}
}
