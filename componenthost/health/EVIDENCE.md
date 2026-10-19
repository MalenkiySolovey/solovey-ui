# Proposition-specific Health evidence

The existing Registry remains the only bounded check executor. Optional Evidence
adds Local HostRef, owner/resource, proof kind/source, observed and desired
revisions, evidence identity/reference, observation time and explicit max age.
The six proof kinds are independent propositions, never a numeric ladder.
Unknown, absent, malformed, wrong-owner, stale, future or wrong-revision evidence
cannot satisfy Require. Every Require executes a fresh owner check; no implicit
cache exists. Legacy code-only Check results preserve their API but do not prove
a stronger proposition. Health never repairs or authorizes an operation.

The real panel checker proves RUNTIME_READY for the listener belonging to the
running panel binary version. Its observed revision hashes that version; it does
not assert that the current settings listener config has been applied, that a
socket is externally reachable, or that VPN/data-plane/end-to-end traffic works.
Registration generation fencing discards completion from a replaced listener.
Update requires fresh matching binary-version evidence and separately retains
its core, SQLite, release/schema and migration owner checks. Doctor consumes the
same fresh proposition and reports its precise limit. Both expose evidence refs.

Registry execution retains the five-second deadline and has 256 worker slots.
An uncooperative checker retains its slot until actual exit; later requests fail
boundedly. Cancellation is not proof that an owner worker has already stopped.
