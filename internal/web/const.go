package web

// Named values for spec §3.7, §3.6.1, §3.4. Every magic number in internal/web/*.go MUST be named here so TestConstNoZeroValueMagic stays green.

// CSRFCookieName names the spec §3.6.2 double-submit cookie.
const CSRFCookieName = "regatta_csrf"

// ApprovalTokenCookieName names the per-ULID HMAC cookie set by RedeemHandler (spec §3.6.1).
const ApprovalTokenCookieName = "regatta_approval_token"

// MaxDiffBytes is the spec §3.4 _diff.tmpl byte cap.
const MaxDiffBytes = 8 * 1024

// MaxFullDiffBytes caps the streamed overflow diff at 1 MiB (spec §3.3 row 4).
const MaxFullDiffBytes = 1 << 20

// fullDiffChunkBytes is the per-write chunk for the streamed overflow diff; keeps a 1 MiB diff off a single buffer (spec §3.3 row 4).
const fullDiffChunkBytes = 32 * 1024

// StaticCacheMaxAgeSeconds is the spec §3.3 row 8 `/ui/static/*` cache window.
const StaticCacheMaxAgeSeconds = 86400

// PollIntervalSeconds is the spec §1.1 htmx poll cadence.
const PollIntervalSeconds = 5

// staticCacheControl is the spec §3.3 row 8 immutable cache directive applied to /ui/static/*.
const staticCacheControl = "public, max-age=86400, immutable"

// noStoreCacheControl is the spec §3.3 default cache directive for every non-asset route (R6 — operator surfaces cannot tolerate stale-cache lies).
const noStoreCacheControl = "no-store"

// uiStaticPrefix is the spec §3.3 row 8 mount point; sub-mux uses it as both route prefix and http.StripPrefix argument.
const uiStaticPrefix = "/ui/static/"

// staticDirName / templatesDirName are the embed-relative folder names; LoadTemplates uses both in one fs.Sub call site.
const (
	staticDirName    = "static"
	templatesDirName = "templates"
)

// usdMicroPerDollar bridges substrate usd_micros payload values to displayable USD.
const usdMicroPerDollar = 1_000_000

// csrfTokenByteWidth is the crypto/rand byte width hex-encoded into CSRFCookieName values (16 -> 32 hex chars).
const csrfTokenByteWidth = 16

// dashboardLast24hWindow is the spend panel rolling window — 24h matches the existing per-pr-cost.yaml Prom recording rule cadence so the UI number stays comparable to the Grafana panel.
const dashboardLast24hWindow = 24

// dashboardWorkItemSampleCount caps the per-bucket identifiers shown in the work-items panel; the bucket header always shows the full count.
const dashboardWorkItemSampleCount = 5

// workItemTitleMaxBytes caps inline kanban-card titles so each card stays single-line; the drawer surfaces the full text on click.
const workItemTitleMaxBytes = 50

// workItemDrawerEventTail caps the per-work-item event tail in the drawer so a chatty agent does not balloon the response.
const workItemDrawerEventTail = 5

// dashboardEventsTailLimit caps the recent-events tail. Each row is one JSON blob bounded by the substrate writer.
const dashboardEventsTailLimit = 30

// dashboardPanelTimeoutSeconds bounds DB reads per polling tick so a stuck sqlite query cannot wedge an htmx-poll connection.
const dashboardPanelTimeoutSeconds = 2

// dashboardSparkBuckets is the 24h spend histogram bucket count — 12 = 2h per bar, the cadence that smooths jitter while still showing the burn-rate trend.
const dashboardSparkBuckets = 12

// dashboardSparkWidth / dashboardSparkHeight / dashboardSparkBarMaxH are the inline-svg geometry the spend sparkline renders into.
const (
	dashboardSparkWidth    = 120
	dashboardSparkHeight   = 24
	dashboardSparkBarMaxH  = 20
	dashboardSparkBaseline = 22
)

// strconvBase10 / strconvBitSize64 name the int parsing args drawer route handlers pass to ParseInt — the substrate's agent / event ids are 64-bit signed integers in base 10.
const (
	strconvBase10    = 10
	strconvBitSize64 = 64
)

// hoursPerDay names the time arithmetic the humanRelativeShort helper does so a future locale-aware version (week / month thresholds) can extend without re-touching call sites.
const hoursPerDay = 24

// bodyPreviewMaxRunes caps the work-item drawer body preview so operators see issue
// context without scrolling. 200 runes matches the dashboard kanban-card preview budget.
const bodyPreviewMaxRunes = 200

// workItemFlowStepCount fixes the 4-bucket pill row (pending → spawning → running → done/crashed) that collapses the 11-value AgentState enum into a single linear flow.
const workItemFlowStepCount = 4

// workItemFlowIdxPending / Spawning / Running / Done index the 4-step pill row built by buildStatusFlow. Named so the switch arms read as a forward-progress sequence, not opaque slice indices.
const (
	workItemFlowIdxPending  = 0
	workItemFlowIdxSpawning = 1
	workItemFlowIdxRunning  = 2
	workItemFlowIdxDone     = 3
)

const workItemFlowLabelCrashed = "crashed"

// pipelineDrawerLimit caps pipeline drawer rows so a many-thousand-agent backlog cannot wedge an htmx swap.
const pipelineDrawerLimit = 50

// defaultTickIntervalSec mirrors the orchestrator default TickInterval (5s)
// so the dashboard tick-stale banner never paints red on every poll when
// Dependencies.TickInterval is zero (composition root wiring miss).
const defaultTickIntervalSec = 5

// exitReasonUnknown labels the histogram bucket for agent.exited events
// whose payload lacks an exit_reason — drift signal the operator should
// see, not silently drop.
const exitReasonUnknown = "unknown"

// exitReasonHistogramWindowSec is the 1h lookback the health panel
// histogram reports — long enough to absorb the soak panel's 60s rolling
// window, short enough to surface a current incident.
const exitReasonHistogramWindowSec = 3600

// tickStaleBannerMultiplier is the multiple of TickInterval at which the
// dashboard tick-stale red banner fires (MAY-48 §AC4). 2× gives one
// missed tick of slack so a single slow SQLite write does not paint the
// banner red on every healthy poll.
const tickStaleBannerMultiplier = 2
