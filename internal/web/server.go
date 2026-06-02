package web

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// assetsFS embeds templates/ + static/ at build time per spec §3.8. Both
// subdirs must contain at least one regular file or go:embed errors;
// htmx-config.js + layout.tmpl + _flash.tmpl satisfy that pre-T5.
//
// `all:` prefix forces underscore-prefixed files (e.g. _flash.tmpl) into the
// embed set; without it, Go's default rule excludes them.
//
//go:embed all:templates all:static
var assetsFS embed.FS

// AssetsFS returns the embedded FS so cmd/regatta can LoadTemplates(AssetsFS())
// without re-declaring the //go:embed directive in two places.
func AssetsFS() fs.FS { return assetsFS }

// Dependencies bundles the side-effect handles NewHandler needs. Shape mirrors
// approval.Dependencies (W7.0 T1) so the seam is familiar across the surface.
// RouteRegistrar is the T6 extension hook: T6's PR will pass a non-nil func
// that mounts /approve/{approval_id}* on the sub-mux. Pre-T6 callers
// (cmd/regatta/serve.go) pass nil and NewHandler nil-guards the call.
type Dependencies struct {
	DB             *state.DB
	Keyring        canon.Keyring
	Templates      *Templates
	Clock          func() time.Time
	Config         Config
	RouteRegistrar func(mux *http.ServeMux, deps Dependencies)
}

// Config holds the boot-time knobs the handler reads. Kept narrow on purpose:
// per spec §3.6.4 + R4, an Authorizer interface is over-design until W8 adds a
// second auth caller.
type Config struct {
	DecisionWindow time.Duration
	PublicHost     string
}

// NewHandler builds the top-level http.Handler that cmd/regatta wires into
// bootListener's mux. The returned handler is the SOLE entry point for UI
// routes; it owns sub-mux dispatch + CSP middleware composition. T6's
// RouteRegistrar extends the sub-mux without re-shaping Dependencies.
func NewHandler(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(uiStaticPrefix, staticHandler())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Spec §3.3 default Cache-Control: no-store on operator surfaces.
		// /ui/static/* sets its own immutable cache above before delegating.
		w.Header().Set("Cache-Control", noStoreCacheControl)
		// mux.HandleFunc("/", ...) is the catch-all; narrow to the literal
		// root so /foo etc. surface as 404 instead of pretending the layout
		// is the page they asked for. T6 mounts /approve/* below via
		// RouteRegistrar, so those paths win on the longest-prefix rule.
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if deps.Templates == nil {
			http.Error(w, "templates not loaded", http.StatusInternalServerError)
			return
		}
		if err := deps.Templates.Render(w, "layout.tmpl", nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	if deps.RouteRegistrar != nil {
		deps.RouteRegistrar(mux, deps)
	}
	return CSPMiddleware(mux)
}

// staticHandler serves /ui/static/* from the embedded FS with the spec §3.3
// immutable cache directive. http.StripPrefix unwinds the route prefix so
// the FileServer sees plain filenames.
func staticHandler() http.Handler {
	sub, err := fs.Sub(assetsFS, staticDirName)
	if err != nil {
		// Should never fire — staticDirName matches the //go:embed directive.
		// Falling back to a 500 keeps the handler buildable in tests that
		// load a synthetic FS via LoadTemplates.
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
