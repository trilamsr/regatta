# Tooling

What this repo pins for Claude Code so every contributor (today,
just you) gets the same setup on `git pull`. Reference, not
lesson.

### First session in the repo

Three prompts you accept once, three prereqs you install once:

1. Trust the repo folder when Claude Code asks.
2. Approve the plugin install prompt - installs the four plugins
   listed under [Pinned plugins](#pinned-plugins).
3. Approve the project MCP servers prompt - enables the three
   entries in `.mcp.json`.
4. `gh auth login` (skip if already authenticated).
5. `export GH_TOKEN=$(gh auth token)` in your shell rc. `gh`
   does not auto-export this; the `github` MCP needs it.
6. `brew install uv` and ensure `go` is on `PATH` (uv for the
   fetch MCP, go for the github MCP via `go run`).

Anchor: `.claude/settings.json`, `.mcp.json`.

### Pinned plugins

`.claude/settings.json` `enabledPlugins` enables four plugins. On
first session in the repo, Claude Code prompts the contributor to
install each from the listed marketplaces. After approval they
load on every subsequent session.

- `superpowers@claude-plugins-official` - engineering-discipline
  skills (`brainstorming`, `test-driven-development`,
  `systematic-debugging`, `verification-before-completion`,
  `writing-plans`, `using-git-worktrees`, code-review skills).
- `ralph-loop@claude-plugins-official` - `/pr-review-loop`,
  `/repo-consistency-loop`, autonomous-loop authoring.
- `claude-mem@thedotmack` - cross-session memory and observation
  search. Plugin code is shared; the corpus is per-contributor
  under `~/.claude/projects/.../memory/`.
- `caveman@caveman` - terse-output skill (`/caveman`,
  `/caveman-commit`, `/caveman-review`, `/caveman-compress`). Cuts
  output tokens without losing technical content.

Both third-party marketplaces have `autoUpdate: true` set, so
plugins refresh on session start without manual
`/plugin marketplace update`.

Anchor: `.claude/settings.json` `enabledPlugins` and
`extraKnownMarketplaces`.

### Pinned MCP servers

`.mcp.json` registers three MCP servers, each wrapped by
`caveman-shrink` to compress tool-description prose in the
`tools/list` catalog (tool semantics and call responses pass
through unchanged):

- `context7` - live library documentation. Use over `WebFetch`
  when asking about a library, SDK, or CLI; preferred over web
  search for API surfaces. No API key.
- `github` - operations on issues, PRs, reviews, code search, and
  CI via GitHub's official `github-mcp-server`. Launched in stdio
  mode via `go run github.com/github/github-mcp-server/cmd/
  github-mcp-server@latest stdio`; first invocation downloads
  modules and compiles (~30s), subsequent invocations use the Go
  build cache. Requires `GITHUB_PERSONAL_ACCESS_TOKEN` in the
  environment (`gh auth token` produces one).
- `fetch` - markdown-converted page retrieval with chunking, via
  `uvx mcp-server-fetch`. Use when you have a known URL and want
  full content; prefer over `WebFetch` for long pages.

Optional: `CAVEMAN_SHRINK_DEBUG=1` to log per-field compression
deltas to stderr when triaging an MCP wrapper issue.

Anchor: `.mcp.json` top level.

### What the repo does NOT pin

- User personal settings (`defaultMode`, `theme`,
  `skipAutoPermissionPrompt`, API tokens). These live in
  `~/.claude/settings.json` per contributor.
- claude-mem observation corpus. The plugin is shared; the data
  is per-contributor under `~/.claude/projects/.../memory/`.
- Caveman session-start hooks. Pin the plugin gives you the slash
  commands; the curl-installer adds the always-on behavior and is
  per-contributor.
