# Foundation regression budget

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
