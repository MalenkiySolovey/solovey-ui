# Build and audit reliability

Vite owns `frontend/dist`, its production entrypoint and its manifest. Before a
backend embeds that output, `scripts/frontend-assets.mjs publish` checks the
complete candidate, copies it to an isolated staging directory, rechecks its
contents and hashes, then swaps complete directory generations. A failed copy,
verification or final rename preserves the previous embedded assets. Staging
and retained backups stay outside the Go embed root. Publication is serialized;
a concurrent invocation fails before touching the destination.

The dist must contain a nonempty `index.html`, a production Vite manifest and
JavaScript assets. Manifest edges and generated HTML/CSS/JavaScript references
must resolve. Symlinks, nonregular files, unsafe paths and directories excluded
by Go are rejected. Go's existing `all:` embedding supports underscore asset
names and Vite metadata. CI/frontend artifacts include hidden metadata so a
downloaded candidate retains the same validation contract.

Optional component bundles keep their existing `assets.json` and runtime closure
owner. For a split full dist, supply `--components-dir` with installed component
packs; the publisher consumes that owner's validated provider proof. Core
frontend publication does not absorb component packaging or installation.
Linux, Windows, release packaging, Docker and OpenWrt use this same publication
owner. A cleanup failure after a successful swap reports the retained backup;
the newly published complete tree remains authoritative.

The native Windows entrypoint accepts explicit `-Architecture amd64|arm64`,
`-Profile full|core` and `-OutputPath`. It derives the repository from its script,
rejects unknown arguments, preserves the caller's directory/environment and
never prompts. A native matching Go/CGO toolchain is required for SQLite; build
failure does not select a weaker profile. The batch wrapper delegates to it.
Frontend tests resolve their root through the native filesystem before Vitest
starts, retaining arguments and exit status.

`frontend/.node-version` pins the workflow Node runtime and its bundled npm.
`scripts/audit/tools.json` pins Go analysis/comparison tools; the installer checks
the resulting executable's module identity. Lockfiles, exact Go version and
existing pinned Actions/download digests retain their current authorities.
The required build, vet, blocking lint, staticcheck, vulnerability and frontend
lint/typecheck/unit gates remain required. Full lint, gosec, E2E, accessibility
and performance retain their existing advisory policy. Vulnerability databases
remain current security evidence rather than frozen historical data.

The required source hygiene check enforces gofmt, LF for owned Unix/service and
workflow text, and syntax for tracked shell entrypoints. Generated component
imports obey the same formatting contract. Windows validates text/Go directly;
Linux also validates each script with its declared shell.

The optional performance workflow benchmarks isolated exact baseline/candidate
commits on one runner. Both use the same toolchain, frontend profile, Go tags,
package set, CPU setting and six one-second samples. The representative set
covers API requests/realtime, tracker compilation, stats/admission service work
and resource projection. Raw outputs, setup logs, identity, pinned benchstat
output and JSON samples/medians are retained. Missing, unequal or incomplete
observations fail the harness visibly. Median increases above the existing 20%
threshold are advisory; inspect samples and benchstat for noise before changing
production code or policy.
