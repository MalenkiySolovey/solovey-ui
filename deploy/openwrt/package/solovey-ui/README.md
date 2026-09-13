# Solovey UI OpenWrt package source

This is the external package source copied into the exact OpenWrt 25.12 SDK
before running `make package/solovey-ui/compile`. It packages a source-bound
rootfs-shaped stage created from current source by
`scripts/openwrt-stage-build.sh`; it never downloads a runtime payload during
installation. The companion SDK driver invokes that producer itself and
re-verifies every manifested payload byte before it invokes the package build.

The recipe intentionally targets the SDK's `ARCH_PACKAGES` value. An artifact
claim therefore names its exact target, subtarget and package architecture in
the generated build evidence rather than treating a Go architecture as an
OpenWrt target matrix.

Package-owned files are limited to the fixed binaries, full-profile component
assets, immutable build identity and source fingerprint, OpenWrt init and
sysupgrade assets, and the GPL-3.0 license copy. `/etc/solovey-ui/db` is
created only on first package installation and is not a package conffile. The
only sysupgrade selections are the stable installation UUID, logical snapshot and its metadata from
`deploy/openwrt/solovey-ui.keep`.

Firmware replacement follows the [qualified maintenance contract](../../../../docs/openwrt-sysupgrade.md):
normal firewall Rollback, verified backup, external whole-SD rewrite, native
configuration restore while Solovey is absent, reboot and exact APK reinstall.
Native Rockchip sysupgrade is unsupported on the qualified R76S SD topology. Public ASU cannot reconstruct an
arbitrary locally built APK. The helper link closures include the same selected
component registrations as the panel, without starting their runtimes; installed
owner metadata is mandatory. Actual restore applies owner normalization inside
the existing rollback-protected database acceptance boundary.

The post-install lifecycle owns one persistent permission contract: the
`/etc/solovey-ui` parent remains `root:root` with mode `0711` (execute-only
traversal for the service), while `/etc/solovey-ui/db` is
`solovey-ui:solovey-ui` with mode `0700`. The owner and broker manifest writers
verify that root-owned, non-writable parent proposition and write root-only
contract files beneath it; they do not redefine the package contract.

The canonical stage contains a closed `payload/` rootfs and a deterministic
version 2 `STAGE_MANIFEST.json`. The manifest records every regular file and
directory, including exact path, entry type, mode and semantic kind. Regular
files additionally bind SHA-256 and size; executables also bind ELF
architecture. Verification rejects extra or missing paths, type changes,
unsupported filesystem entries, mode changes and byte changes.

Target-producing frontend and Go subprocesses run with explicit `env -i`
allowlists, private HOME/config/cache roots and canonical target variables.
The Go producer privately copies and verifies the official Go 1.26.6 archive,
binds the complete extracted toolchain tree, materializes the exact seven-target
module graph into fresh private module/build caches through the public module
proxy and SumDB, and repeats the complete-tree and `go mod verify` checks at the
final build boundary. Known product/E2E selectors and ambient Go roots,
dependency selectors, cgo, compiler and frontend options fail closed. `CC` and
`CXX` are accepted only as the pinned compiler program,
the exact OpenWrt target argument and one shared sysroot argument; those
semantic invocations and the executed tool identities are build facts.

On an eligible Linux SDK host, package current source with:

```sh
scripts/openwrt-package-build.sh \
  --sdk-archive <official-sdk.tar.zst> \
  --go-toolchain-archive <go1.26.6.linux-amd64.tar.gz> \
  --source <source-worktree> \
  --trust-roots-file <public-release-trust-roots.b64> \
  --out <new-evidence-directory>
```

The caller archive is copied into a private root before it is hashed, listed or
extracted, so caller-path replacement cannot change the executed SDK. The
driver verifies its pinned checksum, validates its complete archive
path/symlink contract, extracts it into a fresh private temporary root,
validates every tree entry (path, type, mode, file size/hash and symlink target),
and executes the package build only in that derived tree. The official archive's fixed set of
host-tool absolute symlinks is an archive-SHA-bound compatibility contract;
unrecognized absolute symlinks and escaping relative symlinks fail closed.
The SDK derivation identity records the verified archive and complete tree
actually executed, and that same prepared tree is reverified immediately before
the fixed `/usr/bin/make` object starts `package/solovey-ui/compile`.

Package construction starts through `/usr/bin/env -i`. Its deliberate
top-level `PATH` contains only fixed build-runner base directories; it never
inherits a caller prefix. OpenWrt derives `TARGET_PATH` from that base and its
package rules prepend the complete-tree-authenticated SDK host and host-package
directories through `TARGET_PATH_PKG`. The driver rejects caller
Make selectors and flags, gives Make private HOME/XDG/cache/temporary roots, and
sets locale, timezone and shell values explicitly. The top-level executable is
the resolved regular `/usr/bin/make` object, and
`PACKAGE_HOST_TOOL_AUTHORITY.json` binds its bytes, mode, GNU Make version and
build target together with the exact Node bytes already recorded by the stage.
OpenWrt recursive `$(MAKE)` therefore derives from the same absolute parent
program. SDK-owned tools remain authenticated by the complete SDK-tree proof;
direct package commands use either those tools, source-owned scripts through
the stage-bound Node executable, or fixed trusted build-runner base facilities.
The threat boundary excludes an unrelated process already controlling the
build account and able to mutate arbitrary files concurrently.

The driver creates and verifies the source-bound stage, configures
`CONFIG_NO_STRIP=y` so package construction preserves manifested executable
bytes, invokes `make package/solovey-ui/compile` and `make package/index` in an
explicit package environment, and records the APK v3 ADB metadata/fileset.
`APK_METADATA_PROOF.json` independently canonicalizes and verifies the
source-owned name, version/release, architecture, maintainer, license,
description, dependency/provides sets, package content hash and installed-size
semantics. The driver then extracts the exact copied APK with the verified
SDK's `apk` and compares
the installed rootfs path/type/mode/content exactly with the stage, allowing
only the package manager's defined metadata files. `SDK_DERIVATION.json` and
`APK_ROOTFS_PROOF.json` bind the SDK and installed-rootfs checks; rootfs equality
is deliberately not treated as control-metadata proof. The driver refuses an arbitrary
stage, a preexisting SDK directory, or an existing output directory.
