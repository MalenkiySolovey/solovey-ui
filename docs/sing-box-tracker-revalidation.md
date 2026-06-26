# Sing-box Tracker Revalidation Policy

Validated dependency:

- `github.com/sagernet/sing-box v1.13.13`

> Note: upstream moved the `v1.13.13` tag after publishing. `go.mod` keeps
> `require v1.13.13` but `replace`s it with the fixed release commit
> (`v1.13.13-0.20260603083344-78b2e12fbdd8`, commit `78b2e12`) because the
> original tag commit cached by the Go proxy breaks Windows builds.

The local `ConnTracker` and `StatsTracker` wrap sing-box routed TCP and packet
connections. Any bump of `github.com/sagernet/sing-box` must revalidate this
contract before merge. `go test ./core` enforces that this document and
`core/tracker_policy.go` are updated when the sing-box version changes.

Required checks:

- RoutedConnection signature still matches sing-box adapter.RouterConnectionTracker
- RoutedPacketConnection signature still matches sing-box adapter.RouterConnectionTracker
- wrapped TCP connections always call Done exactly once on Close or terminal I/O error
- wrapped packet connections always call Done exactly once on Close or terminal I/O error
- Reset closes tracked connections and waits for active wrappers before replacing tracker state
- StatsTracker keeps counter pointers stable across Reset for already wrapped connections
- source IP extraction from adapter.InboundContext still uses metadata.Source.Addr

Validation gate for a sing-box bump:

- `go test ./core`
- `go test -race ./core`
- `go test -race ./service ./api`
- Manual smoke check: start core, create one TCP inbound and one UDP-capable
  inbound, confirm stats are collected, then restart core and confirm old
  wrapped connections do not keep changing new counters.

Revalidation log:

- 2026-06-15, v1.13.12 -> v1.13.13: synced with deposist/s-ui-x v1.5.8
  dependency pin. The tracker contract is unchanged according to upstream
  revalidation: routed TCP/packet connection signatures still match
  `adapter.RouterConnectionTracker`, `adapter.InboundContext.Source` remains a
  `Socksaddr`, and the local Done-once/reset/counter-pointer invariants are
  unchanged. Local verification still needs `go test ./core` on a Go-equipped
  environment.
