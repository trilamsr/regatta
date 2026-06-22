// digest subcommand: `regatta digest --date YYYY-MM-DD [--root .]`
// generates `docs/digests/YYYY-MM-DD.md` per spec §6.2 (machine YAML
// front-matter + 7 markdown sections). Renderer logic lives in
// internal/obs/digest; this file is the CLI seam — flag parse, source
// resolution from env, file write.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/obs/digest"
)

// noopSource yields an empty Snapshot with backendUp=false so the
// renderer prints the unreachable banner — used when no Prom endpoint
// env var is set so the cron still produces a digest the operator can
// audit (UX > velocity).
type noopSource struct{}

func (noopSource) Fetch(string) (digest.Snapshot, bool, error) {
	return digest.Snapshot{}, false, nil
}

// runDigest dispatches the `digest` verb. Exit codes: 0 success, 1
// runtime error (filesystem / render), 2 usage error (bad flags / date).
func runDigest(args []string) int {
	fs := flag.NewFlagSet("digest", flag.ContinueOnError)
	date := fs.String("date", "", "UTC day to render, formatted YYYY-MM-DD (required)")
	root := fs.String("root", ".", "repo root; digest writes to <root>/docs/digests/")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta digest --date YYYY-MM-DD [--root .]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *date == "" {
		fs.Usage()
		return 2
	}
	parsed, err := time.Parse("2006-01-02", *date)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regatta digest: --date %q: want YYYY-MM-DD: %v\n", *date, err)
		return 2
	}
	if parsed.After(time.Now().UTC().Truncate(24 * time.Hour)) {
		fmt.Fprintf(os.Stderr, "regatta digest: --date %q is in the future; no events can exist yet\n", *date)
		return 2
	}

	source, err := digest.NewPromSourceFromEnv(os.Getenv)
	var src digest.Source
	switch {
	case err == nil:
		src = source
	case digest.IsNoEndpoint(err):
		src = noopSource{}
	default:
		fmt.Fprintln(os.Stderr, "regatta digest:", err)
		return 1
	}

	out, err := digest.Render(digest.Options{Date: *date, Source: src})
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta digest:", err)
		// Render uses a fixed "want YYYY-MM-DD" phrase for malformed
		// dates so cron-style invocation maps that class to exit 2 and
		// fails loud; everything else is a runtime fault (exit 1).
		if strings.Contains(err.Error(), "want YYYY-MM-DD") {
			return 2
		}
		return 1
	}

	dir := filepath.Join(*root, "docs", "digests")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		fmt.Fprintln(os.Stderr, "regatta digest:", err)
		return 1
	}
	path := filepath.Join(dir, *date+".md")
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "regatta digest:", err)
		return 1
	}
	_, _ = fmt.Fprintln(os.Stdout, path)
	return 0
}
