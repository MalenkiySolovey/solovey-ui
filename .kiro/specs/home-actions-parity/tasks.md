# Implementation Plan: Settings Backup Entry (classic + Nexus)

## Overview

Close the audited Backup parity gap by adding a Backup entry on
`/settings` that is reachable in both UI modes, while leaving the
classic Home buttons unchanged. Reuse `Backup.vue` as-is. Logs and
Usage Stats are explicitly out of scope here and recorded as
`follow-up` in `parity-audit.md`. No backend, API, CSP, CSRF,
route, or `package.json` changes. All edits live under
`frontend/src/` and `.kiro/specs/home-actions-parity/`.

Tasks are ordered so each step has compile-ready prerequisites.
Acceptance for security/build invariants and viewport coverage
defers to the canonical procedures already defined in
`.kiro/specs/nexus-ui-mode/tasks.md`.

## Tasks

- [x] 1. Record the parity audit artefact
  - Create `.kiro/specs/home-actions-parity/parity-audit.md`.
  - Include a markdown table with columns:
    `Action | Classic Home | Nexus Overview | Status | Notes`.
  - Initial rows:
    - `Backup & Restore` — `yes` / `no` /
      `closed-in-this-spec` / new entry on `/settings`
      Maintenance tab; classic Home button preserved.
    - `Logs` — `yes` / `no` / `follow-up` / deferred to a
      separate spec; classic Home button preserved.
    - `Usage Stats` — `yes` / `no` / `follow-up` / deferred to
      a separate spec; classic Home button preserved.
    - `Tile customisation (reloadItems)` — `yes` / `no` /
      `out-of-scope-by-design` / `nexus-ui-mode` requirement
      6.7 (fixed Overview composition).
  - The artefact MUST NOT modify or supersede the
    `nexus-ui-mode` spec documents.
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_

- [x] 2. Add `setting.maintenance` to all locale files
  - For every file in `frontend/src/locales/`, add a single new
    key under the existing `setting` namespace:
    - `en`: `maintenance: 'Maintenance'`
    - `ru`: `maintenance: 'Обслуживание'`
    - other locales: add the key with English fallback if no
      translator-supplied string is available; this is
      acceptable per design.
  - Do NOT rename, delete or move any existing key. Do NOT
    touch `main.backup.*` or `setting.interface`.
  - _Requirements: 8.1, 8.2, 8.3, 10.4_

- [x] 3. Create `MaintenanceTab.vue`
  - Path: `frontend/src/components/settings/MaintenanceTab.vue`.
  - Composition API, `<script setup lang="ts">`.
  - Imports:
    - `Backup from '@/layouts/modals/Backup.vue'`
    - `ref` from `'vue'`
  - Type:
    ```ts
    interface ModalControl { visible: boolean }
    ```
  - State:
    - `const backupModal = ref<ModalControl>({ visible: false })`
  - Template:
    - A `v-row dense` containing one `v-col` with responsive
      `cols="12" sm="6" md="4"` and a `v-btn block variant="tonal"`
      with `prepend-icon="mdi-backup-restore"` and label
      `$t('main.backup.title')`.
    - Click handler: `backupModal.visible = true`.
    - One modal mount mirroring `components/Main.vue`:
      `<Backup v-model="backupModal.visible" :control="backupModal" :visible="backupModal.visible" />`.
  - No `// @ts-ignore`, no `as any`.
  - _Requirements: 2.2, 3.1, 3.2, 3.3, 9.1, 9.2_

- [x] 4. Wire the Maintenance tab into `Settings.vue`
  - Modify `frontend/src/views/Settings.vue`:
    - Add
      `import MaintenanceTab from '@/components/settings/MaintenanceTab.vue'`.
    - In the `<v-tabs>` block, append:
      `<v-tab value="t5">{{ $t('setting.maintenance') }}</v-tab>`.
    - In the `<v-window>` block, append:
      `<v-window-item value="t5"><MaintenanceTab /></v-window-item>`.
    - Wrap the existing top action row (Save / Restart `v-row`)
      with `v-if="tab !== 't5'"` so it does not show on the
      maintenance tab (no form state to persist there).
  - Do NOT modify any other state, computed, watcher, lifecycle
    or save/load logic in `Settings.vue`.
  - _Requirements: 2.1, 2.2, 2.3, 4.3_

- [x] 5. Classic preservation regression check
  - Confirm `frontend/src/components/Main.vue` is not modified
    in this changeset (`git diff` empty for that path).
  - Open the classic Home; confirm the three buttons
    (`Backup & Restore`, `Logs`, `Usage Stats`) still render and
    open their respective modals as before.
  - Confirm classic tile customisation (`reloadItems`) still
    works.
  - Confirm clicking classic `Backup & Restore` still opens the
    same modal with the same initial state.
  - _Requirements: 4.1, 4.2, 4.4_

- [x] 6. UI mode hot-swap regression check
  - In `nexus` mode, open `/settings` → Maintenance tab →
    open Backup modal.
  - Switch UI mode to `classic` via the existing toggle.
  - Confirm app does not crash; `/settings` still renders the
    Maintenance tab; modal state behaves consistently with the
    `nexus-ui-mode` requirement 3.2/3.3 routing-stability
    invariant.
  - Confirm `localStorage` keys other than `sui:ui:mode` are
    unchanged.
  - _Requirements: 5.1, 5.2, 5.3_

- [x] 7. Viewport and RTL pass
  - Run the **Viewport Verification Procedure** from
    `.kiro/specs/nexus-ui-mode/tasks.md`. All four mandatory
    viewports under LTR `en` and RTL `fa`:
    - `1440x900`, `1180x800`, `834x1112`, `390x844`.
  - For each cell verify:
    - `/settings` Maintenance tab renders the Backup control;
    - no horizontal overflow on the document scroll container;
    - on `390x844` the Backup button stretches via `block` and
      does not clip; the tab strip remains usable (existing
      `show-arrows` behaviour);
    - under RTL `fa`, button icon and label mirror correctly.
  - _Requirements: 2.4, 2.5_

- [x] 8. Security and build regression
  - Confirm `TestAdminSecurityHeaders` passes without
    modification:
    `go test ./middleware/... -run TestAdminSecurityHeaders`
    from `c:\s-ui-x`.
  - Run the **External-Origin Regression Procedure** from
    `.kiro/specs/nexus-ui-mode/tasks.md`:
    - **Primary gate (Nexus-source scan):** zero matches. The
      new file lives under `components/settings/`, not under
      Nexus-owned directories, so the gate scope is unchanged.
    - **Build artefact gate (`dist/index.html` shape):** zero
      matches.
    - **Supply-chain gate:** `git diff -- frontend/package.json`
      shows no changes to `dependencies` or `devDependencies`.
  - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5_

- [x] 9. Final release-readiness pass
  - Run from `c:\s-ui-x\frontend`:
    - `npm run test`
    - `npm run lint`
    - `npm run build`
  - Confirm no backend, API, CSRF or CSP drift entered the
    changeset (`git status` outside `frontend/` and
    `.kiro/specs/home-actions-parity/` MUST be empty).
  - Re-run the three gates from Task 8 against the final build.
  - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 9.1, 9.2_

## Notes

- All tasks edit only files under `frontend/` and
  `.kiro/specs/home-actions-parity/`. No backend files are
  modified.
- `components/Main.vue` and `frontend/src/layouts/modals/Backup.vue`
  MUST NOT be modified.
- `package.json` MUST NOT gain new `dependencies` or
  `devDependencies`.
- The existing `script-src 'self'` CSP and the
  `TestAdminSecurityHeaders` Go test are the security gates. Any
  change that would require relaxing them is out of scope.
- `Logs` and `Usage Stats` parity is intentionally out of scope.
  When a future spec picks them up, it can add controls to
  `MaintenanceTab.vue` without disturbing the Backup entry shipped
  by this spec.
- This spec is a follow-up to `nexus-ui-mode` and inherits its
  External-Origin and Viewport Verification procedures. Do not
  re-implement those procedures here; reference them.

## Task Dependency Graph

```json
{
  "waves": [
    { "wave": 1, "tasks": ["1", "2"] },
    { "wave": 2, "tasks": ["3"] },
    { "wave": 3, "tasks": ["4"] },
    { "wave": 4, "tasks": ["5", "6", "7", "8"] },
    { "wave": 5, "tasks": ["9"] }
  ],
  "edges": [
    { "from": "2", "to": "3" },
    { "from": "3", "to": "4" },
    { "from": "4", "to": "5" },
    { "from": "4", "to": "6" },
    { "from": "4", "to": "7" },
    { "from": "4", "to": "8" },
    { "from": "5", "to": "9" },
    { "from": "6", "to": "9" },
    { "from": "7", "to": "9" },
    { "from": "8", "to": "9" }
  ]
}
```

Visual summary:

```
1 (parity audit, independent) ─────────────────────────────┐
2 (locales) → 3 (MaintenanceTab) → 4 (Settings wiring) ─┬─► 5
                                                       ├─► 6
                                                       ├─► 7
                                                       └─► 8
                                                              ↓
                                                              9
```
