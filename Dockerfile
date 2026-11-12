FROM --platform=$BUILDPLATFORM golang:1.26.11-alpine@sha256:ae52b5bc6ed045a9ed26ab03dc19327a802a9cc9aa8b27ed57f5c6b60579b0bb AS component-source-tools

FROM --platform=$BUILDPLATFORM node:alpine@sha256:3ad34ca6292aec4a91d8ddeb9229e29d9c2f689efd0dd242860889ac71842eba AS front-builder
COPY --from=component-source-tools /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}" GOWORK=off
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY frontend/ ./frontend/
COPY components/ ./components/
COPY go.mod go.sum go.extended.mod go.extended.sum ./
COPY deploy/dependencies/sing-quic-integration.json ./deploy/dependencies/
COPY deploy/dependencies/sing-box-ssm-integration.json ./deploy/dependencies/
COPY deploy/dependencies/sing-anytls-integration.json ./deploy/dependencies/
COPY scripts/core-source-provenance.mjs ./scripts/
COPY scripts/quic-integration-provenance.mjs ./scripts/
COPY scripts/ssm-integration-provenance.mjs ./scripts/
COPY scripts/anytls-integration-provenance.mjs ./scripts/
COPY scripts/component-source-inputs.mjs scripts/component-frontend-manifest.mjs scripts/extract-component-frontend.mjs scripts/generate-component-imports.mjs scripts/write-component-installed-metadata.mjs scripts/frontend-assets.mjs scripts/frontend-runtime-closure.mjs ./scripts/
RUN node scripts/quic-integration-provenance.mjs manifest > QUIC_INTEGRATION.json \
    && node scripts/ssm-integration-provenance.mjs manifest > SSM_INTEGRATION.json \
    && node scripts/anytls-integration-provenance.mjs manifest > ANYTLS_INTEGRATION.json \
    && cd frontend \
    && npm ci \
    && SOLOVEY_UI_PROFILE=full npm run build \
    && cd .. \
    && node scripts/extract-component-frontend.mjs --dist frontend/dist --components-dir components --out-dir component-packs --prune-dist \
    && node scripts/write-component-installed-metadata.mjs --components-dir component-packs --out component-packs/installed.json --profile full --binary full \
    && node scripts/frontend-assets.mjs publish --dist frontend/dist --destination web/html --components-dir component-packs \
    && node scripts/generate-component-imports.mjs --profile full --out generated/components_generated.go --cmd-out generated/optional_commands_generated.go

FROM --platform=$TARGETPLATFORM golang:1.26.11-alpine@sha256:ae52b5bc6ed045a9ed26ab03dc19327a802a9cc9aa8b27ed57f5c6b60579b0bb AS backend-builder
WORKDIR /app
ARG TARGETARCH
ARG TARGETVARIANT
ARG SUI_RELEASE_TRUST_ROOTS_B64
ARG CORE_PROVIDER=sing-box
ENV CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOARCH=$TARGETARCH CC=clang CXX=clang++
# The pinned musl archive requires LLVM's linker. The glibc shared library's
# loader and shutdown symbols are not compatible with this Alpine runtime.
RUN apk add --no-cache gcc clang lld musl-dev libc-dev make git wget bash nodejs ca-certificates
COPY . .
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN --mount=type=cache,id=solovey-ui-go-build,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,id=solovey-ui-go-mod,target=/go/pkg/mod,sharing=locked \
    set -euo pipefail; \
    node scripts/core-provider-build.mjs plan --provider "$CORE_PROVIDER" \
      --tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_musl,badlinkname,tfogo_checklinkname0,with_tailscale" > /tmp/core-provider-build.json; \
    MOD_FILE="$(node -p 'require("/tmp/core-provider-build.json").modFile')"; \
    case "$TARGETARCH" in \
      amd64) CRONET_SHA256="a1b6ac7a1f6448121eb9ac896a4af298a17472895bd18d6c3de072f5b67468e5" ;; \
      arm64) CRONET_SHA256="57b95e9c665cbd93caf4d411280404a8989f638869a6e1e86e180396c967a470" ;; \
      arm) CRONET_SHA256="baa866773e1c3b1086b0aa33172c84ac1320602f288be6eb0cf6a1aae9eaeb76" ;; \
      386) CRONET_SHA256="5465847964c21014e9ec4f1b52314b706987aecb3371110ffc4da49960cf085c" ;; \
      *) echo "unsupported target architecture" >&2; exit 1 ;; \
    esac; \
    CRONET_MODULE="github.com/sagernet/cronet-go/lib/linux_${TARGETARCH}_musl"; \
    go mod download -modfile="$MOD_FILE" "$CRONET_MODULE"; \
    go mod verify -modfile="$MOD_FILE"; \
    CRONET_DIR="$(go list -mod=readonly -modfile="$MOD_FILE" -m -f '{{.Dir}}' "$CRONET_MODULE")"; \
    echo "${CRONET_SHA256}  $CRONET_DIR/libcronet.a" | sha256sum -c -; \
    CRONET_VERSION="$(go list -mod=readonly -modfile="$MOD_FILE" -m -f '{{.Version}}' "$CRONET_MODULE")"; \
    printf '{"schema":"solovey.native-cronet/v1","linkage":"static-musl","nativeVersion":"150.0.7871.63","module":"%s","moduleVersion":"%s","archiveSha256":"%s"}\n' "$CRONET_MODULE" "$CRONET_VERSION" "$CRONET_SHA256" > CRONET_INTEGRATION.json
COPY --from=front-builder /app/web/html/ /app/web/html/
COPY --from=front-builder /app/generated/components_generated.go /app/app/components_generated.go
COPY --from=front-builder /app/generated/optional_commands_generated.go /app/cmd/optional_commands_generated.go
COPY --from=front-builder /app/QUIC_INTEGRATION.json /app/SSM_INTEGRATION.json /app/ANYTLS_INTEGRATION.json /app/integration-provenance/
RUN --mount=type=cache,id=solovey-ui-go-build,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,id=solovey-ui-go-mod,target=/go/pkg/mod,sharing=locked \
    set -euo pipefail; \
    if [ "$TARGETARCH" = "arm" ]; then export GOARM=7; [ "$TARGETVARIANT" = "v6" ] && export GOARM=6; fi; \
    readarray -t GO_ARGS < <(node -e 'console.log(require("/tmp/core-provider-build.json").goArgs.join("\n"))'); \
    BUILD_TAGS="$(node -p 'require("/tmp/core-provider-build.json").tags')"; \
    CGO_LDFLAGS_ALLOW='^-l:libcronet[.]a$' go build "${GO_ARGS[@]}" -trimpath -buildvcs=false \
    -ldflags="-buildid= -w -s -checklinkname=0 -linkmode external -extldflags '-fuse-ld=lld' -X github.com/MalenkiySolovey/solovey-ui/config/update.ReleaseTrustRootsBase64=$SUI_RELEASE_TRUST_ROOTS_B64" \
    -tags "$BUILD_TAGS" -o solovey-ui main.go; \
    node -e 'console.log(JSON.stringify(require("/tmp/core-provider-build.json").source))' > /tmp/core-provider-source.json; \
    CGO_ENABLED=0 go run -mod=readonly -modfile=go.mod ./cmd/solovey-artifact-info /app/solovey-ui \
      < /tmp/core-provider-source.json > CORE_PROVIDER.json; \
    if [ "$CORE_PROVIDER" = shtorm-extended ]; then \
      rm /app/integration-provenance/QUIC_INTEGRATION.json /app/integration-provenance/SSM_INTEGRATION.json; \
      node scripts/extended-integration-provenance.mjs manifest > /app/integration-provenance/EXTENDED_INTEGRATION.json; \
    fi

FROM alpine:latest@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4 AS runtime
ENV TZ=Europe/Moscow SUI_DB_FOLDER=/data SUI_COMPONENTS_INSTALLED_FILE=/app/components/installed.json
WORKDIR /app
RUN apk add --no-cache --upgrade ca-certificates gcompat libgcc \
    && mkdir -p /data /cert /app/components \
    && chown -R 65532:65532 /data /cert /app
COPY --from=backend-builder --chown=65532:65532 /app/solovey-ui /app/CRONET_INTEGRATION.json /app/CORE_PROVIDER.json /app/
COPY --from=front-builder --chown=65532:65532 /app/component-packs/ /app/components/
COPY --from=backend-builder --chown=65532:65532 /app/integration-provenance/ /app/
COPY --chown=65532:65532 entrypoint.sh /app/entrypoint.sh
RUN chmod 0555 /app/solovey-ui /app/entrypoint.sh
USER 65532:65532
ENTRYPOINT ["/app/entrypoint.sh"]

# CI runs the actual native client lifecycle on the final non-root image.
# This target is opt-in and never adds its probe to the default release image.
FROM backend-builder AS native-cronet-probe-builder
RUN --mount=type=cache,id=solovey-ui-go-build,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,id=solovey-ui-go-mod,target=/go/pkg/mod,sharing=locked \
    go build -mod=readonly -modfile="$(node -p 'require("/tmp/core-provider-build.json").modFile')" \
    -tags with_musl -ldflags="-w -s -linkmode external -extldflags '-fuse-ld=lld'" \
    -o /native-cronet-probe ./tests/installer/native-cronet

FROM runtime AS native-cronet-probe
COPY --from=native-cronet-probe-builder --chown=65532:65532 /native-cronet-probe /app/native-cronet-probe
ENTRYPOINT ["/app/native-cronet-probe"]

FROM runtime AS release
