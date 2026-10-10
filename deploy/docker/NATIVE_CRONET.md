# Docker native Cronet integration

The Alpine image uses the official static musl Cronet asset selected by
`with_musl`. Cronet's version remains `150.0.7871.63`; the native modules remain
at `v0.0.0-20260912104006-c10c03c318db`, and the Go wrapper remains at
`v0.0.0-20260912104727-0d28acc44093`. No module graph or sing-box semantic
baseline changes. This is the same supported library selection as the existing
generic Linux/OpenWrt producer, compiled with LLVM rather than GNU ld.

The previous Docker recipe dynamically loaded a glibc `libcronet.so` in musl.
An enabled Naive outbound could not construct its engine (`__memcpy_chk`).
Preloading the existing gcompat library allowed construction but hung native
shutdown (`close symbol missing`), so preload alone did not satisfy restart.
The static integration removes this loader mismatch and retains Naive support.

`Dockerfile` remains the owner of target archive SHA256 pins. Each archive is
verified after `go mod verify` and before linking. `/app/CRONET_INTEGRATION.json`
records linkage, native version, selected module/version and archive SHA256 for
build provenance and SBOM consumers. No shared Cronet library is shipped. The
existing final-image user 65532, read-only deployment policy, capability limits,
base image pins and release trust-root injection remain intact.

Default builds end at the `release` target. The opt-in `native-cronet-probe`
target adds only a regression executable to that same runtime image. CI runs
it on native AMD64 and ARM64 with network disabled, no capabilities, a read-only
root and user 65532. Six real Naive client lifecycles exercise native engine
creation, TCP/QUIC socket-pair worker startup, cancellation/drain, engine
shutdown/destruction and repeated initialization under an outer 45 second bound.
It also checks the actual native version and closed-state behavior. This does
not claim proxy traffic, certificate or physical deployment qualification.

The probe is absent from the default release image. Public Cronet APIs and the
same official `cronet-go/all` target selection as sing-box are used; no protocol
implementation or private native ABI code is copied. Existing upstream source
and license notices in `NOTICE.md` apply to the statically linked native asset.
