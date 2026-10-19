# Local Network Planner

`Compile` is a pure, bounded coordination contract over owner intent and fresh
operation-local facts. It allocates only the existing managed coexistence table.
It does not interpret protection, Routing or DNS policy, probe a host, execute a
command, persist desired state or provide a second transaction/broker.

Identity binds Local HostRef, semantic owner, ResourceRef, desired/input revision,
canonical resource/effective endpoint revisions, observed-input and adapter
revisions, current allocation facts and owner-supplied management continuity.
Equivalent input ordering has the same identity; changed observed inputs require
a new plan even when generated configuration bytes would be unchanged.

The existing owner operation fence must surround candidate acceptance and the
immediate pre-mutation `CheckAccepted`. The candidate receipt binds exact
validated artifact bytes. Typed conflicts precede mutation. Candidate validation
is configuration proof, never data-plane or end-to-end evidence. Only the narrow
network platform adapter may materialize the plan through the existing broker.

`Compare` describes bounded owned drift. Late observations cannot authorize a
repair. A managed-looking name never proves ownership, and foreign allocations
cannot be adopted or cleaned up. Repairs and rollback must use the same existing
Change/resource fence and authenticate the current owned before-image.

Durable desired authority, operations, recovery checkpoints, Jobs and Health
remain with the current owners and R3 contracts. This package adds no database
schema, global reconciler, global LKG store or generic resource administration.
