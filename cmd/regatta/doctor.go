// doctor: single preflight that reports PASS/FAIL/SKIP for every
// environment dependency `regatta serve` will touch at runtime, so
// fresh-repo operators see failures before runtime instead of after
// (#910).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/trilamsr/regatta/internal/secrets"
)

const (
	subcmdDoctor          = "doctor"
	checkNameBranchProtect = "branch-protection"
)

// doctorStatus is the per-check verdict; JSON-stable for CI consumers.
type doctorStatus string

const (
	statusPass doctorStatus = "PASS"
	statusFail doctorStatus = "FAIL"
	statusSkip doctorStatus = "SKIP"
)

type doctorCheckResult struct {
	Name   string       `json:"name"`
	Status doctorStatus `json:"status"`
	Hint   string       `json:"hint,omitempty"`
	Error  string       `json:"error,omitempty"`
}

type doctorEnvelope struct {
	Checks  []doctorCheckResult `json:"checks"`
	Summary doctorSummary       `json:"summary"`
}

type doctorSummary struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
}

// gitStateReport captures the git probe fields the git-state check renders.
type gitStateReport struct {
	Branch          string
	Clean           bool
	RemoteReachable bool
}

// doctorEnv injects every external probe so tests pin behavior without
// touching the operator's machine; the production seam is liveDoctorEnv.
type doctorEnv struct {
	secretLookup      func(ctx context.Context, key string) (string, error)
	lookPath          func(name string) (string, error)
	runCmd            func(ctx context.Context, name string, args ...string) ([]byte, error)
	gitState          func(ctx context.Context) (gitStateReport, error)
	validateConfig    func(path string) error
	verifyRepoConfig  func(ctx context.Context) (passed bool, failedIDs []string, err error)
	supervisorPresent func() (installed bool, detail string, err error)
	getenv            func(key string) string // nil → spawner-auth check returns SKIP
	toolPins          []string
	configPath        string
	withDevTools      bool
}

func runDoctor(args []string) int {
	return runDoctorTo(os.Stdout, os.Stderr, args, liveDoctorEnv())
}

func runDoctorTo(out, errW io.Writer, args []string, env doctorEnv) int {
	fs := flag.NewFlagSet(subcmdDoctor, flag.ContinueOnError)
	fs.SetOutput(errW)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON envelope")
	withDev := fs.Bool("with-dev", false, "include dev/CI binaries (make, osv-scanner, gitleaks) in the binaries check")
	var skip stringList
	fs.Var(&skip, "skip", "comma-separated check names to omit (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *withDev {
		env.withDevTools = true
	}
	skipSet := skip.set()

	ctx := context.Background()
	checks := []struct {
		name string
		run  func() doctorCheckResult
	}{
		{"secrets", func() doctorCheckResult { return checkSecrets(ctx, env) }},
		{"binaries", func() doctorCheckResult { return checkBinaries(env) }},
		{"gh-auth", func() doctorCheckResult { return checkGHAuth(ctx, env) }},
		{"git-state", func() doctorCheckResult { return checkGitState(ctx, env) }},
		{"config", func() doctorCheckResult { return checkConfig(env) }},
		{checkNameBranchProtect, func() doctorCheckResult { return checkBranchProtection(ctx, env) }},
		{"supervisor", func() doctorCheckResult { return checkSupervisor(env) }},
		{"spawner-auth", func() doctorCheckResult { return checkSpawnerAuth(env) }},
	}

	results := make([]doctorCheckResult, 0, len(checks))
	for _, c := range checks {
		if skipSet[c.name] {
			continue
		}
		r := c.run()
		r.Name = c.name
		results = append(results, r)
	}

	envelope := doctorEnvelope{Checks: results}
	for _, r := range results {
		switch r.Status {
		case statusPass:
			envelope.Summary.Pass++
		case statusFail:
			envelope.Summary.Fail++
		case statusSkip:
			envelope.Summary.Skip++
		}
	}

	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(envelope); err != nil {
			_, _ = fmt.Fprintln(errW, "doctor: encode json:", err)
			return 2
		}
	} else {
		for _, r := range results {
			if _, err := fmt.Fprintf(out, "%-20s %s", r.Name, r.Status); err != nil {
				return 2
			}
			if r.Hint != "" {
				_, _ = fmt.Fprintf(out, " — %s", r.Hint)
			}
			_, _ = fmt.Fprintln(out)
			if r.Error != "" {
				_, _ = fmt.Fprintf(out, "    %s\n", r.Error)
			}
		}
		_, _ = fmt.Fprintf(out, "\nsummary: %d PASS, %d FAIL, %d SKIP\n",
			envelope.Summary.Pass, envelope.Summary.Fail, envelope.Summary.Skip)
		_, _ = fmt.Fprintln(out, "(All FAILs block `regatta serve`; SKIPs are advisory.)")
	}

	if envelope.Summary.Fail > 0 {
		return 1
	}
	return 0
}

func checkSecrets(ctx context.Context, env doctorEnv) doctorCheckResult {
	if env.secretLookup == nil {
		return doctorCheckResult{Status: statusSkip, Hint: "no secret probe wired"}
	}
	var missing []string
	var adapter string
	for _, key := range secrets.CanonicalKeys {
		src, err := env.secretLookup(ctx, key)
		if err != nil {
			missing = append(missing, key)
			adapter = src
		}
	}
	if len(missing) == 0 {
		return doctorCheckResult{Status: statusPass}
	}
	hint := fmt.Sprintf("set via `regatta secret set <key>` or %s env vars", envVarHint(missing))
	return doctorCheckResult{
		Status: statusFail,
		Hint:   hint,
		Error:  fmt.Sprintf("missing keys %v in adapter chain %q", missing, adapter),
	}
}

func envVarHint(keys []string) string {
	mapped := make([]string, 0, len(keys))
	for _, k := range keys {
		mapped = append(mapped, secretEnvVar(k))
	}
	return strings.Join(mapped, ", ")
}

func secretEnvVar(canonicalKey string) string {
	switch canonicalKey {
	case secrets.KeyAnthropic:
		return envAnthropicAPIKey
	case secrets.KeyGHToken:
		return "GITHUB_TOKEN"
	case secrets.KeyBriefHMACs:
		return "REGATTA_BRIEF_HMAC_KEYS"
	case secrets.KeyAuditHMACKey:
		return "REGATTA_AUDIT_HMAC_KEY"
	}
	return strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(canonicalKey, "regatta."), ".", "_"))
}

// runtimeBinaries are required for the orchestrator to dispatch
// agents against a real repo: spawner, GitHub CLI for PR + checks,
// git for worktree ops. They MUST be on PATH on every host that
// boots `regatta serve`.
var runtimeBinaries = []string{spawnerNameClaude, "gh", "git"}

const (
	binMake     = "make"
	binGitleaks = "gitleaks"
	binOSV      = "osv-scanner"
)

// devBinaries are only used by the dev-time `make` targets +
// security gates that shell out from CI. The distroless runtime
// image has none of these (intentionally — distroless excludes the
// whole shell + build toolchain), so checking them inside the
// container produces a permanent FAIL the operator can't act on.
// checkBinaries treats them as optional unless --with-dev is set.
var devBinaries = []string{binMake}

func checkBinaries(env doctorEnv) doctorCheckResult {
	if env.lookPath == nil {
		return doctorCheckResult{Status: statusSkip, Hint: "no PATH probe wired"}
	}
	required := append([]string{}, runtimeBinaries...)
	if env.withDevTools {
		required = append(required, devBinaries...)
		required = append(required, env.toolPins...)
	}
	var missing []string
	for _, name := range required {
		if _, err := env.lookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return doctorCheckResult{Status: statusPass}
	}
	return doctorCheckResult{
		Status: statusFail,
		Hint:   "install missing binaries onto PATH",
		Error:  fmt.Sprintf("not on PATH: %v", missing),
	}
}

func checkGHAuth(ctx context.Context, env doctorEnv) doctorCheckResult {
	if env.runCmd == nil {
		return doctorCheckResult{Status: statusSkip}
	}
	if _, err := env.runCmd(ctx, "gh", "auth", "status"); err != nil {
		return doctorCheckResult{
			Status: statusFail,
			Hint:   "run `gh auth login`",
			Error:  err.Error(),
		}
	}
	return doctorCheckResult{Status: statusPass}
}

func checkGitState(ctx context.Context, env doctorEnv) doctorCheckResult {
	if env.gitState == nil {
		return doctorCheckResult{Status: statusSkip}
	}
	rep, err := env.gitState(ctx)
	if err != nil {
		return doctorCheckResult{Status: statusFail, Error: err.Error()}
	}
	var issues []string
	if !rep.Clean {
		issues = append(issues, "working tree dirty")
	}
	if !rep.RemoteReachable {
		issues = append(issues, "origin unreachable")
	}
	if len(issues) > 0 {
		return doctorCheckResult{
			Status: statusFail,
			Hint:   "commit or stash; verify `git remote -v` reachable",
			Error:  strings.Join(issues, "; "),
		}
	}
	return doctorCheckResult{Status: statusPass, Hint: "branch=" + rep.Branch}
}

func checkConfig(env doctorEnv) doctorCheckResult {
	if env.validateConfig == nil {
		return doctorCheckResult{Status: statusSkip}
	}
	path := env.configPath
	if path == "" {
		path = defaultRegattaConfig
	}
	if err := env.validateConfig(path); err != nil {
		return doctorCheckResult{
			Status: statusFail,
			Hint:   "fix regatta.yaml or run `regatta validate-config` for full diagnostics",
			Error:  err.Error(),
		}
	}
	return doctorCheckResult{Status: statusPass}
}

func checkBranchProtection(ctx context.Context, env doctorEnv) doctorCheckResult {
	if env.verifyRepoConfig == nil {
		return doctorCheckResult{Status: statusSkip}
	}
	ok, failed, err := env.verifyRepoConfig(ctx)
	if err != nil {
		return doctorCheckResult{
			Status: statusSkip,
			Hint:   "set GITHUB_TOKEN + repo.owner/name in regatta.yaml",
			Error:  err.Error(),
		}
	}
	if !ok {
		return doctorCheckResult{
			Status: statusFail,
			Hint:   "run `regatta verify-repo-config` for the full audit",
			Error:  fmt.Sprintf("failing checks: %v", failed),
		}
	}
	return doctorCheckResult{Status: statusPass}
}

func checkSupervisor(env doctorEnv) doctorCheckResult {
	if env.supervisorPresent == nil {
		return doctorCheckResult{Status: statusSkip}
	}
	installed, detail, err := env.supervisorPresent()
	if err != nil {
		return doctorCheckResult{Status: statusFail, Error: err.Error()}
	}
	if !installed {
		return doctorCheckResult{Status: statusSkip, Hint: detail}
	}
	return doctorCheckResult{Status: statusPass, Hint: detail}
}

// checkSpawnerAuth reports which credential path the spawner will use at runtime: pay-as-you-go (ANTHROPIC_API_KEY), subscription via mounted ~/.claude, or subscription via long-lived CLAUDE_CODE_OAUTH_TOKEN. Mirrors preflightSpawnerAuth's branching so doctor surfaces the same decision the boundary gate makes (#1166-fix3, OAuth-token follow-up). nil env.getenv → SKIP so unit tests pin behavior without touching process env.
func checkSpawnerAuth(env doctorEnv) doctorCheckResult {
	if env.getenv == nil {
		return doctorCheckResult{Status: statusSkip, Hint: "no env probe wired"}
	}
	stripFlag := strings.ToLower(strings.TrimSpace(env.getenv("REGATTA_SPAWNER_STRIP_API_KEY")))
	payAsYouGo := stripFlag == "0" || stripFlag == "false" || stripFlag == "no" || stripFlag == "off"
	if payAsYouGo {
		if strings.TrimSpace(env.getenv(envAnthropicAPIKey)) == "" {
			return doctorCheckResult{
				Status: statusFail,
				Hint:   "REGATTA_SPAWNER_STRIP_API_KEY=0 (pay-as-you-go) requires ANTHROPIC_API_KEY",
				Error:  "ANTHROPIC_API_KEY empty",
			}
		}
		return doctorCheckResult{Status: statusPass, Hint: "pay-as-you-go via ANTHROPIC_API_KEY"}
	}
	if strings.TrimSpace(env.getenv("CLAUDE_CODE_OAUTH_TOKEN")) != "" {
		return doctorCheckResult{Status: statusPass, Hint: "subscription via long-lived OAuth token (CLAUDE_CODE_OAUTH_TOKEN)"}
	}
	home := env.getenv("HOME")
	if home == "" {
		return doctorCheckResult{
			Status: statusFail,
			Hint:   "subscription path requires $HOME OR CLAUDE_CODE_OAUTH_TOKEN",
			Error:  "$HOME empty",
		}
	}
	claudeDir := home + "/.claude"
	if _, err := osStat(claudeDir); err != nil {
		return doctorCheckResult{
			Status: statusFail,
			Hint:   "mount ~/.claude OR set CLAUDE_CODE_OAUTH_TOKEN (run `claude setup-token` on host) OR REGATTA_SPAWNER_STRIP_API_KEY=0 + ANTHROPIC_API_KEY",
			Error:  err.Error(),
		}
	}
	authPath := claudeDir + "/auth.json"
	raw, err := osReadFile(authPath)
	if err != nil {
		return doctorCheckResult{
			Status: statusFail,
			Hint:   "~/.claude mounted but auth.json unreadable — run `claude` on host to log in OR set CLAUDE_CODE_OAUTH_TOKEN OR REGATTA_SPAWNER_STRIP_API_KEY=0 + ANTHROPIC_API_KEY",
			Error:  err.Error(),
		}
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return doctorCheckResult{
			Status: statusFail,
			Hint:   "~/.claude/auth.json unparseable — re-run `claude` on host to reset auth OR set CLAUDE_CODE_OAUTH_TOKEN OR REGATTA_SPAWNER_STRIP_API_KEY=0 + ANTHROPIC_API_KEY",
			Error:  err.Error(),
		}
	}
	return doctorCheckResult{Status: statusPass, Hint: "subscription via mounted ~/.claude"}
}

// osReadFile is var so tests can stub the file-read boundary; mirrors osStat.
var osReadFile = os.ReadFile

// osStat is var so tests can stub the stat boundary without bringing in a filesystem mock framework.
var osStat = func(path string) (isDir bool, err error) {
	st, err := os.Stat(path) //nolint:gosec // operator-controlled path resolved by doctor probe
	if err != nil {
		return false, err
	}
	return st.IsDir(), nil
}

// stringList satisfies flag.Value for repeatable + comma-separated --skip.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}

func (s stringList) set() map[string]bool {
	out := make(map[string]bool, len(s))
	for _, v := range s {
		out[v] = true
	}
	return out
}

func liveDoctorEnv() doctorEnv {
	return doctorEnv{
		secretLookup: func(ctx context.Context, key string) (string, error) {
			chain := secrets.Default(ctx)
			_, src, err := secrets.GetWithSource(ctx, chain, key)
			if err != nil {
				return chain.Name(), err
			}
			return src, nil
		},
		lookPath: exec.LookPath,
		runCmd: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // doctor invokes fixed-name probes (gh, etc.); args originate in doctor.go check funcs.
			return cmd.CombinedOutput()
		},
		gitState:          liveGitState,
		validateConfig:    liveValidateConfig,
		verifyRepoConfig:  liveVerifyRepoConfig,
		supervisorPresent: liveSupervisorPresent,
		getenv:            os.Getenv,
		toolPins:          []string{binOSV, binGitleaks},
		configPath:        "regatta.yaml",
	}
}
