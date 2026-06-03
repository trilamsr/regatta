# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS regatta-builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -o /regatta ./cmd/regatta

FROM debian:12-slim AS tools-builder

ARG GH_VERSION=2.65.0
ARG CLAUDE_VERSION=2.1.161
ARG TARGETARCH

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl tar git libpcre2-8-0 zlib1g nodejs npm \
 && rm -rf /var/lib/apt/lists/*

# gh CLI — static Go binary, downloaded by arch.
RUN set -eux; \
    case "${TARGETARCH:-arm64}" in \
      amd64) GH_ARCH=amd64 ;; \
      arm64) GH_ARCH=arm64 ;; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/gh.tar.gz \
      "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${GH_ARCH}.tar.gz"; \
    tar -xzf /tmp/gh.tar.gz -C /tmp; \
    install -m 0755 "/tmp/gh_${GH_VERSION}_linux_${GH_ARCH}/bin/gh" /usr/local/bin/gh; \
    rm -rf /tmp/gh.tar.gz "/tmp/gh_${GH_VERSION}_linux_${GH_ARCH}"

# claude-code — npm install pulls the platform-specific subpackage via
# postinstall; the wrapper's bin/claude.exe ends up as the native binary
# matching the builder arch. Multi-arch builds rerun this stage per arch.
RUN npm install --prefix /opt/claude --no-audit --no-fund --no-progress \
      "@anthropic-ai/claude-code@${CLAUDE_VERSION}" \
 && install -m 0755 \
      /opt/claude/node_modules/@anthropic-ai/claude-code/bin/claude.exe \
      /usr/local/bin/claude \
 && rm -rf /opt/claude

# Sanity: every binary should exec on the build host kernel.
RUN /usr/local/bin/gh --version \
 && /usr/local/bin/claude --version \
 && /usr/bin/git --version

# Stage a /rootfs overlay with the exact files distroless needs. Locating
# libpcre2-8 and libz at runtime via dpkg keeps the COPY arch-agnostic
# — distroless has no dpkg, so we resolve the paths in this builder.
RUN set -eux; \
    mkdir -p /rootfs/usr/local/bin /rootfs/usr/bin /rootfs/usr/lib; \
    cp /usr/local/bin/gh /rootfs/usr/local/bin/gh; \
    cp /usr/local/bin/claude /rootfs/usr/local/bin/claude; \
    cp /usr/bin/git /rootfs/usr/bin/git; \
    cp -r /usr/lib/git-core /rootfs/usr/lib/git-core; \
    for lib in libpcre2-8.so.0 libz.so.1; do \
      src=$(dpkg -L libpcre2-8-0 zlib1g 2>/dev/null | grep "/${lib}$" | head -1); \
      [ -n "$src" ] || { echo "missing lib: $lib" >&2; exit 1; }; \
      libdir=$(dirname "$src"); \
      mkdir -p "/rootfs${libdir}"; \
      cp "$src" "/rootfs${libdir}/${lib}"; \
    done

FROM gcr.io/distroless/base-debian12:nonroot

COPY --from=regatta-builder /regatta /usr/local/bin/regatta
COPY --from=tools-builder /rootfs /

VOLUME ["/repo", "/data"]
WORKDIR /repo

ENTRYPOINT ["/usr/local/bin/regatta"]
CMD ["serve", "--repo", "/repo", "--db", "/data/regatta.db"]
