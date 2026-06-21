package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/trilamsr/regatta/internal/authz"
	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/health"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/web"
)

// listenerConfig is the bootListener seam; the test harness drives it directly so integration tests exercise the real boot path.
type listenerConfig struct {
	UI         bool
	Addr       string
	DB         *state.DB
	Keyring    approvaltoken.Keyring
	Clock      func() time.Time
	// Authorizer is the OPA-backed gate built at runServe boot. Pre-T3
	// the web handler does not yet consume it; the field is plumbed so
	// W8 T3 can pick it up without touching listener wiring.
	Authorizer *authz.OPAAuthorizer
	// PublicHost is the externally reachable host (no scheme) parsed from
	// --public-url. Reverse-proxy deployments set it so OriginCheck pins
	// the public hostname instead of the inner pod's r.Host (#304).
	PublicHost string
	// Heartbeat is the live /healthz liveness cell the orchestrator
	// Touches every Run-loop tick. bootListener installs it into the
	// /healthz handler; the orchestrator wiring at serve.go shares the
	// same pointer so age_seconds reflects real ticks (#1218). Nil
	// falls back to a fresh never-touched cell — preserved for backward
	// compatibility with the W3 unit harness.
	Heartbeat *health.HeartbeatCell
}

// preflightUIBoot fires BEFORE state.Open so the operator sees the HMAC misconfig at the loud-at-boot moment (spec §1.3 open-q 9.8) rather than as a render-time lie.
func preflightUIBoot(ui bool) error {
	if !ui {
		return nil
	}
	if os.Getenv("REGATTA_HMAC_KEY") != "" {
		return nil
	}
	if envName := os.Getenv("REGATTA_HMAC_KEY_ENV"); envName != "" && os.Getenv(envName) != "" {
		return nil
	}
	return fmt.Errorf("--ui requires REGATTA_HMAC_KEY (or REGATTA_HMAC_KEY_ENV) to be set; refusing to boot")
}

// bootListener returns a configured *http.Server when --ui=true, or nil when --ui=false so the caller skips the listen syscall entirely.
func bootListener(cfg listenerConfig) (*http.Server, error) {
	if !cfg.UI {
		return nil, nil
	}
	if err := preflightUIBoot(true); err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	// W3: content-negotiated /healthz — JSON readiness for the supervisor,
	// legacy `ok\n` literal preserved for the W7.0 contract.
	var healthDB *sql.DB
	if cfg.DB != nil {
		healthDB = cfg.DB.SQL()
	}
	hb := cfg.Heartbeat
	if hb == nil {
		hb = health.NewHeartbeatCell(cfg.Clock)
	}
	mux.HandleFunc("/healthz", health.Handler(health.Dependencies{
		DB:        healthDB,
		Heartbeat: hb,
		Brief:     health.NewBriefCell(),
		Version:   version,
	}))
	cbPath, cbHandler := approval.NewHTTPCallback(approval.Dependencies{
		DB:      cfg.DB,
		Keyring: cfg.Keyring,
		Clock:   cfg.Clock,
	})
	mux.Handle(cbPath, cbHandler)
	// W7.1 T4: mount the operator UI scaffold last so http.ServeMux's
	// longest-prefix-wins rule keeps /healthz + /api/approval/callback above
	// the `/` catch-all (TestServe_RootHandlerWiredIntoBootListener pins it).
	webHandler, err := newWebHandler(cfg)
	if err != nil {
		return nil, err
	}
	mux.Handle("/", webHandler)
	addr := cfg.Addr
	if addr == "" {
		addr = defaultListenerAddr
	}
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}, nil
}

// newWebHandler constructs the W7.1 T4 operator UI handler with templates
// loaded from the package's embed.FS at boot. Template parse failure surfaces
// as a bootListener error (spec §3.4 fail-loud) rather than a render-time lie.
// RouteRegistrar mounts the /approve/* approval flow onto the sub-mux (MAY-116).
func newWebHandler(cfg listenerConfig) (http.Handler, error) {
	tmpls, err := web.LoadTemplates(web.AssetsFS())
	if err != nil {
		return nil, fmt.Errorf("web templates: %w", err)
	}
	return web.NewHandler(web.Dependencies{
		DB:             cfg.DB,
		Keyring:        cfg.Keyring,
		Templates:      tmpls,
		Clock:          cfg.Clock,
		BootedAt:       cfg.Clock(),
		Config:         web.Config{PublicHost: cfg.PublicHost},
		RouteRegistrar: web.RegisterApprovalRoutes,
	}), nil
}

// parsePublicURL extracts the Host (incl. port) from --public-url so OriginCheck
// can pin the externally reachable hostname behind a reverse proxy (#304). Empty
// input returns ("", nil) so the flag stays optional; non-empty input MUST carry
// an http/https scheme — bare hostnames are rejected to keep mis-configurations
// loud at boot rather than silently falling back to r.Host.
func parsePublicURL(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("parse %q: want http:// or https:// scheme", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("parse %q: missing host", raw)
	}
	return u.Host, nil
}
