# R8B1 installed admission decision

## Installed Linux systemd profile

R8B1 qualifies one enabled Direct binding on a Full Official native-hardened
installation, with a verified pinned `nfqws2` and a static IPv4/TCP Strategy.
The administrator provisions the root-owned `/etc/solovey-ui/zapret.json`
profile (`native-linux-systemd-nft-nfqueue/v1`, outbound, interface, engine
SHA-256 and revisioned Strategy), the source-locked `nfqws2` payload under
`/usr/local/lib/solovey-ui/zapret-engine`, and enables the installed Zapret
component before saving the Direct binding. Switch that binding explicitly
OFF before disabling the component. Unsupported configurations fail admission.

The trusted administrator and network manager reserve the selected egress,
queue and mark resources while enabled. Admission rejects foreign nftables
output and egress hooks, legacy iptables rules, foreign tc/TCX owners on the
selected interface, and competing Core TUN or mark configuration. Retire the
binding before changing privileged hooks or remapping the egress device.
The active owned table checks the selected next hop and device both before
queueing and in postrouting. The broker starts the pinned engine with
`--bind-fix4` so its generated packets use the admitted NFQUEUE output device,
not an alternate route available to unbound sockets. This profile does not
support privileged services attaching new hooks to the protected path while ON.

Systemd holds the separate deny table's exact netlink owner descriptor. The
broker consumes a journaled mark serial before it builds a new path. It only
publishes the authenticated pre-connect FD admission after the listener,
owned table and independent deny are verified. Engine or broker loss retires
admission and retains the deny for previously marked sockets. OFF closes the
old Core, retires the owned queue and engine, and releases the deny after
proving that protected TCP sockets have drained. The ordinary framed broker
and capless Panel retain their existing privilege boundaries.

## Historical proof paths

The following notes record the earlier `zapretproof` build and rejected
architecture candidates. They are separate from the installed profile above.


Outbounds owns the binding and Routing selects Direct A. Zapret owns the
revisioned Strategy and compiles it; the Network Planner alone allocates the
queue, packet marks and egress resources. The existing broker is the only
privileged executor. There is no global port selector or return-path capture.

`routing_mark` in the provider needs socket-mark privilege in Panel. Matching
the Panel cgroup alone would also catch Direct B and management traffic. The
existing provider `protect_path` can hand over the specific socket before
connect without a provider bump. Use a separate root-owned, generation-specific
Unix stream in the broker. The general framed transport continues to reject
`SCM_RIGHTS`. The new stream permits one FD and one command. Denial is EOF,
not a status byte, because the pinned provider accepts any one-byte reply.
For the same reason the broker chooses the path in a root-owned, non-writable
directory, never accepts an endpoint supplied by Panel, and never rebinds a
stale generation path. It enables `SO_PASSCRED` before bind. The production
unit does not provision or grant write access to the separate Zapret socket
directory while activation remains unqualified.

The listener must match an attested non-root systemd Panel process to a
broker-local generation grant: manifest and process revisions, writer
credentials, pidfd liveness, fresh unbound TCP/UDP descriptor, kernel namespace
cookie, authorised family, and exact allocation. The broker fences, enrolls
the cookie, binds the interface, marks the socket, then acknowledges. The
socket protocol authenticates the Panel process, not an untrusted Outbound
tag supplied on the wire. Only the Outbounds owner may project this listener
as Direct A's `protect_path`, and only after the generation is committed.

The create-only nftables batch makes its table nonpersistent and owned by the
broker's private netlink port. The port is never transferred or reused after
retirement. The kernel detaches this table on port close, including abrupt
owner exit. Creation, verification and rollback never claim or delete a table
based on the name alone.

An independent pinned IPv4/IPv6 BPF hook checks postrouting device identity,
packet admission bits and the enrolled socket cookie for original traffic.
Generated output needs its own firewall pass bit. For each enabled family,
the Planner snapshots one unicast gateway on the selected ifindex in table
`main` using a bounded kernel dump. No default, multiple defaults, ECMP and
opaque next-hop objects are unsupported. Stage, renew and socket admission
re-read that snapshot rather than relying on the caller's route revision. The
owned table compares each protected packet's actual device and next hop before
queueing and again after routing; this also covers a route-type `OUTPUT`
lookup after NFQUEUE reinjection. A different next hop on the same link does
not match. This path is gateway-scoped, not general LAN or policy routing.

An allocation is not a grant. The adapter checks the NFQUEUE netlink
descriptor against the authenticated `nfqws2` process. It still needs a
complete account of mark writers. The snapshot includes the ordered IPv4/IPv6
routing rules, even when only one family is enabled. Their `fwmark` values and
masks reserve bits in the allocator; a rule change invalidates the generation.
This covers routing selectors, not foreign nftables, iptables, tc or BPF
writers. An unchanged snapshot does not establish that those writers are safe.

The staged generation has no kernel lease or socket authority. Before the
first renewal it rechecks Planner facts, the real route, table owner, pinned
guard IDs and programs, and authenticated queue consumer; it retains the
checkpoint in the existing broker journal against the expected Panel process
revision. A single completed row avoids leaving an active mutation that would
block unrelated broker actions. Before that write the deny hooks live only on
the broker's descriptors, so an early crash leaves no durable pins. After the
write the same links are pinned and verified again before the first lease. A
failed write retires the path even if a record can subsequently be read.
Before setting the first kernel lease, the live path atomically replaces the
prepared receipt with a retained `may_have_leased` receipt under the same
fence. The lease can enable engine raw output before a Panel FD is marked, so
even a generation with no enrolled Panel sockets must keep its deny hooks once
this transition succeeds. A failed journal write cannot open the lease; a
visible but ambiguously synced transition still fences replacement.
Publication requires this receipt and a lease held by the live path.
The publisher also holds the matching broker manifest authority lease.
Rotation cancels that lease and retires the path even while publication or
a socket handler is still draining. Retirement revokes the kernel lease and
closes the owned nftables port, but keeps the deny hooks for issued sockets.
The broker does not install the next manifest until the old owner drains.
The grant's fixed monotonic deadline also caps every kernel lease. Both
marked socket and engine raw output expire without waiting for the broker to
close its listener or the owner port.

A prior-boot receipt can be replaced only after verifying the current kernel
boot ID. In the same boot, the live owner can retire its own unleased path. A
reopened owner can replace a prepared receipt only in the same network
namespace after registering a fresh create-only table and both fresh BPF hooks
at the exact slots recorded in the receipt. The kernel refuses a duplicate
BPF hook priority in that family/hook, even before pinning. This proves the
old hooks are no longer registered without opening global BPF IDs (which
would require `CAP_SYS_ADMIN`). It does not detach any pin or adopt old pass
bits. Occupied, legacy and potentially leased generations still need guarded
recovery.

Production activation remains closed. The experimental Linux socket publisher,
generation journal writer and BPF egress guard require the `zapretproof` build
tag used by the isolated qualification workflow. Normal broker builds omit
them; an enabled binding still rejects runtime projection before Core starts.
The current BPF design would require loading the guard and owning a private
writable bpffs directory under the installed unit. On the disposable systemd
runner the base unit could not create the
device map; adding only `CAP_BPF` in a test unit left `sk_buff` reads denied by
the kernel verifier (which requested `CAP_PERFMON` or `CAP_SYS_ADMIN`). The
installed units and Panel capabilities remain unchanged. The owner must make
partial apply and restart reconcile durable, reject stale socket admissions,
and preserve a matching deny guard for surviving marked sockets. On owner
loss the nftables port closes independently of the guard's lease. Production
reconcile must not reinstall pass-bit rules for a still-leased old generation.
The hooks do not cover a device redirect or L2 rewrite after `POSTROUTING`.
A netdev nftables egress chain also runs before tc. The scoped cgroup egress
and TCX proof used a synthetic pass bit, with the TCX program first in its
chain. It did not prove real generator output, stable hook precedence or
containment after link loss. The installed privilege, final egress and
recovery gates must pass before publication of a socket admission listener.

An exclusive egress namespace is not a drop-in option. At the pinned
Official `8b914e4be510` and Extended `493b451cdac6` revisions, Direct creates
its own sockets. The provider's `netns` option calls `setns` from the
Panel process before socket creation; the installed profile forbids that.
`protect_path` receives an existing socket and returns an acknowledgment,
not a replacement socket from the broker. A veth peer that returns to
shared host routing would also leave the late egress problem unresolved.

Neither candidate qualifies on the current installed profile. The missing
prerequisite is an enforceable owner of the protected path through the last
egress decision, including exclusive queue and mark authority. Operators and
the narrow broker are trusted; if co-resident networking services can modify
these resources, observing their current state is not admission evidence.

A separate unowned nftables deny table is not a substitute for that owner.
In a disposable Ubuntu 24.04.5 guest (Linux 6.8.0-147-generic), the pinned
engine bound its queue in the installed broker's sandbox via a test drop-in.
An already-open marked UDP test socket was blocked after engine and admission
loss, but reached the receiver untreated after the stock `nftables.service`
reload flushed the deny table. The unmarked control continued throughout.
This was not a Core Direct socket. A live port-owned table is exempt from
other ports' flushes, but the kernel releases it on the last close of its
owner port. An orphaned persistent table does not retain live ownership.

A `zapretproof` broker can verify and close its inherited copy of a
deny-only owner port. It requires both original RPC listeners and verifies
that PID 1's store for the fixed broker unit holds the same name, device,
inode and mode. A notify send or a matching name alone is not that proof.
It neither adopts nor removes the stored port and does not issue a grant.
Ordinary binaries and service units do not enable this path.

Retaining the deny port solves a different problem from retaining admission.
Linux queues by number in the network namespace, not by the attested engine
port ID. After an unbind, the same number can be bound by another privileged
listener. Keeping an open copy of the engine FD would not prevent an explicit
unbind, and polling cannot make a queue lookup atomic with the attestation.
Nor does retaining the deny table constrain later transmit hooks. Keep ON
closed until the supported deployment enforces exclusive queue and egress
authority and qualifies recovery with real issued Direct sockets.
