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
Generated output needs its own firewall pass bit. An allocation is not a grant:
the adapter must observe the actual routing policy and exclude mark writers,
then check the NFQUEUE netlink descriptor against the authenticated `nfqws2`
process. Route revision is a control-plane input, not a kernel route proof.

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
The hook sees the routed interface at `POSTROUTING`, not a later redirect or
a changed next hop on the same link. Neither is an admitted installed path
until its exact kernel constraint and rollback are qualified.
