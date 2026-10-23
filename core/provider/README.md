# Embedded core source identity

`core/provider` observes the single embedded engine from Go build metadata.
`pins.json` binds the selected replacement module path, exact version and module
checksum to its source commit and semantic baseline. Official remains the default;
the root `go.mod` and `go.sum` still select the accepted Official integration.

The observed identity distinguishes provider version, runtime semantic version,
source repository/commit and artifact state. A matching source pin yields
`state=VERIFIED`; this verifies source metadata only. A Go module checksum is not
a binary digest, so artifact state remains `UNKNOWN` until the existing
Artifact/Trust and Deployment owners provide independent evidence. This catalog
does not qualify a platform, enable a protocol or authorize a provider switch.

Missing metadata, unknown replacements, local source paths, ambiguous modules and
checksum drift yield `UNKNOWN` with reason codes. They never infer Official.
Unrecognized metadata is not reflected into diagnostics. The observed build
identity is immutable for the process; each caller receives its own reason slice.

The server summary, capability snapshot, Operations status, Doctor and CLI use
this owner. Classic and Nexus share the same wire reader and summary component.
The identity is observational: there is no database setting, migration, new
permission or alternate runtime owner. Runtime registration facts remain owned by
the capability registry and must be evaluated separately.

The pinned Shtorm source is a research identity. Native Extended build selection,
its maintained patches and root replacements, field capabilities, maintenance
transition and artifact qualification are separate R7 gates. A source identity
alone cannot make those operations available.
