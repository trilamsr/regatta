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

// SpecAdapterTypeMarkdownCatalog mirrors the CUE discriminator; cmd/regatta uses it to decide whether spec_adapter.root is meaningful.
const SpecAdapterTypeMarkdownCatalog = "markdown_catalog"

// SpecAdapterTypeGitHubIssues mirrors the CUE discriminator for the
// MVR-1-T4 github_issues adapter; cmd/regatta wires it when set.
const SpecAdapterTypeGitHubIssues = "github_issues"

// LoadFile reads path and runs LoadBytes on its contents.
func LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return LoadBytes(data)
}

// LoadBytes validates YAML bytes against the embedded regatta.v1 CUE schema; the returned error expands every divergence via cue/errors.Details (CUE's native renders elide siblings as "and N more errors").
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

// cueDetails renders a CUE error with every sibling fault enumerated; errors.Details prints one fault per line with position info.
func cueDetails(err error) string {
	return strings.TrimRight(cueerrors.Details(err, nil), "\n")
}

// Prompts mirrors the `prompts` block; only SHA pins land in Go for now. Schema authority: contracts/schemas/regatta.v1.cue §Prompts.
type Prompts struct {
	PlannerSHA      string `yaml:"planner_sha,omitempty" json:"planner_sha,omitempty"`
	SecurityGateSHA string `yaml:"security_gate_sha,omitempty" json:"security_gate_sha,omitempty"`
	AgentBriefSHA   string `yaml:"agent_brief_sha,omitempty" json:"agent_brief_sha,omitempty"`
	// AdaptiveEnrichment toggles the L2 target-repo convention scanner (#966); nil ⇒ default true.
	AdaptiveEnrichment *bool `yaml:"adaptive_enrichment,omitempty" json:"adaptive_enrichment,omitempty"`
}

// Config is the Go form of a validated regatta.yaml; only fields callers reach into are surfaced. Schema authority: contracts/schemas/regatta.v1.cue.
type Config struct {
	Prompts      *Prompts      `yaml:"prompts,omitempty" json:"prompts,omitempty"`
	Repo         *Repo         `yaml:"repo,omitempty" json:"repo,omitempty"`
	SpecAdapter  *SpecAdapter  `yaml:"spec_adapter,omitempty" json:"spec_adapter,omitempty"`
	Safety       *Safety       `yaml:"safety,omitempty" json:"safety,omitempty"`
	AlarmWebhook *AlarmWebhook `yaml:"alarm_webhook,omitempty" json:"alarm_webhook,omitempty"`
	Secrets      *Secrets      `yaml:"secrets,omitempty" json:"secrets,omitempty"`
}

// Secret mirrors `regatta.yaml::secrets.<key>` per #911; absent block surfaces as a nil parent — back-compat preserved.
type Secret struct {
	Source string `yaml:"source" json:"source"`
	Name   string `yaml:"name,omitempty" json:"name,omitempty"`
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
	KeyID  string `yaml:"key_id,omitempty" json:"key_id,omitempty"`
}

// Secrets is the typed view of `regatta.yaml::secrets`; nil ⇒ Default chain (back-compat).
type Secrets struct {
	AnthropicAPIKey *Secret `yaml:"anthropic_api_key,omitempty" json:"anthropic_api_key,omitempty"`
	GHToken         *Secret `yaml:"gh_token,omitempty" json:"gh_token,omitempty"`
	BriefHMAC       *Secret `yaml:"brief_hmac,omitempty" json:"brief_hmac,omitempty"`
	AuditHMAC       *Secret `yaml:"audit_hmac,omitempty" json:"audit_hmac,omitempty"`
	ApprovalToken   *Secret `yaml:"approval_token,omitempty" json:"approval_token,omitempty"`
}

// Repo mirrors `regatta.yaml::repo`; owner+name pair the github_issues adapter (MVR-1-T4) anchors SourceRef.Locator against.
type Repo struct {
	Host          string `yaml:"host,omitempty" json:"host,omitempty"`
	Owner         string `yaml:"owner,omitempty" json:"owner,omitempty"`
	Name          string `yaml:"name,omitempty" json:"name,omitempty"`
	DefaultBranch string `yaml:"default_branch,omitempty" json:"default_branch,omitempty"`
}

// AlarmWebhook is the typed view of `regatta.yaml::alarm_webhook`; empty ListenAddr ⇒ in-process receiver disabled.
type AlarmWebhook struct {
	// ListenAddr is the HTTP bind. Empty ⇒ disabled.
	ListenAddr string `yaml:"listen_addr,omitempty" json:"listen_addr,omitempty"`
	// GHRepo is owner/name of the issue-target repo; empty disables even when ListenAddr is set (partial config never silently no-ops).
	GHRepo string `yaml:"gh_repo,omitempty" json:"gh_repo,omitempty"`
	// GHTokenEnv names the env var holding the GitHub API token (CUE default GITHUB_TOKEN).
	GHTokenEnv string `yaml:"gh_token_env,omitempty" json:"gh_token_env,omitempty"`
}

// Safety is the typed view of `regatta.yaml::safety`; only Go-consumed fields land here.
type Safety struct {
	// Authz — OPA policy hot-reload config; nil ⇒ embed.FS default-deny only, no watcher, no SIGHUP handler.
	Authz *Authz `yaml:"authz,omitempty" json:"authz,omitempty"`
}

// Authz mirrors `safety.authz`; all fields optional — absent block keeps zero-config deployments on the embed.FS default-deny bundle.
type Authz struct {
	// PolicyDir is the operator-supplied path; loader appends `regatta/v1/<tenant>/`. Empty ⇒ serve embed.FS and skip both reload triggers.
	PolicyDir string `yaml:"policy_dir,omitempty" json:"policy_dir,omitempty"`
	// ReloadDebounce is the fsnotify event coalesce window (e.g. "250ms"); empty ⇒ reload.DefaultDebounce.
	ReloadDebounce string `yaml:"reload_debounce,omitempty" json:"reload_debounce,omitempty"`
	// ReloadSighup defaults true; operator sets false to opt-out (HR3 — windows / CI / process already owning SIGHUP).
	ReloadSighup *bool `yaml:"reload_sighup,omitempty" json:"reload_sighup,omitempty"`
	// ReloadFsnotify defaults true; operator sets false on filesystems where inotify/kqueue is unreliable (NFS, some container overlays).
	ReloadFsnotify *bool `yaml:"reload_fsnotify,omitempty" json:"reload_fsnotify,omitempty"`
}

// SpecAdapter is the typed view of `regatta.yaml::spec_adapter`; per-type fields are unioned and zero for the non-matching type.
type SpecAdapter struct {
	Type string `yaml:"type" json:"type"`
	// Root is the markdown_catalog directory containing `.regatta/items/*.md`; CUE-defaulted to "." for the self-host layout. Empty for non-markdown_catalog types.
	Root string `yaml:"root,omitempty" json:"root,omitempty"`
	// Selector is the github_issues label selector (MVR-1-T4); empty for non-github_issues types.
	Selector string `yaml:"selector,omitempty" json:"selector,omitempty"`
	// AcceptanceSection overrides the github_issues H2 anchor; empty falls back to the package default.
	AcceptanceSection string `yaml:"acceptance_section,omitempty" json:"acceptance_section,omitempty"`
	// DefaultLane backfills WorkItem.Lane on github_issues items whose body has no `lane:` metadata. Mirror of the scheduler default-lane wedge from #1048; without it adaptersync drops every operator-filed unlabelled issue with `empty_lane` WARN (#1117). Empty = preserve the original "operator must label every issue" contract.
	DefaultLane string `yaml:"default_lane,omitempty" json:"default_lane,omitempty"`
}

// PlannerPromptSHA returns the operator-pinned planner-prompt sha256; nil-safe at every level. Empty when unpinned.
func (c *Config) PlannerPromptSHA() string {
	if c == nil || c.Prompts == nil {
		return ""
	}
	return c.Prompts.PlannerSHA
}

// AuthzConfig returns the resolved safety.authz block (nil when omitted); nil-safe at every level.
func (c *Config) AuthzConfig() *Authz {
	if c == nil || c.Safety == nil {
		return nil
	}
	return c.Safety.Authz
}

// AlarmWebhookConfig returns the resolved alarm_webhook block, or nil when omitted or listen_addr is empty (disabled).
func (c *Config) AlarmWebhookConfig() *AlarmWebhook {
	if c == nil || c.AlarmWebhook == nil {
		return nil
	}
	if c.AlarmWebhook.ListenAddr == "" {
		return nil
	}
	return c.AlarmWebhook
}

// MarkdownCatalogRoot returns the resolved spec_adapter.root when the adapter type is markdown_catalog; empty for every other type. Empty result keeps the --items-root flag default unchanged.
func (c *Config) MarkdownCatalogRoot() string {
	if c == nil || c.SpecAdapter == nil {
		return ""
	}
	if c.SpecAdapter.Type != SpecAdapterTypeMarkdownCatalog {
		return ""
	}
	return c.SpecAdapter.Root
}

// LoadConfig CUE-validates yaml bytes (same gate as LoadBytes) then decodes the unified value into a typed Config — unified value carries CUE defaults concretely (omitted `spec_adapter.root: .` surfaces as ".", not "").
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
	if err := unified.Decode(&out); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return &out, nil
}

// LoadConfigFile is the file-reading sibling of LoadConfig; mirrors LoadFile's contract.
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return LoadConfig(data)
}
