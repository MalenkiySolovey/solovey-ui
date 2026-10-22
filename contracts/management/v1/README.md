# Solovey management contract v1

Contract-set identity: `solovey.management/v1`. Database schema and product
release versions are independent. Panel owns this canonical source. Node consumes
an exact pinned bundle without importing the Panel module. Each accepted R6 boundary is identified by its exact manifest and qualified in
both repositories. Contract version alone is not a claim that an arbitrary
bundle or untrusted metadata has been accepted.

`schema.json` uses JSON Schema 2020-12, with `x-actions`, `x-inputs` and
`x-reasons` as catalog annotations. `types.gen.go` is generated, never edited.
`codec.go` implements the deliberately bounded schema vocabulary used here;
it is not a general JSON Schema library. The generator rejects unsupported
assertions. No production execution, authentication, transport or persistence
exists in this package.

From the repository root:

```text
node contracts/tools/generate.mjs
node contracts/tools/generate.mjs --check
go test ./contracts/management/v1
```

Generation uses Node standard libraries and the go.mod-selected Go/gofmt
toolchain. No network is needed once the existing toolchain is available.
The manifest records contract version, normalized source-schema SHA256,
generated-model SHA256, vector version/SHA256, generator identity/digest and
gofmt version. Bundle SHA256 hashes sorted `path + NUL + file-SHA256 + LF`
records. All text identities normalize CRLF to LF; no BOM or timestamps.
Git source commit is separate provenance, never the compatibility identity.

## Compatibility and encoding

V1 commands, typed inputs, grants, identity references and SecretRefs are closed
objects. Unknown command fields, unknown action/input schema, malformed identity
and unknown major fail closed. Read-only projections allow bounded additional
descriptive fields; `Decode` ignores them before typed decoding so an unknown
case variant cannot override a known field. Direct `json.Unmarshal` is not the
contract decoder. Unknown reason codes are retained as data but interpreted as
`UNKNOWN`; `Accepted` requires an explicit known accepted reason and version.
Unknown enum values that carry state/authority are rejected, never guessed.

Breaking field requirements, digest/identity algorithms, enum/state meaning or
owner semantics require a deliberate new contract major. Compatible optional
read metadata and new explicit typed Action inputs require schema/catalog and
vector updates with both consumers qualified. There is no general forward-
compatibility claim, generic CRUD input, shell/SQL/OS command or transport status.

Documents are bounded to 256 KiB, depth 24, 48 object fields and 256 array
elements, with tighter schema bounds where specified. Duplicate keys, invalid
UTF-8, trailing JSON, null/wrong-type required fields, fractional/exponent numbers
and integers outside ±(2^53−1) are rejected. Generations and operation counters
are canonical positive uint64 decimal strings, including values above 2^53.
Identifiers are case-sensitive opaque ASCII; no trimming, alias or IP/MAC lookup.
No localized message participates in compatibility or authority.

CommandDigest validates typed input then hashes compact sorted-key JSON of:
contractVersion, actionId, host, resource, generation, inputSchema, inputDigest,
input, and expectedRevision when present. All digest-bearing strings are ASCII
tokens; nested keys are sorted recursively, with Go JSON string quoting and no
whitespace. CommandId, correlationId and grantRef describe delivery/authority,
not owner intent, and are excluded. R5 OwnerInputDigest is preserved exactly:
SHA256(inputSchema UTF-8 + NUL + typed rehearsalRevision/componentId UTF-8).
No clock enters either command digest.

## Identity, ownership and commands

Local Host remains exactly `{kind:"LOCAL",id:"local"}`. Future Node uses the
same HostRef shape with `kind:"NODE"` and a stable `node:<opaque>` NodeID. NodeID
is separate from management public identity reference and mutable network
observations. Public-key rotation and management-path changes do not redefine
NodeID. Minimal enrollment facts are UNENROLLED, ENROLLED, REVOKED; the latter
two bind authority and authorization generation. These facts implement no
enrollment network protocol, certificate issuance or credential store.

ResourceRef is Host + owner + resourceId. Revision is an opaque semantic owner
revision, not a timestamp, row ID or global counter. ExpectedRevision is an exact
mutation fence; commandId is a separate idempotency identity. A target owner
retains canonical Desired State and determines admission, persistence, apply,
Change/Job/Health and recovery. Transport and Panel caches own none of that state.

Only the actual R5 Actions enter the initial catalog: backup.restore and
component.remove. All permission, owner, risk, input, preview, approval and audit
meaning is preserved. TARGET_HOST makes semantic scope explicit, while the
current Panel adapter still admits Local only. It neither enables remote Host
execution nor widens any existing permission. Future Node availability requires
its own compatible owner/capability admission, not just a familiar ActionID.

Restore's required fence is its rehearsal revision, also present in typed input.
Current component removal has owner lifecycle exclusion rather than an exposed
optimistic revision: OWNER_LIFECYCLE is explicit, and the Panel adapter rejects
an ExpectedRevision it cannot enforce. No caller-supplied fence is silently
ignored. Future feature write inputs must be deliberately versioned by their
canonical owners. There are no NodeInbound, NodeRoute or NodeDNS alternatives.
The ResourceRef fixtures cover Inbounds, Outbounds, Routing, DNS, VPN users,
Subscriptions and Components without inventing their production Node owners.
SEC-2 omitted-Failover-final semantics are excluded from v1.

## Authority and replay

ManagementGrant is a semantic binding, not a bearer credential or replacement
Panel session/MFA system. Authentication, trust verification, permission policy
and atomic one-use consumption remain with their accepted authority. Grant
JSON alone proves nothing. Trusted context must match actor/authority, Action,
permission, Host/resource, input digest, authorization/credential/MFA generations,
expiry and nonce/grant identity. The contract exposes no private session object.

Current Panel authorization precedes idempotency lookup. A replay therefore
requires valid authority; consumed/expired authority never becomes valid because
an idempotency record exists. The retry vectors supply newly validated bound
authority after an ambiguous delivery. Same ID+same semantic payload returns
the recorded result/Job without repeating mutation. Same ID+different payload
conflicts. A new ID with stale ExpectedRevision conflicts independently. Old
runner generations cannot publish new truth. This is not a distributed transaction.

## State and proof projections

Capabilities use semantic yes/no/unknown; absent critical truth fails closed.
Component identity/version/Actions are shared; binary/delivery/UI/DB internals
are excluded. ABSENT, INSTALLED_DISABLED, ENABLED_UNSUPPORTED, ENABLED_UNHEALTHY,
ENABLED_HEALTHY and UNKNOWN describe actual facts. Enabled, Active and a returned
Start call are not Health proof. Healthy requires explicit current evidence.

Change/Job preserve existing PENDING/COMMITTED/FAILED/ROLLED_BACK/RECOVERY_REQUIRED
outcomes and owner phase. Progress is bounded bytes, not guessed percent. Cancel
request is separate from completion: a critical section defers cancellation until
the owner's safe point; terminal completion stays terminal. Current adapters
preserve unavailable cancellation rather than inventing support. No new job store.

Evidence names a proposition, owner/resource/Host, exact observed revision, proof
kind/source, original observed time and TTL (1–60 seconds in the accepted current
health boundary). Proof kinds are distinct propositions, not a numeric rank.
Receipt never refreshes observation time; expired, future or old-generation
observations cannot satisfy a current requirement. UNKNOWN remains possible.
Accepted state is not automatically LKG. Reconcile repairs configured intent only.

Artifact identity is content/provenance/signer/manifest metadata independent of
URL/path. ArtifactID preserves the accepted kind+NUL+contentDigest derivation.
Metadata or a matching checksum is not publisher trust: only the existing trusted
verification result can produce VERIFIED admission. Unknown/untrusted artifacts
remain blocked. V1 implements no downloading or installation.

Reason classes are deliberately bounded in x-reasons. Owner-specific bounded
codes may supplement them; temporary errors and messages are not protocol codes.
SecretRef remains masked owner-use/presence only. NetworkIntentRef names owner
intent/revisions, never raw nft/fw4, routes, paths or privileged commands.

## Qualified implementation boundaries

Panel's internal/managementcontract adapters read the existing configured
ResourceIdentity, Action registry, Component manifests/state, operation
Change/Job, Health checker and authenticated release identity. They install no
dispatcher. The eight real component manifests are checked for identity/Action
parity; private UI, database, package and runtime fields are not projected.
Descriptor componentId describes the current declaring component; it does not
require the same UI/binary composition on every Host. Owner/capability admission
still decides whether an Action is available.

A capability container whose truth is unavailable projects unknown even when a
stale nested field says yes. Health PASS requires scoped evidence and known
ACCEPTED reason. The current R3 checker recognizes digest revisions; unfamiliar
owner revisions can be described but cannot establish that proof. Unavailable
proof, replaced generations and stale observations remain UNKNOWN. A current
revision-bound failed check can establish FAIL. Current Local Health has no
exported runner generation; the adapter leaves runtimeGeneration absent. A
consumer requiring that dimension must fail closed. The existing registry's
replacement fence is tested separately with the same observed revision.

The command fixture's grant issuer is a trusted in-memory fixture boundary;
grant JSON does not authenticate itself. Actual Panel StepUp binding/consumption
and Restore authorization-before-replay are also tested directly from the shared
vectors. Browser session/client-identity details stay private to that authority.
Cancellation vectors model an owner-cooperative signal and safe point in test
fixtures. They do not add durable cancellation to current Panel APIs: their real
Job projections preserve cancelSupported=false and absent cancelRequested.

Seven feature domains consume one ResourceRef/Revision shape. Their fixtures
prove scope isolation, owner-local revision authority and rejection of a Panel
cache as desired state. They implement no feature payload or new CRUD Action.
Future feature write inputs still require a deliberate canonical typed addition.
Node's separate conformance implementation imports only this pinned public
bundle and standard libraries. It is proof code, not a production Node design.
