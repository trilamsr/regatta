package web

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
	"github.com/trilamsr/regatta/internal/health"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// assetsFS embeds templates/ + static/ at build time (spec §3.8). The
// `all:` prefix forces underscore-prefixed files (e.g. _flash.tmpl)
// into the embed set; without it, Go's default rule excludes them.
//
//go:embed all:templates all:static
var assetsFS embed.FS

// AssetsFS returns the embedded FS so cmd/regatta can wire templates
// without re-declaring the //go:embed directive in two places.
func AssetsFS() fs.FS { return assetsFS }

// Dependencies bundles the side-effect handles NewHandler needs. Shape
// mirrors approval.Dependencies (W7.0 T1). RouteRegistrar is the T6
// extension hook for /approve/* sub-mux mounting; pre-T6 callers pass
// nil and NewHandler nil-guards the call.
type Dependencies struct {
	DB             *state.DB
	Keyring        approvaltoken.Keyring
	Templates      *Templates
	Clock          func() time.Time
	BootedAt       time.Time
	Config         Config
	RouteRegistrar func(mux *http.ServeMux, deps Dependencies)
	// Heartbeat is the orchestrator's live liveness cell. Web reads Age()
	// to drive the tick-stale red banner (MAY-48); nil disables the banner
	// so test harnesses that skip orchestrator wiring still render.
	Heartbeat *health.HeartbeatCell
	// TickInterval is the orchestrator scheduler cadence. The tick-stale
	// banner fires when Heartbeat.Age() > 2× this value (MAY-48 §AC4).
	// Zero falls back to the orchestrator default so a wiring miss does
	// not paint the banner red on every poll.
	TickInterval time.Duration
}

// Config holds the boot-time knobs the handler reads. Kept narrow on
// purpose: per spec §3.6.4 + R4, an Authorizer interface is over-design
// until W8 adds a second auth caller.
type Config struct {
	DecisionWindow time.Duration
	PublicHost     string
	// GitHubRepo is the "owner/name" slug used to build issue URLs in the
	// work-item drawer. Empty when no github_issues adapter is configured,
	// in which case the drawer suppresses the link.
	GitHubRepo string
}

// NewHandler is the sole entry point for UI routes; owns sub-mux
// dispatch + CSP middleware composition. T6's RouteRegistrar extends
// the sub-mux without re-shaping Dependencies.
func NewHandler(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(uiStaticPrefix, staticHandler())
	// /favicon.ico is requested by every browser on every page load; without
	// this route the catch-all returns 404, which surfaces as a console error
	// (MAY-57). Layout.tmpl additionally emits `<link rel="icon">` pointing at
	// the SVG; the 204 here is the bare-URL fallback for clients that ignore
	// the link element.
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", noStoreCacheControl)
		// Narrow the catch-all to literal root so /foo surfaces as 404
		// instead of rendering the layout. T6 mounts /approve/* via
		// RouteRegistrar; longest-prefix rule lets those win.
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if deps.Templates == nil {
			http.Error(w, "templates not loaded", http.StatusInternalServerError)
			return
		}
		clock := deps.Clock
		if clock == nil {
			clock = time.Now
		}
		if err := deps.Templates.Render(w, "layout.tmpl", dashboardLayoutView{Now: clock()}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	registerDashboardRoutes(mux, deps)
	if deps.RouteRegistrar != nil {
		deps.RouteRegistrar(mux, deps)
	}
	return CSPMiddleware(mux)
}

// staticHandler serves /ui/static/* from the embedded FS with the
// spec §3.3 immutable cache directive.
func staticHandler() http.Handler {
	sub, err := fs.Sub(assetsFS, staticDirName)
	if err != nil {
		// staticDirName tracks the //go:embed directive — this branch
		// is the test-FS escape hatch (LoadTemplates synthetic root).
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "static fs init: "+err.Error(), http.StatusInternalServerError)
		})
	}
	fileSrv := http.FileServer(http.FS(sub))
	return http.StripPrefix(uiStaticPrefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", staticCacheControl)
		fileSrv.ServeHTTP(w, r)
	}))
}
