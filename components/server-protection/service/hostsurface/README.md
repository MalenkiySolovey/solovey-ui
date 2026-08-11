# Server-protection HostSurface owner binding

The single production provider identity is
`server-protection:linux-hostsurface`. Component startup constructs the
release-matched helper client first, wraps it in `HelperOwnerObserver`, passes
that observer to `NewProvider`, and registers that exact provider in the
neutral HostSurface registry consumed by firewall baseline. Duplicate providers with
the same identity are not selected; the registry emits a bounded
`hostsurface_provider_ambiguous` UNKNOWN_OWNER fact instead.

Owner availability is not a helper-installed boolean. The provider preserves
the exact typed stage:

- `HELPER_NOT_INSTALLED`, `HELPER_IDENTITY_MISMATCH`,
  `HELPER_CONTRACT_UNSUPPORTED`, and `OPERATION_NOT_ADVERTISED`;
- `OWNER_OBSERVER_NOT_REGISTERED`, `OWNER_OBSERVER_NOT_BOUND`,
  `OWNER_CONTRACT_MISMATCH`, and `DEPLOYMENT_BINDING_MISMATCH`;
- `OBSERVATION_TIMEOUT`, `OBSERVATION_FAILED`, `OBSERVATION_STALE`,
  `OBSERVATION_AMBIGUOUS`, and `OBSERVATION_SUCCESS`.

The compatibility reason `listener_owner_capability_unavailable` may accompany
a failed stage, but every such fact also carries the exact bounded reason.
No raw helper error, path, `/proc` content, argument, credential, or secret is
copied into HostSurface or API diagnostics.

Each resource request is derived only from the frozen registered resource and
its configured listen intent, source/artifact/deployment identity,
RuntimeRoot binding, ApplicationOwnerContract, owner revision, and
configuration revision. The helper executable SHA-256, negotiated capability
revision, listener-owner contract/observer revisions, provider construction
binding, result revision, and exact typed availability are inputs to the
owner-observation-set revision. Socket/process/service/application identity is
then carried through the socket graph, graph evidence, snapshot input, plan,
and external common binding. A later prepare recomputes those inputs and
rejects drift through the existing plan-revision fence.

Only a current complete `ListenerOwnerFactV1` can produce `MANAGED_EXACT`.
IPv4/IPv6 coverage comes exclusively from its socket identity. In particular,
an IPv6 wildcard covers IPv4 as well only when the fact carries an observed
`IPV6_V6ONLY=false` and both coverage families. Missing `IPV6_V6ONLY`, stale
facts, ambiguous owners, foreign sockets, and unobserved resources remain
fail-closed. Owner reconciliation has no nftables operation path.
