# Installed Zapret ownership

## Multi-Direct and DNS (R8B2 baseline)

The qualified runtime is an installed Linux systemd Official/Full profile for
IPv4/TCP. Explicit Directs share one queue and one pinned `nfqws2`.
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

The Direct editor preserves `bind_interface` and explicit IPv4/IPv6 source
addresses. The interface must match the installed egress, and any selected
source must be an assigned unicast address on that interface. OFF leaves
these ordinary Dial settings in place. A mismatch rejects the edit rather
than selecting another source or replacing the live generation.

Plain TCP and UDP DNS can use the same **Transport via** selector. Cover
the resolver port and endpoint family; UDP requires both UDP and TCP on
that port because a truncated reply is retried over TCP. The DNS editor's
applied state describes the selected binding, not a native packet match.

DoQ and DoH3 use the existing DNS **Transport via** setting and require a Core
build with QUIC. Configure a native QUIC action for the DNS port in every
admitted endpoint family. A conditional list match alone is not enough, and an
earlier matching pass-through profile can prevent coverage. Keep authentication
and explicit nonrecursive hostname bootstrap enabled. The broker rejects an
uncovered DNS detour before replacing the current batch.

Resolver endpoints on the same Direct with the same port, address family and
transport coverage share one admission requirement; their DNS settings stay
independent. Coverage checks are bounded by the broker request frame, not a
resolver count. They do not allocate additional rules, queues or descriptors.
A refusal identifies the uncovered port, family and required transport.

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

The QUIC action defaults to the original engine's pinned blob. An operator
may add `"allowCustomBlobs": true` to the root-owned installed profile to
admit bounded catalog QUIC decoys, in addition to the existing delegation
required for editor-managed strategies. The editor stores hex bytes under a
stable ID, not a path or Lua variable. Asset edits re-seal the referencing
strategies; the broker reads the permission on admission of each generation
and denies those strategies without it. Only the built-in blob qualifies as QUIC DNS
coverage. Do not put secrets in an asset: these bytes are transmitted as
configured. Host file paths are never admitted. Bounded native
Circular requires the installed selected TCP return-path capability.

### Managed Lua execution

In the Strategy editor, select the exact Lua source revisions in dependency
order and reference a declared callback in the native Strategy. Saving or
importing source never applies it. Imported libraries require a provenance
note and explicit approval for the selected revision. Strategy ZIP/JSON does
not carry that approval to a new import.

The root-owned installed Host profile must separately set
`"allowManagedLua": true` along with its managed-strategy delegation. The
broker seals the selected sources in an inherited descriptor and checks their
initializers using the pinned nfqws2 engine before replacing an active plan.
An invalid source or missing dependency is refused without retiring the
active generation. No changes to Panel or Core privileges are required.

Managed callbacks receive selected packet data and private per-flow Lua state.
They may pass or drop; they cannot write back the packet, send arbitrary
packets, or obtain filesystem, shell or package-loader access through the
managed API. The native engine still has its normal network capabilities;
this contract is not a host-wide sandbox for untrusted code. Authorize only
reviewed code, which shares CPU and memory with the engine serving the other
bindings. Existing pinned native actions are unchanged.

Set `"observeNativeProfiles": true` in the root-owned installed profile to
record native profile matches for the next engine generation. Refresh the
Runtime view to see the profile IDs that ran. The engine reports each
matched profile at most once per binding per generation; a match does not
report a packet verdict or prove that an action changed traffic. No packet
contents, destinations or debug output are logged. This is off by default and
adds no work when there is no active engine.

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

The Outbounds owner validates each explicit egress binding and renders the
candidate from one database view. It sends the complete batch to the broker.
The component resolves ordered profiles, the Network Planner allocates marks,
and the broker publishes the batch only after it can serve every member.
Neither the resolver nor the broker chooses a replacement outbound on failure.

There is no fixed binding count. The framed request and root-owned
profile have byte limits. The broker accounts for open descriptors, one
listener and bounded handoff workers per binding against `RLIMIT_NOFILE`.
The Planner allocates one distinct, non-reused mark serial per binding.
The complete nftables transaction must fit one netlink datagram; each
admitted family and port adds selectors to that transaction. The Component
compiles one bounded native argument list and, if enabled, allocates trace
slots only for its compiled profiles. The limiting resource can depend on
the chosen profiles and host. Edit validation previews mark slots and checks
the complete arguments and rule transaction without consuming a mark,
starting an engine, or replacing the live rules. Activation checks again
against its own allocation. Reduce port/profile scope or disable a binding
if the reported budget cannot fit; no partial batch is installed.

Activation preflight also checks available Host memory and the tightest
applicable cgroup v2 hard limit (`memory.max`) in the broker's ancestry.
It separately reports insufficient headroom before throttling (`memory.high`),
which is not a kernel allocation limit. The candidate needs space for its
arguments, selected Lua bundle and loader allowance, the pinned NFQUEUE receive
buffer, and an engine working set. Unselected catalog entries are not charged.
For a replacement, the working-set
estimate incorporates the active engine's observed peak resident memory while
that engine is still running. A replacement that cannot fit even before
accounting for its arguments is rejected before the more expensive profile and
rule validation. A candidate that clears that check is measured again with its
compiled arguments before a configuration save retires the active generation.
Editing an inactive catalog entry does not reserve runtime resources. This snapshot is
not a throughput forecast or a guarantee against memory or CPU contention
after admission. It does not replace the kernel queue limit or the retained
deny and does not route protected traffic through a fallback.

The architecture budget entries for the batch are reviewed individually:

- `resolveZapretStrategies` uses the component's binding resolver for each
  pre-checked Direct. The Direct type is an assertion about the attached
  outbound, not a routing default. R8C adds the resolver's family-specific
  packet scope; the existing Network Planner still allocates the resources.
  The blob-aware resolver adds only an aggregate capability requirement from
  the already-verified definitions. The root-owned broker profile authorizes
  that requirement, without changing Direct selection or packet scope.
  Native profile evidence compiles the same verified bindings and marks with
  an optional observation callback. Its private sink belongs to the installed
  engine generation; only the existing verified observation verb reads it.
- `validateDialedDNSDetours` applies the existing explicit detour check to
  all six dialed remote DNS transports. It does not select an outbound or
  provide a fallback resolver; encrypted bootstrap checks remain.
- `NewBox` still registers the pinned Core's existing default Direct. The
  source fingerprint changes because the inbound construction now passes a
  transient device opener to the one prepared TUN; the default outbound and
  Routing decision are unchanged.
- `installedZapret.prepare` owns one batch worker and one watcher, started
  only after all endpoints and the owned table are ready. The watcher exits
  with the batch authority. Each admitted listener is joined; losing any
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

## R8D and R8E compatibility inventory

This inventory uses Product's `core/registry/{outbound,endpoint,dns}.go`
declarations and the actual pinned option/constructor sources: Official
`8b914e4be5103d5f343dac94f26c3ebeeb226ffc` and Extended
`493b451cdac67dd69c6c9027729e242674ba81e5`. Ordinary Core availability still
depends on provider, compiled features and platform. Lack of Zapret admission
does not make the corresponding Core feature unavailable.

| Product egress class | Disposition and owner |
| --- | --- |
| Direct, both families | **Qualified** for admitted TCP/UDP, including owned source addresses and compatible explicit interface. ICMP is **not applicable** to this TCP/UDP binder. In both providers, `protocol/direct/outbound.go` rejects a Direct's own Dial detour. |
| Inclusive TCP/UDP port ranges | The editor, native compiler and Host Planner carry positive ranges without enumerating ports. The installed nftables classifier includes both endpoints. For a protected Direct, a marked socket outside all admitted scopes is denied, not sent unprotected. Unselected Outbounds retain their ordinary behavior. |
| Fixed HTTP CONNECT, VLESS native TCP; fixed Hysteria2 and TUIC UDP | **Qualified** on both installed providers (#310, #314). Only the outer transport is visible. No claims for traffic decrypted on the remote proxy. |
| VMess, Trojan, AnyTLS, ShadowTLS, SSH; fixed V2Ray HTTP/WS/gRPC/HTTPUpgrade, and QUIC with the appropriate build | **Source-equivalent** when the variant uses the fixed endpoint and supplied dialer (`protocol/*/outbound.go`, `transport/v2ray*`). `internal/entities/outbounds/zapret_outer.go` classifies TCP vs QUIC and checks port and family. These are not individual installed passes. |
| SOCKS4/4a, SOCKS5 with UoT; plugin-free Shadowsocks | **Source-equivalent** with fixed TCP or TCP+UDP scope. SOCKS5 UDP ASSOCIATE can return a different peer and port; a logical TCP network preference does not make that native dial safe. Shadowsocks plugin TCP bypasses the shared handoff. These variants remain **unqualified**, without modifying the original option. |
| Hysteria2 Realm or port hopping; Extended variable transports | **Unqualified**: Realm uses dynamic peers, hopping needs admission for every selected port, and KCP/XHTTP cannot be treated as fixed TCP (XHTTP can use a distinct download socket). UI/validation preserve their settings. |
| Snell v4 and v6 | **Qualified** for v6 with a fixed IPv4 outer TCP socket on both installed providers ([#318](https://github.com/MalenkiySolovey/solovey-ui/pull/318), tested `f80f955b`). The fixture observed TCP and UDP-over-TCP, wrong-port rejection, denial after engine loss and OFF restoration. v4 is **source-equivalent**: `protocol/snell/outbound.go` gives both clients the shared dialer and the configured server, and `ListenPacket` uses the same outer TCP path. An enabled binding requires a covered server port and family on the owning hop. A detoured Snell remains an intermediate hop; OFF preserves its detour. |
| Hysteria v1 | **Not admitted by this Product**. Both pinned clients dial the supplied UDP endpoint with no hopping; `server_ports` activates a changing port set in the Hysteria client. The fixed variant is a candidate for the shared UDP handoff, not a demonstrated incompatibility. Optional XPlus wraps the already-opened socket and makes QUIC payload recognition a separate question. |
| Extended Mieru, TrustTunnel and Sudoku | **Not admitted by this Product**. Mieru projects transport-specific endpoint and port ranges; TrustTunnel constructs HTTP/2 and HTTP/3 client transports; Sudoku supplies tunnel configuration to its client. A constructor receiving a dialer alone does not establish the complete socket set. Fixed configurations remain candidates for an explicit, qualified projection. |
| Tor, Naive and Extended Call | **Unqualified**: Tor has an embedded client and a local SOCKS boundary, Naive's native HTTP/QUIC transport can use Cronet, and Call delegates to a separate bridge/client. None has the fixed selected-FD contract verified by this integration. |
| WireGuard, Tailscale, MASQUE, OpenConnect and OpenVPN; Bridge | **Unqualified**: separate device, peer/relay or VPN-client ownership. For example, WireGuard has per-device dial setup and Tailscale creates additional packet listeners; OpenVPN/OpenConnect construct native devices. Bridge isn't registered by Product: its direct TUN/nft/route changes would require the existing Planner/broker authority. |
| Block; Selector, URLTest, Extended Bond/Failover/limiters and delegating detours | Block is **not applicable**. Groups and limiters invoke member outbounds: the binding belongs to a compatible member, not its container. Frontend reports a statically unambiguous egress in a supported chain; for dynamic group selection it does not infer current state from default order. Missing or ambiguous references do not create a fallback. |
| Upstream-only registrations | Not promoted to Product support. `core/registry` is the source of Product exposure; upstream-only registrations, build-disabled implementations and old metadata are not evidence of an available egress. |

| Shared inputs | Disposition and reason |
| --- | --- |
| `routing_mark`, `route.default_mark`, `netns` | **Compatible** for provider-decoded zero marks and an empty selected Dial namespace. JSON is preserved. **Retain** nonzero marks in the installed profile: both `common/dialer/default.go` versions append `SO_MARK` before `ProtectPath`; the capless Core and the broker's unmarked-FD check cannot compose that there. A named namespace uses a different socket namespace than the broker qualifies. `protect_path` is runtime-only authority. Root namespace holders/unshare require separate platform authority. |
| `default_interface`, auto-detect, network strategy, fallback | **Conditional**. Both providers suppress default binds for explicit source/interface binds. Product checks all selected paths in `internal/singbox/assembly/candidate.go`; no hidden override. Native `network_strategy` selection needs a platform exposing network interfaces to the dialer, and pinned provider field conflicts (source/device/TFO) still apply. |
| Timeouts, keepalive, reuse, address ordering, UDP fragmentation, bind-without-port, TFO and MPTCP | No blanket Zapret denial. Both providers retain these native Dial fields and platform checks. Timeout/keepalive/reuse/bind-without-port affect the same socket setup; alternate family/device and kernel-created MPTCP subflows are not new installed guarantees. |
| TCP/UDP DNS, DoT, DoH, DoQ, DoH3 | **Qualified** for explicit admitted Direct given endpoint family, port, same-port TCP retry for UDP, build capability and nonrecursive bootstrap. Over a proxy, only the fixed outer path is protected. Implicit selection isn't marked automatically. Explicit `tls.insecure` is preserved with a DNS editor warning: TLS authentication is not a packet admission requirement. Default verification and ordinary Core TLS validation are unchanged. |
| Hosts, fakeip, local, resolved, DHCP, mDNS and native VPN DNS; Extended SDNS/fallback | No independent Zapret switch. Hosts/fakeip have no remote socket; local/resolved use resolver-specific ownership; DHCP/mDNS use interface discovery; VPN DNS uses its endpoint. Extended SDNS constructs a stamp-derived DNSCrypt client; fallback delegates to configured DNS transports. These are not equivalent to the six admitted direct detour types. |
| TUN stacks, auto-route, auto-redirect, DNS mode | **Qualified** only for the prepared scoped IPv4/gVisor path above. System/mixed need local listener/INPUT ownership; IPv6 needs its own ingress/return scope. Automatic routes/redirect/system DNS would add privileged writes outside this contract. Ordinary supported TUN is not another Zapret egress. |
| Native options and Lua | The Component owns typed profiles, source review and managed dependencies. Combined TCP/UDP profiles retain one name, order and list scope; passive L7 selection and `pass` retain native selectors without transforming traffic. Template import is review-only: the pinned `dp_copy` replaces marked scalar filters, merges lists and port filters, and copies the ordered action list. The current typed projection cannot yet calculate the effective scope of those operations. Administrator Lua is saved as text only: the pinned engine's Lua process retains network authority and has no per-script isolation. |
| OpenWrt and FriendlyWrt | Stock OpenWrt `25.12.5` removes retained denial on firewall teardown; pinned procd has no equivalent systemd FD store. FriendlyWrt parity and authorized physical signoff remain pending. |
