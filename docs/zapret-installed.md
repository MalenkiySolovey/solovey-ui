# Installed Zapret ownership

# Multi-Direct and DNS (R8B2)

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

The Outbounds owner validates each explicit Direct binding and renders the
candidate from one database view. It sends the complete batch to the broker.
The component resolves ordered profiles, the Network Planner allocates marks,
and the broker publishes the batch only after it can serve every member.
Neither the resolver nor the broker chooses a replacement Direct on failure.

The architecture budget entries for the batch are reviewed individually:

- `resolveZapretStrategies` uses the component's binding resolver for each
  pre-checked Direct. The Direct type is an assertion about the attached
  outbound, not a routing default.
- `installedZapret.prepare` owns one batch worker and one watcher, started
  only after all endpoints and the owned table are ready. The watcher exits
  with the batch authority. At most three listeners are joined; losing any
  listener or the engine cancels the batch. Closing the admission table and
  joining the listeners precede engine retirement. PID 1 retains the deny
  table until the broker can prove all previously marked sockets have drained.
- `startZapretEngine` has one waiter per actual child. It reaps the child and
  reports its result before closing `done`. Failed preparation and normal
  retirement use the same bounded pidfd-backed stop path.
