# Install

Reader: customer-operator installing the Regatta binary.
Read time: 3 minutes.
Expires when: install paths change or v1 binary ships.

## Status

Regatta is pre-implementation. Install paths described here become
load-bearing at the v1 tag; until then, treat them as the contract
the release pipeline will satisfy.

## Supported platforms

- macOS (arm64, x86_64) via Homebrew or `go install`.
- Linux (arm64, x86_64) via `go install` or the tarball.
- Windows via `go install` only; no Homebrew formula planned pre-v1.

## Paths

### Homebrew (macOS, Linux)

```sh
brew install trilamsr/regatta/regatta
```

### Go install

```sh
go install github.com/trilamsr/regatta/cmd/regatta@latest
```

The binary lands in `$GOPATH/bin` (default `$HOME/go/bin`). Add it
to `PATH` if not already present.

### Pinned version

```sh
go install github.com/trilamsr/regatta/cmd/regatta@v0.X.Y
```

Pin to a specific tag for reproducibility; airgapped envs should
mirror the tag via internal Go module proxy.

## Verification

```sh
regatta version
```

Output includes binary version, commit SHA, and provenance
attestation reference. Compare against the release notes for the
tag you installed.

## Build from source (audit posture)

```sh
git clone https://github.com/trilamsr/regatta
cd regatta
make check     # local <60s gate
make ci        # full CI gate
go build -trimpath -o regatta ./cmd/regatta
```

Reproducibility flags + diff-against-published-artifact recipe live
in `../auditor/reproducibility.md`;
that doc is canonical for supply-chain posture.

## Uninstall

```sh
brew uninstall regatta            # if installed via brew
# or:
rm "$(go env GOPATH)/bin/regatta"
```

No state is left under `$HOME`. Per-deployment state (`regatta.db`,
worktrees, audit-sink credentials) lives in the deployment dir, not
the binary's install path.
