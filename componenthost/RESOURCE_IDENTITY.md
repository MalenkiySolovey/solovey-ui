# Local configured resource identity

The Panel listener `core:panel:web` and subscription listener
`core:subscription:default` retain their existing IDs, owner `core`, source
`settings` and configuration revisions. Their optional `identity` read metadata
uses `solovey-ui/resource-identity/v1` and the shared Host reference
`LOCAL` / `local`. It adds no independent persisted ID or configuration editor.

`desiredRevision` is the existing `capabilities.configRevision`, computed by the
settings owner. `observedRevision` is absent; `observationStatus` is `UNKNOWN`
with reason `configured_resource_not_observed`. This is configuration identity,
not proof that a listener exists, is healthy, or is reachable. No observation
time or evidence reference is fabricated. The existing inventory freshness
contract still applies to a snapshot; metadata does not refresh it.

The wrapper must match its parent's ID, owner, source and desired revision.
Strings are bounded and only Local scope is admitted. Missing legacy metadata,
an unsupported schema/Host, a stale binding or an observation claim is unknown.
The registry discards unbound optional metadata, preserving canonical fields
and existing admission behavior. It does not infer a Host or observed authority
for legacy contributors. Snapshot boundaries clone the optional pointer.

Existing resource fingerprints, Protection socket graphs, firewall plan/action
revisions and Network Identity input evidence select canonical fields explicitly
and retain their prior semantics. The wrapper does not override them. In
particular, UNKNOWN observation never proves that an owned resource is absent
or safe to remove. F5's separate owner/resource/recovery admission remains the
authority for component metadata removal.

Logical Backup continues to own canonical settings under its existing single
`core` owner. Restore and mandatory resource re-registration regenerate the
wrapper from the accepted settings, including rollback after a rejected rebind.
No metadata table, backup format, DB identity migration, Resource CRUD, remote
Host resolver, Node transport, polling/probe or Health framework is introduced.
