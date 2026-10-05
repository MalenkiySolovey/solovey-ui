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
