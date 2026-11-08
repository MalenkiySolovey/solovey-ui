# Solovey UI 2026.5.1

This corrective release fixes ordinary signed-v2 update admission after
v2026.5.0. Official remains the default. An ordinary update retains the installed
Official or Shtorm Extended provider through Check, Prepare and Activate.
Changing providers still requires the existing authenticated maintenance flow.

The updater selects the current provider's signed artifact, verifies the
candidate's declared source, architecture and profile, and binds the installed
source and target to the existing Root rollback checkpoint. It retains signature,
expiry, monotonic sequence, backup and recovery checks. Legacy v1 remains
Official-only. Schema remains 1.13; engine pins, capabilities and platform scope
are unchanged from the accepted R7 qualification.

## Upgrade from v2026.5.0 or v2026.4.1

The already published v2026.5.0 binary has a known ordinary updater defect:
Check rejects the signed v2 release envelope. v2026.4.1 also predates this format.
Neither frozen binary can repair itself through its in-app Update action.
Use the supported installer/package/container lifecycle once to reach this
version. No unsigned fallback or channel sequence reset is required.

For an existing native Linux installation, first export and protect a logical
backup and its required secret material. Then run the canonical installer pinned
to this release:

```sh
curl --proto '=https' --tlsv1.2 -fL \
  https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/v2026.5.1/install.sh \
  -o install-v2026.5.1.sh
sudo bash install-v2026.5.1.sh --version v2026.5.1 --backup
```

The existing installer retains the installed provider, settings, clients and
component selection. Do not pass a different `--core-provider` to change an
existing installation. Review status, Doctor, build-info and Backup/Restore
after the upgrade. Keep the compatible pre-upgrade backup for rollback; an older
binary must not open state from an incompatible schema.

OpenWrt/FriendlyWrt APKs, containers and Windows installations use their normal
operator-managed deployment paths with the matching provider package/image.
FriendlyWrt continues to use the shared ARM64 APK and qualified persistent `/opt`
topology; this release does not require reimaging firmware.

The v2026.5.0 tag, assets and immutable OCI versions remain historical release
data. This patch does not include dependency updates, new provider capabilities,
production Node, Bridge integration or Zapret.
