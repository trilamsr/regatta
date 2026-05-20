// schemas/regatta.v1.cue
//
// CUE schema for regatta.yaml v1.
//
// Usage:
//   cue vet regatta.yaml schemas/regatta.v1.cue
//   regatta validate-config             # uses this schema internally
//   regatta migrate-config --from 1 --to 2   # planned for v2; see docs/design.md §Versioning
//
// Status: draft v1. The major version of `version:` in the YAML must equal
// the major in this schema's package path. Migration tool ships before any
// v2 release.

package regattav1

#Config: {
	version: 1
	repo:          #Repo
	spec_adapter:  #SpecAdapter
	ci:            #CI
	pr_template?:  #PRTemplate
	gates:         [...#Gate] & list.MinItems(1)
	lanes?:        [...#Lane]
	hotspots?:     [...string]
	safety:        #Safety
	context?:      #Context
	telemetry?:    #Telemetry
}

#Repo: {
	host:           "github" | "gitlab"
	owner:          string & =~ "^[A-Za-z0-9_.-]+$"
	name:           string & =~ "^[A-Za-z0-9_.-]+$"
	default_branch: *"main" | string
}

// Spec adapter selector. The `type` field discriminates between the per-type
// schemas below.
#SpecAdapter: {
	type: "github_issues" | "gitlab_issues" | "markdown_catalog" | "jira" | "linear" | "custom"

	if type == "github_issues" || type == "gitlab_issues" {
		selector:            string                  // e.g. "label:planned"
		acceptance_section?: *"## Acceptance" | string
	}
	if type == "markdown_catalog" {
		path:    string                          // path relative to repo root
		format:  *"github_checkbox" | "rubric"   // - [ ] / - [x]  vs.  ☐/⧗/☑
	}
	if type == "jira" {
		project: string
		jql:     string
	}
	if type == "linear" {
		team:    string
		states:  [...string]
	}
	if type == "custom" {
		command:          string                // executable on PATH
		timeout_seconds?: *30 | int & >0 & <=300
	}
}

#CI: {
	command:          string & =~ ".+"
	timeout_minutes:  *30 | int & >0 & <=180
}

#PRTemplate: {
	citation_section_required: *true | bool
	release_notes_required:    *false | bool
}

#Gate: {
	id:                string & =~ "^[a-z0-9_-]+$"
	type:              "deterministic" | "ai"

	if type == "deterministic" {
		command:    string & =~ ".+"
		blocks_on:  *["exit_nonzero"] | [...string]
	}
	if type == "ai" {
		model:               string             // e.g. "claude-opus-4-7"
		prompt?:             string             // path relative to repo root
		severity_block:      [...string] & list.MinItems(1)
		rigorous_label?:     string
	}
}

#Lane: {
	id:               string & =~ "^[a-z0-9_-]+$"
	paths:            [...string] & list.MinItems(1)
	max_concurrency:  *1 | int & >=1 & <=8
}

#Safety: {
	destructive_ops_deny:    [...string]
	agent_creds_scope:       "dev_only" | "test" | "scoped"
	iteration_cap:           *50 | int & >=1 & <=500
	spend_cap_usd:           *50 | int & >=0
	spend_cap_usd_per_day:   *200 | int & >=0
	canary_rate:             *0.05 | float & >=0 & <=0.2
}

#Context: {
	trusted_doc_paths?:               [...string]
	agent_guidance_path?:             string
	agent_guidance_codeowners_check:  *true | bool
}

#Telemetry: {
	digest_path:    *"docs/regatta-digest.md" | string
	state_db_path:  *"./.regatta/regatta.db" | string
	audit_sink?:    string   // S3 URL with object-lock or transparency-log endpoint
}

// Toplevel: every regatta.yaml validates as #Config.
#Config
