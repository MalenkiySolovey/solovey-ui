# Lost-artifacts recovery - narrowed scope (retry 2)

## Source

This is a narrowed reclassification of classification.md from the first singleton attempt after the retry that exposed pre-Cluster-C assertions in chaos tests. Only scope markers 1 and 7 are kept for this commit: Go test-only plus frontend test-only. Chaos, forensic baseline, audit docs, CI workflows, aggregate scripts, and e2e/compose infra are deferred.

## Add list (46 files)

| Marker | Path | Category | Action |
|---|---|---|---|
| 1 | `api/integration_auth_flow_test.go` | Go test-only | add |
| 1 | `api/integration_ws_lifecycle_test.go` | Go test-only | add |
| 1 | `api/perf_http_test.go` | Go test-only | add |
| 1 | `api/realtime_bench_test.go` | Go test-only | add |
| 1 | `api/security_authz_test.go` | Go test-only | add |
| 1 | `api/security_csrf_test.go` | Go test-only | add |
| 1 | `api/security_login_lockout_test.go` | Go test-only | add |
| 1 | `api/security_rollback_path_test.go` | Go test-only | add |
| 1 | `api/security_session_test.go` | Go test-only | add |
| 1 | `api/security_token_test.go` | Go test-only | add |
| 1 | `api/security_ws_origin_test.go` | Go test-only | add |
| 1 | `database/backup_bench_test.go` | Go test-only | add |
| 1 | `database/importxui/integration_import_full_test.go` | Go test-only | add |
| 1 | `database/importxui/plan_bench_test.go` | Go test-only | add |
| 1 | `database/importxui/plan_extra_test.go` | Go test-only | add |
| 1 | `database/integration_backup_restore_test.go` | Go test-only (external _test package accepted) | add |
| 1 | `database/security_rollback_path_test.go` | Go test-only | add |
| 1 | `ipmonitor/integration_enforce_path_test.go` | Go test-only | add |
| 1 | `ipmonitor/ipmonitor_bench_test.go` | Go test-only | add |
| 1 | `realtime/hub_bench_test.go` | Go test-only | add |
| 1 | `realtime/hub_extra_test.go` | Go test-only | add |
| 1 | `service/audit_writer_extra_test.go` | Go test-only | add |
| 1 | `service/integration_session_rotation_test.go` | Go test-only (external _test package accepted) | add |
| 1 | `service/integration_stats_pipeline_test.go` | Go test-only | add |
| 1 | `service/restart_manager_extra_test.go` | Go test-only | add |
| 1 | `service/security_backup_confidentiality_test.go` | Go test-only | add |
| 1 | `service/security_ssrf_test.go` | Go test-only | add |
| 1 | `service/security_token_test.go` | Go test-only | add |
| 1 | `service/setting_extra_test.go` | Go test-only | add |
| 1 | `service/stats_bench_test.go` | Go test-only | add |
| 1 | `service/stats_extra_test.go` | Go test-only | add |
| 1 | `service/telegram_backup_envelope_extra_test.go` | Go test-only | add |
| 1 | `service/telegram_bench_test.go` | Go test-only | add |
| 1 | `service/token_use_debouncer_bench_test.go` | Go test-only | add |
| 1 | `service/user_extra_test.go` | Go test-only | add |
| 7 | `frontend/src/plugins/__tests__/api.spec.ts` | frontend test-only | add |
| 7 | `frontend/src/plugins/__tests__/httputil.spec.ts` | frontend test-only | add |
| 7 | `frontend/src/store/__tests__/csrf.spec.ts` | frontend test-only | add |
| 7 | `frontend/tests/e2e/a11y.spec.ts` | frontend test-only | add |
| 7 | `frontend/tests/e2e/login.spec.ts` | frontend test-only | add |
| 7 | `frontend/tests/e2e/migrate-xui-happy.spec.ts` | frontend test-only | add |
| 7 | `frontend/tests/e2e/observability.spec.ts` | frontend test-only | add |
| 7 | `frontend/tests/e2e/security-headers.spec.ts` | frontend test-only | add |
| 7 | `frontend/tests/e2e/settings-paths.spec.ts` | frontend test-only | add |
| 7 | `frontend/tests/e2e/tokens.spec.ts` | frontend test-only | add |
| 7 | `frontend/tests/e2e/ws-reconnect.spec.ts` | frontend test-only | add |

## Skip list (deferred)

- scope-marker 2 chaos tests: 17 files, deferred to a separate chaos singleton after clusters; retry 1 showed tests/chaos/cron_sync_chaos_test.go:53 contains a pre-Cluster-C assertion, and similar assertions may exist in other chaos files.
- scope-marker 3 baseline artifacts: 836 files, deferred as forensic.
- scope-marker 4 audit docs: 15 files, deferred.
- scope-marker 5 CI workflows: 5 files, deferred.
- scope-marker 6 aggregate scripts: 2 files, deferred.
- scope-marker 8 e2e/chaos compose infra: 5 files, deferred.
- secret-scan skips from first classification: 3 files, deferred with forensic scope.

## Counts

| Marker | Count |
|---|---:|
| 1 | 35 |
| 7 | 11 |
| total | 46 |
