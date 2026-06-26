# Release Policy

Version source of truth:

- `config/identity/version` contains the application release version.
- The value must be SemVer-shaped as `YYYY.RELEASE.PATCH[-PRERELEASE]`.
- Solovey UI release numbers are independent from upstream S-UI/S-UI-X
  versions. Use year-based SemVer: `YYYY.RELEASE.PATCH[-PRERELEASE]`.
- The value must not include a leading `v`.
- The value must not include build metadata.
- Prerelease identifiers must be lowercase SemVer identifiers.

Git release tags:

- Git tag names use `v` plus the exact `config/identity/version` value.
- Example: `config/identity/version` = `2026.1.0`, Git tag = `v2026.1.0`.

Database version policy:

- `settings.version` records the newest application version that successfully
  migrated or adapted the database.
- `settings.version` must never be downgraded by an older binary.
- Legacy database values with only `MAJOR.MINOR` are accepted for comparison
  and treated as `MAJOR.MINOR.0`.

Release checklist:

- Update `config/identity/version`.
- Add `Unreleased` changelog entries before cutting the tag, then move them
  under the release heading.
- Add or update migrations when the schema changes.
- Run `go test ./config ./database ./service`.
- Run the full validation gate before publishing artifacts.
