package web

// Named values for spec §3.7, §3.6.1, §3.4. Every magic number in
// internal/web/*.go MUST be named here so TestConstNoZeroValueMagic stays
// green and A+ "zero magic numbers" holds.

// CSRFCookieName names the spec §3.6.2 double-submit cookie. T7 owns the
// concrete middleware; T4 pins the name so the seam survives parallel work.
const CSRFCookieName = "regatta_csrf"

// ApprovalTokenCookieName names the per-ULID HMAC cookie set by RedeemHandler
// (spec §3.6.1). T7 owns the read path; T4 pins the name.
const ApprovalTokenCookieName = "regatta_approval_token"

// MaxDiffBytes is the spec §3.4 _diff.tmpl byte cap; T6 enforces at render.
const MaxDiffBytes = 8 * 1024

// MaxFullDiffBytes caps the streamed overflow diff at 1 MiB (spec §3.3 row 4).
const MaxFullDiffBytes = 1 << 20

// StaticCacheMaxAgeSeconds is the spec §3.3 row 8 `/ui/static/*` cache window.
const StaticCacheMaxAgeSeconds = 86400

// PollIntervalSeconds is the spec §1.1 htmx poll cadence; T8/T11 reference at render.
const PollIntervalSeconds = 5

// staticCacheControl is the spec §3.3 row 8 immutable cache directive applied
// to /ui/static/*; computed once so the asset handler ships it verbatim.
const staticCacheControl = "public, max-age=86400, immutable"

// noStoreCacheControl is the spec §3.3 default cache directive for every
// non-asset route (R6 — operator surfaces cannot tolerate stale-cache lies).
const noStoreCacheControl = "no-store"

// uiStaticPrefix is the spec §3.3 row 8 mount point; sub-mux uses it as both
// the route prefix and the http.StripPrefix argument.
const uiStaticPrefix = "/ui/static/"

// staticDirName is the embed-relative folder for assets; LoadTemplates also
// uses templatesDirName to keep both fs.Sub calls in one place.
const (
	staticDirName    = "static"
	templatesDirName = "templates"
)

// usdMicroPerDollar bridges substrate `usd_micros` payload values to displayable
// USD; named here so TestConstNoZeroValueMagic stays green.
const usdMicroPerDollar = 1_000_000
