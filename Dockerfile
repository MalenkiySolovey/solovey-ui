FROM --platform=$BUILDPLATFORM node:alpine@sha256:3ad34ca6292aec4a91d8ddeb9229e29d9c2f689efd0dd242860889ac71842eba AS front-builder
WORKDIR /app
COPY frontend/ ./frontend/
COPY components/ ./components/
COPY scripts/component-frontend-manifest.mjs scripts/extract-component-frontend.mjs scripts/generate-component-imports.mjs scripts/write-component-installed-metadata.mjs ./scripts/
RUN cd frontend \
    && npm ci \
    && SOLOVEY_UI_PROFILE=full npm run build \
    && cd .. \
    && node scripts/extract-component-frontend.mjs --dist frontend/dist --components-dir components --out-dir component-packs --prune-dist \
    && node scripts/write-component-installed-metadata.mjs --components-dir component-packs --out component-packs/installed.json --profile full --binary full \
    && node scripts/generate-component-imports.mjs --profile full --out generated/components_generated.go --cmd-out generated/optional_commands_generated.go

FROM --platform=$TARGETPLATFORM golang:1.26.4-alpine@sha256:7a3e50096189ad57c9f9f865e7e4aa8585ed1585248513dc5cda498e2f41812c AS backend-builder
WORKDIR /app
ARG TARGETARCH
ARG TARGETVARIANT
ARG CRONET_GO_VERSION=e7f6f6f5b7ce226f686f6cb5d068a63da6657ccd
ARG CRONET_GO_ASSET_TAG=v148.0.7778.96-1
ENV CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOARCH=$TARGETARCH CC=gcc
RUN apk add --no-cache gcc musl-dev libc-dev make git wget bash ca-certificates
RUN --mount=type=cache,id=solovey-ui-go-build,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,id=solovey-ui-go-mod,target=/go/pkg/mod,sharing=locked \
    set -e; \
    case "$TARGETARCH" in \
      amd64) CRONET_SHA256="dc7293a929dffa695aae1a89555e7366158fa0a3f40bbe3012d445bc05c99672" ;; \
      arm64) CRONET_SHA256="1518e73270c7b49694592bc0448ba1033a80ff4084bfb92cfa5baacec627bd9f" ;; \
      arm) CRONET_SHA256="40deac370a3257deff8d348382ce59a3948600e3d9f211215b0c453bab5d3657" ;; \
      386) CRONET_SHA256="0ddbd9575ce8f5b39a13115e2b7d9f60d578d4fb1a84c7baca10d89f920392d0" ;; \
      *) echo "unsupported target architecture" >&2; exit 1 ;; \
    esac; \
    CRONET_URL="https://github.com/SagerNet/cronet-go/releases/download/${CRONET_GO_ASSET_TAG}/libcronet-linux-${TARGETARCH}.so"; \
    wget -q -O ./libcronet.so "$CRONET_URL"; \
    echo "${CRONET_SHA256}  ./libcronet.so" | sha256sum -c -; \
    chmod 755 ./libcronet.so
COPY . .
COPY --from=front-builder /app/frontend/dist/ /app/web/html/
COPY --from=front-builder /app/generated/components_generated.go /app/app/components_generated.go
COPY --from=front-builder /app/generated/optional_commands_generated.go /app/cmd/optional_commands_generated.go
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN set -e; \
    if [ "$TARGETARCH" = "arm" ]; then export GOARM=7; [ "$TARGETVARIANT" = "v6" ] && export GOARM=6; fi; \
    go build -ldflags="-w -s -checklinkname=0" \
    -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,badlinkname,tfogo_checklinkname0,with_tailscale" \
    -o solovey-ui main.go

FROM alpine:latest@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4
ENV TZ=Europe/Moscow SUI_DB_FOLDER=/data SUI_COMPONENTS_INSTALLED_FILE=/app/components/installed.json
WORKDIR /app
RUN apk add --no-cache --upgrade ca-certificates gcompat libgcc \
    && mkdir -p /data /cert /app/components \
    && chown -R 65532:65532 /data /cert /app
COPY --from=backend-builder --chown=65532:65532 /app/solovey-ui /app/libcronet.so /app/
COPY --from=front-builder --chown=65532:65532 /app/component-packs/ /app/components/
COPY --chown=65532:65532 entrypoint.sh /app/entrypoint.sh
RUN chmod 0555 /app/solovey-ui /app/entrypoint.sh /app/libcronet.so
USER 65532:65532
ENTRYPOINT ["/app/entrypoint.sh"]
