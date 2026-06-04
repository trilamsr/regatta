# Container runbook

Reader: operator running Regatta inside Docker against a target repo
mounted from the host.
Read time: 5 minutes.
Expires when: the runtime image base, env-var contract, or mount
layout changes.

## Status

Stage 1 of containerization. The image ships:

- a static `regatta` binary built from `cmd/regatta`,
- `git` (Debian 12 binary + libpcre2-8 + libz copied into the image),
- `gh` v2.65.0 — static Go release binary,
- `claude` v2.1.161 — native binary extracted from
  `@anthropic-ai/claude-code` (the npm package now ships a single
  per-platform binary; no Node runtime needed at runtime).

Runtime base is `gcr.io/distroless/base-debian12:nonroot`. There is no
shell, no package manager, no `apk`/`apt`/`npm`. The entrypoint runs
as UID 65532 (`nonroot`). No published registry tag yet — operators
build locally. Stage 2 will publish to a registry and pin a digest.

## Security baseline

| dimension | Stage 1 distroless |
|---|---|
| Container user | `nonroot` (UID 65532) — non-zero, no shell entry |
| Shell present | no (`/bin/sh` does not exist) |
| Package manager | none — image is content-addressable, immutable |
| Writable paths | `/repo`, `/data`, `/tmp` only; `/usr` and `/lib` are read by the kernel but cannot be modified by the entrypoint user |
| CA certs | pre-baked in `distroless/base-debian12` |

A compromised agent prompt cannot rewrite `/usr/local/bin/claude` or
`/usr/local/bin/regatta` from inside the container: the UID-65532
entrypoint has no write permission on system paths and there is no
shell to chain into a privilege escalation. This closes the
load-bearing root-uid gap surfaced by the Stage 1 review (#518).

Per-arch image weight (single-platform tarball, `docker save | wc -c`):

| arch | distroless (this stage) | alpine (#518) | delta |
|---|---|---|---|
| arm64 | ~130 MB | ~438 MB | -308 MB |

The claude native binary (~241 MB after extraction) dominates the
remaining weight; the runtime base itself is ~8 MB.

## Build

```sh
docker build -t regatta:latest .
```

Multi-stage layout:

1. `golang:1.25-alpine` builder → `CGO_ENABLED=0 go build -trimpath`.
2. `debian:12-slim` tools builder → downloads `gh` static release,
   `npm install`s the pinned `@anthropic-ai/claude-code` so its
   postinstall resolves the native per-platform binary, stages a
   `/rootfs` overlay with git + git-core + the two glibc libraries
   (`libpcre2-8`, `libz`) that distroless does not ship.
3. `gcr.io/distroless/base-debian12:nonroot` runtime → two `COPY`
   layers (regatta binary + the `/rootfs` overlay).

Build args available for reproducibility pins: `GH_VERSION`,
`CLAUDE_VERSION` (current defaults: 2.65.0 and 2.1.161).

## Run

### Quickstart (env-file)

```sh
cp .env.example .env
$EDITOR .env                  # fill in ANTHROPIC_API_KEY + GH_TOKEN

docker volume create regatta-data

docker run --rm \
  --name regatta \
  --env-file .env \
  -v "$PWD":/repo \
  -v regatta-data:/data \
  -p 8080:8080 \
  regatta:latest
```

The default `CMD` runs `serve --repo /repo --db /data/regatta.db`.

### Mount strategy

| Mount | Type | Why |
|---|---|---|
| `/repo` | bind from host | target repo. Agents commit + push from here, so it must be writable and point at the real working tree. |
| `/data` | named volume | substrate sqlite DB lives here. A named volume survives `docker rm`; a bind would couple DB lifecycle to the host path. |

The container's `WORKDIR` is `/repo`; relative paths in operator
commands resolve against the bind-mounted repo.

The entrypoint runs as UID 65532. Bind-mount host directories must
either be world-writable or owned by UID 65532 / a group the host
shares with 65532.

OS scope: chown is load-bearing only on Linux container hosts. On
Docker Desktop (macOS / Windows) the VM's file-sharing layer
(gRPC-FUSE / virtiofs) translates ownership transparently — no chown
needed.

The simplest pattern on Linux:

```sh
sudo chown -R 65532:65532 ./path-to-repo
```

### Environment

| Var | Required | Source |
|---|---|---|
| `ANTHROPIC_API_KEY` | yes | Claude planner + L4 adapter both call `os.Getenv("ANTHROPIC_API_KEY")`. |
| `GH_TOKEN` | yes | inherited by the `gh` CLI when the spawner opens PRs. `repo` + `workflow` scopes. |
| `REGATTA_BRIEF_HMAC_KEYS` | no | brief-envelope HMAC keyring. Format `kid:secret[,kid:secret...]`. Single-tenant local dev can omit. |

A template lives in `.env.example` at the repo root.

## Verification

Distroless has no shell, so `docker exec regatta sh -c '...'` does not
work. Verify each tool by running it as the entrypoint:

```sh
docker run --rm regatta:latest version
docker run --rm --entrypoint /usr/local/bin/gh    regatta:latest --version
docker run --rm --entrypoint /usr/local/bin/claude regatta:latest --version
docker run --rm --entrypoint /usr/bin/git         regatta:latest --version
```

Each command should exit 0 and print a version line. UID assertion:

```sh
docker inspect regatta:latest --format '{{.Config.User}}'
# expected: 65532
```

To smoke-test Anthropic reachability without spawning a planner, run
`claude` non-interactively:

```sh
docker run --rm -e ANTHROPIC_API_KEY \
  --entrypoint /usr/local/bin/claude regatta:latest --version
```

Anthropic egress is exercised the moment the agent loop starts; there
is no separate wget shim in the image (and no shell to call one from).

## Troubleshooting

### `permission denied` writing to bind-mounted `/repo`

The entrypoint runs as UID 65532. Either chown the host directory
to 65532 (Linux) or rely on Docker Desktop's file-sharing translation
(macOS). Running `--user root` is not supported — distroless has no
shell to fall back to and several binaries refuse uid 0.

### `regatta` exits immediately with no log

Check `/data` is writable by UID 65532 — sqlite needs O_RDWR on the
DB path.

OS scope: root-owned named volumes are a Linux-host concern. Docker
Desktop (macOS / Windows) hosts named volumes inside the VM and the
share layer remaps ownership to the container uid transparently. If
you are on Linux and the named volume was created with root ownership,
fix it from a one-shot helper container that does have a shell:

```sh
docker run --rm -v regatta-data:/data alpine \
  chown -R 65532:65532 /data
```

### `gh: authentication required`

`GH_TOKEN` is missing or has wrong scopes. The container does not
attempt `gh auth login`; the env-var path is the only auth surface.

### Image rebuild after upstream `claude-code` release

`CLAUDE_VERSION` is a Dockerfile build-arg pinned to 2.1.161. Override
with `--build-arg CLAUDE_VERSION=<next>` and rebuild. Same pattern
for `GH_VERSION`.

## Related

- [`getting-started.md`](getting-started.md) — host-binary install path.
- [`install.md`](install.md) — pinned releases + provenance.
- [`configure.md`](configure.md) — `regatta.yaml` schema.
