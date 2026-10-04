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
