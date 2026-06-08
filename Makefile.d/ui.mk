# SvelteKit operator console (v5.1). Wave 1 htmx at internal/web/ stays
# untouched until S1 phase-3 deletes it.
.PHONY: ui-dev ui-build ui-install

ui-install:  ## Install web/console/ npm deps. Run once after clone; subsequent builds use the lockfile.
	cd web/console && npm install

ui-dev:  ## Run SvelteKit dev server on :5173. Proxies /api to :8080 (regatta serve).
	cd web/console && npm run dev

ui-build:  ## Build SvelteKit bundle to web/console/build/. Embedded by Go binary post-S1 phase-3.
	cd web/console && npm ci && npm run build
