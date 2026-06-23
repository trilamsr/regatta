package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthcheckSubcommand_Exit0OnOK asserts exit=0 when /healthz returns 200 (C0).
func TestHealthcheckSubcommand_Exit0OnOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rc := runHealthcheck([]string{"--url", srv.URL})
	if rc != 0 {
		t.Fatalf("rc=%d want 0", rc)
	}
}

// TestHealthcheckSubcommand_Exit1OnFail asserts exit=1 on non-200 or transport error (C0).
func TestHealthcheckSubcommand_Exit1OnFail(t *testing.T) {
	cases := []struct {
		name  string
		probe func(ctx context.Context, url string) error
	}{
		{"non200", func(ctx context.Context, url string) error { return fmt.Errorf("status=503") }},
		{"transport", func(ctx context.Context, url string) error { return errors.New("conn refused") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := runHealthcheckWith([]string{"--url", "http://localhost:0/healthz"}, tc.probe)
			if rc != 1 {
				t.Fatalf("rc=%d want 1", rc)
			}
		})
	}
}

// TestHealthcheckSubcommand_BadFlagsExit2 covers usage error path (C0).
func TestHealthcheckSubcommand_BadFlagsExit2(t *testing.T) {
	rc := runHealthcheck([]string{"--nope"})
	if rc != 2 {
		t.Fatalf("rc=%d want 2", rc)
	}
}

// TestHealthcheckSubcommand_5xxRealServerExit1 round-trips an httptest 500 (C0).
func TestHealthcheckSubcommand_5xxRealServerExit1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	rc := runHealthcheck([]string{"--url", srv.URL})
	if rc != 1 {
		t.Fatalf("rc=%d want 1", rc)
	}
}
