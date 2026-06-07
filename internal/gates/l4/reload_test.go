package l4

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// snapshotActiveSlot pins the package-level active slot for the test's
// lifetime; without this, -count>1 leaks the test-fixture slot into
// later tests (#913).
func snapshotActiveSlot(t *testing.T) {
	t.Helper()
	prev := active.Load()
	t.Cleanup(func() { active.Store(prev) })
}

// TestReloader_SIGHUP_SwapsPromptAndSHA: disk-edit + SIGHUP swaps body + SHA.
func TestReloader_SIGHUP_SwapsPromptAndSHA(t *testing.T) {
	snapshotActiveSlot(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "adversarial.tmpl")
	if err := os.WriteFile(path, []byte("v1 {{ .PRSHA }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rl := &Reloader{
		Path:            path,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		DisableFsnotify: true,
	}
	reloaded := make(chan Result, 4)
	rl.OnReload = func(r Result) { reloaded <- r }
	started := make(chan struct{})
	rl.OnStart = func() { close(started) }

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = rl.Run(ctx) }()
	<-started

	// Drain boot reload.
	select {
	case <-reloaded:
	case <-time.After(2 * time.Second):
		t.Fatal("no boot reload")
	}

	out1, sha1, err := RenderPrompt(Input{PRSHA: "abc"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out1, "v1 abc") {
		t.Fatalf("expected v1 render, got %q", out1)
	}

	if err := os.WriteFile(path, []byte("v2 {{ .PRSHA }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := os.FindProcess(os.Getpid())
	if err := p.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-reloaded:
		if r.Err != nil {
			t.Fatalf("reload err: %v", r.Err)
		}
		if r.Trigger != TriggerSighup {
			t.Fatalf("trigger=%q want sighup", r.Trigger)
		}
		if r.Skipped {
			t.Fatal("reload was skipped; expected SHA change")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SIGHUP did not trigger reload")
	}

	out2, sha2, err := RenderPrompt(Input{PRSHA: "abc"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "v2 abc") {
		t.Fatalf("expected v2 render after reload, got %q", out2)
	}
	if sha1 == sha2 {
		t.Fatalf("expected PromptSHA to change after reload, got %q twice", sha1)
	}
}

// TestReloader_FsnotifyDebounce: 5 writes in window -> exactly 1 reload.
func TestReloader_FsnotifyDebounce(t *testing.T) {
	snapshotActiveSlot(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "adversarial.tmpl")
	if err := os.WriteFile(path, []byte("vA {{ .PRSHA }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rl := &Reloader{
		Path:          path,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Debounce:      100 * time.Millisecond,
		DisableSighup: true,
	}
	reloaded := make(chan Result, 16)
	rl.OnReload = func(r Result) { reloaded <- r }
	started := make(chan struct{})
	rl.OnStart = func() { close(started) }

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = rl.Run(ctx) }()
	<-started

	select {
	case <-reloaded:
	case <-time.After(2 * time.Second):
		t.Fatal("no boot reload")
	}

	for i := 0; i < 5; i++ {
		if err := os.WriteFile(path, []byte("vB"+string(rune('0'+i))+" {{ .PRSHA }}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var got int
	deadline := time.After(1 * time.Second)
loop:
	for {
		select {
		case r := <-reloaded:
			if r.Trigger == TriggerFsnotify {
				got++
			}
		case <-deadline:
			break loop
		}
	}
	if got != 1 {
		t.Fatalf("debounce: got %d fsnotify reloads, want exactly 1", got)
	}
}

// TestReloader_SHAShortCircuit_NoSwap: identical bytes -> Skipped.
func TestReloader_SHAShortCircuit_NoSwap(t *testing.T) {
	snapshotActiveSlot(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "adversarial.tmpl")
	body := []byte("same {{ .PRSHA }}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	rl := &Reloader{
		Path:            path,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		DisableFsnotify: true,
	}
	reloaded := make(chan Result, 4)
	rl.OnReload = func(r Result) { reloaded <- r }
	started := make(chan struct{})
	rl.OnStart = func() { close(started) }

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = rl.Run(ctx) }()
	<-started
	<-reloaded // boot

	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := os.FindProcess(os.Getpid())
	if err := p.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-reloaded:
		if !r.Skipped {
			t.Fatalf("expected Skipped=true for identical bytes, got %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIGHUP did not produce a reload result")
	}
}

// TestReloader_BadTemplate_RetainsLastGood: parse-fail keeps prior slot.
func TestReloader_BadTemplate_RetainsLastGood(t *testing.T) {
	snapshotActiveSlot(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "adversarial.tmpl")
	if err := os.WriteFile(path, []byte("good {{ .PRSHA }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rl := &Reloader{
		Path:            path,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		DisableFsnotify: true,
	}
	reloaded := make(chan Result, 4)
	rl.OnReload = func(r Result) { reloaded <- r }
	started := make(chan struct{})
	rl.OnStart = func() { close(started) }

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = rl.Run(ctx) }()
	<-started
	<-reloaded
	_, goodSHA, _ := RenderPrompt(Input{PRSHA: "x"}, 10)

	if err := os.WriteFile(path, []byte("bad {{ .Unclosed "), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := os.FindProcess(os.Getpid())
	if err := p.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-reloaded:
		if r.Err == nil {
			t.Fatal("expected reload Err for malformed template")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reload result")
	}

	_, afterSHA, err := RenderPrompt(Input{PRSHA: "x"}, 10)
	if err != nil {
		t.Fatalf("RenderPrompt errored after bad reload: %v", err)
	}
	if afterSHA != goodSHA {
		t.Fatalf("expected retain-last-good SHA %q, got %q", goodSHA, afterSHA)
	}
}
