# Component packaging and runtime state

Components are in-process. Code availability, pack files, installed inventory,
enabled policy, active lifecycle and durable data are separate facts.

| Delivery | Compiled backend / frontend bytes | Pack files and installed inventory |
| --- | --- | --- |
| Generic Linux full | Full optional Go code; core frontend embedded; extracted component frontend in packs | Installer installs all component packs and full inventory |
| Generic Linux core/minimal | Separate minimal binary omits optional in-process component imports and optional frontend | No optional component packs selected |
| Generic Linux custom selection | Any selected in-process component requires the full binary, including other compiled optional backend code; no custom public binary per selection | Installer copies selected packs, removes unselected pack directories, and writes selected inventory |
| OpenWrt APK | Canonical producer builds full backend and core embedded frontend | All component frontend packs and full initial inventory are package-owned under `/usr/lib/solovey-ui`; runtime state does not rewrite APK bytes |
| FriendlyWrt | Same OpenWrt APK for the appropriate target | Same package plus deployment storage injection; no package fork |
| Windows full | One full executable per architecture; optional Go backend and full frontend are compiled/embedded | No separately extracted component packs in the ZIP; runtime installed/enabled state gates activation |

**Disable** preserves installed inventory and durable data, changes enabled
policy and reconciles lifecycle. **Remove** removes the component from installed
runtime inventory and reconciles/stops activity while preserving durable data.
Owner-specific readiness, routes, jobs and runtime access follow reconciliation
and active-state checks. Installed and enabled do not alone prove active or
operationally ready. Defaults can leave components disabled.

The runtime Remove operation does not delete compiled Go code, embedded frontend
or package-owned pack bytes. Physical omission/deletion is claimed only for an
actual minimal binary or the generic Linux installer's selected pack file
operations. Drop Data is a separate explicitly authorized owner operation.
The update UI cannot disable/remove itself through its own management surface.

Source authority: `install.sh` (`resolve_binary_profile`, component pack
installation), `scripts/generate-component-imports.mjs`,
`scripts/openwrt-stage-build.sh`, `.github/workflows/windows.yml`,
`components/panel-update-ui/service/runtime.go`, and the installstate,
enabledstate and lifecycle owners under `internal/components/`.

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
