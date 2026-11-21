# Component Packs

Directories under `components/` bind the optional, in-process packs into the
Product. A registration or a compatibility descriptor is not a second source owner.

Backend code registers itself from `init()` through `componenthost/registry`. Manifest-driven composition generates `app/components_generated.go`; `app/components_full.go` only verifies that generated composition is present. The `minimal` profile must not import these packs. A component keeps its own layers inside the pack and talks to core through narrow `componenthost` contracts and injected `componenthost.Deps` rather than importing sibling components or composition roots.

Frontend code should register UI contributions through `src/componentSystem` in `MalenkiySolovey/solovey-frontend` and named slots instead of making core import component views directly.

Product prepares the accepted UI projection from `components/sources.lock.json`.
There is no editable Product UI fallback for a locked pack. In particular,
`solovey-component-import-xui` owns source parsing, mapping and preview; Product
binds them to the Backend database lifecycle and existing destination owners.
`solovey-component-paid-subscriptions` owns commercial state transitions and
customer bot behavior. Backend supplies the transaction and owns Client
entitlements; Product binds both to the existing ComponentHost.

Zapret is a headless source input: Full links its inert descriptor and Core
does not link the component module. It is not shipped as an installed pack
in this slice. An enabled Outbound binding is rejected by runtime preflight
until a production packet-path adapter is qualified.

Panel Update is intrinsic. Frontend owns the maintenance UI, Backend owns the
HTTP and authorization boundary, Host owns component management, and Product
binds the installed updater and publisher. `panel-update-ui` is only a legacy
upgrade and backup identifier, not a pack or live lifecycle entry.
