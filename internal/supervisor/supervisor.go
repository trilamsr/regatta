// Package supervisor implements `regatta install-service` /
// `uninstall-service` — spec PHASE-AUTONOMY-W3 §3.1 / §3.8.
//
// One Go path renders + bootstraps OS-native init contracts (systemd
// on Linux, launchd on macOS) so the operator runs ONE command and
// never restarts the loop. Re-runs are idempotent; uninstall is a
// 5-row reverse matrix that exits zero on already-clean.
package supervisor

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"text/template"
	"time"
)

// nameRE is the per-target namespace charset (spec §6.4): lowercase
// alphanumeric + hyphen, 1-32 chars, must start with [a-z0-9]. Stops
// path traversal (`..`), launchd Label corruption (`/`), systemd unit
// injection (`\n`).
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidateName enforces the [a-z0-9-]{1,32} whitelist on --name suffixes
// (spec §6.4). Empty is accepted by the supervisor (single-target default)
// but the CLI flag-parse site rejects empty separately if needed.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if !nameRE.MatchString(name) {
		return fmt.Errorf("name %q must match [a-z0-9][a-z0-9-]{0,31} (lowercase alphanumeric + hyphen, 1-32 chars, leading [a-z0-9])", name)
	}
	return nil
}

//go:embed templates/regatta.service.tmpl
var systemdTemplate string

//go:embed templates/regatta.plist.tmpl
var launchdTemplate string

//go:embed templates/regatta.crontab
var cronTemplate string

// Options bundles the install/uninstall flag set so callers (cmd
// wrappers + tests) share one structured input.
type Options struct {
	Mode       Mode // user|system
	DryRun     bool
	Force      bool
	NoCron     bool
	Name       string // namespace suffix for multi-target-repo install side-by-side (#929); empty ⇒ single-target layout (no suffix), preserving back-compat with pre-#929 installs. Validated against [a-z0-9][a-z0-9-]{0,31} (spec §6.4).
	HealthzURL string // operator override for post-bootstrap /healthz polling (#667); empty ⇒ DefaultHealthzURL
	EnvFile    string // operator override for env-file path; empty ⇒ OS+mode default. launchd has no native EnvironmentFile, so on darwin the plist wraps `regatta serve` in `/bin/sh -lc` that sources this file (followup to #826).
	Out        io.Writer
	Err        io.Writer
	Now        func() time.Time
	GOOS       string // override for tests
	Binary     string // override binary path (testing); empty ⇒ os.Executable
	HomeDir    string // override $HOME (testing)
	UID        int    // override geteuid (testing); -1 ⇒ real
}

// DefaultHealthzURL is the loopback /healthz the supervisor polls when
// the operator does not supply --healthz-url — matches the :8080 default
// of cmd/regatta/serve.go::defaultListenerAddr. Non-default --addr deploys
// MUST override via Options.HealthzURL or the post-install poll false-rolls-back (#667).
const DefaultHealthzURL = "http://127.0.0.1:8080/healthz"

// ResolveHealthzURL picks the operator override, falling back to the
// :8080 loopback default.
func ResolveHealthzURL(opts Options) string {
	if opts.HealthzURL != "" {
		return opts.HealthzURL
	}
	return DefaultHealthzURL
}

// Mode discriminates user vs system install.
type Mode int

// Install modes per spec §3.8.
const (
	ModeUser Mode = iota
	ModeSystem
)

// OS literals; broken out so the switch matrix stays grep-able.
const (
	osDarwin = "darwin"
	osLinux  = "linux"
)

// regattaName is the base namespace used in paths + the default systemd
// unit name when --name is empty. Extracted so the single-target default
// is one symbol grep.
const regattaName = "regatta"

// regattaUnit is the default systemd unit filename for single-target
// installs (Name=="").
const regattaUnit = "regatta.service"

// String stringifies Mode for diagnostic + idempotency-status output.
func (m Mode) String() string {
	if m == ModeSystem {
		return "system"
	}
	return "user"
}

// Plan captures the fully-resolved install context — produced before
// any filesystem mutation so --dry-run prints the same struct an actual
// install would consume.
type Plan struct {
	OS           string
	Mode         Mode
	Label        string // launchd
	UnitName     string // systemd
	UnitPath     string
	BinaryPath   string
	WorkingDir   string
	LogDir       string
	ConfigPath   string
	EnvFile      string
	HomePath     string
	PathEnv      string
	User         string
	ReadWritePaths string
}

// rfc3339Stamp is the .bak suffix format for the diff-aware overwrite
// path (spec §3.1 step 4 idempotency branch B).
const rfc3339Stamp = "20060102T150405Z"

// fpf wraps fmt.Fprintf — supervisor output is operator-facing logging,
// errcheck noise on every print clutters the install code path; the
// Fprintf error mode is "the operator's tty closed" which is unrecoverable.
func fpf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
func fpln(w io.Writer, a ...any)               { _, _ = fmt.Fprintln(w, a...) }

// Install runs the full install pipeline; returns named errors that
// the caller maps to exit codes.
func Install(opts Options) error {
	opts = normalize(opts)
	plan, err := buildPlan(opts)
	if err != nil {
		return err
	}
	rendered, err := renderUnit(plan)
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}
	checkEnvFileHygiene(plan, opts.Err)
	if opts.DryRun {
		fpln(opts.Out, "=== plan ===")
		fpf(opts.Out, "os:        %s\n", plan.OS)
		fpf(opts.Out, "mode:      %s\n", plan.Mode)
		fpf(opts.Out, "unit:      %s\n", plan.UnitPath)
		fpf(opts.Out, "binary:    %s\n", plan.BinaryPath)
		fpf(opts.Out, "workdir:   %s\n", plan.WorkingDir)
		fpf(opts.Out, "logs:      %s\n", plan.LogDir)
		fpf(opts.Out, "env-file:  %s\n", plan.EnvFile)
		fpf(opts.Out, "healthz:   %s\n", ResolveHealthzURL(opts))
		fpln(opts.Out, "=== rendered ===")
		fpln(opts.Out, rendered)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0o750); err != nil {
		return fmt.Errorf("mkdir unit dir: %w", err)
	}
	switch idempotency(plan.UnitPath, rendered) {
	case idemIdentical:
		fpf(opts.Out, "already installed (unchanged): %s\n", plan.UnitPath)
		return nil
	case idemDifferent:
		if !opts.Force {
			return fmt.Errorf("existing unit at %s differs from rendered template; re-run with --force to overwrite (a .bak file will be written)", plan.UnitPath)
		}
		backup := plan.UnitPath + ".bak." + opts.Now().UTC().Format(rfc3339Stamp)
		if err := copyFile(plan.UnitPath, backup); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		fpf(opts.Out, "existing unit differs; backed up to %s; reinstalling\n", backup)
	case idemMissing:
		// fresh install — fall through
	}
	if err := writeAtomic(plan.UnitPath, rendered, unitFileMode(plan)); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}
	if err := validateUnit(plan.UnitPath, plan.OS, rendered, opts.Err); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if !opts.NoCron && plan.OS == osLinux {
		if err := installCronBlock(opts); err != nil {
			fpf(opts.Err, "WARN: cron install failed: %v\n", err)
		}
	}
	checkSecurityModule(plan.OS, opts.Out)
	fpf(opts.Out, "installed %s\n", plan.UnitPath)
	return nil
}

// Uninstall reverses Install per spec §3.8 5-row matrix; always exit 0
// on already-clean state (collects best-effort errors instead of failing).
func Uninstall(opts Options) error {
	opts = normalize(opts)
	plan, err := buildPlan(opts)
	if err != nil {
		return err
	}
	var didSomething bool
	var errs []string
	if _, err := os.Stat(plan.UnitPath); err == nil {
		if rmErr := os.Remove(plan.UnitPath); rmErr != nil {
			errs = append(errs, fmt.Sprintf("remove unit: %v", rmErr))
		} else {
			didSomething = true
			fpf(opts.Out, "removed %s\n", plan.UnitPath)
		}
	}
	if !opts.NoCron && plan.OS == osLinux {
		removed, err := removeCronBlock()
		if err != nil {
			errs = append(errs, fmt.Sprintf("strip cron: %v", err))
		} else if removed {
			didSomething = true
			fpln(opts.Out, "stripped cron block")
		}
	}
	if !didSomething {
		fpln(opts.Out, "INFO: nothing to remove (already uninstalled)")
	}
	if len(errs) > 0 {
		return fmt.Errorf("partial uninstall: %s", strings.Join(errs, "; "))
	}
	return nil
}

// normalize applies defaults so call sites + tests share one path.
func normalize(o Options) Options {
	if o.Out == nil {
		o.Out = os.Stdout
	}
	if o.Err == nil {
		o.Err = os.Stderr
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	return o
}

// buildPlan resolves all install parameters; pure function (no FS writes).
func buildPlan(opts Options) (Plan, error) {
	p := Plan{OS: opts.GOOS, Mode: opts.Mode}
	if opts.Name != "" {
		if err := ValidateName(opts.Name); err != nil {
			return p, err
		}
	}
	bin := opts.Binary
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return p, fmt.Errorf("os.Executable: %w", err)
		}
		canon, err := filepath.EvalSymlinks(exe)
		if err == nil {
			exe = canon
		}
		bin = exe
	}
	p.BinaryPath = bin

	home := opts.HomeDir
	if home == "" {
		home = os.Getenv("HOME")
	}
	p.HomePath = home

	// nameSuffix is the optional per-target subdirectory + label suffix (#929);
	// empty ⇒ legacy single-target layout.
	nameSuffix := opts.Name
	switch p.OS {
	case osDarwin:
		p.Label = "com.regatta.serve"
		if nameSuffix != "" {
			p.Label = p.Label + "." + nameSuffix
		}
		if opts.Mode == ModeUser {
			p.UnitPath = filepath.Join(home, "Library", "LaunchAgents", p.Label+".plist")
			p.WorkingDir = filepath.Join(home, ".local", "share", "regatta", nameSuffix)
			p.LogDir = filepath.Join(home, "Library", "Logs", "regatta", nameSuffix)
		} else {
			p.UnitPath = filepath.Join("/Library/LaunchDaemons", p.Label+".plist")
			p.WorkingDir = filepath.Join("/var/lib/regatta", nameSuffix)
			p.LogDir = filepath.Join("/var/log/regatta", nameSuffix)
		}
		p.PathEnv = resolveMacPath(bin)
		p.EnvFile = resolveDarwinEnvFile(opts, home, nameSuffix)
		if err := sanitizeEnvFile(p.EnvFile); err != nil {
			return p, err
		}
	case osLinux:
		p.UnitName = regattaUnit
		if nameSuffix != "" {
			p.UnitName = "regatta-" + nameSuffix + ".service"
		}
		if opts.Mode == ModeUser {
			p.UnitPath = filepath.Join(home, ".config", "systemd", "user", p.UnitName)
			p.WorkingDir = filepath.Join(home, ".local", "share", "regatta", nameSuffix)
			p.LogDir = filepath.Join(home, ".local", "state", "regatta", nameSuffix)
			p.User = currentUser()
		} else {
			p.UnitPath = filepath.Join("/etc/systemd/system", p.UnitName)
			p.WorkingDir = filepath.Join("/var/lib/regatta", nameSuffix)
			p.LogDir = filepath.Join("/var/log/regatta", nameSuffix)
			p.User = regattaName
		}
		p.ConfigPath = filepath.Join("/etc/regatta", nameSuffix, "regatta.yaml")
		p.EnvFile = filepath.Join("/etc/regatta", nameSuffix, "env")
		if opts.EnvFile != "" {
			p.EnvFile = opts.EnvFile
		}
		p.ReadWritePaths = p.WorkingDir + " " + p.LogDir
	default:
		return p, fmt.Errorf("unsupported OS %q (use container runbook docs/operator/container.md)", p.OS)
	}
	return p, nil
}

// resolveDarwinEnvFile picks the operator override, falling back to the
// user-mode / system-mode default. launchd has no native EnvironmentFile
// equivalent — the plist wraps `regatta serve` in `/bin/sh -lc` that
// sources this path so ANTHROPIC_API_KEY + GH_TOKEN land in serve's env
// (followup to #826). nameSuffix carves a per-target subdir when --name
// is set (#929); empty preserves the single-target default.
func resolveDarwinEnvFile(opts Options, home, nameSuffix string) string {
	if opts.EnvFile != "" {
		return opts.EnvFile
	}
	if opts.Mode == ModeSystem {
		return filepath.Join("/etc/regatta", nameSuffix, "env")
	}
	return filepath.Join(home, ".config", "regatta", nameSuffix, "env")
}

// sanitizeEnvFile rejects characters that would break out of the
// double-quoted shell sourcing string in the plist wrapper — `"`, `$`,
// backtick, newline, null, plus `\` for parity with sanitizeShellPath
// (asymmetry was the root cause of #859). Paths with spaces are fine
// because we double-quote.
func sanitizeEnvFile(p string) error {
	if strings.ContainsAny(p, "\"$`\n\x00\\") {
		return fmt.Errorf("env-file path %q contains a shell metacharacter (one of \" $ ` \\ newline null) that would break the launchd sourcing wrapper", p)
	}
	return nil
}

// sanitizeShellPath rejects metacharacters that would escape the
// double-quoted `/bin/sh -lc` wrapper embedding WorkingDir + BinaryPath
// on macOS — `"`, `$`, backtick, newline, null, plus `\` for parity with
// the EnvFile sanitizer. field names the rejected struct field for
// operator-facing diagnostics.
func sanitizeShellPath(field, p string) error {
	if strings.ContainsAny(p, "\"$`\n\x00\\") {
		return fmt.Errorf("%s path %q contains a shell metacharacter (one of \" $ ` \\ newline null) that would break the launchd sourcing wrapper", field, p)
	}
	return nil
}

// resolveMacPath picks the PATH ordering by inspecting the running
// binary's prefix — spec §3.4 (brew detection fix #2; `which` is only
// a tiebreaker, never authoritative).
func resolveMacPath(bin string) string {
	switch {
	case strings.HasPrefix(bin, "/opt/homebrew"):
		return "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
	case strings.HasPrefix(bin, "/usr/local"):
		return "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin"
	}
	return "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin"
}

func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return regattaName
}

func unitFileMode(p Plan) os.FileMode {
	if p.OS == osLinux {
		return 0o644
	}
	return 0o644
}

// renderUnit applies text/template substitution; binary path sanitised
// against template-injection (spec §9 adversarial risk).
func renderUnit(p Plan) (string, error) {
	if err := sanitizePath(p.BinaryPath); err != nil {
		return "", err
	}
	var tmpl string
	switch p.OS {
	case osDarwin:
		if err := sanitizeShellPath("working-dir", p.WorkingDir); err != nil {
			return "", err
		}
		if err := sanitizeShellPath("binary", p.BinaryPath); err != nil {
			return "", err
		}
		tmpl = launchdTemplate
	case osLinux:
		for _, f := range []struct {
			name, val string
		}{
			{"working-dir", p.WorkingDir},
			{"log-dir", p.LogDir},
			{"env-file", p.EnvFile},
			{"config-path", p.ConfigPath},
			{"unit-name", p.UnitName},
			{"read-write-paths", p.ReadWritePaths},
			{"user", p.User},
		} {
			if err := sanitizeSystemdValue(f.name, f.val); err != nil {
				return "", err
			}
		}
		tmpl = systemdTemplate
	default:
		return "", fmt.Errorf("unsupported OS %q", p.OS)
	}
	t, err := template.New("unit").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, p); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// sanitizePath rejects newlines + null bytes — basic template-injection
// guard for operator-supplied paths.
func sanitizePath(p string) error {
	if strings.ContainsAny(p, "\n\x00") {
		return fmt.Errorf("binary path contains illegal character")
	}
	return nil
}

// sanitizeSystemdValue rejects newline + null in any value embedded in
// the systemd unit file. A newline lets an attacker inject arbitrary
// directives (e.g. `\nExecStartPre=/bin/rm -rf /`); a null byte truncates
// the unit on parse. field names the rejected directive for diagnostics.
func sanitizeSystemdValue(field, v string) error {
	if strings.ContainsAny(v, "\n\x00") {
		return fmt.Errorf("%s value %q contains newline or null — systemd unit injection risk", field, v)
	}
	return nil
}

// idempotency outcomes per spec §3.1 step 4.
type idemState int

const (
	idemMissing idemState = iota
	idemIdentical
	idemDifferent
)

func idempotency(path string, rendered string) idemState {
	existing, err := os.ReadFile(path) //nolint:gosec // operator-controlled install path
	if err != nil {
		return idemMissing
	}
	if bytes.Equal(existing, []byte(rendered)) {
		return idemIdentical
	}
	return idemDifferent
}

func writeAtomic(path, content string, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src) //nolint:gosec // .bak path is derived from caller-supplied unit
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600) //nolint:gosec // .bak path derived from install target
}

// validateUnit runs plutil / systemd-analyze when available; missing
// validator ⇒ text-schema fallback + WARN (spec §3.1 step 5 fix #1).
func validateUnit(path, goos, rendered string, errW io.Writer) error {
	switch goos {
	case osDarwin:
		if _, err := exec.LookPath("plutil"); err != nil {
			fpln(errW, "WARN: plutil not on PATH; applied text-schema validation only — recommend installing the validator before next install")
			return textValidatePlist(rendered)
		}
		out, err := exec.Command("plutil", "-lint", path).CombinedOutput() //nolint:gosec // path is the just-written unit file
		if err != nil {
			return fmt.Errorf("plutil -lint: %w: %s", err, out)
		}
	case osLinux:
		if _, err := exec.LookPath("systemd-analyze"); err != nil {
			fpln(errW, "WARN: systemd-analyze not on PATH; applied text-schema validation only — recommend installing the validator before next install")
			return textValidateUnit(rendered)
		}
		out, err := exec.Command("systemd-analyze", "verify", path).CombinedOutput() //nolint:gosec // path is the just-written unit file
		if err != nil {
			// systemd-analyze emits non-zero on stricter checks even when
			// the unit is acceptable for our install; demote to WARN.
			fpf(errW, "WARN: systemd-analyze verify: %v: %s\n", err, out)
		}
	}
	return nil
}

func textValidatePlist(s string) error {
	if !strings.Contains(s, "<?xml") {
		return fmt.Errorf("plist missing XML preamble")
	}
	if !strings.Contains(s, "<plist version=\"1.0\">") {
		return fmt.Errorf("plist missing version=1.0 open tag")
	}
	if strings.Count(s, "<dict>") != strings.Count(s, "</dict>") {
		return fmt.Errorf("plist <dict> tags unbalanced")
	}
	if strings.Count(s, "<array>") != strings.Count(s, "</array>") {
		return fmt.Errorf("plist <array> tags unbalanced")
	}
	if !strings.Contains(s, "</plist>") {
		return fmt.Errorf("plist missing closing tag")
	}
	return nil
}

func textValidateUnit(s string) error {
	for _, req := range []string{"[Service]", "ExecStart=", "[Install]"} {
		if !strings.Contains(s, req) {
			return fmt.Errorf("systemd unit missing required %q", req)
		}
	}
	return nil
}

const cronBegin = "# BEGIN regatta cron"
const cronEnd = "# END regatta cron"

func installCronBlock(opts Options) error {
	current, _ := readCrontab()
	stripped := stripCronBlock(current)
	if !strings.HasSuffix(stripped, "\n") && stripped != "" {
		stripped += "\n"
	}
	return writeCrontab(stripped + cronTemplate)
}

func removeCronBlock() (bool, error) {
	current, err := readCrontab()
	if err != nil {
		// No crontab installed (or crontab tool missing) ⇒ nothing to strip.
		return false, nil //nolint:nilerr // absence == no-op success
	}
	stripped := stripCronBlock(current)
	if stripped == current {
		return false, nil
	}
	return true, writeCrontab(stripped)
}

func stripCronBlock(s string) string {
	begin := strings.Index(s, cronBegin)
	end := strings.Index(s, cronEnd)
	if begin < 0 || end < 0 || end < begin {
		return s
	}
	tail := s[end+len(cronEnd):]
	tail = strings.TrimLeft(tail, "\n")
	head := s[:begin]
	return head + tail
}

func readCrontab() (string, error) {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func writeCrontab(content string) error {
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(content)
	return cmd.Run()
}

// checkEnvFileHygiene WARN-only check on env-file presence + mode. Fail-open
// because the operator may set env vars via shell before launchd loads, or
// rely on a system-wide envd config — fail-closed would block first-time
// installs on a fresh laptop.
func checkEnvFileHygiene(plan Plan, errW io.Writer) {
	if plan.EnvFile == "" {
		return
	}
	info, err := os.Stat(plan.EnvFile)
	if err != nil {
		fpf(errW, "WARN: env-file %s missing — set ANTHROPIC_API_KEY + GH_TOKEN before the loop starts, or pre-create the file at chmod 0600\n", plan.EnvFile)
		return
	}
	if info.Mode().Perm() != 0o600 {
		fpf(errW, "WARN: env-file %s mode %#o — secrets are world/group readable; recommend `chmod 0600 %s`\n", plan.EnvFile, info.Mode().Perm(), plan.EnvFile)
	}
}

// checkSecurityModule emits the SELinux / AppArmor advisory described
// in spec §3.1 step 7 — instructional only, never blocks the install.
func checkSecurityModule(goos string, out io.Writer) {
	if goos != "linux" {
		return
	}
	if _, err := exec.LookPath("sestatus"); err == nil {
		s, err := exec.Command("sestatus").Output()
		if err == nil && bytes.Contains(s, []byte("Current mode:                   enforcing")) {
			fpln(out, `NOTE: SELinux is enforcing. If the unit fails to start with a permission denial,
      generate + load a local policy module:
          sudo ausearch -m AVC -ts recent | audit2allow -M regatta_local
          sudo semodule -i regatta_local.pp
      Then re-run: regatta install-service`)
		}
	}
	if _, err := exec.LookPath("aa-status"); err == nil {
		fpln(out, `NOTE: AppArmor detected. If startup fails with a policy denial, switch the
      regatta profile to complain mode via aa-complain /path/to/profile.`)
	}
}
