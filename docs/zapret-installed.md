# Installed Zapret ownership

## Multi-Direct and DNS (R8B2 baseline)

The qualified runtime is an installed Linux systemd Official/Full profile for
IPv4/TCP. Up to three explicit Directs share one queue and one pinned `nfqws2`.
They can use the same strategy or different, mark-isolated strategies. The
Panel and Core remain unprivileged. A shared engine failure denies all admitted
bindings; ordinary outbounds and the management path are independent.

Edit a Direct's binding in its Dial options and manage reusable profiles under
Zapret. Installed managed strategies require explicit administrator delegation
(`allowManagedStrategies` on the root-owned profile). The editor accepts the
bounded TCP profile syntax and immutable built-in Lua asset references, not
arbitrary scripts or host paths. Strategy catalog text and binding intent are
portable; installed admission, socket grants and network resources are not.

For DoH over HTTPS/TCP or DoT, select the bound Direct as **Transport via**.
Keep certificate verification enabled and provide a nonrecursive bootstrap
resolver for hostname endpoints. Configure the strategy for the destination TCP
port. Before changing or removing a referenced Direct, update its DNS detours.
DoH3, DoQ, IPv6 and installed Extended/OpenWrt are outside this qualification.

## R8C transport admission

The Linux systemd broker can stage ordered TCP and UDP profiles against a
single queue and engine. It matches the configured destination ports separately
for IPv4 and IPv6. The root-owned installed profile must opt in with
`"ipv6": true` and have a qualified IPv6 route on the same nominated egress.
An explicitly IPv6-only Strategy is rejected if the profile cannot admit it.

DoQ and DoH3 use the existing DNS **Transport via** setting and require a Core
build with QUIC. Configure a native QUIC action for the DNS port in every
admitted endpoint family. A conditional list match alone is not enough, and an
earlier matching pass-through profile can prevent coverage. Keep authentication
and explicit nonrecursive hostname bootstrap enabled. The broker rejects an
uncovered DNS detour before replacing the current batch.

Use the existing Strategy editor for ordered profiles and bounded inline host
and IP filters. The only QUIC blob is the original engine's pinned default;
arbitrary host paths, uploaded Lua and return-dependent Circular are not admitted.

TUN is rejected for this installed profile. The capless service does not expose
`/dev/net/tun`, and opening the device in a capless test context does not grant
`TUNSETIFF`. Adopting a Planner-owned device would require a narrowly scoped
broker FD handoff and explicit route ownership, not changes to the Panel's
effective capabilities. The systemd retained deny table is not a procd
activation contract, and the current guard rejects fw4's foreign output and
postrouting hooks. OpenWrt/FriendlyWrt are not covered by the systemd profile.

## Lifecycle ownership

The Outbounds owner validates each explicit Direct binding and renders the
candidate from one database view. It sends the complete batch to the broker.
The component resolves ordered profiles, the Network Planner allocates marks,
and the broker publishes the batch only after it can serve every member.
Neither the resolver nor the broker chooses a replacement Direct on failure.

The architecture budget entries for the batch are reviewed individually:

- `resolveZapretStrategies` uses the component's binding resolver for each
  pre-checked Direct. The Direct type is an assertion about the attached
  outbound, not a routing default. R8C adds the resolver's family-specific
  packet scope; the existing Network Planner still allocates the resources.
- `validateEncryptedDNSDetours` extends the DNS owner's existing detour
  check to DoQ and DoH3. It validates the explicit transport and bootstrap
  reference; it does not select an outbound or provide a fallback resolver.
- `installedZapret.prepare` owns one batch worker and one watcher, started
  only after all endpoints and the owned table are ready. The watcher exits
  with the batch authority. At most three listeners are joined; losing any
  listener or the engine cancels the batch. Closing the admission table and
  joining the listeners precede engine retirement. PID 1 retains the deny
  table until the broker can prove all previously marked sockets have drained.
  The R8C worker and watcher use the existing peer liveness check on a
  duplicated close-on-exec pidfd. The batch owns the duplicate and closes it
  only after the watcher and listeners have exited; preparation failure
  closes it before returning. An interrupted poll is retried with the same
  descriptor; an unobservable or dead peer still cancels admission.
- `startZapretEngine` has one waiter per actual child. It reaps the child and
  reports its result before closing `done`. Failed preparation and normal
  retirement use the same bounded pidfd-backed stop path.
