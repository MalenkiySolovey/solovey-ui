
## OpenWrt and FriendlyWrt assets

- OpenWrt 25.12.5 x86/64: `solovey-ui-openwrt-x86-64.apk`.
- OpenWrt 25.12.5 rockchip/armv8: `solovey-ui-openwrt-rockchip-armv8.apk`.
- FriendlyWrt 25.12 on the qualified NanoPi R76S: the **same rockchip APK** plus
  `solovey-ui-friendlywrt-storage.json` and the existing actual `/opt` mount proof.

Each asset has an exact `.sha256` sidecar. APKs use the source-owned full package
producer and verified SDK/Go, package metadata, source fingerprint and extracted
rootfs provenance. The descriptor is copied byte-for-byte from the release tag.
No separate FriendlyWrt package, per-component APK or SD/eMMC/NVMe/USB variant is
built. Storage topology belongs to deployment qualification, not APK identity.
Built/source-qualified targets do not imply physical qualification of every board.

The existing signed `solovey-ui-release.json` covers the generic Linux/component
update set; APKs, the FriendlyWrt descriptor and Windows ZIPs are outside its
signature scope. Component Disable/Remove controls runtime state and preserves
durable data; it does not remove compiled optional code from a full executable
or package-owned bytes from an APK.
