// Package embedded ships the default-deny policy bundle compiled into the
// regatta binary. Spec §3.5 — single-tenant deployments rely on this
// bundle for zero-config authz; multi-tenant deployments override via the
// substrate policy_revision event (T2's policies primitive).
package embedded

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"sort"

	"github.com/trilamsr/regatta/internal/authz"
)

// FS holds the verbatim default-deny Rego sources from spec §3.5.
// Scope is locked to regatta/v1/default — tenant bundles MUST live in
// substrate events, never on disk in the binary (R5 mitigation).
//
//go:embed regatta/v1/default
var FS embed.FS

// DefaultBundleSHA256 is the hex-encoded SHA-256 over a canonical-JSON map
// {path -> file_contents} of every file in FS. R11 mitigation — silent
// drift between binary versions changes authz outcomes invisibly; the
// stability test asserts this constant equals a hard-coded hex string.
var DefaultBundleSHA256 = computeBundleSHA256()

// Loader implements authz.BundleLoader against the embedded default-deny
// bundle (spec §3.5). disk.Loader uses one as its Fallback so a missing /
// empty policy_dir still serves valid Rego at boot. The loader is
// stateless — a single value is safe for concurrent use.
type Loader struct{}

// NewLoader returns an authz.BundleLoader that reads from the embedded
// default-deny FS. Returned interface (not pointer) lets callers pass it
// to disk.Loader.Fallback without re-typing.
func NewLoader() authz.BundleLoader { return Loader{} }

// Tenants returns the single-tenant slot per spec §3.5.
func (Loader) Tenants(_ context.Context) ([]string, error) {
	return []string{authz.DefaultTenant}, nil
}

// ActiveBundle returns the embed.FS bundle for the default tenant; any
// other tenant ⇒ ErrPolicyMissing (slim variant locks to one slot).
func (Loader) ActiveBundle(_ context.Context, tenant string) (string, map[string]string, error) {
	if tenant != authz.DefaultTenant {
		return "", nil, authz.ErrPolicyMissing
	}
	files, err := Files()
	if err != nil {
		return "", nil, err
	}
	return DefaultBundleSHA256, files, nil
}

// Files returns the default-deny bundle as a path->body map. Disk-loader
// fallback path (slim hot-reload: policy_dir empty/missing) and any future
// in-process consumer reuse this instead of re-walking FS.
func Files() (map[string]string, error) {
	out := map[string]string{}
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, rerr := fs.ReadFile(FS, path)
		if rerr != nil {
			return rerr
		}
		out[path] = string(b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// computeBundleSHA256 hashes a sorted [path, content] slice; the slice
// shape (not a map) guarantees byte-stable order across Go versions.
func computeBundleSHA256() string {
	files := map[string]string{}
	if err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, rerr := fs.ReadFile(FS, path)
		if rerr != nil {
			return rerr
		}
		files[path] = string(b)
		return nil
	}); err != nil {
		// embed.FS reads cannot fail at runtime; a failure here means the
		// binary itself is corrupt. Surface as a sentinel so the stability
		// test catches it instead of silently rendering an empty hash.
		return "embed-walk-error:" + err.Error()
	}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([][2]string, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, [2]string{k, files[k]})
	}
	canon, err := json.Marshal(ordered)
	if err != nil {
		return "canon-error:" + err.Error()
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:])
}
