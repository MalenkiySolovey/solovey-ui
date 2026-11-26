# Component packaging and runtime state

Components are in-process. Code availability, pack files, installed inventory,
enabled policy, active lifecycle and durable data are separate facts.

External Component source inputs are accepted in `components/sources.lock.json`.
Each entry binds an approved source repository, immutable Go module version,
source commit/tree, Go checksums, manifest hash and the complete frontend file
inventory/digest. `node scripts/component-source-inputs.mjs prepare` resolves the
real module and verifies both provider modfiles before producing disposable,
generated `.component-sources/<id>` build inputs. Local replacements, workspace
substitution, source drift, unlisted frontend files and symlinks fail closed.
The existing `component.json` parser remains the runtime manifest authority.

The resolver also reconstructs the downloaded module's complete Git tree and
checks the contribution's `HOST_UI_PIN.json` against the product's public UI
exports, frontend dependency lock and Node version. The installed pack verifier
derives entry identity from the accepted lock, so offline pack verification
does not require the producer checkout or a module cache. Frontend policy
ratchets inspect the verified external source alongside local contributions.

The canonical source/NOTICE/SBOM producer includes the component module archive,
original license and notice, source commit/tree, frontend source digest and
product input digest. These records extend the existing provenance output;
the product release channel and in-process installation model are unchanged.

Frontend builds run preparation automatically. Vite compiles verified external
entries into the same application and packaging walks their dynamic imports,
CSS and locale chunks. Core/minimal selection excludes those entries. External
first-party UI uses `@solovey/component-ui`, whose public exports forward to the
existing registry, settings, secret fields and HTTP owners. This is a build API
for reviewed first-party contributions; installed code still uses the existing
ComponentHost admission and product update channel.

| Delivery | Compiled backend / frontend bytes | Pack files and installed inventory |
| --- | --- | --- |
| Generic Linux full | Full optional Go code; core frontend embedded; extracted component frontend in packs | Installer installs all component packs and full inventory |
| Generic Linux core/minimal | Separate minimal binary omits optional in-process component imports and optional frontend | No optional component packs selected |
| Generic Linux custom selection | Any selected in-process component requires the full binary, including other compiled optional backend code; no custom public binary per selection | Installer copies selected packs, removes unselected pack directories, and writes selected inventory |
| OpenWrt APK | Canonical producer builds full backend and core embedded frontend | All component frontend packs and full initial inventory are package-owned under `/usr/lib/solovey-ui`; runtime state does not rewrite APK bytes |
| FriendlyWrt | Same OpenWrt APK for the appropriate target | Same package plus deployment storage injection; no package fork |
| Windows full | One full executable per architecture; optional Go backend and full frontend are compiled/embedded | No separately extracted component packs or initial `installed.json` in the ZIP; runtime installed/enabled state gates activation |

**Disable** preserves installed inventory and durable data, changes enabled
policy and reconciles lifecycle. **Remove** removes the component from installed
runtime inventory and reconciles/stops activity while preserving durable data.
Owner-specific readiness, routes, jobs and runtime access follow reconciliation
and active-state checks. Installed and enabled do not alone prove active or
operationally ready. Defaults can leave components disabled.

In particular, a fresh Windows ZIP contains full code but does not establish
optional installed inventory. `InstalledIDs` returns an empty set when metadata
is absent; existing explicit metadata remains authoritative on an existing
deployment. Full binary availability must not be described as all components
installed, enabled or active. This release audit documents that current behavior
without changing Windows initialization or component runtime semantics.

The runtime Remove operation does not delete compiled Go code, embedded frontend
or package-owned pack bytes. Physical omission/deletion is claimed only for an
actual minimal binary or the generic Linux installer's selected pack file
operations. Drop Data is a separate explicitly authorized owner operation.
Panel Update is intrinsic and has no pack or installed/enabled lifecycle state.
Historical `panel-update-ui` metadata is accepted for compatibility; trying to
change the old ID through the component management API is rejected.

Native systemd deployments explicitly inject `SUI_COMPONENTS_MANAGEMENT=installer`.
Their pack directory and inventory are one installer-owned generation:
`root:solovey-ui`, directories `0750`, data/metadata files `0640`. The panel
can read the selected inventory and change enabled policy in its database;
runtime Install/Remove are unavailable. Use the native installer's component
selection to change the installed generation. Atomic metadata publication sets
ownership/mode before rename, and transaction rollback restores the saved generation.
The service cannot replace metadata or pack assets. The secretbox creation
umask is scoped to its function's subshell. Portable runtime inventory retains
its existing process-owned `0600` atomic-write contract; native permissions
are not imposed on Docker, Windows or OpenWrt package production.

Source authority: `install.sh` (`resolve_binary_profile`, component pack
installation), `scripts/generate-component-imports.mjs`,
`scripts/openwrt-stage-build.sh`, `.github/workflows/windows.yml`,
`composition/componentmanagement/manager.go`, and the installstate,
enabledstate and lifecycle adapters under `componenthost/`. The manager uses
the shared `solovey-host` component policy; Backend owns the HTTP boundary.

## OpenWrt release mapping

| Consumer | Release assets | Qualification scope |
| --- | --- | --- |
| OpenWrt 25.12.5 x86/64 | `solovey-ui-openwrt-x86-64.apk` | Source-owned target profile; built and provenance-verified by canonical producer |
| OpenWrt 25.12.5 rockchip/armv8 | `solovey-ui-openwrt-rockchip-armv8.apk` | Source-owned target profile; physical support remains bounded by existing qualification |
| FriendlyWrt 25.12, qualified NanoPi R76S | Same rockchip APK + `solovey-ui-friendlywrt-storage.json` | Existing deployment-storage contract and actual `/opt` mount proof |

BUILT, SOURCE-QUALIFIED and PHYSICALLY QUALIFIED are distinct claims. Publishing
an APK adds no new physical qualification. Package identity depends on the
OpenWrt release, target/subtarget, package architecture, immutable product source
and package build/release contract. SD/eMMC/NVMe/USB do not change it; changes to
persistence topology require deployment qualification. No per-component APKs
or storage-medium-specific APKs are produced.

The existing signed `solovey-ui-release.json` retains generic Linux/update
semantics. It does not sign APKs, Windows ZIPs or the FriendlyWrt descriptor.
OpenWrt assets have exact SHA-256 sidecars and source-bound canonical producer
evidence retained with the release workflow. A new signing schema is not needed
to publish these additional canonical assets, and is not introduced here.

## Declared in-process Host API

Migrated Components declare `hostAPI.version: solovey.component-host/v1` and
the required `lifecycle`, `api` and `dropData` ports in their existing manifest.
Registration validates compatibility and adapts `PortLifecycle` into the same
supervisor, installed/enabled state and observed lifecycle used by other
Components. `RegisterPortAPIRoutes` retains the authenticated ComponentHost
route group. Unknown versions/ports and unavailable dependencies fail closed;
a typed Component cannot fall back to the legacy broad route registrar.

Telegram consumes `scheduler`, `outbound.http`, `audit.record` and
`http.request`. Its `settings.drop-owned` port is available only to the data
drop entry point and is bound to the manifest's Component ID. The settings
owner uses the existing durable ownership declaration, including secret keys,
even after runtime settings contributions have been unregistered. The Component
receives no database handle or caller-controlled setting-key deletion API.

The typed port bundle has the actual Local Host reference and only declared
dependencies. It contains no Runtime, filesystem, executor or database handle.
Existing Components can retain the legacy contract until their real consumers
are migrated. Legacy migration/restore/backup hooks must be migrated explicitly
before typed admission; the adapter rejects them rather than silently omitting
their semantics. This is an in-process contract, not a process sandbox.

## BlockCheck

Full can install BlockCheck independently of Zapret. It is disabled by
default. Only an authenticated administrator can edit its catalog or start a
bounded foreground run. Disable cancels that run and preserves the catalog and
history. The existing component-owned settings and database backup hold
`blockcheckGroupsV1` and `blockcheckHistoryV1`. Administrators can see retained
run history for up to 14 days, within a 512 KiB storage budget. A group whose
details do not fit is explicitly marked omitted. The stored reports exclude
attempted addresses, raw DNS answer sets and local socket details. Core/minimal
omit the Component and its UI.

Each service card can run independently. Check All uses one active generation
and the enabled groups in catalog order, runs up to three probes at a time,
and streams per-probe and per-group results. Cancellation names that exact
operation. The legacy single-response endpoint still has a separate response
budget. The saved-catalog byte limit bounds database and transfer work, not
the number of checks in a streamed suite.

Panel mode borrows an active anonymous HTTP or mixed ingress in the current
Core generation. It models a local proxy client, not a remote client identity.
The Core's DNS rules receive that ingress context; configured DNS transport is
not inferred. The normal routing path may inspect the request before choosing
an outbound. The dispatch guard admits only the requested literal address and
port, or the original name resolved to that one permitted address, through a
reviewed leaf outbound. An address rewrite, native rejection, or unverified
egress yields no endpoint result and never retries through another path. A
successful ingress CONNECT is not an endpoint TCP observation. Bare TCP,
forced DNS transports, UDP, authenticated/TLS ingresses and ingress detours
are not supported by this local diagnostic context.

The selected Outbound mode borrows the active Core generation. It supports
Direct and the audited literal-TCP HTTP, SOCKS and Snell paths, with no
diagnostic fallback to another Outbound. Neither a CONNECT acknowledgment
nor a Snell stream that defers the endpoint request proves a TCP handshake at
the destination. An HTTP response or verified TLS handshake over the stream
is reported separately. An explicit DNS server uses its existing configuration
and detour; without one, lookup uses the active Core's DNS policy without
ingress metadata. Configured transport is not wire evidence (UDP can retry
over TCP). Probe results do not assert the effective interface, a group member,
or a Zapret match when the owner does not observe it.

For QUIC and HTTP/3, an applied Direct Outbound can open a connected UDP/443
socket to the admitted literal address. The probe verifies TLS 1.3 on that
socket; HTTP/3 issues one HEAD request without following redirects. A proxy's
advertised UDP support does not establish a connected-datagram contract. On
other paths the probes report unsupported without retrying via Direct or TCP.

Compare DNS runs for the same target, family, Host, Outbound and generation.
The report distinguishes NXDOMAIN, an empty answer, a resolver error, a
policy-rejected address and different address sets. A difference alone does
not prove DNS tampering, and the report does not expose the raw answer sets.

HOST_BASELINE can use ordinary, unmarked Linux Host sockets in an attested
systemd installation. The broker observes the socket namespace and local
packet hooks; it does not connect to the test target. BlockCheck requires a
consistent main-table route and local address before and after connecting
to the system nameserver and to the target. It does not override the firewall,
name servers or routing. A loopback DNS stub cannot attest its own upstream
socket; TUN, policy routes and unreviewed packet steering can also make the
baseline unobservable. Transport-specific DNS probes are not forced on this
path. These observations are not a guarantee about third-party steering or
filtering upstream of the observed Host route.
