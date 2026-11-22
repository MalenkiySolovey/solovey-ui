# Installed Zapret ownership

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
