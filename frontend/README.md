# Solovey UI Frontend

Vue/Vuetify frontend for the Solovey UI panel.

## Development

```sh
npm ci
npm run dev
```

## Checks

```sh
npm run lint
npm run test:unit
npm run build
```

Playwright checks are kept separately because they start a local panel server:

```sh
npx playwright install chromium
npx playwright test
```
