FROM --platform=$BUILDPLATFORM node:alpine@sha256:3ad34ca6292aec4a91d8ddeb9229e29d9c2f689efd0dd242860889ac71842eba AS front-builder
WORKDIR /app
COPY frontend/ ./
# npm ci (not install) so the image is built from the exact, audited
# package-lock.json. This matches CI/release and fails closed on lockfile drift.
RUN npm ci && npm run build

FROM --platform=$TARGETPLATFORM golang:1.26.4-alpine@sha256:7a3e50096189ad57c9f9f865e7e4aa8585ed1585248513dc5cda498e2f41812c AS backend-builder
WORKDIR /app
ARG TARGETARCH
ARG TARGETVARIANT
ARG CRONET_GO_VERSION=e7f6f6f5b7ce226f686f6cb5d068a63da6657ccd
ARG CRONET_GO_ASSET_TAG=v148.0.7778.96-1
ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-D_LARGEFILE64_SOURCE"
ENV GOARCH=$TARGETARCH

RUN apk update && apk add --no-cache \
    gcc \
    musl-dev \
    libc-dev \
    make \
    git \
    wget \
    bash \
    ca-certificates

ENV CC=gcc

# The naive outbound is loaded through cronet-go's purego path. Keep the native
# library pinned by release tag and per-arch sha256; never fetch releases/latest.
RUN set -e; \
    CRONET_ARCH="$TARGETARCH"; \
    case "$CRONET_ARCH" in \
      amd64) CRONET_SHA256="dc7293a929dffa695aae1a89555e7366158fa0a3f40bbe3012d445bc05c99672" ;; \
      arm64) CRONET_SHA256="1518e73270c7b49694592bc0448ba1033a80ff4084bfb92cfa5baacec627bd9f" ;; \
      arm)   CRONET_SHA256="40deac370a3257deff8d348382ce59a3948600e3d9f211215b0c453bab5d3657" ;; \
      386)   CRONET_SHA256="0ddbd9575ce8f5b39a13115e2b7d9f60d578d4fb1a84c7baca10d89f920392d0" ;; \
      *) echo "no pinned libcronet sha256 for arch ${CRONET_ARCH}" >&2; exit 1 ;; \
    esac; \
    CRONET_URL="https://github.com/SagerNet/cronet-go/releases/download/${CRONET_GO_ASSET_TAG}/libcronet-linux-${CRONET_ARCH}.so"; \
    echo "cronet-go source pin: ${CRONET_GO_VERSION}; pinned asset tag: ${CRONET_GO_ASSET_TAG}"; \
    echo "Downloading ${CRONET_URL}"; \
    wget -q -O ./libcronet.so "$CRONET_URL"; \
    echo "${CRONET_SHA256}  ./libcronet.so" | sha256sum -c -; \
    chmod 755 ./libcronet.so

COPY . .
COPY --from=front-builder /app/dist/ /app/web/html/

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Build inside the target platform container so CGO uses Alpine's native musl
# toolchain. The image keeps naive outbound via purego + prebuilt libcronet,
# avoiding a Chromium/cronet source build inside Docker.
RUN set -e; \
    if [ "$TARGETARCH" = "arm" ]; then export GOARM=7; [ "$TARGETVARIANT" = "v6" ] && export GOARM=6; fi; \
    go build -ldflags="-w -s -checklinkname=0" \
    -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,badlinkname,tfogo_checklinkname0,with_tailscale" \
    -o solovey-ui main.go

FROM alpine:latest@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4
# Match defaultValueMap["timeLocation"] in service settings.
ENV TZ=Europe/Moscow
WORKDIR /app
RUN set -ex && apk add --no-cache --upgrade bash tzdata ca-certificates nftables gcompat libgcc
COPY --from=backend-builder /app/solovey-ui /app/libcronet.so /app/
COPY entrypoint.sh /app/
RUN chmod +x /app/entrypoint.sh
ENTRYPOINT [ "./entrypoint.sh" ]
