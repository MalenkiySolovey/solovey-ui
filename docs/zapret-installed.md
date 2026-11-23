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
DoH3, DoQ, IPv6 and Extended are separate from this R8B2 baseline. See the
R8C admission requirements below.

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

Use the existing Strategy editor for ordered profiles and bounded native
filters. Under **Shared native lists**, create a HostList (domains) or an
IPSet (addresses or prefixes). Select it by ID in a profile's **Native
filters**, either to include or to exclude matches. Each filter uses inline
entries or one shared list, not both. These lists affect the selected
Strategy's native match only; they do not select an outbound or change DNS.

Editing a referenced list revalidates all its Strategies in the same catalog
transaction. A changed active Strategy is reapplied as a new revision; an
unused list edit does not restart the engine. Remove the references before
deleting a list. Strategy export includes the referenced lists; import rejects
an existing list ID with different contents rather than overwriting it.

The only QUIC blob is the original engine's pinned default; arbitrary host
paths, uploaded Lua and return-dependent Circular are not admitted.

The systemd retained deny table is not a procd activation contract, and the
current guard rejects fw4's foreign output and postrouting hooks.
OpenWrt/FriendlyWrt are not covered by the systemd profile.

## Optional scoped TUN ingress

TUN requires separate, administrator-owned provisioning. The normal installed
broker unit does not expose `/dev/net/tun`; the operator must make the device
available to that unit only. Panel and Core still have no network capabilities
or device-node access. A Linux build with gVisor and exactly one TUN inbound
named `svzap-tun0` is required. The inbound uses an explicit IPv4 address and
MTU, `stack: gvisor`, `dns_mode: disabled`, and no automatic routes or redirects.
Its tag, address and MTU must match the root-owned `tun.inbound` profile.

The profile's `tun.scope` names one stable ingress interface, one IPv4 client,
one destination prefix, a reserved route table, and a rule priority. The
operator provisions the exact ingress/source/destination lookup followed by a
terminal `prohibit`, after the standard `local` rule and before any competing
policy rule. The table must be empty outside an active generation. The Planner
validates the scope and the broker is the only device/route writer. It admits
only its nonpersistent TUN route and rejects conflicting hooks. The
operator must preserve the scope, reservation and terminal rule across restart;
firewall managers or privileged software that can replace them are not a
supported coexistence profile. No service changes the default route or
automatically enables IP forwarding.

Routing still chooses the outbound. A marked Zapret Direct shares the same
engine and queue as other protected flows; ordinary outbounds are not converted
to protected Directs by the TUN. Ingress and established return forwarding use
an exact device/index and IPv4 endpoint match for TCP/UDP only. The retained
deny blocks kernel forwarding or local delivery for the reserved ingress if
admission is lost, even if Core still holds its descriptor. The separate
one-shot descriptor endpoint is bound to the same Panel process and grant; the
framed broker transport still denies generic descriptor transfer. Add, edit,
or remove the managed TUN through a full Core generation. Remove it before the
last Zapret binding is switched OFF. Ordinary Direct remains available after
OFF; the administrator's independently provisioned ingress prohibit remains.

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
- `NewBox` still registers the pinned Core's existing default Direct. The
  source fingerprint changes because the inbound construction now passes a
  transient device opener to the one prepared TUN; the default outbound and
  Routing decision are unchanged.
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
  The optional TUN adds at most one joined listener to that same batch. It
  terminates after attempting the single attested handoff, or when the batch
  retires. An unexpected listener failure cancels the batch. After closing the
  admission table, the broker revokes the link before releasing its own FD.
- `startZapretEngine` has one waiter per actual child. It reaps the child and
  reports its result before closing `done`. Failed preparation and normal
  retirement use the same bounded pidfd-backed stop path.
