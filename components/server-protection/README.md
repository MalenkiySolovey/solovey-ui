# Server Protection

`server-protection` is an optional, disabled-by-default Solovey UI component.
It inventories listeners, detects ownership collisions, records sanitized
public-surface observations, resolves endpoint-scoped protection decisions,
maintains bounded score and graylist metadata, and generates deterministic
endpoint-managed firewall and nginx stream-fronting previews. Mutating
workflows remain manual and require the release-matched restricted helper plus
exact Linux capability detection.

## Runtime model

- Generic host registries provide listener, inbound, fallback-site, health,
  and component lifecycle facts without importing this component.
- Profiles attach to stable resource identities. Listener or TLS changes
  produce fingerprint drift instead of silently changing policy.
- Public-surface observations are reduced to bounded classes before they reach
  the component. Request bodies, credentials, cookies, tokens, private paths,
  and raw SNI corpora are not stored.
- The socket graph keeps TCP/UDP and IPv4/IPv6 claims distinct, detects
  wildcard/exact collisions, and permits sharing only through an explicit
  adapter multiplexing contract. Unknown, stale, or ambiguous ownership blocks
  apply.
- The firewall generator owns only `table inet solovey_protection`. It renders
  exact endpoint matches, recovery/trusted-source exemptions, bounded timeout
  sets, conservative rate behavior, counters, and no global ruleset flush.
- A protection decision is never an applied action. Capability resolution may
  select a safer supported intent or observe-only; only exact helper
  verification can report a current applied action.
- A persisted operation journal provides process, instance, PID, revision, and
  lease fencing for manual system operations.
- Recovery revisions are immutable, SHA-256 verified, retained by bounded
  policy, and protected from pruning while an operation needs recovery.

## Nginx stream fronting

Preview, Sync, Apply, and Rollback are separate manual actions. Routes can use
only registry-backed loopback resources. SNI and ALPN values are static map
keys, never dynamic destinations. Raw nginx fragments, remote relays,
arbitrary addresses, optimistic PROXY protocol, and dynamic `proxy_pass`
targets are rejected. Unknown traffic is sent only to a registered local decoy
or a deterministic close path.

Only the component-managed nginx root is mutable. The helper accepts typed
requests to validate an exact candidate, install an immutable revision, switch
the controlled active reference, reload, verify exact process/listener facts,
or restore an exact previous revision. It does not accept arbitrary commands,
paths, flags, signals, service names, or raw configuration. User-managed nginx
configuration is never copied or edited.

Apply verifies the exact candidate revision and binary identity, checks the
master/workers/listeners, and runs bounded target health. Failed reload or
health enters the typed rollback workflow. A failed rollback retains the
fenced operation and a secret-free recovery bundle.

## Local Proxy Guard

Local Proxy Guard is an experimental, disabled-by-default control for an
already running, core-owned SOCKS, HTTP, or Mixed inbound. It never creates or
reconfigures a proxy, changes the system proxy, edits bind/port/credentials/TLS,
or chooses traffic destinations. Its only guard effect is a provider-owned
endpoint lease that fences conflicting inbound and TLS edits. The component
stores a secret-free projection of that lease.

Loopback is eligible with an explicit no-auth warning. Private-network exposure
requires configured authentication and retains an exposure warning. Public,
wildcard, unspecified, unknown, external, stale, ambiguous, and system-proxy
integrated cases fail closed. SOCKS5 and eligible SOCKS4/SOCKS4a, HTTP forward
and CONNECT, and all Mixed protocols require fresh exact loopback transactions.
SOCKS UDP association is dependent on its TCP control connection and is
diagnostic-only; it is not a static UDP listener claim.

Preview is read-only, Prepare only reserves authority, and Apply persists a
marker before fencing, health, revalidation, and activation. Disable or rollback
releases only the guard lease and leaves the inbound unchanged. Restart never
promotes a merely prepared reservation.

## Safety boundaries

- Unknown capability, ownership, inventory, SSH state, health, lock, helper,
  revision, or rollback facts fail closed.
- Port ownership transfer is explicit and cannot be triggered by an inbound
  edit.
- Unmanaged firewall rewrites, global firewall ownership, user nginx mutation,
  and node runtime behavior are outside this component.
- Disabling the component unregisters scopes, observations, tickers, workers,
  and host registry entries. Removing code does not implicitly remove stored
  data or mutate external state.
- Drop Data removes only component tables and refuses while an operation still
  requires rollback, provider authority, or ambiguous recovery unless the exact
  destructive confirmation contract is met.

System-level deployment and acceptance evidence is maintained outside the
production repository. Product claims are limited to the behavior implemented
and verified by the ordinary product test suites.
