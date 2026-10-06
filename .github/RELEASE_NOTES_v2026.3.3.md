# Solovey UI 2026.3.3

This stable patch updates the networking core and strengthens admission,
restore, configuration and component recovery while preserving the supported
deployment and storage contracts.

- The core is sing-box v1.13.18 with separately reviewed supporting modules
  and Cronet 150 native artifacts. Newer compatible security dependencies are
  retained.
- TCP and UDP IP admission is atomic. Connection wrappers from a retired core
  generation cannot change the new generation's counters after restart.
- Token authorization, configuration validation and import completion retain
  their owning lifetimes. Restore lets admitted work finish on its original
  database generation, bounds late requests, and restores truthful auth/cache
  state or the protected fallback.
- Save, import, client delivery and Doctor consume the same build capabilities.
  Stored WARP endpoints normalize to WireGuard without renaming their identity;
  references captured at initialization retain their full restart contract.
- Paid-subscription recovery validates exact invoice and payment metadata,
  retains durable poll/cancel state, and preserves owned data when disabled.
- Recent health observations, websocket/history lifetime, editor guidance,
  traffic timezone display and Telegram discovery/settings are improved.
- Frontend assets are verified before atomic publication. Native Windows build
  invocation and test path identity are consistent; audit tools are pinned and
  performance comparisons use the same runner and toolchain.

Independent behavioral integration closes the frozen S-UI-X v1.5.12-beta6
source boundary. Bounded FriendlyWrt 25.12.5 qualification on NanoPi R76S covers
TCP/UDP admission and tracker restart, finite restore, WARP/WireGuard reapply,
and installed paid-component persistence with synthetic nonmonetary state.

The complete release transaction builds Linux full/core archives and the
component bundle, Windows amd64/arm64 packages, Docker amd64/arm64 images,
OpenWrt packages and the shared FriendlyWrt storage descriptor. Exact SHA-256
sidecars accompany downloadable assets; the signed release manifest authorizes
the generic Linux and component update set.
