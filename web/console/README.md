# regatta operator console (SvelteKit)

v5.1 operator console scaffold. Replaces the Wave 1 htmx prototype at
`internal/web/` per S1 phase-3 of the in-flight roadmap.

See `docs/engineer/specs/2026-06-08-svelte-console-ux-design.md` for the
design system, IA, palette, and font-pair decisions.

## Dev

```sh
make ui-dev      # vite dev server on :5173, proxies /api to :8080
```

Or directly:

```sh
cd web/console
npm install
npm run dev
```

## Build

```sh
make ui-build    # emits web/console/build/ for Go embed
```

Or directly:

```sh
cd web/console
npm run build
```

## Stack

- SvelteKit 2 + Svelte 5 (runes mode)
- TypeScript strict
- `@sveltejs/adapter-static` — emits SPA bundle for Go `embed.FS`
- shadcn-svelte components land under `src/lib/components/ui/` on demand
  (`npx shadcn-svelte@latest add <component>`)

## Out of scope for the scaffold

Backend wiring, auth, and component installs happen in subsequent slices —
this PR ships design system + bootstrap only.
