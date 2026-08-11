# Server Protection Engineering Boundaries

This component follows the host component and registry contracts. New resource
types should extend those generic seams instead of creating component-specific
host APIs.

## Package responsibilities

- Keep packages and files named for stable responsibilities, not milestones.
- Core and sibling components must not import `server-protection`.
- Product source must not import test-only packages or add runtime switches for
  external integration fixtures.
- Process execution belongs only to the restricted helper adapters. Other
  services use typed helper requests.
- Linux-specific behavior must have a fail-closed non-Linux implementation.
- Core-owned local-proxy facts and health live in neutral host contracts and
  core/resource-inventory providers. This component may consume them but must
  not gain credential access or become a second runtime or lease authority.
- Local Proxy Guard may reserve, fence, activate, renew, or release only the
  provider-owned exact endpoint lease. It must never accept bind, port,
  destination, credential, raw proxy configuration, or system-proxy mutation
  input.

## Safety invariants

- Unknown or incomplete capability, inventory, ownership, listener, health,
  revision, lock, or rollback facts are unsupported.
- Validate managed roots after symlink resolution and keep all mutations below
  the resolved managed boundary.
- Preserve exact operation, instance, PID, revision, and lease fencing across
  every transition.
- Never expose arbitrary shell, system service, signal, executable, flag, path,
  configuration, or destination inputs through helper or API contracts.
- Redact secrets before persistence, logging, audit, or recovery-bundle output.
- A recovery bundle is required before publishing a terminal rollback failure.
- A prepared local-proxy reservation is pre-mutation state. Restart must release
  it and must never infer applied state from component history alone.
- Mixed health is atomic across every protocol in its exact reference. SOCKS UDP
  association is diagnostic-only and cannot satisfy a static UDP-listener gate.

## Verification

Use focused package and state-machine tests while developing. Before release,
run the component architecture tests, the normal full and minimal product test
profiles, frontend type checking/build, and a production SQLite binary build.
System-level fixtures and acceptance controllers are maintained separately and
must consume only exported product packages.
