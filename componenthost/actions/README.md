# Semantic owner actions

The fixed descriptor catalog covers two existing write commands:

| Action | Existing owner | Semantic permission | Existing authority |
| --- | --- | --- | --- |
| `backup.restore` | Data Lifecycle | `backup.restore` | authenticated browser admin scope and one-use step-up for `database:restore:<rehearsal revision>` |
| `component.remove` | RuntimeManager component lifecycle | `component.uninstall` | authenticated browser update/admin scope and one-use step-up for `component:<id>` |

Each descriptor supplies owner, permission, Local Host/resource scope, risk,
typed input schema, current preview support, step-up policy and audit class.
This catalog executes nothing. It defines no new session, role, grant, token,
database, command dispatcher or generic execution endpoint. Other actions remain
with their current owners until a real seam is migrated.

The owner reconstructs an invocation from its actual typed request, using the
existing operation-coordination Host/owner/resource/correlation tuple. It calls
`Require` before mutation or maintenance. Missing authorization, unknown action
or permission, foreign Host/resource, mismatched schema/input/decision, or
cancelled context denies execution. A policy result is an in-process value for
that invocation, never a reusable approval receipt.

Core API policy adapts the current session/scopes and existing step-up engine.
Credential/MFA generations, session binding, client identity, expiry, nonce and
one-use consumption remain there. Restore approvals retain their exact archive
rehearsal binding; the owner independently rehearses again. Component removal
has no separate preview beyond its ID and still uses safe removal admission,
self-removal rejection and compensation.

Component action authority also requires the catalog's component identity and
an actual registered manifest declaration. ComponentHost middleware supplies
that identity after active-state admission; a request header cannot supply it.
Registry admission rejects unknown or foreign action declarations before
publishing owner state. Typed request-helper ports do not grant action policy.

OpenWrt's existing pre-start preservation recovery has no browser session. Its
private policy only admits `backup.restore` for the independently verified fixed
preservation pair's exact rehearsal revision. This preserves the existing
recovery authority; it grants no other action or general system bypass.

Restore audit retains the existing durable Change correlation. Removal audit
adds the actual command's operation correlation and typed input digest. Removal
does not gain a fictitious durable job or recovery journal from this metadata.
