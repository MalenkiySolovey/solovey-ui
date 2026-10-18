# Safe component removal

Runtime removal changes installed ownership; it never calls Drop Data or a
package/firewall cleanup helper. Disable the component first. A retained runtime
instance, failed Start/Stop, external authority, unfinished migration, active
lease/operation, retained declared data, or unknown impact blocks removal before
the installed manifest is written. The package-managed profile remains immutable.

Retained tables (including empty schemas), settings, secrets and declared file
roots keep their installed owner discoverable by Backup. Use the separate owner
Drop Data workflow when deletion is intended; its preview, backup, assurance and
recovery rules continue to apply. Applied compatible migration journal rows are
history. Nonterminal, failed or incompatible rows retain ownership.

The data-lifecycle owner supplies a presence-only removal projection. It avoids
row scans and recursive file walks; full Drop Data previews retain their existing
counts. The outer read combines that projection, the existing owner authority
inspector and a fresh neutral resource inventory. Privileged/host-bound owners
require verified external authority. Inventory errors and unknown owner results
fail closed. No background read can commit installed metadata.

One removal read slot is retained until actual completion. The shared resource
provider read boundary likewise holds at most 128 workers until actual return.
Cancellation, timeout, panic, exhausted capacity and late values cannot authorize
removal. The request deadline is at most three seconds (or the caller's earlier
deadline); cancellation does not forcibly stop an uncooperative owner. Its slot
remains occupied until it returns.

Final admission uses the existing cross-domain operation fence and current
Supervisor. Busy admission fails immediately. The supervisor excludes migration
and Start/Stop, checks retained/unknown instance authority, and allows only the
trusted local resource recheck plus synchronous metadata commit inside its fence.
Owner callbacks run before this fence. The recheck catches a migration/data
footprint changed after the first observation. A stale Drop Data preview must
also revalidate installed ownership under that same operation fence.

Failed installation compensation uses the same safety admission. A clean rollback
can restore the prior uninstalled state; a partial migration or uncertain Start
retains installed metadata and reports failure. No automatic cleanup or generic
rollback framework is introduced.

The Remove API requires the existing browser step-up mechanism with operation
`component.remove` and target `component:<id>`. Grants retain session/client,
current MFA and single-use binding. Password-only action bodies and bearer tokens
cannot replace that grant. The UI obtains the grant through the shared credential
helper, clears the entered credential after each attempt, and posts an empty body
with `X-Step-Up-Token`.

The architecture ratchet rejects direct metadata-removing calls outside this
guard, including boolean compensation setters. Its reviewed worker budget admits
one bounded removal read and replaces the prior resource-provider launch with the
capacity-controlled launch; unrelated worker debt is unchanged.
