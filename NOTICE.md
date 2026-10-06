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
[SagerNet/sing-box](https://github.com/SagerNet/sing-box) v1.13.14 through the
Go module dependency declared by this repository. Dependency source and
license notices remain authoritative for that code.

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
