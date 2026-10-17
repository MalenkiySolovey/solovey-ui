# Platform facts

Authenticated `GET /api/v1/operations/deployment/platform-facts` projects the
existing selected deployment provider. The read has no persistence or lifecycle
effects. `Posture`, its signed bytes, doctor and all mutation contracts retain
their existing behavior.

Consumers inspect semantic availability and reason, retaining backend evidence:
systemd actual unit configuration, procd installed-owner supervision contract,
or Docker's local observation. Supervision availability does not attest running
readiness. Closed broker availability does not authorize a verb; root identity
never substitutes for a broker. Unit sandbox configuration does not prove an
arbitrary process's effective capabilities.

Unknown/mismatched providers and invalid, future or stale evidence yield UNKNOWN.
Missing broker is UNAVAILABLE. Opaque data/hardening hashes cannot establish
persistent mounts, apt/dpkg, apk/opkg, nft/fw4, container engine, route or network
facts. Those unobserved capabilities remain UNKNOWN. Existing portable Update's
`UpdateLifecycle` remains the package/operator lifecycle owner; this projection
does not expose package operations or infer their backend from a distro name.

The manager serializes observation with its existing lock and gives trusted
providers a five-second context. Busy reads return UNKNOWN immediately. This
does not preempt a provider that violates its context contract and creates no
worker, active network probe or fallback mechanism. Existing providers and their
accepted platform qualification are reused unchanged.
