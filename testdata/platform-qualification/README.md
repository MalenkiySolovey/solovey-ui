# Installed artifact qualification

This opt-in fixture consumes an already built Extended panel. It does not build
the product, publish artifacts, or replace the accepted protocol qualification.
The Mieru client uses the SDK from the selected `go.extended.mod` graph and tests
authenticated TCP and UDP echo over its TCP and UDP carriers.

Run `node testdata/platform-qualification/runtime-campaign.mjs configuration.json`
on an isolated fixture with a free panel port. The configuration selects the
artifact, pinned peer executable, private output directory and optional Core or
Official artifacts. Windows CI invokes the production installation preparation
owner first, then runs the installed binary on the native runner architecture.
The loaded Cronet DLL is checked against the installed companion digest.

The campaign exercises configuration/startup, credentials and SecretRef identity,
core/process restart, native Naive initialization, occupied-port failure/recovery,
canonical backup, authenticated restore rehearsal/execution and restored traffic.
An optional Official artifact also exercises unsupported desired-state retention
and return to Extended. Container and emulator adapters are explicit configuration
inputs; they cannot turn their observations into physical hardware proof.

Only `result.json` is suitable for CI upload. It records completion separately
from individual checks, so a partial run cannot be interpreted as a passing
campaign. Credentials, database, raw API responses, exports and logs remain
private fixture state. Resource admission and restore step-up authorization use
the actual product owners.
