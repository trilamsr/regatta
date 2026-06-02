# Vendored static assets — provenance pin

Single source of truth for every byte under `internal/web/static/`. The
SHA-256 in each entry below is asserted at build time by
`make verify-vendored-assets`; mismatch fails `make check`. Operators can
re-derive the supply chain from this file alone.

## htmx.min.js

- Asset path: `internal/web/static/htmx.min.js`
- URL served from: `/ui/static/htmx.min.js`
- Upstream URL: https://raw.githubusercontent.com/bigskysoftware/htmx/v2.0.4/dist/htmx.min.js
- Upstream release: https://github.com/bigskysoftware/htmx/releases/tag/v2.0.4
- Version pinned: `v2.0.4`
- License: Zero-Clause BSD (0BSD) — https://github.com/bigskysoftware/htmx/blob/v2.0.4/LICENSE
- SHA-256: `e209dda5c8235479f3166defc7750e1dbcd5a5c1808b7792fc2e6733768fb447`
- Retrieved on: 2026-06-01
- Re-pin command:
  ```
  curl -fsSL -o internal/web/static/htmx.min.js \
    https://raw.githubusercontent.com/bigskysoftware/htmx/v2.0.4/dist/htmx.min.js
  shasum -a 256 internal/web/static/htmx.min.js
  ```

## tailwind.min.css

- Asset path: `internal/web/static/tailwind.min.css`
- URL served from: `/ui/static/tailwind.min.css`
- Build tool: `tailwindcss@3.4.1` (https://github.com/tailwindlabs/tailwindcss/releases/tag/v3.4.1)
- Build source: `internal/web/css/input.css` + `internal/web/tailwind.config.js`
- License: MIT — https://github.com/tailwindlabs/tailwindcss/blob/v3.4.1/LICENSE
- SHA-256: `a6e879d0792e9facec4c2314cc6be8c9baf7e99ba47442dae7304239124eef30`
- Retrieved on: 2026-06-01
- Re-build command:
  ```
  make build-tailwind
  ```
  Requires `npx` + node ≥ 18 on the developer machine. CI does NOT run
  the build; the committed output is what ships.

## Why this file is load-bearing

Closes spec §3.5 (Tailwind compiled at build time, committed; CI does
not run npx) + §3.7 (CSP allows only `'self'` for `style-src` +
`script-src`; vendored bytes are the only satisfier) + §8 #3 (CDN
compromise is N/A because vendored — the SRI verification at the
Makefile gate is the supply-chain anchor if the file is ever altered).

The Tailwind SHA-256 will change every time `make build-tailwind` re-runs
(e.g. after T4's templates land and unlock new utility classes). Update
this file in the same commit that re-runs the build. The htmx SHA-256
must NOT change until the version pin moves; mismatch fails CI.
