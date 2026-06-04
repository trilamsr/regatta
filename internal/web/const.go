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
