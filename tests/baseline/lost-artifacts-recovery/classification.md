# Lost Artifacts Recovery Classification

STOP: expanded audit-scope add-list is larger than the 80-file safety limit; no git add or commit was performed.

Expanded add candidate files after blacklist/package/secret checks: 928

| Status | Path | Category | Action |
|---|---|---|---|
| ` M` | `README.md` | foreign WIP / M modified | skip |
| ` M` | `api/import_xui_test.go` | foreign WIP / M modified | skip |
| ` M` | `api/session_test.go` | foreign WIP / M modified | skip |
| ` M` | `api/test_helpers_test.go` | foreign WIP / M modified | skip |
| ` M` | `frontend/package-lock.json` | blacklist dependency manifest | skip |
| ` M` | `frontend/package.json` | blacklist dependency manifest | skip |
| ` M` | `frontend/src/layouts/modals/Endpoint.vue` | blacklist exact Endpoint.vue | skip |
| ` M` | `frontend/vitest.config.ts` | foreign WIP / M modified | skip |
| ` M` | `go.mod` | blacklist dependency manifest | skip |
| ` M` | `go.sum` | blacklist dependency manifest | skip |
| ` M` | `service/runtime.go` | foreign WIP / M modified | skip |
| ` M` | `service/token_use_debouncer.go` | foreign WIP / M modified | skip |
| `??` | `.github/workflows/audit-chaos.yml` | scope-marker 5 (audit CI workflow) | add |
| `??` | `.github/workflows/audit-frontend.yml` | scope-marker 5 (audit CI workflow) | add |
| `??` | `.github/workflows/audit-go.yml` | scope-marker 5 (audit CI workflow) | add |
| `??` | `.github/workflows/audit-perf.yml` | scope-marker 5 (audit CI workflow) | add |
| `??` | `.github/workflows/audit.yml` | scope-marker 5 (audit CI workflow) | add |
| `??` | `.golangci.yml` | not audit / no scope marker | skip |
| `??` | `CONTRIBUTING.md` | not audit / no scope marker | skip |
| `??` | `Makefile` | not audit / no scope marker | skip |
| `??` | `api/integration_auth_flow_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `api/integration_ws_lifecycle_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `api/perf_http_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `api/realtime_bench_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `api/security_authz_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `api/security_csrf_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `api/security_login_lockout_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `api/security_rollback_path_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `api/security_session_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `api/security_token_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `api/security_ws_origin_test.go` | scope-marker 1 (test-only Go; package api matches tracked package) | add |
| `??` | `database/backup_bench_test.go` | scope-marker 1 (test-only Go; package database matches tracked package) | add |
| `??` | `database/importxui/integration_import_full_test.go` | scope-marker 1 (test-only Go; package importxui matches tracked package) | add |
| `??` | `database/importxui/plan_bench_test.go` | scope-marker 1 (test-only Go; package importxui matches tracked package) | add |
| `??` | `database/importxui/plan_extra_test.go` | scope-marker 1 (test-only Go; package importxui matches tracked package) | add |
| `??` | `database/integration_backup_restore_test.go` | Go test candidate, package mismatch (package=database_test; tracked=database) | stop |
| `??` | `database/security_rollback_path_test.go` | scope-marker 1 (test-only Go; package database matches tracked package) | add |
| `??` | `docs/audit/chaos/` | expanded directory, scope-marker(s) 4, 1 add file(s) | add (expanded by file) |
| `??` | `docs/audit/ci/` | expanded directory, scope-marker(s) 4, 6 add file(s) | add (expanded by file) |
| `??` | `docs/audit/frontend/` | expanded directory, scope-marker(s) 4, 2 add file(s) | add (expanded by file) |
| `??` | `docs/audit/perf/` | expanded directory, scope-marker(s) 4, 1 add file(s) | add (expanded by file) |
| `??` | `docs/audit/security/` | expanded directory, scope-marker(s) 4, 4 add file(s) | add (expanded by file) |
| `??` | `docs/audit/start-prompt.md` | scope-marker 4 (audit doc artifact) | add |
| `??` | `frontend/playwright.config.ts` | not audit / no scope marker | skip |
| `??` | `frontend/src/plugins/__tests__/` | expanded directory, scope-marker(s) 7, 2 add file(s) | add (expanded by file) |
| `??` | `frontend/src/store/__tests__/csrf.spec.ts` | scope-marker 7 (frontend test-only artifact) | add |
| `??` | `frontend/tests/e2e/a11y.spec.ts` | scope-marker 7 (frontend test-only artifact) | add |
| `??` | `frontend/tests/e2e/helpers.ts` | not audit / no scope marker | skip |
| `??` | `frontend/tests/e2e/login.spec.ts` | scope-marker 7 (frontend test-only artifact) | add |
| `??` | `frontend/tests/e2e/migrate-xui-happy.spec.ts` | scope-marker 7 (frontend test-only artifact) | add |
| `??` | `frontend/tests/e2e/observability.spec.ts` | scope-marker 7 (frontend test-only artifact) | add |
| `??` | `frontend/tests/e2e/security-headers.spec.ts` | scope-marker 7 (frontend test-only artifact) | add |
| `??` | `frontend/tests/e2e/settings-paths.spec.ts` | scope-marker 7 (frontend test-only artifact) | add |
| `??` | `frontend/tests/e2e/tokens.spec.ts` | scope-marker 7 (frontend test-only artifact) | add |
| `??` | `frontend/tests/e2e/ws-reconnect.spec.ts` | scope-marker 7 (frontend test-only artifact) | add |
| `??` | `ipmonitor/integration_enforce_path_test.go` | scope-marker 1 (test-only Go; package ipmonitor matches tracked package) | add |
| `??` | `ipmonitor/ipmonitor_bench_test.go` | scope-marker 1 (test-only Go; package ipmonitor matches tracked package) | add |
| `??` | `realtime/hub_bench_test.go` | scope-marker 1 (test-only Go; package realtime matches tracked package) | add |
| `??` | `realtime/hub_extra_test.go` | scope-marker 1 (test-only Go; package realtime matches tracked package) | add |
| `??` | `scripts/` | expanded directory, mixed classification: 2 add, 2 skip | partial / stop before staging |
| `??` | `service/audit_writer_extra_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/integration_session_rotation_test.go` | Go test candidate, package mismatch (package=service_test; tracked=service) | stop |
| `??` | `service/integration_stats_pipeline_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/integration_subsecret_rotate_test.go` | blacklist *secret* | skip |
| `??` | `service/restart_manager_extra_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/security_backup_confidentiality_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/security_ssrf_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/security_token_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/setting_extra_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/stats_bench_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/stats_extra_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/telegram_backup_envelope_extra_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/telegram_bench_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/token_use_debouncer_bench_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `service/user_extra_test.go` | scope-marker 1 (test-only Go; package service matches tracked package) | add |
| `??` | `tests/baseline/env.md` | scope-marker 3 (audit baseline artifact) | add |
| `??` | `tests/baseline/lost-artifacts-recovery/` | expanded directory, mixed classification: 2 recovery bookkeeping | partial / stop before staging |
| `??` | `tests/baseline/phase0/` | expanded directory, scope-marker(s) 3, 51 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/phase1/` | expanded directory, scope-marker(s) 3, 11 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/phase2/` | expanded directory, scope-marker(s) 3, 18 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/phase3/` | expanded directory, scope-marker(s) 3, 30 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/phase4/` | expanded directory, scope-marker(s) 3, 151 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/phase5/` | expanded directory, scope-marker(s) 3, 50 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/phase6/` | expanded directory, mixed classification: 231 add, 7 skip | partial / stop before staging |
| `??` | `tests/baseline/phase7/` | expanded directory, scope-marker(s) 3, 141 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/phase8/` | expanded directory, scope-marker(s) 3, 40 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/phaseV/` | expanded directory, scope-marker(s) 3, 30 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/post-fix-47/` | expanded directory, scope-marker(s) 3, 22 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/post-fix-48/` | expanded directory, scope-marker(s) 3, 34 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/post-fix-cluster-A/` | expanded directory, scope-marker(s) 3, 25 add file(s) | add (expanded by file) |
| `??` | `tests/baseline/run-command.ps1` | scope-marker 3 (audit baseline artifact) | add |
| `??` | `tests/chaos/` | expanded directory, scope-marker(s) 2,8, 20 add file(s) | add (expanded by file) |
| `??` | `tests/e2e/` | expanded directory, scope-marker(s) 8, 2 add file(s) | add (expanded by file) |
| `??` | `verify-anchor4-records.txt` | not audit / no scope marker | skip |
| `??` | `verify-anchor4.txt` | not audit / no scope marker | skip |
| `??` | `verify-anchor40.txt` | not audit / no scope marker | skip |
| `??` | `verify-anchor41.txt` | not audit / no scope marker | skip |
| `??` | `verify-gosec.txt` | not audit / no scope marker | skip |
| `??` | `verify-govulncheck.txt` | not audit / no scope marker | skip |
| `??` | `verify-race-fresh.txt` | not audit / no scope marker | skip |
| `??` | `verify-race.txt` | not audit / no scope marker | skip |
| `??` | `verify-test.txt` | not audit / no scope marker | skip |

## Expanded Add Counts

| Marker | Count |
|---|---:|
| 1 | 36 |
| 2 | 17 |
| 3 | 836 |
| 4 | 15 |
| 5 | 5 |
| 6 | 2 |
| 7 | 12 |
| 8 | 5 |
| total | 928 |

## Skips

M-modified skipped from pre-status: 12
Untracked files skipped after expansion: 32
Secret-scan skips: 3
Ambiguous Go/package rows: 2

### Secret-Scan Skips
- `tests/baseline/phase6/playwright/html/index.html`: secret scan hit (password=)
- `tests/baseline/phase6/rg-initial-password-path.junit.xml`: secret scan hit (password=)
- `tests/baseline/phase6/rg-initial-password-path.txt`: secret scan hit (password=)

### Ambiguous
- `database/integration_backup_restore_test.go`: Go test candidate, package mismatch package=database_test; tracked=database
- `service/integration_session_rotation_test.go`: Go test candidate, package mismatch package=service_test; tracked=service
