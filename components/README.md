# Component Packs

Each directory under `components/` is an optional Solovey UI component pack.

Backend code registers itself from `init()` through `componenthost/registry`. Manifest-driven composition generates `app/components_generated.go`; `app/components_full.go` only verifies that generated composition is present. The `minimal` profile must not import these packs. A component keeps its own layers inside the pack and talks to core through narrow `componenthost` contracts and injected `componenthost.Deps` rather than importing sibling components or composition roots.

Frontend code should register UI contributions through `frontend/src/componentSystem` and named slots instead of making core import component views directly.
