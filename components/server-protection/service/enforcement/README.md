# Protection policy compilation

This package validates existing scoped action contracts, binds them to current
resource and endpoint revisions, coalesces equivalent contributions, and emits
the typed request consumed by Network Planner. Native and external evidence
share the existing lifetime bounds and contribution identity.

`AdmitActions` checks the contract, source class, scope and lifetime.
`BindActions` checks the canonical target revisions before combining equivalent
contributions. `NetworkIntent` translates those contributions, management
exemptions and bounded UDP policy into `network/intent` values. None of these
results proves that a rule has been applied or authorizes an operation.

The firewall workflow supplies current owner facts and retains management
preflight, operation coordination, checkpointing and compensation. Network
Planner owns allocation, conflict checks, accepted plans and revision fencing.
The existing privileged broker admits and executes protected operations through
the platform adapter. Disabling or stopping the component does not grant this
package authority to remove external resources.

Policy evaluation and capability resolution remain in the existing `decision`,
`policy`, `scoring` and `response` packages. The compiler has no Backend storage,
HTTP, shell, filesystem, broker, renderer or runtime dependency. Unknown or stale
capability facts must still pass the existing resolver and Planner gates.
