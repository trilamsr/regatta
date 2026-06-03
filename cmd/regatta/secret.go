// secret subcommand tree. Per
// docs/engineer/specs/2026-06-02-phase-autonomy-w6-secret-credential-fetch.md
// §6 the surface is `regatta secret set|get|status`. `get` refuses to
// print raw values without `--unsafe`; every CLI invocation emits a
// structured audit_event{action=secret_get|secret_set, source=…,
// unsafe=…} that omits the value substring.
//
// `list` is intentionally dropped (spec §6 deletion-default tiebreaker
// — `status` subsumes it).
package main

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/secrets"
)

const (
	subcmdSecret    = "secret"
	secretSubGet    = "get"
	secretSubSet    = "set"
	secretSubStatus = "status"
	secretSubList   = "list"
)

// platformSetter is implemented by the darwin keychain and Linux pass
// adapters. The narrow read-only Fetcher interface stays public; Set/
// Delete are a CLI-only concern bound to the platform adapter.
type platformSetter interface {
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
}

// secretDeps bundles I/O streams + time so tests inject deterministic
// inputs. Audit event time is taken from now() to keep prod usage
// terse.
type secretDeps struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
}

// runSecret dispatches `regatta secret <subcmd>`.
func runSecret(args []string) int {
	return runSecretWithDeps(secretDeps{
		Args:   args,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Now:    time.Now,
	})
}

// runSecretWithDeps is the testable entry point.
func runSecretWithDeps(d secretDeps) int {
	if len(d.Args) == 0 {
		_, _ = fmt.Fprintln(d.Stderr, "usage: regatta secret set|get|status [args]")
		return 2
	}
	switch d.Args[0] {
	case secretSubSet:
		return runSecretSet(d)
	case secretSubGet:
		return runSecretGet(d)
	case secretSubStatus:
		return runSecretStatus(d)
	case secretSubList:
		_, _ = fmt.Fprintln(d.Stderr, "regatta secret: 'list' was removed — did you mean `regatta secret status`?")
		return 2
	default:
		_, _ = fmt.Fprintf(d.Stderr, "regatta secret: unknown subcommand %q\n", d.Args[0])
		return 2
	}
}

// runSecretSet writes a value to the first writable adapter. Prompts
// via /dev/tty equivalent; in tests the value comes from stdin.
func runSecretSet(d secretDeps) int {
	fs := flag.NewFlagSet("secret set", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	if err := fs.Parse(d.Args[1:]); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		_, _ = fmt.Fprintln(d.Stderr, "usage: regatta secret set <key>")
		return 2
	}
	key := fs.Arg(0)
	if err := secrets.ValidateKey(key); err != nil {
		_, _ = fmt.Fprintf(d.Stderr, "regatta secret set: %v\n", err)
		return 2
	}
	setter, src, err := writableAdapter()
	if err != nil {
		_, _ = fmt.Fprintf(d.Stderr, "regatta secret set: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(d.Stderr, "Enter value for %s (echo on; pipe in non-interactive shells): ", key)
	reader := bufio.NewReader(d.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		_, _ = fmt.Fprintf(d.Stderr, "regatta secret set: read: %v\n", err)
		return 1
	}
	value := strings.TrimRight(line, "\r\n")
	if value == "" {
		_, _ = fmt.Fprintln(d.Stderr, "regatta secret set: empty value rejected")
		return 1
	}
	ctx := context.Background()
	if err := setter.Set(ctx, key, []byte(value)); err != nil {
		_, _ = fmt.Fprintf(d.Stderr, "regatta secret set: %v\n", err)
		return 1
	}
	emitAuditSecret(d, auditSecretEvent{
		Action:    "secret_set",
		Key:       key,
		Source:    src,
		Unsafe:    false,
		Timestamp: d.Now().UTC(),
	})
	_, _ = fmt.Fprintf(d.Stdout, "regatta secret set: wrote %s to %s\n", key, src)
	return 0
}

// runSecretGet refuses unless `--unsafe`. Without the flag, prints a
// presence summary (no value). The audit event is emitted before
// writing the value to stdout so an aborted stdout pipe still leaves
// the audit row in the chain.
func runSecretGet(d secretDeps) int {
	fs := flag.NewFlagSet("secret get", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	unsafe := fs.Bool("unsafe", false, "print raw value to stdout (auditable)")
	// Re-order args so the key (positional) can appear before or after
	// the flag — operator muscle-memory `secret get <key> --unsafe`
	// must work identically to `secret get --unsafe <key>`.
	if err := fs.Parse(reorderFlagsLast(d.Args[1:])); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		_, _ = fmt.Fprintln(d.Stderr, "usage: regatta secret get <key> [--unsafe]")
		return 2
	}
	key := fs.Arg(0)
	if err := secrets.ValidateKey(key); err != nil {
		_, _ = fmt.Fprintf(d.Stderr, "regatta secret get: %v\n", err)
		return 2
	}
	ctx := context.Background()
	fetcher := secrets.Default(ctx)
	v, err := fetcher.Get(ctx, key)
	source := fetcher.Name()
	present := true
	if errors.Is(err, secrets.ErrNotFound) {
		present = false
		source = "missing" //nolint:goconst
	} else if err != nil {
		_, _ = fmt.Fprintf(d.Stderr, "regatta secret get: %v\n", err)
		return 1
	}
	emitAuditSecret(d, auditSecretEvent{
		Action:    "secret_get",
		Key:       key,
		Source:    source,
		Unsafe:    *unsafe,
		Timestamp: d.Now().UTC(),
	})
	if !present {
		_, _ = fmt.Fprintf(d.Stdout, "%s: missing (chain=%s)\n", key, fetcher.Name())
		return 1
	}
	if *unsafe {
		// Raw value to stdout. Operator opted in.
		_, _ = d.Stdout.Write(v.Bytes())
		_, _ = d.Stdout.Write([]byte("\n"))
		return 0
	}
	_, _ = fmt.Fprintf(d.Stdout, "%s: present source=%s bytes=%d\n", key, source, v.Len())
	return 0
}

// runSecretStatus walks every canonical key and prints (key, source,
// present) rows where source is the adapter that actually resolved
// the key (per spec §9 example: "regatta.audit_hmac_key: env" vs
// "regatta.gh_token: keychain"). Operators debugging rotation drift
// need per-key source, not the chain name (#653). Never prints values.
func runSecretStatus(d secretDeps) int {
	ctx := context.Background()
	fetcher := secrets.Default(ctx)
	_, _ = fmt.Fprintln(d.Stdout, "key                          source     present")
	for _, key := range secrets.CanonicalKeys {
		_, source, err := secrets.GetWithSource(ctx, fetcher, key)
		present := "yes"
		if errors.Is(err, secrets.ErrNotFound) {
			source = "missing" //nolint:goconst
			present = "no"
		} else if err != nil {
			source = "error"
			present = "no"
		}
		_, _ = fmt.Fprintf(d.Stdout, "%-29s %-10s %s\n", key, source, present)
	}
	_, _ = fmt.Fprintf(d.Stdout, "chain: %s\n", fetcher.Name())
	return 0
}

// auditSecretEvent is the on-disk shape of the `audit_event{action=
// secret_*}` row. Value NEVER appears here — assertion enforced by
// TestCLI_SecretGet_EmitsAuditEventWithoutValue.
type auditSecretEvent struct {
	Action    string    `json:"action"`
	Key       string    `json:"key"`
	Source    string    `json:"source"`
	Unsafe    bool      `json:"unsafe"`
	User      string    `json:"user,omitempty"`
	TTY       bool      `json:"tty"`
	Timestamp time.Time `json:"timestamp"`
	Signature string    `json:"signature,omitempty"`
}

// emitAuditSecret signs (HMAC-SHA256 over canonical JSON) and writes
// the event to stderr — the operator pipes stderr into the audit chain
// the same way other regatta CLI commands do. Bootstrap chicken-egg
// (audit key missing): unsigned event still emitted so a fresh box
// can run `regatta secret set regatta.audit_hmac_key` and have an
// audit row of the very write that closes the loop.
func emitAuditSecret(d secretDeps, ev auditSecretEvent) {
	ev.User = fmt.Sprintf("uid=%d", os.Getuid())
	ev.TTY = isTerminal(d.Stdout)
	// Sign with audit HMAC key if available. Bootstrap miss → unsigned.
	fetcher := secrets.Default(context.Background())
	if kv, err := fetcher.Get(context.Background(), secrets.KeyAuditHMACKey); err == nil {
		body, _ := json.Marshal(struct {
			Action    string    `json:"action"`
			Key       string    `json:"key"`
			Source    string    `json:"source"`
			Unsafe    bool      `json:"unsafe"`
			User      string    `json:"user"`
			TTY       bool      `json:"tty"`
			Timestamp time.Time `json:"timestamp"`
		}{ev.Action, ev.Key, ev.Source, ev.Unsafe, ev.User, ev.TTY, ev.Timestamp})
		mac := hmac.New(sha256.New, kv.Bytes())
		mac.Write(body)
		ev.Signature = hex.EncodeToString(mac.Sum(nil))
	}
	body, _ := json.Marshal(ev)
	_, _ = fmt.Fprintf(d.Stderr, "audit_event %s\n", body)
}

// reorderFlagsLast moves `--flag` tokens to the front so flag.Parse
// (which stops at the first non-flag) does not skip them when an
// operator types positional-before-flag.
func reorderFlagsLast(args []string) []string {
	var flags, pos []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			pos = append(pos, a)
		}
	}
	return append(flags, pos...)
}

// isTerminal probes whether stdout is a TTY — used in audit events
// to flag interactive vs piped invocations.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
