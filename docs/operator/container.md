# Container runbook

Reader: operator running Regatta inside Docker against a target repo
mounted from the host.
Read time: 5 minutes.
Expires when: the runtime image base, env-var contract, or mount
layout changes.

## Status

Stage 1 of containerization. The image ships:

- a static `regatta` binary built from `cmd/regatta`,
- `git` + `github-cli` for the spawner's shell-outs,
- `nodejs` + `npm` with `@anthropic-ai/claude-code` pre-installed.

There is no published registry tag yet — operators build locally.
Stage 2 will publish to a registry and pin a digest.

## Build

```sh
docker build -t regatta:latest .
```

The build is a multi-stage layout: a `golang:1.25-alpine` builder
runs `go mod download` then `CGO_ENABLED=0 go build -trimpath`, and
the runtime stage starts from `alpine:3.20`. Expect ~250 MB final
image; the builder layer is discarded.

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

### Environment

| Var | Required | Source |
|---|---|---|
| `ANTHROPIC_API_KEY` | yes | Claude planner + L4 adapter both call `os.Getenv("ANTHROPIC_API_KEY")`. |
| `GH_TOKEN` | yes | inherited by the `gh` CLI when the spawner opens PRs. `repo` + `workflow` scopes. |
| `REGATTA_BRIEF_HMAC_KEYS` | no | brief-envelope HMAC keyring. Format `kid:secret[,kid:secret...]`. Single-tenant local dev can omit. |

A template lives in `.env.example` at the repo root.

## Verification

After `docker run`, confirm the toolchain inside the container:

```sh
docker exec regatta regatta version
docker exec regatta gh --version
docker exec regatta claude --version
```

`claude --version` reaching the binary confirms the npm install
succeeded; non-zero exit means the image is broken — rebuild before
launching agents.

To smoke-test Anthropic reachability without spawning a planner:

```sh
docker exec regatta sh -c \
  'wget -q -O- --header="x-api-key: $ANTHROPIC_API_KEY" \
     https://api.anthropic.com/v1/models | head -c 200'
```

A JSON response (any 2xx body) confirms egress + auth.

## Troubleshooting

### `claude: command not found` inside the container

The npm global bin is on `PATH` because `apk add npm` configures
`/usr/local/bin`. If you've rebuilt with a different base image and
this breaks, re-add `ENV PATH=/usr/local/lib/node_modules/.bin:$PATH`
before the `ENTRYPOINT` line.

### `regatta` exits immediately with no log

Check `/data` is writable — sqlite needs O_RDWR on the DB path. If
the named volume was created with a non-root owner, fix with:

```sh
docker run --rm -v regatta-data:/data alpine \
  chown -R 0:0 /data
```

### `gh: authentication required`

`GH_TOKEN` is missing or has wrong scopes. The container does not
attempt `gh auth login`; the env-var path is the only auth surface.

### Files in `/repo` end up owned by root on the host

Stage 1 runs the entrypoint as uid 0 inside the container. Bind-mount
writes therefore land as root-owned on the host, which trips `git`
on subsequent host-side commits. Workarounds until Stage 2 ships a
uid-mapping flag:

```sh
# After the container exits, reclaim ownership from the host:
sudo chown -R "$(id -u):$(id -g)" .
```

Or pass `--user "$(id -u):$(id -g)"` to `docker run` — note that
this disables npm/apk inside the container if you later `docker exec`
as the same user.

### Image rebuild after upstream `claude-code` release

The Dockerfile pins `@anthropic-ai/claude-code@latest`, so a rebuild
picks up the newest npm publish. For reproducible deployments, pin
an explicit version in the `RUN npm install -g ...` line before
shipping to operators.

## Related

- [`getting-started.md`](getting-started.md) — host-binary install path.
- [`install.md`](install.md) — pinned releases + provenance.
- [`configure.md`](configure.md) — `regatta.yaml` schema.
