# Solovey UI Frontend

One Vue/Vuetify application provides Classic and Nexus. Its component UI inputs are prepared and pinned explicitly; see [BUILD_INPUTS.md](BUILD_INPUTS.md).

Inside the Product checkout, install dependencies once and use the Product adapter. It verifies component provenance and supplies the generated manifest SHA256 to each task:

```sh
npm ci --prefix frontend
node scripts/frontend.mjs dev
node scripts/frontend.mjs typecheck
node scripts/frontend.mjs test
node scripts/frontend.mjs build
```

The same frontend source works in an isolated directory with its own locked npm dependencies and a trusted `.component-inputs` directory. Supply `SOLOVEY_UI_INPUTS_SHA256` from the trusted producer's preparation result, then run `npm run typecheck`, `npm run lint`, `npm run test` and `npm run build` there. There is no parent-directory discovery or Go resolution in the frontend. Full includes its declared contributions; Core/minimal exclude optional UI from the compiled application.

For managed Product browser checks, install Chromium with `npx playwright install chromium` inside frontend, then run `node scripts/frontend.mjs test:e2e` from Product. The adapter explicitly supplies the server entrypoint, result directory and optional import-fixture directory. An independently supplied server can instead be used with `SUI_E2E_SKIP_WEB_SERVER=1` and explicit E2E connection settings.
