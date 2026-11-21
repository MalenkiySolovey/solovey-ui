# R8B1 installed admission decision

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
Publication requires both the exact receipt and a lease held by the live path.
A new generation can replace a prior-boot receipt only after verifying the
current kernel boot ID. In the same boot broker restart does not adopt old pins
or pass bits. Only the live owner can release a verified idle generation; after
socket admission, same-boot replacement still needs a verified drain.

Production activation remains closed. Before wiring it, the broker must prove
Netfilter BPF load and a private writable bpffs directory under the installed
unit. On the disposable systemd runner the base unit could not create the
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
