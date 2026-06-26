FROM --platform=$BUILDPLATFORM node:alpine@sha256:3ad34ca6292aec4a91d8ddeb9229e29d9c2f689efd0dd242860889ac71842eba AS front-builder
WORKDIR /app
COPY frontend/ ./
# npm ci (not install) so the image is built from the exact, audited
# package-lock.json. This matches CI/release and fails closed on lockfile drift.
RUN npm ci && npm run build

FROM golang:1.26.4-alpine@sha256:7a3e50096189ad57c9f9f865e7e4aa8585ed1585248513dc5cda498e2f41812c AS backend-builder
WORKDIR /app
ARG TARGETARCH
ARG TARGETVARIANT
ARG CRONET_GO_VERSION=v148.0.7778.96-1
ARG CRONET_GO_COMMIT=e7f6f6f5b7ce226f686f6cb5d068a63da6657ccd
ARG CRONET_GO_REPO=https://github.com/sagernet/cronet-go.git
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
    unzip \
    bash \
    curl \
    ca-certificates \
    gnupg \
    python3 \
    xz

ENV CC=gcc

COPY . .
COPY --from=front-builder /app/dist/ /app/web/html/

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Build Docker images with the same naive outbound support as Linux release
# artifacts. cronet-go provides the static libcronet.a and musl toolchain env
# required by sing-box's with_naive_outbound build tag.
RUN set -e; \
    git init /tmp/cronet-go; \
    git -C /tmp/cronet-go remote add origin "${CRONET_GO_REPO}"; \
    git -C /tmp/cronet-go fetch --depth=1 origin "${CRONET_GO_VERSION}"; \
    git -C /tmp/cronet-go checkout FETCH_HEAD; \
    test "$(git -C /tmp/cronet-go rev-parse HEAD)" = "${CRONET_GO_COMMIT}"; \
    git -C /tmp/cronet-go submodule update --init --recursive --depth=1; \
    rm -f /tmp/cronet-go/naiveproxy/src/build/linux/sysroot_scripts/keyring.gpg; \
    (cd /tmp/cronet-go && GPG_TTY=/dev/null ./naiveproxy/src/build/linux/sysroot_scripts/generate_keyring.sh); \
    cronet_target="linux/${TARGETARCH}"; \
    cd /tmp/cronet-go; \
    go run ./cmd/build-naive --target="${cronet_target}" --libc=musl download-toolchain; \
    while IFS= read -r line; do \
        line="${line#export }"; \
        [[ -z "${line}" ]] && continue; \
        export "${line}"; \
    done < <(go run ./cmd/build-naive --target="${cronet_target}" --libc=musl env); \
    cd /app; \
    if [ "$TARGETARCH" = "arm" ]; then export GOARM=7; [ "$TARGETVARIANT" = "v6" ] && export GOARM=6; fi; \
    go build -ldflags="-w -s -checklinkname=0 -linkmode external -extldflags '-static'" \
    -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,badlinkname,tfogo_checklinkname0,with_tailscale,with_naive_outbound,with_musl" \
    -o solovey-ui main.go

FROM alpine:latest@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4
# Match defaultValueMap["timeLocation"] in service settings.
ENV TZ=Europe/Moscow
WORKDIR /app
RUN set -ex && apk add --no-cache --upgrade bash tzdata ca-certificates nftables
COPY --from=backend-builder /app/solovey-ui /app/
COPY entrypoint.sh /app/
RUN chmod +x /app/entrypoint.sh
ENTRYPOINT [ "./entrypoint.sh" ]
