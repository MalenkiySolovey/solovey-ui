#!/bin/sh

set -eu

source_dir=${1:?source directory is required}
stage_dir=${2:?staging directory is required}
node_program=${3:?stage-bound Node program is required}

[ -d "$source_dir" ] || { echo "source directory is unavailable" >&2; exit 1; }
[ -d "$stage_dir" ] || { echo "staging directory is unavailable" >&2; exit 1; }
[ -f "$node_program" ] && [ -x "$node_program" ] || { echo "stage-bound Node program is unavailable" >&2; exit 1; }

"$node_program" "$source_dir/scripts/openwrt-stage-manifest.mjs" verify \
	--stage "$stage_dir" --source-root "$source_dir"
