package disk_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/authz"
	"github.com/trilamsr/regatta/internal/authz/policies/disk"
	"github.com/trilamsr/regatta/internal/authz/policies/embedded"
)

// Disk loader reads `.rego` modules from policy_dir into the BundleLoader shape.
func TestLoader_ReadsDiskFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "regatta", "v1", "default")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "package regatta.v1.approval.view\n\ndefault decision := {\"allow\": true, \"reason\": \"disk-loaded\"}\n"
	if err := os.WriteFile(filepath.Join(root, "approval.rego"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	l := &disk.Loader{Dir: dir, Fallback: embeddedFallback()}
	sha, files, err := l.ActiveBundle(context.Background(), authz.DefaultTenant)
	if err != nil {
		t.Fatalf("ActiveBundle: %v", err)
	}
	if sha == "" {
		t.Fatalf("empty sha")
	}
	if got, want := files["regatta/v1/default/approval.rego"], body; got != want {
		t.Fatalf("file body mismatch: %q vs %q", got, want)
	}
}

// Empty / missing policy_dir delegates to fallback (embed.FS default-deny).
func TestLoader_EmptyDirFallsBackToEmbed(t *testing.T) {
	t.Parallel()
	l := &disk.Loader{Dir: t.TempDir(), Fallback: embeddedFallback()}
	sha, files, err := l.ActiveBundle(context.Background(), authz.DefaultTenant)
	if err != nil {
		t.Fatalf("ActiveBundle: %v", err)
	}
	if sha != embedded.DefaultBundleSHA256 {
		t.Fatalf("sha = %q want fallback %q", sha, embedded.DefaultBundleSHA256)
	}
	if len(files) == 0 {
		t.Fatalf("expected fallback files")
	}
}

// Editor backup files (.swp/~/.tmp/.bak/hidden) MUST NOT enter the bundle hash.
func TestLoader_IgnoresEditorBackupFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "regatta", "v1", "default")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	keep := "package regatta.v1.run.view\n\ndefault decision := {\"allow\": false, \"reason\": \"d\"}\n"
	if err := os.WriteFile(filepath.Join(root, "run.rego"), []byte(keep), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, drop := range []string{"approval.rego.swp", "approval.rego~", ".approval.rego.tmp", "approval.rego.bak"} {
		if err := os.WriteFile(filepath.Join(root, drop), []byte("garbage"), 0o644); err != nil {
			t.Fatalf("write %s: %v", drop, err)
		}
	}

	l := &disk.Loader{Dir: dir, Fallback: embeddedFallback()}
	_, files, err := l.ActiveBundle(context.Background(), authz.DefaultTenant)
	if err != nil {
		t.Fatalf("ActiveBundle: %v", err)
	}
	if len(files) != 1 {
		for k := range files {
			t.Logf("file: %s", k)
		}
		t.Fatalf("expected 1 .rego file, got %d", len(files))
	}
}

// SHA is byte-stable across processes — required for the Reloader short-circuit.
func TestLoader_SHADeterministic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "regatta", "v1", "default")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "approval.rego"), []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	l := &disk.Loader{Dir: dir, Fallback: embeddedFallback()}
	sha1, _, err := l.ActiveBundle(context.Background(), authz.DefaultTenant)
	if err != nil {
		t.Fatalf("ActiveBundle 1: %v", err)
	}
	sha2, _, err := l.ActiveBundle(context.Background(), authz.DefaultTenant)
	if err != nil {
		t.Fatalf("ActiveBundle 2: %v", err)
	}
	if sha1 != sha2 {
		t.Fatalf("non-deterministic sha: %q vs %q", sha1, sha2)
	}
}

// Unknown tenant returns an ErrPolicyMissing-wrapped error in slim variant.
func TestLoader_UnknownTenant(t *testing.T) {
	t.Parallel()
	l := &disk.Loader{Dir: t.TempDir(), Fallback: embeddedFallback()}
	_, _, err := l.ActiveBundle(context.Background(), "tenant-x")
	if err == nil {
		t.Fatalf("expected error for unknown tenant")
	}
	if !strings.Contains(err.Error(), "tenant") && !errors.Is(err, authz.ErrPolicyMissing) {
		t.Fatalf("expected tenant error, got %v", err)
	}
}

// embeddedFallback returns a BundleLoader that serves the T4 embed.FS bundle.
func embeddedFallback() authz.BundleLoader { return embedded.NewLoader() }
