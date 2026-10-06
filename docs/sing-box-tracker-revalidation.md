# Sing-box Tracker Revalidation Policy

Validated dependency:

- `github.com/sagernet/sing-box v1.13.18`
- `github.com/sagernet/sing v0.8.13` (official core minimum: `v0.8.12`)

The local `ConnTracker` and `StatsTracker` wrap sing-box routed TCP and packet
connections. Any bump of `github.com/sagernet/sing-box` must revalidate this
contract before merge. `core/tracker/policy_support_test.go` owns the validated
version and checklist. `go test ./core/tracker` rejects a dependency version
that has not been revalidated. Keep this document with that existing owner.

Required checks:

- RoutedConnection signature still matches sing-box adapter.ConnectionTracker
- RoutedPacketConnection signature still matches sing-box adapter.ConnectionTracker
- wrapped TCP connections always call Done exactly once on Close or terminal I/O error
- wrapped packet connections always call Done exactly once on Close or terminal I/O error
- ConnTracker.Reset fences the old map/wait-group generation before closing and draining its wrappers
- StatsTracker keeps counter pointers stable across Reset for already wrapped connections
- source IP extraction from adapter.InboundContext still uses metadata.Source.Addr
- atomic IP admission runs exactly once before counters or wrappers in both TCP and packet paths
- a core restart creates new stats/connection owners; late old I/O cannot change new counters or tracking
- runtime health and stats projections belong to the current core generation

Validation gate for a sing-box bump:

- `go mod verify` and `go mod tidy -diff`
- `go test -count=1 ./core/... ./service/... ./api/...`
- `go test -race -count=1 ./core/tracker ./core/runtime ./service ./api`
- current tagged capability/configuration and full/core platform build gates
- Manual smoke check: start core, create one TCP inbound and one UDP-capable
  inbound, confirm stats are collected, then restart core and confirm old
  wrapped connections do not keep changing new counters.

`StatsTracker.Reset` is intentionally different from a core restart: it zeros
existing atomic counters in place, preserving pointers held by wrappers. A
subsequent `core/runtime.Core.Start` constructs a new box with fresh trackers.
`TestCoreRestartFencesLateTrackerIOFromNewGeneration` holds an admitted TCP or
packet completion across actual Stop/Start, proves the old counters are exercised,
and verifies independent new traffic and connection tracking. Terminal I/O,
timeouts and concurrent repeated Close are covered in `core/tracker/terminal_test.go`.
These host regressions do not replace the required deployment smoke.

Revalidation log:

- 2026-10-06, v1.13.14 -> v1.13.18: reviewed official immutable source
  `45ca32dcb966f07f97fc888fe8586e359dbe8405`. Both methods of
  `adapter.ConnectionTracker` and `adapter.InboundContext.Source` remain
  compatible; the latter still uses `Socksaddr.Addr`. Current-owner Windows
  tests passed for tracker admission/lifetime, actual core restart generations,
  health and the exact module identity. Runtime identity now binds the official
  .18 module checksum/source revision; stronger existing security pins remain.
  Sing .8.13 is selected separately for source-verified UDP/cancellation and
  untrusted-length/succinct-set fixes. The full gate includes native Linux race,
  supported platform/profile linking and current security analysis before merge;
  bounded deployed restart/IP smoke remains required separately.

- 2026-06-15, v1.13.12 -> v1.13.13: synced with deposist/s-ui-x v1.5.8
  dependency pin. The tracker contract is unchanged according to upstream
  revalidation: routed TCP/packet connection signatures still match
  `adapter.RouterConnectionTracker`, `adapter.InboundContext.Source` remains a
  `Socksaddr`, and the local Done-once/reset/counter-pointer invariants are
  unchanged. Local verification still needs `go test ./core` on a Go-equipped
  environment.
- 2026-07-02, v1.13.13 -> v1.13.14: synced the release dependency update after
  reviewing alireza0/s-ui v1.5.1 security fixes. The `adapter.RouterConnectionTracker`
  signatures for routed TCP and packet connections are unchanged, route tracking
  still passes the selected rule and outbound through the same arguments, and
  `adapter.InboundContext.Source.Addr` is still the source IP boundary used by
  local stats. Local non-race verification passed with
  `go test -count=1 ./core/tracker`; the local Windows race gate did not reach
  test execution because Zig/LLD could not link the Go race runtime against
  `WaitOnAddress` / `WakeByAddress*`.
