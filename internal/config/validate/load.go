// Package validate loads and CUE-validates regatta.yaml against
// schemas/regatta.v1.cue. Exit point for `regatta validate-config`.
package validate

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/encoding/yaml"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// SpecAdapterTypeMarkdownCatalog mirrors the CUE discriminator value
// `markdown_catalog`. cmd/regatta uses it to decide whether the typed
// `spec_adapter.root` accessor is meaningful (other types return "").
const SpecAdapterTypeMarkdownCatalog = "markdown_catalog"

// LoadFile reads path and runs LoadBytes on its contents.
func LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return LoadBytes(data)
}

// LoadBytes validates YAML bytes against the embedded regatta.v1 CUE
// schema. Returns nil on full concrete validity. On failure the
// returned error enumerates every divergence: CUE's native error
// renders elide siblings as "(and N more errors)"; LoadBytes expands
// them via cue/errors.Details so the caller sees every offending field.
func LoadBytes(data []byte) error {
	if len(data) == 0 {
		return errors.New("regatta.yaml is empty")
	}

	ctx := cuecontext.New()

	schema := ctx.CompileString(schemas.RegattaV1CUE, cue.Filename("regatta.v1.cue"))
	if err := schema.Err(); err != nil {
		return fmt.Errorf("schema compile: %s", cueDetails(err))
	}

	cfgFile, err := yaml.Extract("regatta.yaml", data)
	if err != nil {
		return fmt.Errorf("yaml parse: %s", cueDetails(err))
	}
	cfg := ctx.BuildFile(cfgFile)
	if err := cfg.Err(); err != nil {
		return fmt.Errorf("yaml build: %s", cueDetails(err))
	}

	unified := schema.Unify(cfg)
	if err := unified.Validate(cue.Concrete(true), cue.All()); err != nil {
		return errors.New(cueDetails(err))
	}
	return nil
}

// cueDetails renders a CUE error with every sibling fault enumerated.
// errors.Details prints one fault per line with position info; we trim
// trailing whitespace to keep the message tidy when wrapped in another
// error.
func cueDetails(err error) string {
	return strings.TrimRight(cueerrors.Details(err, nil), "\n")
}

// Prompts mirrors the `prompts` block in regatta.yaml. Only the SHA
// pins land in Go for now; planner_path stays implicit (the
// orchestrator resolves it from cmd-line flags or the embedded
// default). Schema authority is contracts/schemas/regatta.v1.cue
// §Prompts.
type Prompts struct {
	PlannerSHA      string `yaml:"planner_sha,omitempty" json:"planner_sha,omitempty"`
	SecurityGateSHA string `yaml:"security_gate_sha,omitempty" json:"security_gate_sha,omitempty"`
	AgentBriefSHA   string `yaml:"agent_brief_sha,omitempty" json:"agent_brief_sha,omitempty"`
}

// Config is the Go form of a validated regatta.yaml. Only the fields
// callers reach into are surfaced; the rest lives in the YAML and is
// validated by LoadBytes against the CUE schema. New fields go in
// alongside their CUE peer in contracts/schemas/regatta.v1.cue.
type Config struct {
	Prompts      *Prompts      `yaml:"prompts,omitempty" json:"prompts,omitempty"`
	SpecAdapter  *SpecAdapter  `yaml:"spec_adapter,omitempty" json:"spec_adapter,omitempty"`
	Safety       *Safety       `yaml:"safety,omitempty" json:"safety,omitempty"`
	AlarmWebhook *AlarmWebhook `yaml:"alarm_webhook,omitempty" json:"alarm_webhook,omitempty"`
}

// AlarmWebhook is the typed view of `regatta.yaml::alarm_webhook`. Empty
// ListenAddr ⇒ in-process receiver disabled; the operator can still
// run cmd/regatta-alarm-webhook out-of-process. Schema authority:
// contracts/schemas/regatta.v1.cue §AlarmWebhook.
type AlarmWebhook struct {
	// ListenAddr is the HTTP bind. Empty ⇒ disabled.
	ListenAddr string `yaml:"listen_addr,omitempty" json:"listen_addr,omitempty"`
	// GHRepo is owner/name of the issue-target repo. Empty disables
	// even when ListenAddr is set; partial config never silently
	// no-ops.
	GHRepo string `yaml:"gh_repo,omitempty" json:"gh_repo,omitempty"`
	// GHTokenEnv names the env var holding the GitHub API token.
	// Defaulted to GITHUB_TOKEN by CUE.
	GHTokenEnv string `yaml:"gh_token_env,omitempty" json:"gh_token_env,omitempty"`
}

// Safety is the typed view of `regatta.yaml::safety`. Only the fields a
// Go consumer needs land here; everything else stays CUE-only until a
// callsite emerges. Schema authority: contracts/schemas/regatta.v1.cue
// §Safety.
type Safety struct {
	// Authz carries the OPA policy hot-reload config. Nil ⇒ embed.FS
	// default-deny only, no watcher, no SIGHUP handler.
	Authz *Authz `yaml:"authz,omitempty" json:"authz,omitempty"`
}

// Authz mirrors `safety.authz` in the CUE schema. Slim single-tenant W8
// spec (docs/engineer/specs/2026-06-02-s3-t1-w8-opa-slim.md §3.5). All
// fields optional — an absent block keeps zero-config deployments running
// off the embed.FS default-deny bundle.
type Authz struct {
	// PolicyDir is the operator-supplied path to <policy_dir>; the disk
	// loader appends `regatta/v1/<tenant>/`. Empty ⇒ serve embed.FS and
	// skip both reload triggers.
	PolicyDir string `yaml:"policy_dir,omitempty" json:"policy_dir,omitempty"`
	// ReloadDebounce is the fsnotify event coalesce window (e.g. "250ms").
	// Empty ⇒ reload.DefaultDebounce.
	ReloadDebounce string `yaml:"reload_debounce,omitempty" json:"reload_debounce,omitempty"`
	// ReloadSighup defaults to true via the CUE schema. Operator sets
	// false to opt the SIGHUP handler out (HR3 — windows / CI / any
	// process that already owns SIGHUP).
	ReloadSighup *bool `yaml:"reload_sighup,omitempty" json:"reload_sighup,omitempty"`
	// ReloadFsnotify defaults to true via the CUE schema. Operator sets
	// false on filesystems where inotify / kqueue is unreliable (NFS,
	// certain container overlays).
	ReloadFsnotify *bool `yaml:"reload_fsnotify,omitempty" json:"reload_fsnotify,omitempty"`
}

// SpecAdapter is the typed view of `regatta.yaml::spec_adapter`. Only
// the markdown_catalog discriminator + its Root field land here today
// because cmd/regatta needs them to drive the SpecAdapter wiring at
// boot time. Other adapter types stay CUE-only until a Go consumer
// emerges. Schema authority: contracts/schemas/regatta.v1.cue
// §SpecAdapter.
type SpecAdapter struct {
	Type string `yaml:"type" json:"type"`
	// Root is the markdown_catalog directory (relative to repo root)
	// containing `.regatta/items/*.md`. Defaulted by CUE to "." for
	// the self-host layout. Empty for non-markdown_catalog types.
	Root string `yaml:"root,omitempty" json:"root,omitempty"`
}

// PlannerPromptSHA returns the operator-pinned hex-encoded sha256 of
// the planner system prompt. Empty when unpinned. Nil-safe at every
// level so callers don't have to chain nil guards: a fresh Config{}
// or a nil receiver both yield "" without panicking. Used by
// cmd/regatta to feed program.LoadPlannerPrompt.
func (c *Config) PlannerPromptSHA() string {
	if c == nil || c.Prompts == nil {
		return ""
	}
	return c.Prompts.PlannerSHA
}

// AuthzConfig returns the resolved safety.authz block, or nil when the
// operator omitted it. Nil-safe at every level so a fresh Config{} or a
// nil receiver both yield nil. cmd/regatta calls this to decide whether
// to start the OPA hot-reload Reloader goroutine.
func (c *Config) AuthzConfig() *Authz {
	if c == nil || c.Safety == nil {
		return nil
	}
	return c.Safety.Authz
}

// AlarmWebhookConfig returns the resolved alarm_webhook block, or nil
// when the operator omitted it or left listen_addr empty (disabled).
// cmd/regatta serve calls this to decide whether to start the
// in-process alarm-webhook listener goroutine.
func (c *Config) AlarmWebhookConfig() *AlarmWebhook {
	if c == nil || c.AlarmWebhook == nil {
		return nil
	}
	if c.AlarmWebhook.ListenAddr == "" {
		return nil
	}
	return c.AlarmWebhook
}

// MarkdownCatalogRoot returns the resolved spec_adapter.root when the
// adapter type is markdown_catalog. Empty string for every other
// adapter type (or a nil Config). cmd/regatta uses this to decide
// whether to override the --items-root flag default with the yaml
// field; an empty result keeps the flag default unchanged.
func (c *Config) MarkdownCatalogRoot() string {
	if c == nil || c.SpecAdapter == nil {
		return ""
	}
	if c.SpecAdapter.Type != SpecAdapterTypeMarkdownCatalog {
		return ""
	}
	return c.SpecAdapter.Root
}

// LoadConfig CUE-validates the yaml bytes (same gate as LoadBytes) and
// then decodes the unified value into a typed Config. The unified
// value carries CUE's defaults concretely, so an omitted
// `spec_adapter.root: .` surfaces as ".", not "".
//
// Callers that only need the schema check stay on LoadBytes; callers
// that need typed access to a field (planner SHA, markdown adapter
// root) call LoadConfig.
func LoadConfig(data []byte) (*Config, error) {
	if len(data) == 0 {
		return nil, errors.New("regatta.yaml is empty")
	}
	ctx := cuecontext.New()
	schema := ctx.CompileString(schemas.RegattaV1CUE, cue.Filename("regatta.v1.cue"))
	if err := schema.Err(); err != nil {
		return nil, fmt.Errorf("schema compile: %s", cueDetails(err))
	}
	cfgFile, err := yaml.Extract("regatta.yaml", data)
	if err != nil {
		return nil, fmt.Errorf("yaml parse: %s", cueDetails(err))
	}
	cfgVal := ctx.BuildFile(cfgFile)
	if err := cfgVal.Err(); err != nil {
		return nil, fmt.Errorf("yaml build: %s", cueDetails(err))
	}
	unified := schema.Unify(cfgVal)
	if err := unified.Validate(cue.Concrete(true), cue.All()); err != nil {
		return nil, errors.New(cueDetails(err))
	}

	var out Config
	// Decode the unified value so CUE defaults (e.g. spec_adapter.root="."
	// for markdown_catalog) land in the Go struct.
	if err := unified.Decode(&out); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return &out, nil
}

// LoadConfigFile is the file-reading sibling of LoadConfig; mirrors
// LoadFile's contract.
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return LoadConfig(data)
}
