# Notices

Solovey UI is a modified GPL-3.0 panel project. The distributed source remains
subject to the repository's GPL-3.0 license and to the licenses of its
third-party dependencies.

This repository contains substantial modifications by MalenkiySolovey,
including branding, installer and update flow, diagnostics, backup and rollback
logic, release packaging, frontend/backend changes, component boundaries, and
private-use adaptations.

Modification notice:

- Project renamed to Solovey UI.
- Service, CLI, install paths, release process, and component runtime were
  changed.
- The project keeps attribution links in the README while avoiding operational
  dependency on external panel repositories.

License:

- The project remains licensed under GNU GPL v3.0.
- Original copyright and license notices should remain intact where present.

Project lineage and retained attribution:

- [alireza0/s-ui](https://github.com/alireza0/s-ui)
- [deposist/s-ui-x](https://github.com/deposist/s-ui-x)
- [admin8800/s-ui](https://github.com/admin8800/s-ui)
- [shenaba/2s-ui](https://github.com/shenaba/2s-ui)
- [printfer/v2sing](https://github.com/printfer/v2sing)
- [Sub-Store](https://github.com/sub-store-org/Sub-Store)

The current networking runtime uses
[SagerNet/sing-box](https://github.com/SagerNet/sing-box) v1.14.2 through the
Go module dependency declared by this repository. Dependency source and
license notices remain authoritative for that code.

The QUIC transport dependency has an explicit maintained integration delta:
official `SagerNet/sing-quic` at
`6a3a24d65b99587fad1d4cdd567c88f212acdd63` is resolved to
[`MalenkiySolovey/sing-quic`](https://github.com/MalenkiySolovey/sing-quic)
at `b2eea8ca8762dd1e438171e2f7b56f9715c30cbb`. Its original Copyright (C)
2022 nekohasekai and GPL-3.0-or-later notices remain intact; integration additions
are Copyright (C) 2026 MalenkiySolovey under GPL-3.0-or-later. The original
protocol verifiers and implementations remain authoritative. A constructor hook,
per-service accepted-parent lifetime owner and three small service adapters expose
authenticated parent control; TUIC initial-payload close is synchronized.
Original S-UI v1.6.4 is a behavioral reference without literal quicgrace transfer.
Official sing-box v1.14.2 is the semantic baseline. This patched dependency build
is not byte-identical to unmodified official sing-box. Exact module checksums,
the complete replayable eight-file patch and its digest are recorded under
`deploy/dependencies/`, and package metadata names the replacement explicitly.

The 1.14 runtime foundation and nested-rule compatibility adapt existing
Solovey owners against official source
`af6e64c3b69e6132ebaee0e1a3d24e93903f6709`. No upstream implementation or
tests were copied. Cronet native package assets come from the selected
`github.com/sagernet/cronet-go/lib` modules at
`v0.0.0-20260912104006-c10c03c318db`, authenticated by Go module checksums
and the target-specific SHA256 in the package producer. The Cronet source
and GPL-3.0 notices remain authoritative for those assets.

The Alpine Docker producer selects the official static musl Cronet asset from
those same pinned modules, using LLVM to link it. The final image records its
selected archive hash and module/version in `CRONET_INTEGRATION.json`; it does
not ship the glibc shared object. This is packaging integration, without a
native implementation patch or new module version. The original Cronet source
and license notices remain applicable to the statically linked bytes.

The project-wide architecture audit compared additional firewall, panel,
installer, and networking projects. No production code was copied from those
comparison sources. GPL/AGPL and unknown-license comparison sources are
reference-only unless a later change explicitly records a compatible transfer
and updates this notice.

The IP-01/IP-02 policy loading and atomic admission changes independently adapt
behavior reviewed in deposist/s-ui-x v1.5.12-beta6
(`08814445d1497bc8b52bdc3af2eda784a5ea26cb`), primarily witnessed by
`590b08af39537ab4c85441513743c2360bbc187d` in its IP monitor and tracker.
No upstream implementation or tests were copied. Solovey's IP monitor retains
policy, privacy and persistence ownership; its tracker uses an injected atomic
observer. This records a partial behavioral adaptation, not full S-UI-X parity.

TOK-01/TOK-02/TOK-03 independently adapt token ownership, reset-policy denial
and successful account-mutation authorization refresh from the same pinned
S-UI-X target, witnessed by `590b08af39537ab4c85441513743c2360bbc187d`
in `service/user.go`, `api/apiService.go`, `api/apiV2Handler.go` and
`api/apiHandler.go`. Solovey retains its account/token semantic owner, hashed
token storage, scoped permissions, typed sessions and step-up policy. Its
authoritative request check and revision-fenced snapshot refresh are native
implementations; no upstream implementation or tests were copied.

RULE-01/RULE-02 and GEO-01 independently adapt bounded rule-condition
validation, shared Doctor findings and pre-narrowing geosite enum checks from
the pinned S-UI-X target above. Witnesses are
`590b08af39537ab4c85441513743c2360bbc187d` (`core/rule_conditions.go`,
`service/config.go`, `service/doctor.go`) and
`4d2fc76e12a7b972c3126c6000bc57bdd50ec959` (`service/geosite_v2ray.go`,
`service/geosite_ru_smart.go`). Solovey uses the existing validation, config
storage and geosite owners with the official sing-box v1.13.14 grammar.
No upstream code or tests were copied; these are partial behavioral adaptations.

TOK-04 and IMPORT-01 independently adapt import-completion authorization
refresh and checked source SQLite handle extraction from the same pinned
target. Witnesses are `590b08af39537ab4c85441513743c2360bbc187d`
(`api/apiHandler.go`, `api/apiHandler_routes_test.go`) and
`9bcf2cd2fc27a995138a419e7566e6f3778ecffe`
(`database/importxui/source.go`, `database/importxui/history_routing.go`).
Solovey retains component-owned import transactions and the host's token
snapshot owner; native restore signals durable acceptance before optional
retention. Source connections enforce their read-only untrusted boundary.
No upstream implementation or tests were copied.

RESTORE-01 independently adapts live database operation quiescence from the
same pinned target, witnessed by `590b08af39537ab4c85441513743c2360bbc187d`
(`database/db.go`, `database/backup.go`, `paidsub/payment.go`,
`paidsub/poll.go`, `service/user.go`,
`database/integration_backup_restore_test.go`). Solovey's SQLite owner drains
finite semantic operations and actual driver connection lifetimes, rejects
retired handles, and supplies an expiring private restore context. Existing
backup and feature owners retain durable acceptance, exact rollback and
context-aware rebinding. Schema, steady SQLite settings and deployment/storage
contracts are retained. No upstream implementation or tests were copied.

CAP-01 independently adapts actual allowed/build-available type projection
from the same pinned S-UI-X target. Exact behavioral witnesses are
`4d2fc76e12a7b972c3126c6000bc57bdd50ec959`,
`590b08af39537ab4c85441513743c2360bbc187d`,
`d5717d95253d4cc1dfbf60894b0c7f08390253ad` and
`08814445d1497bc8b52bdc3af2eda784a5ea26cb`
(`core/capabilities/*`, `api/apiHandler.go`, frontend typed projections).
Solovey keeps official runtime registrations and compiled eligibility in the
same owner contributions; panel aliases remain entity-owned. WireGuard is
directly registered, and official OOM support is retained. Pinned official
sing-box v1.13.14 sources are secondary implementation authority. No upstream
implementation or tests were copied; full parity remains v1.5.10.

CAP-02/CAP-03/CAP-04 independently adapt eligibility-aware save, runtime
selection and diagnostics from pinned S-UI-X v1.5.12-beta6. Behavioral witnesses
are `590b08af39537ab4c85441513743c2360bbc187d` and
`ccd7cc47437b2caed630f217a30811ecaf4eee88` (`service/config.go`, entity
services, capability save/projection tests, `service/doctor.go` and Doctor
capability tests). Solovey entity, reference and diagnostic owners consume the
same narrow registration facts; they retain persistence, runtime and UI
behavior. Stored WARP identity maps to the official WireGuard endpoint without
renaming storage. Pinned sing-box v1.13.14 default endpoint, Tailscale DNS and
DERP lifetimes require full restart for references captured at initialization.
No upstream implementation or tests were copied; full parity remains v1.5.10.

CAP-05/CAP-06 independently adapt capability-aware import and local client
delivery from the same pinned target. The behavioral witness is
`590b08af39537ab4c85441513743c2360bbc187d`
(`database/importxui/{plan.go,importer.go,report.go}`,
`core/capabilities/{capabilities.go,protocols.json}`, `util/genLink.go`).
Solovey captures target facts for canonical import planning, preserves source
mapping and transaction ownership, and reports excluded objects explicitly.
Inbound owners provide immutable credential/delivery schema; local adapters
consume serving-inbound availability while portable codecs retain encoding.
Official sing-box v1.13.14 validates option-dependent candidates before import
commit. No upstream implementation or tests were copied; the full parity
boundary remains v1.5.10.

PAID-02/PAID-03 independently adapt legacy purchase fail-closed handling and
recoverable provider invoice intent from deposist/s-ui-x v1.5.12-beta6
08814445d1497bc8b52bdc3af2eda784a5ea26cb, introducing witness
590b08af39537ab4c85441513743c2360bbc187d (paidsub model/schema/payment and
schema/cryptobot_order tests). Solovey retains its component-owned immutable
grant snapshot, schema, injected database lifetime and provider transport;
durable constraints arbitrate creation and application. No upstream code or
tests were copied; full parity remains v1.5.10.

PAID-07/PAID-08/PAID-09 independently adapt exact provider payment metadata,
invoice identity and bounded failure handling from that frozen target
(`paidsub/provider_cryptobot.go`, `payment_authenticity_test.go`,
`provider_cryptobot_reconcile_test.go`; witness `590b08af39537ab4c85441513743c2360bbc187d`).
The existing Solovey provider adapter retains injected transport and consumes
immutable purchase facts; normalized outcomes stay inside paid-subscriptions.
No upstream implementation or tests were copied; full parity remains v1.5.10.

PAID-04/PAID-05/PAID-06 and the remaining PAID-03 recovery boundary independently
adapt bounded provider reconciliation, persistent fair work rotation, legacy
invoice cancellation and terminal-only expiry from the same frozen target
(`paidsub/poll.go`, `provider_cryptobot.go`, `provider_cryptobot_reconcile_test.go`,
`payment.go`; witness `590b08af39537ab4c85441513743c2360bbc187d`). Solovey's paid
component owns recovery/application transactions, cancellation and cursor tables,
and recoverable refund intent; its existing adapters and injected database
lifetime retain external operations. No upstream implementation or tests were
copied, and full parity remains v1.5.10.

Wave 5 HEALTH-01/FE-01/WS-02/FE-02 independently adapt bounded recent probe
observations, stale history/socket generation fencing and Firefox preload
recovery from deposist/s-ui-x v1.5.12-beta6
`08814445d1497bc8b52bdc3af2eda784a5ea26cb`, behavioral witness
`590b08af39537ab4c85441513743c2360bbc187d` (`service/health_snapshot.go`,
`cronjob/failoverProbe.go`, `frontend/src/components/IpHistoryModal.vue`,
`frontend/src/store/ws.ts`, `frontend/src/router/preload-error.ts`). Solovey's
core probe runtime owns ephemeral observations, stats consumes a copy, and
existing frontend feature/router/transport owners retain their lifecycles.
No upstream code or tests were copied; full parity remains v1.5.10.

Wave 5 editor/presentation adaptation independently uses behavioral witnesses
590b08af39537ab4c85441513743c2360bbc187d and
4d2fc76e12a7b972c3126c6000bc57bdd50ec959 from deposist/s-ui-x
(v1.5.12-beta6, 08814445d1497bc8b52bdc3af2eda784a5ea26cb, GPL-3.0).
Scope: shared save explanations, core navigation projections, traffic timezone,
inbound guidance/address identity and selected technical labels. No literal
source, test or translation transfer; full parity boundary remains v1.5.10.

Wave 5 Telegram component behavior is independently adapted from the frozen
deposist/s-ui-x v1.5.12-beta6 target (08814445d1497bc8b52bdc3af2eda784a5ea26cb),
witness 42baef5bed45a9a5b91d728e7e2250b524e4a41b (GPL-3.0): bounded chat discovery
and save-before-Test settings lifecycle. No literal source/tests/translations
transferred; paid-subscriptions ownership and full parity boundary stay unchanged.

Build asset publication, native Windows invocation/path identity, source hygiene,
audit tool reproducibility and controlled benchmark comparison are independently
adapted from deposist/s-ui-x v1.5.12-beta6
(08814445d1497bc8b52bdc3af2eda784a5ea26cb, GPL-3.0), behavioral witnesses
4d2fc76e12a7b972c3126c6000bc57bdd50ec959 and
590b08af39537ab4c85441513743c2360bbc187d. Existing Solovey component manifests,
runtime closure and platform package owners retain their contracts. No upstream
source or tests were copied; the complete parity boundary remains v1.5.10.

The networking dependency update uses official sing-box v1.13.18 source
45ca32dcb966f07f97fc888fe8586e359dbe8405 and sing v0.8.13 source
7c349dacf402256d3a7029746073b05d2ead584a through their Go modules. The latter
adds reviewed UDP/cancellation and untrusted-length/succinct-set hardening beyond
the core's v0.8.12 minimum. Frozen deposist/s-ui-x v1.5.12-beta6 dependency and
tracker changes, witnessed by 87c1fd049aeb2a7d9046656c801a4e643e73099a,
590b08af39537ab4c85441513743c2360bbc187d and
9bcf2cd2fc27a995138a419e7566e6f3778ecffe, informed independent revalidation of
the existing Solovey owners. No upstream application source or tests were copied.
Module licenses and source notices remain authoritative; the complete parity
boundary remains v1.5.10 until deployment qualification is accepted.

Completed source and bounded physical qualification close the frozen
deposist/s-ui-x v1.5.12-beta6 parity boundary
08814445d1497bc8b52bdc3af2eda784a5ea26cb. All 44 implementation units are
complete with sing-box v1.13.18. The existing Solovey owners were validated for
TCP/UDP admission and tracker restart, SQLite restore lifetime and authority,
WARP/WireGuard save/apply/reapply, and installed paid-component persistence with
synthetic nonmonetary state on FriendlyWrt 25.12.5. Earlier v1.5.10 boundaries
above record integration history. No upstream application source or tests were
copied; existing module licenses and notices remain authoritative.
# Stage 2 private runtime API adaptation

The private CoreRuntime API boundary independently adapts official sing-box
v1.14.2 (af6e64c3b69e6132ebaee0e1a3d24e93903f6709, GPL-3.0-or-later)
service/api, daemon/attached_service.go, daemon/server.go and StartedService
protocol semantics through the existing Go module. Generation publication,
credential redaction, typed observations and bounded operations remain Solovey
owners. No upstream application source or tests were copied; raw browser API
and host daemon controls are excluded. This is partial Stage 2 integration and
does not advance the original S-UI complete parity boundary or product release.

Live client-flow listing independently adapts the behavior of alireza0/s-ui
v1.6.3 (13abbdc431ae31ec78a54f5e6643b7fd70b6e634) and its pinned frontend
691346925b65e485b1cc5c70ca952819bbbe6047. Official v1.14.2 connection IDs and
protocol authentication contexts supply runtime identity; Solovey owns stable
client bindings, retired-inbound admission and scoped Classic/Nexus projection.
Parent transport limitations remain explicit. No application source or tests
were copied. Existing dependency licenses remain authoritative.

Maintenance, current outbound groups and bounded log projection independently
adapt the same pinned original S-UI behavior and official sing-box v1.14.2
StartedService semantics. Solovey's existing settings owner persists deliberate
maintenance; its existing lifecycle serializer applies intent, and the shared
Classic/Nexus consumers expose generation-bound actual state. Member probes
retain the existing bounded probe owner. No upstream application source or
tests were copied; this partial integration does not advance complete parity.

Stage 2 Snell integration uses official sing-box v1.14.2
(af6e64c3b69e6132ebaee0e1a3d24e93903f6709) and its pinned sing-snell module
v0.0.0-20260829071736-20f2eaec77c3 (GPL-3.0-or-later). Solovey independently
implements version-specific validation, stable managed credentials, admission
and shared editors. Original S-UI v1.6.3 backend
13abbdc431ae31ec78a54f5e6643b7fd70b6e634 and frontend Snell witness
691346925b65e485b1cc5c70ca952819bbbe6047 (ancestor of the accepted v1.6.3
frontend gitlink f859e16953cd733293618f626cc19b8466e00fd3) provide behavioral
references only. No application source or tests were copied. Dependency
licenses/notices remain authoritative. This partial integration does not
advance the complete original-SUI parity boundary or the product release.

Stage 2 selected DNS integration links the official sing-box v1.14.2
mDNS and resolved constructors at af6e64c3b69e6132ebaee0e1a3d24e93903f6709
(GPL-3.0-or-later). Original S-UI backend 13abbdc431ae31ec78a54f5e6643b7fd70b6e634
and frontend 691346925b65e485b1cc5c70ca952819bbbe6047 provide behavior witnesses.
Solovey independently owns read-only environment classification, portable DNS
validation, persisted service references and shared Classic/Nexus fields. No
application source or tests were transferred. The existing godbus/dbus/v5
v5.2.2 dependency (BSD-2-Clause) supplies local D-Bus metadata calls; its
license remains authoritative. No platform privilege or release boundary
changes are implied by this partial integration.

Stage 2 selected subscription integration independently adapts behavioral
witnesses from original S-UI v1.6.3 util/genLink.go, util/host.go and subscription
adapters at 13abbdc431ae31ec78a54f5e6643b7fd70b6e634, and shared editor witnesses
at frontend 691346925b65e485b1cc5c70ca952819bbbe6047 (GPL-3.0-or-later).
Official sing-box v1.14.2 at af6e64c3b69e6132ebaee0e1a3d24e93903f6709 and its
pinned sing-quic module remain the runtime schema and port-range authorities.
SIP002 and Mihomo client documentation guide format-specific projection;
Solovey's existing subscription, TLS, client and settings owners retain
canonical state, public disclosure policy and lifecycle. No application source
or tests were copied. Existing dependency licenses remain authoritative. This
partial integration does not advance the complete original-SUI parity boundary
or the product release.

Stage 2 selected frontend statistics and random-bound behavior independently
adapts original S-UI frontend Stats.vue, History.vue, randomUtil.ts and utils.test.ts
at 691346925b65e485b1cc5c70ca952819bbbe6047 (GPL-compatible project lineage).
Backend util/password.go at 13abbdc431ae31ec78a54f5e6643b7fd70b6e634 is a behavior
witness only; Solovey retains its modern Argon2id policy. Existing shared fetch,
timezone and cryptographic utility owners provide the implementation. No original
application source or tests were copied; no dependency or complete-parity/release
boundary advances are implied.

Stage 2 selected auto-HTTPS behavior adapts the original S-UI
network/auto_https_conn.go and tests at
13abbdc431ae31ec78a54f5e6643b7fd70b6e634 as GPL-compatible behavior witnesses.
Existing Solovey network/autohttps owns bounded classification, stream replay,
validated authority and caller deadline preservation. No original application
source or tests were copied; no TLS, dependency, privilege, complete-parity or
release boundary changes are implied.

Stage 2 Wave5 CORE-57 cache/lifecycle qualification uses official sing-box
v1.14.2 source af6e64c3b69e6132ebaee0e1a3d24e93903f6709 with a narrow
service/ssmapi patch. Public replacement MalenkiySolovey/sing-box source
afd12f10473eb4d2d1459897488deb623527782e resolves as
v1.14.3-0.20261010211422-afd12f10473e. This pseudo-version is module metadata;
the semantic baseline remains official v1.14.2. Original and integration source
are GPL-3.0-or-later. The replayable delta, source pin and checksums are in
deploy/dependencies/sing-box-ssm-cache.patch and sing-box-ssm-integration.json.
Only the SSM cache/lifecycle seam changes; protocol services, toolchain and
dependency requirements retain the baseline. Solovey independently owns private
deployment files, redacted diagnostics and its existing backup contribution.
Original S-UI v1.6.4 SSM registration was a behavioral reference; no application
source was copied. This is patched core source, not byte-identical official
sing-box; QUIC integration remains separately recorded. No physical-parity,
product-version or release-boundary advancement is implied.

CORE-57 physical follow-up preserves normal SSM hot removal when HTTP shutdown
has already closed the served TCP listener. Only that owner-local expected
net.ErrClosed is accepted; TLS/cache errors retain their existing reporting.
The complete cumulative source patch remains confined to the five recorded files.
