package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// assetsFS lives in server.go (T4) under `//go:embed all:templates all:static`.
// T5's tests reach static/* via that package-level handle — re-declaring the
// embed here would shadow T4's wider directive.

// htmxPinnedSHA256 is the SHA-256 hex digest of internal/web/static/htmx.min.js,
// pinned at vendoring time. Any mutation of the on-disk bytes — accidental
// edit or supply-chain tamper — fails this test. The same hash is documented
// in internal/web/static/VENDORED.md; both must move together.
const htmxPinnedSHA256 = "e209dda5c8235479f3166defc7750e1dbcd5a5c1808b7792fc2e6733768fb447"

func TestVendoredAssets_TailwindFileExists(t *testing.T) {
	f, err := assetsFS.Open("static/tailwind.min.css")
	if err != nil {
		t.Fatalf("open tailwind.min.css: %v", err)
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read tailwind.min.css: %v", err)
	}
	// Floor is the Tailwind v3 preflight reset baseline (~3 KiB minified).
	// Ceiling is 50 KiB — an untouched CDN bundle is ≥150 KiB, so anything
	// above 50 KiB means purge did not run. The realistic upper bound grows
	// as T4's templates land; revisit the cap if a future utility-heavy
	// template legitimately blows past 50 KiB.
	const minBytes, maxBytes = 2 * 1024, 50 * 1024
	if n := len(buf); n < minBytes || n > maxBytes {
		t.Fatalf("tailwind.min.css size %d outside sanity range [%d, %d]", n, minBytes, maxBytes)
	}
}

func TestVendoredAssets_HtmxFileExistsAndSHAMatchesPin(t *testing.T) {
	f, err := assetsFS.Open("static/htmx.min.js")
	if err != nil {
		t.Fatalf("open htmx.min.js: %v", err)
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read htmx.min.js: %v", err)
	}
	sum := sha256.Sum256(buf)
	got := hex.EncodeToString(sum[:])
	if got != htmxPinnedSHA256 {
		t.Fatalf("htmx.min.js sha256 mismatch:\n  on-disk: %s\n  pinned : %s", got, htmxPinnedSHA256)
	}

	vendored, err := os.ReadFile(filepath.Join("static", "VENDORED.md"))
	if err != nil {
		t.Fatalf("read VENDORED.md: %v", err)
	}
	if !bytes.Contains(vendored, []byte(htmxPinnedSHA256)) {
		t.Fatalf("VENDORED.md does not cite pinned htmx sha256 %q", htmxPinnedSHA256)
	}
}

func TestVendoredAssets_NoNPMDepInGoMod(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	// Catches accidental node/npm bridge tooling lingering in go.mod.
	bad := regexp.MustCompile(`(?i)\b(node|npm|browserify|webpack|rollup|esbuild)\b`)
	if loc := bad.FindIndex(body); loc != nil {
		line := body[clampLo(loc[0]-40):clampHi(loc[1]+40, len(body))]
		t.Fatalf("go.mod contains npm-bridge token near %q", string(line))
	}
}

func TestMakeCheck_VerifyVendoredAssetsTargetWired(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("make is POSIX-only; CI runs make check on linux/macOS")
	}
	root := repoRoot(t)
	cmd := exec.Command("make", "-n", "check")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n check: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "verify-vendored-assets") {
		t.Fatalf("make -n check did not reference verify-vendored-assets:\n%s", out)
	}
}

func TestVendoredAssets_TailwindContentPurgedNotJIT(t *testing.T) {
	f, err := assetsFS.Open("static/tailwind.min.css")
	if err != nil {
		t.Fatalf("open tailwind.min.css: %v", err)
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read tailwind.min.css: %v", err)
	}
	// Build-time purge replaces every @tailwind directive with concrete
	// rules. An unpurged CDN bundle (≥150 KiB) still carries them.
	if bytes.Contains(buf, []byte("@tailwind")) {
		t.Fatalf("tailwind.min.css contains @tailwind directive — purge did not run")
	}
	if n := len(buf); n > 50*1024 {
		t.Fatalf("tailwind.min.css %d bytes > 50 KiB cap — purge failed or CDN bundle committed", n)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func clampLo(x int) int {
	if x < 0 {
		return 0
	}
	return x
}

func clampHi(x, upper int) int {
	if x > upper {
		return upper
	}
	return x
}
