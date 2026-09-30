#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
docker info >/dev/null
build_root=$(mktemp -d)
trap 'rm -rf -- "$build_root"' EXIT
go test -c -o "$build_root/deployment.test" ./service/deployment
chmod 0755 "$build_root"
docker run --rm --read-only --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
 --tmpfs /tmp:rw,nosuid,nodev,noexec --tmpfs /data:rw,uid=65532,gid=65532,mode=0700 \
 --tmpfs /cert:rw,uid=65532,gid=65532 --tmpfs /run/solovey-ui:rw,uid=65532,gid=65532 \
 --mount "type=bind,source=$build_root,target=/probe,readonly" \
 --env SOLOVEY_TEST_DOCKER_PERSISTENCE=1 --env SOLOVEY_DEPLOYMENT_PROFILE=docker-host-unprivileged \
 --env SUI_DB_FOLDER=/data ubuntu:24.04 \
 /probe/deployment.test -test.v -test.timeout=45s -test.run='^TestDockerRuntimeDoctorPersistence$'
