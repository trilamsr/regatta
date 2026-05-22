# Third-party notices

This file enumerates third-party dependencies embedded in shipped
Regatta artifacts and their licenses. Regenerate via:

```bash
go install github.com/google/go-licenses@latest
go-licenses report ./... > NOTICES.md.next
```

Expires when: `go.mod` direct dependencies change.

## Direct dependencies

Populated by the release pipeline at tag time. Until the first
tagged release lands, treat the live `go.mod` as the source of
truth and run the regeneration command above to produce a snapshot
report.

## Procurement notes

- All direct dependencies are scanned at build time by the release
  workflow (see `.github/workflows/release.yml` once Wave 3 lands).
- Customer audit teams can request a current SBOM via the
  escalation path in `SECURITY.md`.
