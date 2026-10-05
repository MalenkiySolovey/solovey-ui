# Go audit runner shards

`audit-go.yml` keeps ordinary Ubuntu tests in one job. Windows tests and each
OS's race suite use four independent GitHub-hosted runners. Every runner retains
`-count=1 -p 1 -timeout 30m`; race suites retain `-race` and their 60-minute job
limit. Tests and assertions are unchanged.

The shared `scripts/go-test.mjs` accepts one-based `--shard-index N
--shard-total M` before `-- <package patterns>`. Both coordinates are required,
with `1 <= N <= M <= 16`. Sharded invocations require explicit `-p 1` and
`-count=1`; invalid inputs fail before invoking Go. Existing invocations without
coordinates remain supported. Example:

```sh
make audit:test-go-race PS="pwsh -NoProfile -ExecutionPolicy Bypass" \
  GO_TEST_ARGS="--shard-index 2 --shard-total 4" \
  GO_TEST_NAME_SUFFIX="-shard-2-of-4"
```

Package membership comes exclusively from `go list`, including build tags and
the race build constraint when sharding. Sorted, deduplicated packages are
allocated longest first to the lightest shard, with lexical package ties and
lower shard index ties. Each selected list is sorted again before execution.
Linux then applies the existing ordinary/root capability routing and root
executor. An empty shard does not invoke Go; an empty universe fails.

`go-test-shard-weights.json` records platform/race package durations from the
linked successful audits. Windows race uses the mean of two complete isolated
runs to limit the influence of runner timing variation. Missing or newly added packages receive weight 1
and are still tested. Weights never select or exclude packages. To rebalance,
use package seconds from complete command logs and review per-shard job and
queue times as well: package seconds exclude build/setup/queue overhead. Keep
the mechanism deterministic and the runner isolation intact.

The exact legacy `test-go (windows-latest)` and `test-go-race (<os>)` checks
are always-run gates. Only a `success` matrix result passes; failure,
cancellation, skip and missing results fail. Gates also publish command JUnit
and logs so missing shard evidence cannot make the dashboard appear green.
Each shard has a unique artifact and result filename. The dashboard waits for
all shards and gates and preserves separate artifact directories with
`merge-multiple: false`. Its existing recursive aggregation shows a shard/gate
breakdown without overwriting evidence.

Focused validation:

```sh
node --test scripts/go-test.test.mjs
node --test scripts/audit/go-shards.test.mjs  # requires frontend's yaml dependency
node scripts/audit/validate-workflows.js
```

Performance acceptance requires successful remote suites, exact shard union,
unchanged race/root coverage, complete dashboard artifacts, and measured
wall-clock improvement including queue time. Predictions from weights alone
are not performance evidence.
