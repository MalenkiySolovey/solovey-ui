# Pinned QUIC integration source

`sing-quic-integration.json` is the canonical exact dependency/source pin.
Go resolves the original sing-quic import path to one versioned public integration
module, authenticated by the two recorded Go module checksums. Official sing-box
v1.14.2 and its selected quic-go transport remain pinned. No source is modified in
a shared Go module cache.

`sing-quic-parent-control.patch` reproduces the complete eight-file integration
delta against the recorded official source commit. Original copyright and GPL
notices remain authoritative; the integration additions use GPL-3.0-or-later.
The fork's `SOLOVEY_INTEGRATION.md` describes each seam and its tests. This is
patched dependency source, not a byte-identical unmodified official runtime.

Validate a normal checkout and print package metadata:

    node scripts/quic-integration-provenance.mjs check
    node scripts/quic-integration-provenance.mjs build-info

Verify replay against both immutable commits already present in the small source
clone (read-only; only an owned temporary patch scratch tree is removed):

    node scripts/quic-integration-provenance.mjs replay /path/to/existing/sing-quic-clone

Runtime parent targets combine the protocol's original authenticated user with
the existing Solovey principal binding, exact inbound epoch, runtime generation
and service-local parent handle. Addresses never authorize a kick. Routed flows
remain in their existing trackers. The package build metadata explicitly names
the replacement commit, baseline, patch digest, checksums and license; Go build
information also retains the module replacement for SBOM consumers.
