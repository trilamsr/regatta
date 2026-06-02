# syntax=docker/dockerfile:1.7
# Multi-stage build for regatta. Follows the official Go Docker pattern:
# https://docs.docker.com/language/golang/build-images/

# Stage 1 builder
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Module download cached as its own layer so source edits don't refetch deps.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO off keeps the binary static so the alpine runtime needs no libc shim.
# -trimpath strips local build paths for reproducibility (matches `make ci`).
ENV CGO_ENABLED=0
RUN go build -trimpath -o /regatta ./cmd/regatta

# Stage 2 runtime
FROM alpine:3.20

# git + github-cli — the spawner shells to both.
# nodejs + npm — claude-code is distributed via npm.
# ca-certificates — TLS to api.anthropic.com + api.github.com.
RUN apk add --no-cache \
      ca-certificates \
      git \
      github-cli \
      nodejs \
      npm \
 && npm install -g @anthropic-ai/claude-code@latest \
 && npm cache clean --force

COPY --from=builder /regatta /usr/local/bin/regatta

# /repo: bind-mount of the target repo (read-write — agents open PRs).
# /data: named volume for the substrate DB so state survives container restart.
VOLUME ["/repo", "/data"]

WORKDIR /repo

ENTRYPOINT ["regatta"]
CMD ["serve", "--repo", "/repo", "--db", "/data/regatta.db"]
