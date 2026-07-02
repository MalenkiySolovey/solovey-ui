# Component Packs

Each directory under `components/` is an optional Solovey UI component pack.

Backend code registers itself from `init()` through `componenthost/registry` and is imported only by `app/components_full.go`. The `minimal` profile must not import these packs. A component keeps its own layers inside the pack and talks to core through `componenthost.Deps` rather than importing sibling components or composition roots.

Frontend code should register UI contributions through `frontend/src/componentSystem` and named slots instead of making core import component views directly.
