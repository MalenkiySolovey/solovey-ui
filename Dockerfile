FROM --platform=$BUILDPLATFORM node:alpine@sha256:3ad34ca6292aec4a91d8ddeb9229e29d9c2f689efd0dd242860889ac71842eba AS front-builder
WORKDIR /app
COPY frontend/ ./frontend/
COPY components/ ./components/
COPY scripts/component-frontend-manifest.mjs scripts/extract-component-frontend.mjs scripts/generate-component-imports.mjs scripts/write-component-installed-metadata.mjs scripts/frontend-assets.mjs scripts/frontend-runtime-closure.mjs ./scripts/
RUN cd frontend \
    && npm ci \
    && SOLOVEY_UI_PROFILE=full npm run build \
    && cd .. \
    && node scripts/extract-component-frontend.mjs --dist frontend/dist --components-dir components --out-dir component-packs --prune-dist \
    && node scripts/write-component-installed-metadata.mjs --components-dir component-packs --out component-packs/installed.json --profile full --binary full \
    && node scripts/frontend-assets.mjs publish --dist frontend/dist --destination web/html --components-dir component-packs \
    && node scripts/generate-component-imports.mjs --profile full --out generated/components_generated.go --cmd-out generated/optional_commands_generated.go

FROM --platform=$TARGETPLATFORM golang:1.26.6-alpine@sha256:af8d6740070b8906d12eae1c3e3ea0957fb63f492051ea05e354c38ef9fe88df AS backend-builder
WORKDIR /app
ARG TARGETARCH
ARG TARGETVARIANT
ARG SUI_RELEASE_TRUST_ROOTS_B64
ENV CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOARCH=$TARGETARCH CC=gcc
RUN apk add --no-cache gcc musl-dev libc-dev make git wget bash ca-certificates
COPY . .
RUN --mount=type=cache,id=solovey-ui-go-build,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,id=solovey-ui-go-mod,target=/go/pkg/mod,sharing=locked \
    set -e; \
    case "$TARGETARCH" in \
      amd64) CRONET_SHA256="23109c55b08829bb1a68682a61a64852b1bbf243753b153cbd8f6e4ea2c05294" ;; \
      arm64) CRONET_SHA256="3c1fcfcb56261a4b318ffb6d2227b9726782edb5a894e74db2b534406db62498" ;; \
      arm) CRONET_SHA256="e74d958fec2564e5a62b57b21feb01f4fb1eae8c3efbae2cbbf4b75b2967d165" ;; \
      386) CRONET_SHA256="c6348c93a339d92da3bb4c90a213279171ee0862f7116e5e39f0b8d09159bb9c" ;; \
      *) echo "unsupported target architecture" >&2; exit 1 ;; \
    esac; \
    CRONET_MODULE="github.com/sagernet/cronet-go/lib/linux_${TARGETARCH}"; \
    go mod download "$CRONET_MODULE"; \
    go mod verify; \
    CRONET_DIR="$(go list -m -f '{{.Dir}}' "$CRONET_MODULE")"; \
    cp "$CRONET_DIR/libcronet.so" ./libcronet.so; \
    echo "${CRONET_SHA256}  ./libcronet.so" | sha256sum -c -; \
    chmod 755 ./libcronet.so
COPY --from=front-builder /app/web/html/ /app/web/html/
COPY --from=front-builder /app/generated/components_generated.go /app/app/components_generated.go
COPY --from=front-builder /app/generated/optional_commands_generated.go /app/cmd/optional_commands_generated.go
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN --mount=type=cache,id=solovey-ui-go-build,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,id=solovey-ui-go-mod,target=/go/pkg/mod,sharing=locked \
    set -e; \
    if [ "$TARGETARCH" = "arm" ]; then export GOARM=7; [ "$TARGETVARIANT" = "v6" ] && export GOARM=6; fi; \
    go build -ldflags="-w -s -checklinkname=0 -X github.com/MalenkiySolovey/solovey-ui/config/update.ReleaseTrustRootsBase64=$SUI_RELEASE_TRUST_ROOTS_B64" \
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
