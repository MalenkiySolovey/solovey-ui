# Foundation regression budget

R7 inline External Subscriptions adds one reviewed owner-local `tx.Save(&saved)`
site (`e857ad88...`) in `*Service.SaveSubscription`. The existing source owner
first reserves its row identity, then seals the inline source with owner/row/field
AAD and validates it through EndpointIR, before the same transaction saves the
sealed value and records the Change. Invalid content rolls back the reservation;
reads expose only its masked reference. The existing component SQLite backup
contribution retains this column. `TestInlineSourceUsesOwnedContentAndExistingRefreshPipeline`
covers rollback, binding, masking and refresh/materialization. No foreign owner,
worker, new table, Runtime or policy authority is admitted. The R1 base and all
other budget sites remain unchanged.

R7 MASQUE Outbound reviews the existing `core/registry.outboundDeclarations`
site from `ee279e14...` to `aadf4b88...`. The sole declaration adds the native
Extended/QUIC registration and an Official unavailable fact. Direct fallback
selection, ownership and budget count remain unchanged; the old exact digest
is retired without regenerating the legacy inventory.

R1 source: `bb306959fe17f49bfcb201cb69eb39cf7f7e4030`. The external R1 evidence is under workspace `.artifacts/architecture-stabilization-r1/`: final report, ownership map, lifecycle audit and `09-ratchet/legacy-baseline.json` (source inventory digest `3a366e7ae6c1c04d919988807f07ca9e6b01ac564bd083e5b88d23bde6e01301`). R1's 627 lexical candidates remain advisory; they are not imported as 627 exceptions.

Run `go test ./internal/components/architecture` and, after `npm ci --prefix frontend`, `node --test scripts/audit/architecture-policy.test.mjs` plus `node scripts/audit/architecture-policy.mjs`. Existing Backend/profile/race CI runs the Go tests; CI Frontend runs the frontend guard.

The Go inventory parses every production Go file (including cronjob, all build targets and untracked new source). Test/fixture/generated dependency trees are excluded. Budgets identify rule, path, containing declaration and a formatted AST digest, with multiplicity. Traversal order, line numbers, comments and formatting do not change identity. Deletions pass. Adding a duplicate site cannot borrow the budget of another declaration or file.

| Guard | Semantic scope / reviewed current cases |
|---|---|
| Broad Runtime | Import-resolved references to the core service.Runtime type, including aliases and local variables. Owner-internal Runtime definitions are distinct. Existing Host/Component adapters are debt to narrow one consumer at a time. Dot importing the owner is prohibited. |
| Component imports | Foreign component implementation imports rejected. Own implementation and app/cmd composition allowed; neutral Host ports remain the path between Components. Existing purity tests also apply. |
| OS exec | Only the six existing semantic execution owner packages allowed. Existing trusted executable mechanism test remains required. |
| Root RPC | Named Request types in ops/Protection helper contracts cannot gain command/argv/shell/env/environment/serviceName fields. Read-only process facts and anonymous procd observation responses are not mutation requests. Existing typed operation schemas remain unchanged. |
| Component model mutations | Exact mutation expressions in adapters importing canonical core models, GORM or SQLite require owner review; raw Table mutation chains are also covered. Existing owner-local repositories, Import destination writes, Remote Outbound materialization, Paid grant/store writes and contribution wiring are retained compatibility seams. This does not assert every matched method is foreign; owner-local SQL is legitimate. A new write must use an owner port, or document and test its ownership explicitly. |
| Worker lifetime | Every goroutine launch and time ticker creation is frozen. Full launch/closure AST is bound. Existing scheduler, managed notifier/refresh, HTTP serving, cancellation/drain and owner recovery mechanisms are reviewed in R1's lifecycle audit. A new worker needs bounded/cancel/join owner evidence before admission. |
| Shadow Node config | Reserved NodeRouting/NodeDNS/NodeInboundConfig/NodeOutboundConfig declarations rejected; existing site artifact/target metadata remains legitimate. This is a bounded schema guard, not a claim to infer arbitrary dataflow. |
| Direct policy | New Go Direct literals/failover constants require policy review of the full containing declaration. Existing explicit selection/defaults/import mappings and unrelated Fallback exposure mode literals are permitted exact sites. Frontend TypeScript/Vue AST guards ||, ?? and conditional Direct fallbacks. Six inspected sites are retained: Failover omitted-final for SEC-2; the existing Rule editor outbound default; Inbound draft transport default (not an egress policy); RegionalPresetDrawer's visible Direct outbound selection; preset direction inversion and read projection of existing rule-set tags. These extra AST candidates are not automatically security findings. |

No automatic budget refresh command is shipped. Do not regenerate or expand the legacy JSON to make a gate pass. New legitimate sites require an explicit semantic owner/lifetime/policy review in the PR and focused behavioral proof; eliminate a dependency or consume an existing narrow port first. Remove retired entries when migrating their owner. Existing entries are compatibility budgets, never security waivers.

F4 retires the eight broad Runtime type sites in Telegram's component and settings-observer composition. Its root production package additionally rejects Runtime selector access (including `host.Runtime` / lifecycle Host access) and core Runtime constructors/default access. The narrow service.RuntimeProvider interface in the child service package remains legitimate. Actual component/API tests supply transport and audit ports with no broad Runtime; other Components retain their exact reviewed legacy budgets.

Limits: these checks establish stated AST/import boundaries, not a general Go/SQL/TypeScript dataflow proof. Wrappers, dynamic SQL, indirect function values, arbitrary Node schema names and runtime effective policy still require owner review and behavioral tests. New Root RPC contracts must retain closed typed verbs; preserving a field list cannot prove their implementation safe. R1 observations remain useful review input outside these reliable bounds.

R3 B3 reviews exactly the existing `componenthost/health.Registry.Check` worker
site, replacing digest `041c73a6…` with `109e493e…`; the worker budget count is
unchanged. It retains the five-second caller deadline, buffered result and panic
closure, adds a fixed registry-wide 256-slot admission bound, releases the slot
only when the checker actually exits, and rejects a result after registration
generation replacement or cancellation. An uncooperative checker can retain a
bounded slot but cannot cause unbounded worker creation. Deterministic barriers
cover cancellation, saturation, replacement and late completion. No worker kill,
repair or new owner goroutine is introduced. The old exact digest is retired;
the legacy budget is not regenerated or expanded.

Current B3 CI exposed the existing web serving worker dereferencing a field
cleared by immediate Stop. Its exact `web.Server.Start` worker is reviewed from
`d712a8a6…` to `98ed5edf…`: it captures the initialized HTTP server and listener,
closes an owner completion channel on exit, and clears readiness before closing
that channel. Stop joins it within its existing 30-second shutdown context
before clearing/reusing fields. Timeout retains owner state and closes the
listener as before. No worker is added and the budget count stays unchanged.
The real Start/Stop/reuse regression checks worker completion without sleeps.

R5 D7 retires the exact createNewOutbounds raw-save budget site cb4a9b47... in
import-xui/database/routing_apply.go. Import now invokes the existing canonical
Outbounds.Save owner, which validates eligibility, namespace, protocol options
and references in the caller transaction. Its unshadowed package-qualified Save
call is recognized as an owner port, not a raw SQL mutation. The guard is closed
to that exact package/function; arbitrary Save receivers, unrelated imports and
locally shadowed owner names remain gated. Alias/shadow/mixed-call fixtures prove
this distinction. The R1 base identity and all other exact legacy sites remain
unchanged; no new budget allowance or Direct-policy/worker exception is added.

R5 D8 policy review: the existing RenderClash (500091cd... -> 24b0e033...)
and RenderXray (dce4e009... -> 66698f87...) declarations now call the shared
EndpointIR projection before serialization. Both existing Direct-literal sites
per declaration retain their multiplicity and policy: Clash keeps its explicit
reserved/default config groups and skips an explicitly supplied direct resource;
Xray keeps its existing explicit freedom/blackhole shell resources. No routing
default or Failover omitted-final rule changes. Unsupported endpoint semantics
return an error before either shell is emitted; they cannot select Direct.
The new ValidateEndpointProjection site 816b6bbc... only classifies explicitly
supplied selector/urltest/failover/direct/block resources as existing policy
objects for the current serializers. It does not synthesize a policy, outbound,
member, final or fallback. TestEndpointProjectionDoesNotInventPolicy proves this
for every current target, including omitted final, input immutability and denial;
existing custom Clash group and Xray balancer tests remain required. SEC-2 stays
BOUNDED_DEFERRED and no Failover migration/interpretation is introduced.
The exact retired syncSingleConnectionToOutbound raw-save site df8da257... is
removed: materialization uses the canonical Outbounds.Save owner recognized by
the D7 closed port guard. Remote missing metadata retains its existing bounded
ownership. R1 base identity and all unrelated exact sites remain unchanged; the
total inventory allowance count is unchanged. No worker/Runtime admission or
automatic legacy inventory regeneration occurs.

R7 E1 reviews the existing `core/box.NewBox` Direct site from `b2370b22...`
to `581fe87f...`. Its containing declaration now constructs the same managers
through narrow provider ABI adapters and initializes native Scope ownership.
The Direct fallback closure and `route.final` input are unchanged. Both pinned
outbound managers retain the same selection: an explicit final must resolve;
otherwise the first configured outbound is selected, with Direct synthesized
only when there is no configured outbound. A missing explicit final fails startup.
`TestBoxProviderAdaptersPreserveOutboundFinal` proves these cases through the
shared Box for both build flavors, without network traffic. Existing HTTP/DNS
path witnesses also require omitted detours to use an explicit proxy final.
This reviews one exact existing site; no additional Direct site, allowance,
Failover policy or SEC-2 interpretation is introduced.

R7 E3 reviews the existing `core/registry.inboundDeclarations` Direct site from
`6fc6344a...` to `f08e6d44...`. The declaration adds Mieru and TrustTunnel
registrations with compile-time capability facts and authenticated Users
bindings. The existing explicit Direct inbound registration is unchanged; the
table does not select a route, detour, outbound, final or fallback. Native
listeners retain the existing CoreRuntime/Scope lifetime, Inbounds owns their
save/apply and Users owns their credentials. Official build stubs make the new
types unavailable. Existing registry/capability, candidate rollback and real
authenticated runtime tests qualify this seam. This reviews one changed
declaration with the same Direct-literal multiplicity; the R1 base identity,
all other sites and total allowance remain unchanged. SEC-2 stays BOUNDED_DEFERRED.

R7 E3b reviews that same `inboundDeclarations` site from `f08e6d44...` to
`c936c51f...`. Its only production delta is two authenticated declaration rows
for SSH and MTProxy. The explicit Direct registration and every route/final/
detour/fallback decision are unchanged. Existing Inbounds save/apply owns the
listeners, Users owns authentication credentials and the common SecretRef
adapter owns sealed SSH host-key material in the existing Inbound row. Scope
shutdown, final-user revocation, credential rotation, port reuse and runtime
restart are exercised against the selected canonical Extended engine. Official
stubs keep these capabilities unavailable and preserve historical fields.
This updates one exact existing declaration after owner/lifetime review; it adds
no Direct site, worker, privilege, policy choice or inventory allowance. The R1
base and unrelated entries stay fixed; no legacy inventory regeneration occurs.

R7 E3c reviews that same `inboundDeclarations` site from `c936c51f...` to
`56df4379...`. The declaration adds only the Sudoku registration with its
compile-time capability fact. Its existing explicit Direct registration and
all route/final/detour/fallback decisions are unchanged. Inbounds retains
save/apply and serving Scope ownership; the canonical engine queue drains raw
and detached HTTP sessions. SecretRef owns the master and per-user delivery
key bag. Sudoku does not declare authenticated Users: KIP is shared key
material, not a revocable or attributable user principal. The registry tests,
complete-candidate rollback, real TCP/UDP session drain, credential rotation,
private Backup/Restore and Classic/Nexus browser cases qualify that boundary.
Official stubs keep the capability unavailable and preserve historical state.
This replaces one exact existing declaration digest after owner/lifetime review;
Direct-literal multiplicity, the R1 base, unrelated sites and total allowance
stay fixed. SEC-2 remains BOUNDED_DEFERRED.

R7 Bond reviews two existing Direct sites: `core/registry.inboundDeclarations`
changes from `56df4379...` to `0b430995...`, and `core/box.NewBox` changes from
`581fe87f...` to `b387c953...`. The registry adds the Bond serving declaration
with its Extended compile-time fact. NewBox calls the existing inbound identity
owner with the actual decoded protocol options, allowing that owner to derive
member namespaces rather than trusting caller-supplied bindings. Each member
retains its credential identity/epoch; publishing or retiring a group publishes
or retires those epochs together. Bonded traffic has no invented client binding.
Native Scope remains the sole owner of child objects; runtime inspection and
existing parent controls borrow those actual objects through a snapshot.
The explicit Direct registrations and every route/final/fallback decision keep
their existing semantics. These are exact reviewed digest replacements with no
new Direct site, multiplicity or budget allowance. SEC-2 remains BOUNDED_DEFERRED.

R7 Call reviews the existing `core/registry.inboundDeclarations` Direct site
from `c8bb9d8a...` to `7a9e59ce...`. Its only production delta is the native
Call registration with the Extended compile-time capability fact. The explicit
Direct registration and route/final/detour/fallback policy are unchanged.
Inbounds retains candidate validation and save/apply; the existing SecretRef
owner seals platform cookies, account passwords and private room links in the
Inbound row. Call creates no authenticated Users or client principal. The
selected native engine owns creator workers, reconnect/signaling transports,
bridges and relay teardown through its serving Scope. Constructor/private
diagnostic race checks, complete-candidate rollback, private archive restore
and provider registry checks qualify these boundaries; remote platform traffic
remains an open R7 gate. Official retains an unavailable capability and its
existing state-preservation path. This replaces one exact reviewed declaration
digest with unchanged Direct multiplicity, R1 base and total allowance; it does
not regenerate the legacy inventory or admit a new worker.

R7 Call Outbound reviews the existing `core/registry.outboundDeclarations`
Direct site from `7f906ce0...` to `39a2d075...`. The only added declaration is
Call with its Extended capability fact. Outbounds retains candidate validation,
save/apply and the existing row; SecretRef seals its room link and optional
cookie bundle with row/field binding. The selected engine owns joiner,
signaling, peer and relay lifetimes through the same serving Scope, independently
of the inbound creator. Official keeps the capability unavailable and preserves
stored state. Focused candidate rollback, private archive restore and native
worker retirement checks qualify these owners. Remote authenticated traffic
remains an open R7 gate. The Direct registration and route/final/detour/fallback
decisions are unchanged. This replaces one exact reviewed declaration digest
with no new Direct site, worker allowance or R1 inventory regeneration.

R7 native client Outbounds reviews that same `outboundDeclarations` Direct
site from `39a2d075...` to `ee279e14...`. Mieru and TrustTunnel add native
Extended registrations; TrustTunnel retains its actual `with_quic` build
constraint. Outbounds owns rows and candidate save/apply, and the shared
SecretRef owner seals each remote password with row/field binding. The engine
owns native clients and retires their admitted request/health workers through
its serving Scope. Focused candidate rollback, masked edit, private archive
restore and source retirement checks qualify these boundaries. Official
retains unavailable capability facts and lossless stored-state preservation.
The Direct declaration, routing decisions, worker allowance and R1 inventory
remain unchanged; only this exact reviewed digest is replaced.

R7 native Bond/CoreFailover Outbounds reviews the existing declaration site
from `ee279e14...` to `f58fba0d...` and the existing tag reference scanner from
`082a464f...` to `cddcb505...`. The registry adds two native Extended groups;
the scanner protects canonical member references during rename/delete/reload.
Outbounds retains row and credential ownership. Anonymous native selectors
borrow the actual canonical runtime members, including their mutable selection
and existing Panel health policy; the outer native group owns its wire protocol.
The scanner also remains the authority for dependency-cycle interpretation.
Its existing final/Direct policy is unchanged. These replace two exact reviewed
digests without a new Direct site, worker allowance or R1 inventory regeneration.
R7 Sudoku Outbound reviews the same `outboundDeclarations` Direct site from
`ee279e14...` to `4d701dbd...`. The added declaration selects the native Extended
Sudoku client. Outbounds owns its existing row, complete-candidate validation
and save/apply; SecretRef seals the client key with row/field binding and
preserves the selected native key language. The shared Classic/Nexus editor
edits that owner and its nested HTTP-mask/TLS options. The canonical engine
owns dials, handshakes, multiplex callbacks and connections through client
retirement. Focused transactional credential/private archive checks and native
TCP/UDP/restart qualification cover those seams. Official keeps Sudoku
unavailable and preserves stored state. This exact declaration digest review
adds no Direct routing decision, worker allowance or new R1 inventory.

R7 XHTTP and accepted native outbound groups compose at scanOutboundRowsForTag with reviewed digest df09e05a1c7b9d2ea00ba85c908370985714bd2a9dba79b17737ad2c3d4914f6. The existing reference owner protects transport.download.detour and native group membership during rename/delete and eager reload. Its existing Direct/final policy is unchanged; no source inventory or new fallback site is introduced.

R7 native XHTTP EndpointIR reviews the existing `ValidateEndpointProjection`
Direct site from `816b6bbc...` to `ec5f5b74...`. The canonical subscription owner
admits XHTTP only in representations preserving its protocol, path, host and
mode. Standard padding is explicit; custom padding, headers or unknown critical
options require a lossless representation or an explicit rejection. The early
return for explicit Direct/group endpoints and all routing/final decisions are
unchanged. Existing parser, round-trip and actual activation suites cover this
boundary. This replaces one exact declaration digest without a new Direct
site, worker allowance or R1 inventory regeneration.

R7 Mieru share delivery reviews the same existing projection declaration from
`ec5f5b74...` to `8216c1dc...`. The EndpointIR owner permits the native simple URI
only for its explicit credential, endpoint, port-binding and carrier fields;
unknown critical options require JSON. Explicit Direct/group handling and all
routing decisions remain unchanged. The existing URI suite proves semantic
round-trip and refusal to discard a detour. This replaces one exact digest;
there is no new Direct decision or worker.

R7 Sudoku delivery reviews the same projection declaration from `8216c1dc...`
to `b20fd020...`. The compact client adapter admits only its declared typed fields,
standard padding and representable HTTP-mask settings; custom padding, TLS,
host overrides and other critical options require JSON. Users remains the
split-key owner; private serving master material never enters delivery.
Existing URI and subscription suites prove default equivalence, semantic
round-trip and actual private delivery without canonical writes. Explicit
Direct/group treatment and routing policy remain unchanged. Only this existing
digest is replaced; no Direct site or worker allowance is added.

The accepted Sudoku, native outbound groups and MASQUE consumers compose at the existing outbound registry declaration with reviewed digest 368115e9a46640be0a2505510a26b3e33b63cc6db1021f4eb2446ecbec1f9750. Each selected native constructor keeps its availability gate. This composition adds no Direct routing decision or inventory site.

R7 source download detours keep the two existing unsync mutations unchanged in
`unsyncConnectionsForDeletedOutbound`. The exported admission wrapper now checks
canonical source references before delegating. This review moves the two exact
existing digest entries to the private helper; no mutation allowance is added.

R7 automatic source health reviews the changed existing refresh closure and one
bounded worker launch in Service.probeSourceTargets. The Component's existing
Start/Stop owner owns the schedule; each admitted round has a 20-second parent
context, at most 32 sources, 128 targets per source and eight workers joined
before return. Per-probe deadlines are at most 15 seconds. Cancellation stops
admission and waits for workers; restore admission encloses the round. Only
CoreRuntime owns health truth and generation/target fences. Cursors are bounded
bookkeeping, not another scheduler or health authority. These two exact launch
digests are reviewed; ticker and unrelated R1 inventory entries are unchanged.

The reviewed source-health refresh closure retains the Host-owned OutboundHTTP port added by download detours. Its lifetime remains the existing Component worker; the port does not introduce another runtime or download scheduler.
