// Package disk implements an authz.BundleLoader that reads Rego modules
// + data.json from a directory tree rooted at <policy_dir>/regatta/v1/.
// Empty / missing / unreadable directory delegates to a Fallback loader
// (typically the T4 embed.FS default-deny bundle). Slim single-tenant
// hot-reload spec: §3.3.1 of docs/engineer/specs/2026-06-02-s3-t1-w8-opa-slim.md.
package disk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/trilamsr/regatta/internal/authz"
)

// Loader reads `.rego` + `data.json` from Dir; falls back to Fallback when
// Dir is empty / missing / contains no matching files. Implements
// authz.BundleLoader (spec §3.3.1). Slim variant: tenant MUST be "default";
// any other value returns ErrPolicyMissing (Phase X opens this up).
type Loader struct {
	// Dir is the policy_dir root. Files under Dir/regatta/v1/default/ are
	// loaded; empty string is identical to a missing directory and routes
	// to Fallback.
	Dir string

	// Fallback supplies the default-deny embed.FS bundle when Dir yields
	// nothing usable. Nil ⇒ the empty-disk case bubbles ErrPolicyMissing
	// up to the Authorizer, which would be a deployment bug.
	Fallback authz.BundleLoader
}

// Tenants returns the slim single-tenant slot. Phase X expands this.
func (l *Loader) Tenants(_ context.Context) ([]string, error) {
	return []string{authz.DefaultTenant}, nil
}

// ActiveBundle satisfies authz.BundleLoader. Walks Dir/regatta/v1/<tenant>/,
// returns canonical SHA + path->body map, or delegates to Fallback.
func (l *Loader) ActiveBundle(ctx context.Context, tenant string) (string, map[string]string, error) {
	if tenant != authz.DefaultTenant {
		// Slim variant locks to single tenant. Unknown tenant ⇒ same
		// signal Authorizer maps to ErrTenantUnknown via store lookup
		// miss; keeps the fallback contract surface aligned with T1.
		return "", nil, fmt.Errorf("disk: %w: %q", authz.ErrPolicyMissing, tenant)
	}
	files, err := l.scan(tenant)
	if err != nil || len(files) == 0 {
		if l.Fallback == nil {
			if err != nil {
				return "", nil, err
			}
			return "", nil, authz.ErrPolicyMissing
		}
		return l.Fallback.ActiveBundle(ctx, tenant)
	}
	return bundleSHA(files), files, nil
}

// scan walks Dir/regatta/v1/<tenant>/ and returns the path->body map. An
// empty result is normal (callers fall back); an explicit filesystem error
// (other than "no such file or directory") surfaces so the operator sees
// permission / IO issues instead of silently serving embed.FS.
func (l *Loader) scan(tenant string) (map[string]string, error) {
	if l.Dir == "" {
		return nil, nil
	}
	root := filepath.Join(l.Dir, "regatta", "v1", tenant)
	st, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("disk: stat %s: %w", root, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("disk: %s is not a directory", root)
	}
	out := map[string]string{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			// Skip dot-directories (e.g. .git) wholesale.
			if d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isPolicyFile(d.Name()) {
			return nil
		}
		// Path is the operator-supplied policy_dir descendant filtered
		// through filepath.WalkDir + isPolicyFile; trusted by config.
		body, rerr := os.ReadFile(path) //nolint:gosec // G304: operator-supplied policy_dir
		if rerr != nil {
			return fmt.Errorf("disk: read %s: %w", path, rerr)
		}
		rel, rerr := filepath.Rel(l.Dir, path)
		if rerr != nil {
			return fmt.Errorf("disk: rel %s: %w", path, rerr)
		}
		// Normalize to forward-slash; Rego module names + the canonical
		// SHA both need byte-stable keys across darwin/linux/windows.
		out[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}

// isPolicyFile accepts `.rego` + `data.json`; rejects every editor-backup
// pattern from spec HR2 (the loader is the SHA source, so a stray .swp
// would make every save flap the bundle hash and thrash the reloader).
func isPolicyFile(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.HasSuffix(name, ".swp") ||
		strings.HasSuffix(name, ".swo") ||
		strings.HasSuffix(name, ".swx") ||
		strings.HasSuffix(name, ".tmp") ||
		strings.HasSuffix(name, ".bak") ||
		strings.HasSuffix(name, "~") {
		return false
	}
	return strings.HasSuffix(name, ".rego") || name == "data.json"
}

// bundleSHA canonicalizes files into a JSON-encoded sorted slice and
// returns hex(sha256). Matches the T4 embedded bundle's hash shape so a
// disk copy of the embed.FS bundle yields the same SHA — invariant the
// stability test relies on.
func bundleSHA(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([][2]string, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, [2]string{k, files[k]})
	}
	canon, _ := json.Marshal(ordered)
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:])
}
