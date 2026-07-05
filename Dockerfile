# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.25
ARG BUN_VERSION=1.3.14

FROM oven/bun:${BUN_VERSION} AS bun

FROM golang:${GO_VERSION}-bookworm AS build
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        bc \
        ca-certificates \
        curl \
        gzip \
        make \
        zstd \
    && rm -rf /var/lib/apt/lists/*
COPY --from=bun /usr/local/bin/bun /usr/local/bin/bun

WORKDIR /src
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/root/.bun/install/cache \
    make build-release

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 consortium \
    && useradd --system --uid 10001 --gid consortium --home-dir /nonexistent --shell /usr/sbin/nologin consortium \
    && mkdir -p /data \
    && chown consortium:consortium /data

COPY --from=build /src/bin/consortium-release /usr/local/bin/consortium

ENV EMBED_FRONTEND=true \
    BIND_ADDR=0.0.0.0:8080 \
    DB_PATH=/data/consortium.db

USER 10001:10001
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/consortium"]
