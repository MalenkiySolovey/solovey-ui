# Baseline run 2026-05-23

Рабочая директория: `C:\s-ui-x`

Окружение и версии: [`env.md`](env.md).

## Итог

- Go build/vet/test/race: green.
- Frontend: red из-за `npm ci` на Windows `EPERM unlink`; последующие `lint/build/test` также red после неполной установки зависимостей.
- Go static analysis: red для `staticcheck`, `golangci-lint`, `gosec`, `govulncheck`.
- Отдельный frontend `typecheck` script отсутствует; `build` содержит `vue-tsc --noEmit`.
- E2E не запускался: целевая заглушка для последующих фаз.
- Инструменты `staticcheck`, `golangci-lint`, `gosec`, `govulncheck` установились успешно.

## Команды

| Команда | Статус | Лог | JUnit |
|---|---:|---|---|
| `go version` | green | [`phase0/go-version.txt`](phase0/go-version.txt) | [`phase0/go-version.junit.xml`](phase0/go-version.junit.xml) |
| `node --version` | green | [`phase0/node-version.txt`](phase0/node-version.txt) | [`phase0/node-version.junit.xml`](phase0/node-version.junit.xml) |
| `npm --version` | green | [`phase0/npm-version.txt`](phase0/npm-version.txt) | [`phase0/npm-version.junit.xml`](phase0/npm-version.junit.xml) |
| `go build ./...` | green | [`phase0/go-build.txt`](phase0/go-build.txt) | [`phase0/go-build.junit.xml`](phase0/go-build.junit.xml) |
| `go vet ./...` | green | [`phase0/go-vet.txt`](phase0/go-vet.txt) | [`phase0/go-vet.junit.xml`](phase0/go-vet.junit.xml) |
| `go test ./...` | green | [`phase0/go-test.txt`](phase0/go-test.txt) | [`phase0/go-test.junit.xml`](phase0/go-test.junit.xml) |
| `go test ./... -race -count=1` | green | [`phase0/go-test-race.txt`](phase0/go-test-race.txt) | [`phase0/go-test-race.junit.xml`](phase0/go-test-race.junit.xml) |
| `go test -v ./database/importxui/...` | green, skips | [`phase0/go-test-importxui-verbose.txt`](phase0/go-test-importxui-verbose.txt) | [`phase0/go-test-importxui-verbose.junit.xml`](phase0/go-test-importxui-verbose.junit.xml) |
| `gosec ./...` | red | [`phase1/gosec.txt`](phase1/gosec.txt) | [`phase1/gosec.junit.xml`](phase1/gosec.junit.xml) |
| `govulncheck ./...` | red | [`phase1/govulncheck.txt`](phase1/govulncheck.txt) | [`phase1/govulncheck.junit.xml`](phase1/govulncheck.junit.xml) |
| `staticcheck ./...` | red | [`phase1/staticcheck.txt`](phase1/staticcheck.txt) | [`phase1/staticcheck.junit.xml`](phase1/staticcheck.junit.xml) |
| `golangci-lint config verify -v` | green | [`phase1/golangci-lint-config-verify.txt`](phase1/golangci-lint-config-verify.txt) | [`phase1/golangci-lint-config-verify.junit.xml`](phase1/golangci-lint-config-verify.junit.xml) |
| `golangci-lint run` | red | [`phase1/golangci-lint.txt`](phase1/golangci-lint.txt) | [`phase1/golangci-lint.junit.xml`](phase1/golangci-lint.junit.xml) |
| `npm ci` | red | [`phase0/npm-ci.txt`](phase0/npm-ci.txt), [`phase0/npm-ci-debug.txt`](phase0/npm-ci-debug.txt) | [`phase0/npm-ci.junit.xml`](phase0/npm-ci.junit.xml) |
| `npm run typecheck` | skipped | [`phase0/npm-run-typecheck.txt`](phase0/npm-run-typecheck.txt) | [`phase0/npm-run-typecheck.junit.xml`](phase0/npm-run-typecheck.junit.xml) |
| `npm run lint` | red | [`phase0/npm-run-lint.txt`](phase0/npm-run-lint.txt), [`phase0/npm-run-lint-debug.txt`](phase0/npm-run-lint-debug.txt) | [`phase0/npm-run-lint.junit.xml`](phase0/npm-run-lint.junit.xml) |
| `npm run build` | red | [`phase0/npm-run-build.txt`](phase0/npm-run-build.txt), [`phase0/npm-run-build-debug.txt`](phase0/npm-run-build-debug.txt) | [`phase0/npm-run-build.junit.xml`](phase0/npm-run-build.junit.xml) |
| `npm run test` | red | [`phase0/npm-run-test.txt`](phase0/npm-run-test.txt), [`phase0/npm-run-test-debug.txt`](phase0/npm-run-test-debug.txt) | [`phase0/npm-run-test.junit.xml`](phase0/npm-run-test.junit.xml) |
| `e2e` | skipped | [`phase0/e2e.txt`](phase0/e2e.txt) | [`phase0/e2e.junit.xml`](phase0/e2e.junit.xml) |

## Красное

- `npm ci`: `EPERM: operation not permitted, unlink 'C:\s-ui-x\frontend\node_modules\@rolldown\binding-win32-x64-msvc\rolldown-binding.win32-x64-msvc.node'`. Это зафиксировано как baseline red, без попытки чинить frontend или чистить `node_modules`.
- `npm run lint`, `npm run build`, `npm run test`: завершились `exit 1`; после failed `npm ci` диагностика показывает отсутствие `frontend/node_modules/.bin/eslint.cmd`, `vue-tsc.cmd`, `vitest.cmd` в [`phase0/frontend-node-modules-diagnostic.txt`](phase0/frontend-node-modules-diagnostic.txt).
- `staticcheck`: unused/deprecated/simplification findings, включая `api\apiService.go:439 U1000`, `core\tracker_stats.go SA1019`, `service\client.go SA4010`, `database\importxui\source\ssh\ssh.go S1017`.
- `golangci-lint`: подтверждает rule-set `errcheck`, `gosec`, `bodyclose`, `noctx`, `nilness` через `govet`, `unparam`, `ineffassign`, `gosimple`. Среди важных baseline findings: unchecked errors в `api/apiService.go`, `api/session.go`, `app/app.go`, `sub/*`, `web/web.go`; `nilness` в `database/importxui/profile_crypto.go`, `database/importxui/source/ssh/ssh.go`, `sub/subService.go`.
- `gosec`: 55 issues. Подтверждены классы `G104`, `G115`, `G304`, `G101`, `G703`; отдельного `G306` в этом baseline не найдено.
- `govulncheck`: 12 called vulnerabilities: `golang.org/x/net@v0.51.0`, `golang.org/x/crypto@v0.48.0`, Go stdlib `go1.26.2`. Исправленные версии указаны в логе.

## Требует `test-db`

В `test-db/` сейчас только `.gitkeep`, реальные DB-фикстуры не добавлялись. Verbose-прогон importxui показывает skipped тесты, требующие `test-db/x-ui.db` и/или `test-db/s-ui.db`:

- `TestImport_DryRun_NoMutation`
- `TestImport_Reality_DedupAndShape`
- `TestImport_Trojan_Grpc`
- `TestImport_Wireguard_AsEndpoint`
- `TestImport_Clients_AggregateByEmail`
- `TestImport_Clients_DefaultsDeterministic`
- `TestImport_Idempotent`
- `TestImport_StrategyReplace_OverwritesInbound`
- `TestImport_StrategySkip_KeepsExisting`
- `TestImport_AuditEntryCreated`
- `TestImport_Clients_AllTrafficEmailsPresent`
- `TestDialectMHSanaeiDetectsFixture`
- `TestPlan_ProducesPreviewJSON`
- `TestPlan_ConflictDetection`
- `TestApply_HashMismatch`
- `TestApply_RespectsPerObjectAction`
- `TestApply_RespectsRenamedTag`
- `TestApply_EmitsProgressAndRecordsRollbackPath`
- `TestApply_ImportsSettingsAndNewPasswordAdmins`
- `TestFileSourceAcquireReturnsExistingPath`
- `TestSSHSourceAcquireDownloadsFixture`
- `TestSSHSourceRequiresHostKeyConfirmation`

## Phase 2 (per package unit tests)

Дата прогона: 2026-05-24.

### Команды Phase 2

| Команда | Статус | Лог | JUnit |
|---|---:|---|---|
| `go build ./...` | green | [`phase2/build.txt`](phase2/build.txt) | [`phase2/build.junit.xml`](phase2/build.junit.xml) |
| `go vet ./...` | green | [`phase2/vet.txt`](phase2/vet.txt) | [`phase2/vet.junit.xml`](phase2/vet.junit.xml) |
| `go test ./... -count=1 -timeout 5m` | green | [`phase2/test.txt`](phase2/test.txt) | [`phase2/test.junit.xml`](phase2/test.junit.xml) |
| `go test ./... -race -count=1 -timeout 10m` | green | [`phase2/test-race.txt`](phase2/test-race.txt) | [`phase2/test-race.junit.xml`](phase2/test-race.junit.xml) |
| `go test ./... -coverprofile=tests/baseline/phase2/coverage/coverage.out` | green | [`phase2/coverage/coverage.txt`](phase2/coverage/coverage.txt) | [`phase2/coverage/coverage.junit.xml`](phase2/coverage/coverage.junit.xml) |
| `go tool cover -func=tests/baseline/phase2/coverage/coverage.out` | green | [`phase2/coverage/coverage-func.txt`](phase2/coverage/coverage-func.txt) | [`phase2/coverage/coverage-func.junit.xml`](phase2/coverage/coverage-func.junit.xml) |

Дополнительные sanity-rerun артефакты: [`phase2/go-test-targeted-smoke.txt`](phase2/go-test-targeted-smoke.txt), [`phase2/go-test-service-race-rerun.txt`](phase2/go-test-service-race-rerun.txt).

### Green/red по пакетам

`go test ./...` и финальный `go test ./... -race` зелёные по всем Go-пакетам. Новых red baseline тестов не оставлено; known broken behavior закреплено через skipped XFAIL или current-behavior anchor без правок production-кода.

Coverage total: 42.1% statements, полный function report: [`phase2/coverage/coverage-func.txt`](phase2/coverage/coverage-func.txt).

| Пакет | test | race | coverage | Порог 60% |
|---|---:|---:|---:|---:|
| `api` | green | green | 53.9% | ниже |
| `cronjob` | green | green | 61.4% | ok |
| `database/importxui` | green | green | 24.6% | ниже, часть fixture-тестов требует `test-db` |
| `ipmonitor` | green | green | 81.4% | ok |
| `realtime` | green | green | 92.5% | ok |
| `service` | green | green | 47.5% | ниже |

Дельта к baseline 2026-05-23:
- Go build/vet/test/race остались green.
- Frontend/npm и static analysis red-команды из Фазы 0/1 не запускались и не исправлялись в этом диалоге.
- Coverage baseline до Фазы 2 не существовал; Фаза 2 добавила первый `coverage.out` и absolute total 42.1%. Порог 60% пока не достигнут для `service`, `api`, `database/importxui`.

### Добавленные тестовые файлы

- `api/realtime_extra_test.go`
- `cronjob/xuiSyncJob_extra_test.go`
- `database/importxui/plan_extra_test.go`
- `ipmonitor/ipmonitor_extra_test.go`
- `realtime/hub_extra_test.go`
- `service/audit_writer_extra_test.go`
- `service/restart_manager_extra_test.go`
- `service/secret_settings_extra_test.go`
- `service/setting_extra_test.go`
- `service/stats_extra_test.go`
- `service/telegram_backup_envelope_extra_test.go`
- `service/user_extra_test.go`

### Skipped / XFAIL anchors

- `TestXUISyncJobExtraFailureSummaryKeepsLastErr_XFAILIssue4`: skipped, blocked by issue 4 (`last_run_summary` хранит generic `failed` вместо real `lastErr`).
- `TestLegacySecretboxFallbackDoesNotAuditAfterFix_XFAILIssue17`: skipped, blocked by issue 17 (legacy fallback audit noise).
- `TestLoadCacheEntryFailsClosedOnClientIPReadError_XFAILIssue18`: skipped, blocked by issue 18 (ошибка чтения `client_ips` глотается).
- `TestUserServiceMigrateLegacyTokensKeepsDisabled_XFAILIssue27`: skipped, blocked by issue 27 (legacy disabled token включается обратно).
- `TestValidateOptionalHTTPURLRejectsFragment_XFAILIssue30`: skipped, blocked by issue 30 (URL fragment сейчас принимается).
- `TestConsumeWSTokenTimingRegressionAnchor_XFAILIssue33`: skipped, blocked by issue 33 (timing-sensitive anchor включать после constant-time фикса).
- `TestUserServiceLoginLockedDocumentedAtAPILayer`: skipped, потому что lockout находится в API rate-limit, а не в `UserService.Login`; production boundary не менялся.

Current-behavior anchor без skip:
- `TestApplyExtraWireguardNoPeersSkipCurrentSummary_XFAILIssue6`: фиксирует текущее поведение issue 6 — wireguard endpoint skip считается как `Inbounds.Skipped`, потому что `Endpoints.Skipped` в отчёте ещё нет.

### Regression anchors по реестру

- Issue 4: cron sync failed summary (`XFAIL`).
- Issue 6: wireguard endpoint skip summary (current behavior anchor).
- Issue 16: audit writer overflow/drop counter, batched flush, stop flush.
- Issue 17: secretbox legacy fallback audit noise (`XFAIL`) и primary candidate no-audit green.
- Issue 18: ipmonitor DB read error fail-closed (`XFAIL`).
- Issue 27: legacy token migration disabled state (`XFAIL`).
- Issue 30: URL fragment validation (`XFAIL`), user-info rejection green.
- Issue 33: websocket token double-spend/expiry/capacity/rate-limit green; timing anchor skipped until fix.

## Phase 3 (integration tests)

Дата прогона: 2026-05-24.

### Команды Phase 3

| Команда | Статус | Лог | JUnit |
|---|---:|---|---|
| `go build ./...` | green | [`phase3/build.txt`](phase3/build.txt) | [`phase3/build.junit.xml`](phase3/build.junit.xml) |
| `go vet ./...` | green | [`phase3/vet.txt`](phase3/vet.txt) | [`phase3/vet.junit.xml`](phase3/vet.junit.xml) |
| `go test ./... -count=1 -timeout 5m` | green | [`phase3/test.txt`](phase3/test.txt) | [`phase3/test.junit.xml`](phase3/test.junit.xml) |
| `go test ./... -race -count=1 -timeout 15m` | red, flaky full-suite race | [`phase3/test-race.txt`](phase3/test-race.txt) | [`phase3/test-race.junit.xml`](phase3/test-race.junit.xml) |
| `go test ./... -coverprofile=tests/baseline/phase3/coverage/coverage.out` | green | [`phase3/coverage/coverage.txt`](phase3/coverage/coverage.txt) | [`phase3/coverage/coverage.junit.xml`](phase3/coverage/coverage.junit.xml) |
| `go tool cover -func=tests/baseline/phase3/coverage/coverage.out` | green | [`phase3/coverage/coverage-func.txt`](phase3/coverage/coverage-func.txt) | [`phase3/coverage/coverage-func.junit.xml`](phase3/coverage/coverage-func.junit.xml) |

Дополнительные диагностические артефакты: [`phase3/test-targeted-final.txt`](phase3/test-targeted-final.txt) зелёный для новых integration-slices; race rerun по `api` 5/5 green: [`phase3/test-race-api-rerun-1.txt`](phase3/test-race-api-rerun-1.txt), [`phase3/test-race-api-rerun-2.txt`](phase3/test-race-api-rerun-2.txt), [`phase3/test-race-api-rerun-3.txt`](phase3/test-race-api-rerun-3.txt), [`phase3/test-race-api-rerun-4.txt`](phase3/test-race-api-rerun-4.txt), [`phase3/test-race-api-rerun-5.txt`](phase3/test-race-api-rerun-5.txt).

`-tags=integration` не запускался отдельно: новые интеграционные тесты добавлены обычными `*_test.go` без build-tag изоляции.

### Green/red по сценариям

| Сценарий | Статус | Файл |
|---|---:|---|
| login -> CSRF -> save settings reject/protected-key -> realtime `TopicConfigInvalidated`; audit `login_success`, `sub_path_changed`, `settings_save_rejected_key` | green + XFAIL success-audit | `api/integration_auth_flow_test.go` |
| ws-token -> connect -> `connected` -> publish -> close -> reconnect; multiple clients; max per user/IP | green + XFAIL slow-client drop | `api/integration_ws_lifecycle_test.go` |
| `RotateSessionGeneration()` -> ws close 4401 `session_rotated` -> audit `ws_tokens_invalidated` | green | `service/integration_session_rotation_test.go` |
| backup envelope -> open -> `ImportDB`; migration failure after rename restores fallback | fallback green, table-count XFAIL on `tls` sentinel | `database/integration_backup_restore_test.go` |
| x-ui import full fixture Plan/Apply strategies/adminMode/includes | skipped: требует `test-db/x-ui.db` и `test-db/s-ui.db` | `database/importxui/integration_import_full_test.go` |
| `XUISyncJob.RunProfile`: success, недоступный source, min-interval; `last_run_status`/`last_run_summary` | green | `cronjob/integration_xui_sync_test.go` |
| `StatsService.SaveStats` без core | green; test-core realtime publish XFAIL | `service/integration_stats_pipeline_test.go` |
| `ClientService.RotateSubSecret` -> reload через APIv2 -> realtime config invalidation | green | `service/integration_subsecret_rotate_test.go` |
| `ipmonitor.Allow` на наполненной DB с limit/mode | green; issue 18 XFAIL не дублировался | `ipmonitor/integration_enforce_path_test.go` |

### Coverage delta к Phase 2

Coverage total: 42.1% -> 42.5% statements, полный function report: [`phase3/coverage/coverage-func.txt`](phase3/coverage/coverage-func.txt).

| Пакет | Phase 2 | Phase 3 | Дельта |
|---|---:|---:|---:|
| `api` | 53.9% | 56.0% | +2.1 |
| `service` | 47.5% | 48.0% | +0.5 |
| `database` | 66.3% | 66.5% | +0.2 |
| `database/importxui` | 24.6% | 24.6% | +0.0 |
| `cronjob` | 61.4% | 61.4% | +0.0 |
| `ipmonitor` | 81.4% | 82.4% | +1.0 |
| `realtime` | 92.5% | 92.5% | +0.0 |

### Race detector

Финальный `go test ./... -race -count=1 -timeout 15m` red: race в full-suite запуске между фоновым `service/token_use_debouncer.go` flush и переинициализацией test DB из `api/settings_save_test.go:111`. Это не исправлялось в Phase 3. Пять повторов `go test ./api -race -count=1 -timeout 5m` не воспроизвели race: 0/5 red, 5/5 green. Классификация: flaky/cross-package full-suite race, требует отдельного production/test-hook фикса в фазах надёжности.

### Skipped / XFAIL anchors

- `TestIntegrationAuthFlowSettingsSaveSuccessAudit_XFAILPhase3`: успешный settings save пока не пишет `settings_save_*` audit event; привязано к п. 26 и observability gap.
- `TestIntegrationRealtimeWSSlowClientDrop_XFAILPhase3`: нужен hook для детерминированного slow writer / заполнения send queue; привязано к п. 32.
- `TestIntegrationBackupEnvelopeRestorePreservesBackupTableCounts`: XFAIL runtime skip, если restore меняет счётчик `tls` из-за no-TLS sentinel; привязано к п. 11.
- `TestIntegrationImportXUIFullFixturePlanApply`: skipped без `test-db/x-ui.db` и `test-db/s-ui.db`; см. [`env.md`](env.md).
- `TestIntegrationStatsPipelineRealtimeWithTestCore_XFAILPhase3`: нужен test-core или hook для подмены `core.Core`/`StatsTracker`; привязано к п. 26.

### Integration anchors по реестру

- п. 2, 8: importxui full Plan/Apply fixture path закреплён skipped integration-anchor до появления `test-db`.
- п. 4, 5, 7, 41: xui sync in-process success/fail/min-interval обновляет run fields и audit.
- п. 11: backup restore consistency и fallback после migration failure покрыты интеграционно; `tls` count mismatch оставлен XFAIL.
- п. 18: `ipmonitor.Allow` enforce path покрыт интеграционно, XFAIL на DB read error из Phase 2 не дублировался.
- п. 26: stats nil-core smoke green; audit/observability gaps закреплены XFAIL для settings success и test-core publish.
- п. 32, 33: websocket lifecycle, reconnect, multiple clients, ws-token capacity и session rotation close/audit покрыты in-process; slow-client drop оставлен XFAIL.
- Sub-secret rotation получил integration-anchor: смена `sub_secret`, APIv2 reload и realtime config invalidation.

## Phase 4 (security)

Дата прогона: 2026-05-24.

Статическая security-часть выполнена без DAST, без dependency upgrades и без production-фиксов. Gosec red оставлен как классифицированный baseline; govulncheck triage вынесен в отдельный диалог.

### Команды Phase 4

| Команда | Статус | Лог | JUnit |
|---|---:|---|---|
| `go build ./...` | green | [`phase4/build.txt`](phase4/build.txt) | [`phase4/build.junit.xml`](phase4/build.junit.xml) |
| `go vet ./...` | green | [`phase4/vet.txt`](phase4/vet.txt) | [`phase4/vet.junit.xml`](phase4/vet.junit.xml) |
| `go test ./... -count=1 -timeout 5m` | green | [`phase4/test.txt`](phase4/test.txt) | [`phase4/test.junit.xml`](phase4/test.junit.xml) |
| `go test ./... -run Security -count=1` | green | [`phase4/test-security.txt`](phase4/test-security.txt) | [`phase4/test-security.junit.xml`](phase4/test-security.junit.xml) |
| `go test ./... -race -count=1 -timeout 15m` | green | [`phase4/test-race.txt`](phase4/test-race.txt) | [`phase4/test-race.junit.xml`](phase4/test-race.junit.xml) |
| `gosec ./...` | red, classified baseline | [`phase4/gosec.txt`](phase4/gosec.txt) | [`phase4/gosec.junit.xml`](phase4/gosec.junit.xml) |
| `go test ./... -coverprofile=tests/baseline/phase4/coverage/coverage.out` | green | [`phase4/coverage/coverage.txt`](phase4/coverage/coverage.txt) | [`phase4/coverage/coverage.junit.xml`](phase4/coverage/coverage.junit.xml) |
| `go tool cover -func=tests/baseline/phase4/coverage/coverage.out` | green | [`phase4/coverage/coverage-func.txt`](phase4/coverage/coverage-func.txt) | [`phase4/coverage/coverage-func.junit.xml`](phase4/coverage/coverage-func.junit.xml) |

Дополнительные артефакты Phase 4: [`phase4/step-a.txt`](phase4/step-a.txt), [`phase4/step-b.txt`](phase4/step-b.txt), [`phase4/steps-c-j.txt`](phase4/steps-c-j.txt), [`phase4/coverage/coverage-normalize.txt`](phase4/coverage/coverage-normalize.txt). Coverage profile был нормализован в `coverage.out` после того, как Windows/Go run оставил профиль как `coverage`.

### Security review docs

- Gosec triage: [`docs/audit/security/gosec-triage.md`](../../docs/audit/security/gosec-triage.md).
- AuthZ matrix: [`docs/audit/security/authz-matrix.md`](../../docs/audit/security/authz-matrix.md).
- CSRF matrix: [`docs/audit/security/csrf-matrix.md`](../../docs/audit/security/csrf-matrix.md).

Gosec classification: 55 total findings, из них true positive 16, nosec 18, mitigated_in_review 21. В production-код не добавлялись `//nosec` / `//nolint`, `.golangci.yml` не менялся.

### Green/red по security-сценариям

| Сценарий | Статус | Файл |
|---|---:|---|
| AuthZ scopes для v2 middleware, wrong scope 403, correct scope 200 | green + current-contract/XFAIL anchors | `api/security_authz_test.go` |
| Дублированные `/import-xui/*` v1/v2 route contracts | green anchor | `api/security_authz_test.go` |
| CSRF matrix для protected POST: missing/expired token rejected, rotated session rejected before handler | green | `api/security_csrf_test.go` |
| Login lockout/rate-limit: 10+ wrong logins, `login_blocked`, recovery after window/reset | green | `api/security_login_lockout_test.go` |
| SSRF validation: private/link-local/multicast/file/scheme/user-info cases | green + XFAIL gaps | `service/security_ssrf_test.go` |
| Rollback path validation: missing/outside/traversal/symlink | green + database package XFAIL anchor | `api/security_rollback_path_test.go`, `database/security_rollback_path_test.go` |
| API token extraction, constant-time scope helper, WS token double-spend/expiry/capacity | green | `api/security_token_test.go`, `service/security_token_test.go` |
| Session cookie flags, max-age, rotation invalidates old cookie | green + Strict SameSite XFAIL | `api/security_session_test.go` |
| WS origin enforcement and `ws_origin_rejected` audit | green | `api/security_ws_origin_test.go` |
| Backup audit confidentiality and config passphrase redaction | green + zeroization hook XFAIL | `service/security_backup_confidentiality_test.go` |

### Coverage delta к Phase 3

Coverage total: 42.5% -> 42.7% statements, полный function report: [`phase4/coverage/coverage-func.txt`](phase4/coverage/coverage-func.txt).

| Пакет | Phase 3 | Phase 4 | Дельта |
|---|---:|---:|---:|
| `api` | 56.0% | 57.5% | +1.5 |
| `service` | 48.0% | 48.1% | +0.1 |
| `database` | 66.5% | 66.5% | +0.0 |
| `util/ssrf` | 0.0% | 59.7% | +59.7 |
| `ipmonitor` | 82.4% | 82.4% | +0.0 |
| `realtime` | 92.5% | 92.5% | +0.0 |

### Skipped / XFAIL anchors

- `TestSecurityAuthZAPIV2HTTPAuthStatus_XFAILPhase4`: текущий v2 invalid/expired token contract возвращает HTTP 200 + `success:false`, desired contract 401/403; привязка к п. 34.
- `TestSecurityTelegramProxyURLRejectsUserInfo_XFAILPhase4`: Telegram proxy URL сейчас допускает user-info для proxy auth; нужна продуктовая политика.
- `TestSecurityOptionalHTTPURLRejectsPrivateHosts_XFAILIssue30`: `validateOptionalHTTPURL` пока не блокирует private hosts; привязка к п. 30.
- `TestSecurityRollbackPathDatabasePackageAnchor_XFAILPhase4`: `validateRollbackPath` находится в `api`, executable coverage есть в `api/security_rollback_path_test.go`.
- `TestSecuritySessionStrictSameSiteConfig_XFAILPhase4`: Strict SameSite сейчас не конфигурируется.
- `TestSecurityTelegramBackupZeroizationOnError_XFAILIssue25`: нужна production/test hook видимость вызова wipe/zeroization на error path; привязка к п. 25.

### Security anchors по реестру

- п. 25: backup confidentiality redaction green, deterministic zeroization оставлен XFAIL.
- п. 30: SSRF/user-info/scheme tests green, private-host optional URL gap XFAIL.
- п. 33: WS token double-spend/expiry/capacity green; Phase 2 timing XFAIL не дублировался.
- п. 34: v2 token auth status зафиксирован current-contract + XFAIL desired 401/403.
- п. 35: `/import-xui/*` v1/v2 duplicate route contract закреплён test-anchor.
- п. 38: G304/path findings классифицированы, rollback path security tests добавлены.
- п. 47: Phase 3 race finding добавлен в реестр как отдельный P2 Concurrency issue.

## Phase 5 (perf & reliability)

Дата прогона: 2026-05-24.

Phase 5 выполнена без правок production-кода, без dependency upgrades, без frontend/npm и без live-нагрузки на внешние сервисы. Все новые сценарии in-process или через `httptest`. Детальный perf baseline: [`docs/audit/perf/baseline.md`](../../docs/audit/perf/baseline.md).

### Команды Phase 5

| Команда | Статус | Лог | JUnit |
|---|---:|---|---|
| `go build ./...` | green | [`phase5/build.txt`](phase5/build.txt) | [`phase5/build.junit.xml`](phase5/build.junit.xml) |
| `go vet ./...` | green | [`phase5/vet.txt`](phase5/vet.txt) | [`phase5/vet.junit.xml`](phase5/vet.junit.xml) |
| `go test ./... -count=1 -timeout 5m` | green | [`phase5/test.txt`](phase5/test.txt) | [`phase5/test.junit.xml`](phase5/test.junit.xml) |
| `go test ./... -race -count=1 -timeout 15m` | green | [`phase5/test-race.txt`](phase5/test-race.txt) | [`phase5/test-race.junit.xml`](phase5/test-race.junit.xml) |
| `go test -bench=. -benchmem -run=^$ ./database/...` | green | [`phase5/bench-backup.txt`](phase5/bench-backup.txt) | [`phase5/bench-backup.junit.xml`](phase5/bench-backup.junit.xml) |
| backup mem/cpu pprof | green | [`phase5/pprof-backup-mem.txt`](phase5/pprof-backup-mem.txt), [`phase5/pprof-backup-cpu.txt`](phase5/pprof-backup-cpu.txt) | JUnit рядом |
| API load cpu pprof | green | [`phase5/pprof-api-load-cpu.txt`](phase5/pprof-api-load-cpu.txt) | [`phase5/pprof-api-load-cpu.junit.xml`](phase5/pprof-api-load-cpu.junit.xml) |
| `go test ./... -bench=. -benchmem -run=^$ -benchtime=2s` | green | [`phase5/bench-all.txt`](phase5/bench-all.txt), [`phase5/bench-all.clean.txt`](phase5/bench-all.clean.txt) | [`phase5/bench-all.junit.xml`](phase5/bench-all.junit.xml) |
| coverage | green | [`phase5/coverage/coverage.txt`](phase5/coverage/coverage.txt), [`phase5/coverage/coverage-func.txt`](phase5/coverage/coverage-func.txt) | [`phase5/coverage/coverage.junit.xml`](phase5/coverage/coverage.junit.xml), [`phase5/coverage/coverage-func.junit.xml`](phase5/coverage/coverage-func.junit.xml) |

Coverage total: 42.7% -> 43.0% statements. `api` вырос до 59.5%; `service` остался 48.1%; `database/importxui` остался 24.6% и по-прежнему ограничен отсутствием `test-db` fixture.

### Добавленные Phase 5 файлы

- `database/backup_bench_test.go`
- `service/stats_bench_test.go`
- `service/audit_writer_bench_test.go`
- `service/telegram_bench_test.go`
- `service/token_use_debouncer_bench_test.go`
- `database/importxui/plan_bench_test.go`
- `ipmonitor/ipmonitor_bench_test.go`
- `realtime/hub_bench_test.go`
- `api/realtime_bench_test.go`
- `cronjob/xuiSyncJob_bench_test.go`
- `api/perf_http_test.go`

### Ключевые perf anchors

- Backup `GetDb` full 100k: 5.996s/op, 616.6 MB/op, 29.15 MB output; full 1M: 84.8s/op, 5.99 GB/op, 289.6 MB output. Exclude-heavy path остаётся около 110-112 ms/op.
- `StatsService.SaveStats`: 100 clients 7.81 ms/op, 1000 clients 77.64 ms/op; рост близок к линейному.
- Audit writer overload: 10,000 events -> 5,904 drops/op; current FIFO теряет `lost_warn_security=5000` и `lost_info=904`, severity priority отсутствует.
- `ipmonitor.Allow`: known IP при 1000 clients x 5 IP ~2.1 us/op; reject over limit ~3.0 us/op.
- Realtime hub: publish to 1000 subscribers 361.7 us/op, 0 drops; WS connect/disconnect 20 clients 35.3 ms/op.
- Cron lost-network sync: 3 attempts, ожидаемый backoff 300 ms, observed 303.4 ms.
- HTTP `httptest`: `/load` 9.28 ms/op, `/stats` 5.03 ms/op, `/onlines` 4.1 us/op, `/save` 0.83 ms/op, `/import-xui/reports` 3.23 ms/op.

### pprof hot spots

- Backup CPU raw top: `runtime.cgocall` 49.38s / 62.57%; application cumulative: `database.GetDb`/`copyBackupTable` ~64.0s, `gorm.FindInBatches` ~48.0s.
- Backup alloc top: `sqlite3.(*SQLiteConn).query` 846 MB, `database/sql.driverArgsConnLocked` 831 MB, `reflect.unsafe_New` 741 MB; application cumulative `database.GetDb`/`copyBackupTable` ~5.7 GB alloc-space.
- API `/load` CPU raw top: `runtime.cgocall` 25.96s / 79.32%; application cumulative: `ApiService.LoadData` 29.44s, `getData` -> `SettingService.GetFinalSubURI` 27.04s, `gorm.(*processor).Execute` 27.57s.

Top files:
- [`phase5/profiles/backup-cpu.top.txt`](phase5/profiles/backup-cpu.top.txt)
- [`phase5/profiles/backup-mem.top.txt`](phase5/profiles/backup-mem.top.txt)
- [`phase5/profiles/api-load-cpu.top.txt`](phase5/profiles/api-load-cpu.top.txt)

### Реестр и ограничения

Performance-anchor теперь есть для п. 16, 18, 32, 36, 40, 44. Для п. 44 Phase 5 закрепляет server-side import report/rollback-adjacent API latency и rate-limit; реальный frontend health-check polling/e2e остаётся задачей Phase 6.

Phase 3 race finding #47 не воспроизвёлся в Phase 5 `go test ./... -race`, но остаётся отдельным зарегистрированным P2 concurrency issue и не исправлялся здесь. Frontend/npm red baseline, staticcheck/golangci/gosec/govulncheck red baseline не запускались и не исправлялись.

## Phase 6 (frontend)

Дата прогона: 2026-05-24.

Phase 6 выполнена без правок production-кода Go и без правок `frontend/src`, кроме новых test-only файлов. Ручная модификация `frontend/src/layouts/modals/Endpoint.vue` не трогалась. Детальный frontend baseline: [`docs/audit/frontend/baseline.md`](../../docs/audit/frontend/baseline.md), accessibility/security baseline: [`docs/audit/frontend/accessibility.md`](../../docs/audit/frontend/accessibility.md).

### Команды Phase 6

| Команда | Статус | Лог | JUnit |
|---|---:|---|---|
| Windows EPERM diagnostic для `@rolldown` binding | green после закрытия держателей файла | [`phase6/windows-eperm-diagnostic-rerun.txt`](phase6/windows-eperm-diagnostic-rerun.txt) | [`phase6/windows-eperm-diagnostic-rerun.junit.xml`](phase6/windows-eperm-diagnostic-rerun.junit.xml) |
| удаление `frontend/node_modules` | green | [`phase6/remove-node-modules.txt`](phase6/remove-node-modules.txt) | [`phase6/remove-node-modules.junit.xml`](phase6/remove-node-modules.junit.xml) |
| `npm ci` в `frontend/` | green | [`phase6/npm-ci.txt`](phase6/npm-ci.txt) | [`phase6/npm-ci.junit.xml`](phase6/npm-ci.junit.xml) |
| `npm run lint --prefix frontend` | green | [`phase6/npm-run-lint-final-rerun.txt`](phase6/npm-run-lint-final-rerun.txt) | [`phase6/npm-run-lint-final-rerun.junit.xml`](phase6/npm-run-lint-final-rerun.junit.xml) |
| `npm run build --prefix frontend` | green | [`phase6/npm-run-build-final-rerun.txt`](phase6/npm-run-build-final-rerun.txt) | [`phase6/npm-run-build-final-rerun.junit.xml`](phase6/npm-run-build-final-rerun.junit.xml) |
| `typecheck` script | skipped: отдельного script нет, `vue-tsc --noEmit` входит в build | [`phase6/npm-run-typecheck.txt`](phase6/npm-run-typecheck.txt) | [`phase6/npm-run-typecheck.junit.xml`](phase6/npm-run-typecheck.junit.xml) |
| исходный `npm run test --prefix frontend` до новых anchors | green, 11 files / 53 tests | [`phase6/npm-run-test-existing.txt`](phase6/npm-run-test-existing.txt) | [`phase6/npm-run-test-existing.vitest.junit.xml`](phase6/npm-run-test-existing.vitest.junit.xml) |
| финальный Vitest baseline | green, 15 files / 76 tests | [`phase6/vitest.txt`](phase6/vitest.txt) | [`phase6/vitest.junit.xml`](phase6/vitest.junit.xml) |
| `npx playwright install chromium` | green | [`phase6/playwright-install-chromium.txt`](phase6/playwright-install-chromium.txt) | [`phase6/playwright-install-chromium.junit.xml`](phase6/playwright-install-chromium.junit.xml) |
| Playwright e2e smoke | green: 8 passed, 4 skipped/XFAIL | [`phase6/playwright.txt`](phase6/playwright.txt), [`phase6/playwright/html/index.html`](phase6/playwright/html/index.html) | [`phase6/playwright.junit.xml`](phase6/playwright.junit.xml) |
| axe accessibility scan | green collector, violations documented | [`phase6/a11y/axe-results.json`](phase6/a11y/axe-results.json), [`phase6/summarize-axe-findings.txt`](phase6/summarize-axe-findings.txt) | [`phase6/summarize-axe-findings.junit.xml`](phase6/summarize-axe-findings.junit.xml) |
| security headers smoke | green для HTTP security headers | [`phase6/security-headers/headers.json`](phase6/security-headers/headers.json) | included in [`phase6/playwright.junit.xml`](phase6/playwright.junit.xml) |

### Добавленные Phase 6 файлы

- `frontend/src/store/__tests__/ws.spec.ts`
- `frontend/src/store/__tests__/csrf.spec.ts`
- `frontend/src/plugins/__tests__/httputil.spec.ts`
- `frontend/src/plugins/__tests__/api.spec.ts`
- `frontend/playwright.config.ts`
- `frontend/tests/e2e/helpers.ts`
- `frontend/tests/e2e/login.spec.ts`
- `frontend/tests/e2e/migrate-xui-happy.spec.ts`
- `frontend/tests/e2e/settings-paths.spec.ts`
- `frontend/tests/e2e/ws-reconnect.spec.ts`
- `frontend/tests/e2e/tokens.spec.ts`
- `frontend/tests/e2e/observability.spec.ts`
- `frontend/tests/e2e/a11y.spec.ts`
- `frontend/tests/e2e/security-headers.spec.ts`
- `tests/e2e/run-server.js`
- `tests/e2e/panel-server/main.go`
- `docs/audit/frontend/baseline.md`
- `docs/audit/frontend/accessibility.md`

### E2E окружение

Рабочий `node_modules` восстановлен: повторный `npm ci` прошёл после закрытия локальных Node/VS Code держателей `rolldown-binding.win32-x64-msvc.node` и удаления `frontend/node_modules`. `npm cache clean --force` не понадобился.

Полный `sui` server для e2e не использовался: обычный `go run .` завершался на `naive outbound is disabled when built without with_naive_outbound tag`, а `go run -tags with_naive_outbound .` на Windows упирался в build constraints `github.com/metacubex/mihomo/transport/cro/cronet-go/all`. Production-код не поддерживает `SUI_DISABLE_CORE=1`. Поэтому Phase 6 smoke запущен через test-only helper `tests/e2e/panel-server/main.go`: он поднимает локальные API/session/security middleware на SQLite test DB и Vite frontend, но не стартует sing-box/core, cron и внешние сервисы. Сгенерированные plaintext e2e credentials после прогона удалены из артефактов; см. [`phase6/redact-e2e-generated-secrets.txt`](phase6/redact-e2e-generated-secrets.txt).

### Accessibility и security headers

Axe baseline собран по страницам login, dashboard, migrate-xui, settings, audit. Количество violations: login 6, dashboard 5, migrate-xui 5, settings 5, audit 6. Основные классы: `button-name`, `label`, `aria-required-children`, `image-alt`, `color-contrast`, landmarks/H1, `aria-tooltip-name`, `empty-table-header`. Это зафиксировано как accessibility debt, без правок UI в этом диалоге.

Security headers smoke green: `Content-Security-Policy` содержит `frame-ancestors 'none'`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`. `Strict-Transport-Security` условно skipped на HTTP dev-инстанции и должен проверяться на HTTPS.

### Frontend anchors по реестру

- п. 32: Vitest закрепляет `WsRuntime` happy path, no-token, open timeout, malformed message, close/reconnect, `closeCount>=3` fallback и отсутствие auto-reconnect из degraded polling; Playwright `ws-reconnect.spec.ts` оставлен `test.fixme`.
- п. 43: MigrateXui apply error UX разобран в baseline; e2e apply path оставлен `test.fixme` до fixture/UX фикса.
- п. 44: rollback `setTimeout + location.reload` race разобран в baseline; e2e rollback оставлен `test.fixme`.
- п. 45: password reveal через `JSON.stringify(generatedAdmins)` разобран в baseline; e2e warning/download anchor оставлен `test.fixme`.
- п. 46: `adminModeItems` и fallback/reset-required semantics разобраны в baseline; e2e migration contract оставлен `test.fixme`.
- Дополнительно закреплены `csrfStore`, `HttpUtils`, mutating API CSRF header/abort behavior, realtime unknown-event pitfalls и security headers.

Skipped/XFAIL в Playwright: 3 сценария `migrate-xui-happy.spec.ts` по п. 43/44/45/46 из-за отсутствия fixture DB и открытых UX-contract gaps; 1 сценарий `ws-reconnect.spec.ts` по п. 32 до фикса авто-возврата из degraded в connected.

## Phase 7 (chaos)

Дата прогона: 2026-05-24.

Phase 7 выполнена без правок production-кода, без dependency upgrades, без исправления known red static/security baseline и без внешних сетевых вызовов. Runbook: [`docs/audit/chaos/runbook.md`](../../docs/audit/chaos/runbook.md). Артефакты команд лежат в [`phase7/`](phase7/).

### Команды Phase 7

| Команда | Статус | Лог | JUnit |
|---|---:|---|---|
| `go build ./...` | green | [`phase7/build.txt`](phase7/build.txt) | [`phase7/build.junit.xml`](phase7/build.junit.xml) |
| `go vet ./...` | green | [`phase7/vet.txt`](phase7/vet.txt) | [`phase7/vet.junit.xml`](phase7/vet.junit.xml) |
| `go test -tags=chaos ./tests/chaos/... -count=1 -timeout 10m` | green, skips/XFAIL | [`phase7/test-chaos.txt`](phase7/test-chaos.txt) | [`phase7/test-chaos.junit.xml`](phase7/test-chaos.junit.xml) |
| `go test ./... -count=1 -timeout 5m` | green | [`phase7/test.txt`](phase7/test.txt) | [`phase7/test.junit.xml`](phase7/test.junit.xml) |
| `go test ./... -race -count=1 -timeout 15m` | red, issue 47 | [`phase7/test-race.txt`](phase7/test-race.txt) | [`phase7/test-race.junit.xml`](phase7/test-race.junit.xml) |
| `go test -race -tags=chaos ./tests/chaos -run TestTokenUseDebouncerRaceVsDBReinitChaosIssue47` | red, deterministic issue 47 anchor | [`phase7/test-chaos-race-anchor.txt`](phase7/test-chaos-race-anchor.txt) | [`phase7/test-chaos-race-anchor.junit.xml`](phase7/test-chaos-race-anchor.junit.xml) |
| Playwright WS reconnect chaos | green command, expected-fail scenario | [`phase7/playwright-ws-chaos.txt`](phase7/playwright-ws-chaos.txt) | [`phase7/playwright-ws-chaos/junit.xml`](phase7/playwright-ws-chaos/junit.xml) |
| Docker chaos | skipped: Docker CLI unavailable | [`phase7/docker-chaos-script.txt`](phase7/docker-chaos-script.txt) | [`phase7/docker-chaos-script.junit.xml`](phase7/docker-chaos-script.junit.xml) |

### Добавленные Phase 7 файлы

- `docs/audit/chaos/runbook.md`
- `tests/chaos/helpers_test.go`
- `tests/chaos/sighup_test.go`, `tests/chaos/sighup_linux_test.go`, `tests/chaos/sighup_windows_test.go`
- `tests/chaos/sqlite_locked_test.go`
- `tests/chaos/importdb_chaos_test.go`
- `tests/chaos/xui_import_kill_test.go`
- `tests/chaos/telegram_down_test.go`
- `tests/chaos/warp_down_test.go`
- `tests/chaos/system_info_ipv6_only_test.go`
- `tests/chaos/cron_sync_chaos_test.go`
- `tests/chaos/ipmonitor_fail_closed_test.go`
- `tests/chaos/xui_rate_limit_unbounded_test.go`
- `tests/chaos/token_use_debouncer_race_test.go`, `tests/chaos/race_enabled_test.go`, `tests/chaos/race_disabled_test.go`
- `tests/chaos/docker-compose.chaos.yml`, `tests/chaos/run-docker-chaos.ps1`, `tests/chaos/run-docker-chaos.sh`
- `frontend/tests/e2e/ws-reconnect-chaos.spec.ts`

### Green/red по chaos-сценариям

| Сценарий | Статус | Реестр |
|---|---:|---|
| `ImportDB` вызывает `SendSighup` hook один раз и сохраняет строки restore | green | п. 10 |
| Реальный 3s SIGHUP timer | skipped на Windows, требует Linux runner | п. 10 |
| SQLite `BEGIN IMMEDIATE` lock во время `GetDb` | green current-behavior anchor | п. 11 |
| `ImportDB` rollback после post-rename foreign-key failure | green | п. 11, 14 |
| stage read cancel удаляет `.temp` и не трогает fallback/live DB | green | п. 11 |
| versioned backup без `settings.config` отклоняется | green | п. 12 |
| xui `Apply` cancel после первого progress event: tx rollback, `applyMu` released, audit не пишется, backup валиден | green | п. 1 chaos input, п. 11 |
| Telegram down/backoff cancel | skipped/XFAIL: нет exported hook | п. 22 |
| WARP API down | skipped/XFAIL: нет exported hook, внешние вызовы запрещены | п. 31 |
| `GetSystemInfo` smoke на текущем host | green; IPv6-only DI skipped | п. 23 |
| 50 lost-source cron sync runs | green: 3 attempts, ~300ms/run, run fields, memory bounded | п. 40 |
| ipmonitor transient DB read error fail-closed | skipped/XFAIL: нужен DB fault hook | п. 18 |
| 10,000 unique IP для xui rate-limit | green current behavior; bounded map size XFAIL | п. 36 |
| WS reconnect 5 offline/online циклов | Playwright expected-fail до healing reconnect | п. 32 |
| token-use debouncer vs DB reinit | red under `-race`, deterministic chaos anchor | п. 47 |

### Race detector

Full-suite `go test ./... -race` снова red на п. 47: `token_use_debouncer.go` flush читает текущий `database.GetDB()`/GORM handle одновременно с `database.InitDB()` в API тестовом lifecycle. В Phase 7 это проявилось в `TestAPIImportXUIReportsRateLimitPhase5`; отдельный chaos anchor [`phase7/test-chaos-race-anchor.txt`](phase7/test-chaos-race-anchor.txt) воспроизводит ту же гонку детерминированно через `RecordTokenUse` + `StopTokenUseDebouncer` + параллельный `InitDB`.

### Платформенные блокеры

- Windows: настоящий `SIGHUP` timer не проверяется, нужен Linux runner.
- Docker: Docker CLI на машине не найден, compose chaos помечен skipped.
- `test-db`: реальные `test-db/x-ui.db` и `test-db/s-ui.db` отсутствуют; Phase 7 использует синтетический x-ui SQLite для cancel/rollback path.
- DI hooks: п. 18, 22, 23, 31, 36 требуют fault-injection/exported test hooks или package-local chaos tests в отдельных фикс-диалогах.

## Подтверждение пунктов аудита

- Системный паттерн "молчащие ошибки" подтверждён `golangci-lint`/`errcheck` и `gosec G104`.
- Особое внимание Фазы 1 к `gosec G304/G101` подтверждено: есть несколько `G304` file inclusion/path findings и `G101` false-positive/secret-pattern findings.
- Пробел аудита по внешним проверкам закрыт для `go test -race`, `go vet`, `staticcheck`, `golangci-lint`, `govulncheck`, `gosec`.
- Frontend checks пока не закрыты зелёным baseline из-за failed `npm ci`; это отдельный red baseline, не исправлялся.

## Phase 8 (CI)

Дата прогона: 2026-05-24.

Phase 8 выполнена без правок production-кода, без dependency upgrades и без исправления known red baseline. Добавлена CI-упаковка:

- GitHub Actions: [`.github/workflows/audit.yml`](../../.github/workflows/audit.yml), [`.github/workflows/audit-go.yml`](../../.github/workflows/audit-go.yml), [`.github/workflows/audit-frontend.yml`](../../.github/workflows/audit-frontend.yml), [`.github/workflows/audit-chaos.yml`](../../.github/workflows/audit-chaos.yml), [`.github/workflows/audit-perf.yml`](../../.github/workflows/audit-perf.yml).
- Aggregator: [`scripts/audit/aggregate.sh`](../../scripts/audit/aggregate.sh), [`scripts/audit/aggregate.ps1`](../../scripts/audit/aggregate.ps1).
- CI docs: [`docs/audit/ci/design.md`](../../docs/audit/ci/design.md), [`docs/audit/ci/required-checks.md`](../../docs/audit/ci/required-checks.md), [`docs/audit/ci/runbook.md`](../../docs/audit/ci/runbook.md), [`docs/audit/ci/existing-workflows.md`](../../docs/audit/ci/existing-workflows.md), [`docs/audit/ci/audit-targets.md`](../../docs/audit/ci/audit-targets.md).
- Local dashboard: [`phase8/summary.html`](phase8/summary.html), [`phase8/summary.json`](phase8/summary.json).

### Команды Phase 8

| Команда | Статус | Лог | JUnit |
|---|---:|---|---|
| `go build ./...` | green | [`phase8/build.txt`](phase8/build.txt) | [`phase8/build.junit.xml`](phase8/build.junit.xml) |
| `go vet ./...` | green | [`phase8/vet.txt`](phase8/vet.txt) | [`phase8/vet.junit.xml`](phase8/vet.junit.xml) |
| `go test ./...` | green | [`phase8/test-go.txt`](phase8/test-go.txt) | [`phase8/test-go.junit.xml`](phase8/test-go.junit.xml) |
| `go test ./... -race -count=1` | red, issue 47 | [`phase8/test-go-race.txt`](phase8/test-go-race.txt) | [`phase8/test-go-race.junit.xml`](phase8/test-go-race.junit.xml) |
| `staticcheck ./...` + `golangci-lint run` | red baseline | [`phase8/lint-go.txt`](phase8/lint-go.txt) | [`phase8/lint-go.junit.xml`](phase8/lint-go.junit.xml) |
| frontend typecheck script | skipped: no separate script | [`phase8/fe-typecheck.txt`](phase8/fe-typecheck.txt) | [`phase8/fe-typecheck.junit.xml`](phase8/fe-typecheck.junit.xml) |
| `npm run lint` | green | [`phase8/fe-lint.txt`](phase8/fe-lint.txt) | [`phase8/fe-lint.junit.xml`](phase8/fe-lint.junit.xml) |
| `npm run build` | green | [`phase8/fe-build.txt`](phase8/fe-build.txt) | [`phase8/fe-build.junit.xml`](phase8/fe-build.junit.xml) |
| `npm run test` | green, 15 files / 76 tests | [`phase8/test-fe.txt`](phase8/test-fe.txt) | [`phase8/test-fe.junit.xml`](phase8/test-fe.junit.xml) |
| workflow YAML parse | green | [`phase8/workflow-yaml-parse.txt`](phase8/workflow-yaml-parse.txt) | [`phase8/workflow-yaml-parse.junit.xml`](phase8/workflow-yaml-parse.junit.xml) |
| `act` dry-run | skipped: `act` not installed | [`phase8/act-dryrun.txt`](phase8/act-dryrun.txt) | [`phase8/act-dryrun.junit.xml`](phase8/act-dryrun.junit.xml) |
| aggregate dashboard | green via PowerShell fallback | [`phase8/aggregate.txt`](phase8/aggregate.txt) | [`phase8/aggregate.junit.xml`](phase8/aggregate.junit.xml) |

Локальный `make` не установлен, поэтому команды из Makefile запускались прямыми эквивалентами через `tests/baseline/run-command.ps1`. Локальный `bash` указывает на WSL launcher без установленного дистрибутива, поэтому `aggregate.sh` будет проверяться на Ubuntu CI; на Windows Phase 8 dashboard сгенерирован через `aggregate.ps1`.

### CI gates

REQUIRED:
- `build` на `ubuntu-latest` и `windows-latest`;
- `vet` на `ubuntu-latest` и `windows-latest`;
- `test-go` на `ubuntu-latest` и `windows-latest`;
- `fe-lint`;
- `fe-build`;
- `fe-vitest`.

SOFT:
- `test-go-race` из-за п. 47;
- `gosec`, `govulncheck`, `staticcheck`, `golangci-lint`;
- `fe-e2e`, `accessibility`;
- `chaos-tests`, `docker-chaos`;
- `bench-all` / perf regression warning.

### Registry anchors in CI

- п. 47: `test-go-race` и chaos race anchor закреплены как soft checks.
- п. 32: frontend e2e/WS reconnect остаётся soft до healing reconnect фикса.
- п. 16, 18, 36, 40: perf/chaos anchors включены в nightly/weekly signal, но не блокируют merge.
- `govulncheck`: 12 called vulnerabilities остаются blocked до отдельного triage; dependency upgrades в Phase 8 не выполнялись.

## Vuln triage 2026-05-24

Dependency-only triage выполнен без правок production-кода, без npm/frontend изменений и без мажорных bump'ов. Детальный отчёт: [`../../docs/audit/security/govulncheck-triage.md`](../../docs/audit/security/govulncheck-triage.md). Повторный scan: [`phase1/govulncheck-after.txt`](phase1/govulncheck-after.txt).

### Закрытые GO-ID

Закрыты все 12 called vulnerabilities из [`phase1/govulncheck.txt`](phase1/govulncheck.txt):
`GO-2026-5026`, `GO-2026-5020`, `GO-2026-5019`, `GO-2026-5018`, `GO-2026-5017`, `GO-2026-5013`, `GO-2026-4986`, `GO-2026-4982`, `GO-2026-4980`, `GO-2026-4977`, `GO-2026-4971`, `GO-2026-4918`.

Остаточные called vulnerabilities: нет. `govulncheck ./...` после апгрейдов: `No vulnerabilities found.`

### Dependency delta

- Добавлен `toolchain go1.26.3` при сохранении `go 1.25.7`; language directive не повышался до 1.26.x.
- `golang.org/x/crypto`: v0.48.0 -> v0.52.0.
- `golang.org/x/net`: v0.51.0 -> v0.55.0.
- Транзитивно обновлены `golang.org/x/term` v0.40.0 -> v0.43.0, `x/sys` v0.41.0 -> v0.45.0, `x/text` v0.34.0 -> v0.37.0, `x/mod` v0.33.0 -> v0.35.0, `x/sync` v0.19.0 -> v0.20.0, `x/tools` v0.42.0 -> v0.44.0.

### Команды после апгрейдов

| Команда | Статус | Лог |
|---|---:|---|
| `go build ./...` | green | [`phaseV/audit-build.txt`](phaseV/audit-build.txt) |
| `go vet ./...` | green | [`phaseV/audit-vet.txt`](phaseV/audit-vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | [`phaseV/audit-test-go.txt`](phaseV/audit-test-go.txt) |
| `go test ./... -race -count=1` | green в этом прогоне; п. 47 не воспроизвёлся | [`phaseV/audit-test-go-race.txt`](phaseV/audit-test-go-race.txt) |
| `gosec ./...` | red baseline, не ухудшился: 55 -> 55 | [`phaseV/gosec-delta.txt`](phaseV/gosec-delta.txt), [`phaseV/gosec-after.txt`](phaseV/gosec-after.txt) |
| `govulncheck ./...` | green | [`phaseV/audit-vuln.txt`](phaseV/audit-vuln.txt) |
| `make audit:*` | skipped locally: `make` отсутствует в PATH; выполнены прямые эквиваленты | [`phaseV/make-unavailable.txt`](phaseV/make-unavailable.txt) |

Новых регрессий и новых пунктов реестра не добавлено.

## Post-fix п. 47 2026-05-24

Фикс выполнен точечно для token-use debouncer vs DB reinit race. Frontend не затрагивался; `frontend/src/layouts/modals/Endpoint.vue` не менялся.

### Команды

| Команда | Статус | Сравнение с baseline | Лог | JUnit |
|---|---:|---|---|---|
| `go test -race -tags=chaos -run TestTokenUseDebouncerRaceVsDBReinitChaosIssue47 ./tests/chaos/... -count=1 -timeout 5m` | red before | race воспроизведён | [`post-fix-47/race-before.txt`](post-fix-47/race-before.txt) | [`post-fix-47/race-before.junit.xml`](post-fix-47/race-before.junit.xml) |
| `go test -race -tags=chaos -run TestTokenUseDebouncerRaceVsDBReinitChaosIssue47 ./tests/chaos/... -count=10 -timeout 10m` | green | п. 47 anchor стал GREEN, 10/10 | [`post-fix-47/race-after.txt`](post-fix-47/race-after.txt) | [`post-fix-47/race-after.junit.xml`](post-fix-47/race-after.junit.xml) |
| `go build ./...` | green | без регрессии | [`post-fix-47/build.txt`](post-fix-47/build.txt) | [`post-fix-47/build.junit.xml`](post-fix-47/build.junit.xml) |
| `go vet ./...` | green | без регрессии | [`post-fix-47/vet.txt`](post-fix-47/vet.txt) | [`post-fix-47/vet.junit.xml`](post-fix-47/vet.junit.xml) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-47/test.txt`](post-fix-47/test.txt) | [`post-fix-47/test.junit.xml`](post-fix-47/test.junit.xml) |
| `go test ./... -race -count=1 -timeout 15m` | green | п. 47 full-suite race закрыт | [`post-fix-47/test-race.txt`](post-fix-47/test-race.txt) | [`post-fix-47/test-race.junit.xml`](post-fix-47/test-race.junit.xml) |
| `go test -tags=chaos ./tests/chaos/... -count=1 -timeout 10m` | green | без регрессии | [`post-fix-47/test-chaos.txt`](post-fix-47/test-chaos.txt) | [`post-fix-47/test-chaos.junit.xml`](post-fix-47/test-chaos.junit.xml) |
| `staticcheck ./...` | red baseline | known red, без новых findings по fix-файлам | [`post-fix-47/staticcheck.txt`](post-fix-47/staticcheck.txt) | [`post-fix-47/staticcheck.junit.xml`](post-fix-47/staticcheck.junit.xml) |
| `golangci-lint run` | red baseline | known red, не хуже Phase 8 | [`post-fix-47/golangci-lint.txt`](post-fix-47/golangci-lint.txt) | [`post-fix-47/golangci-lint.junit.xml`](post-fix-47/golangci-lint.junit.xml) |
| `gosec ./...` | red baseline | 55 -> 55 findings | [`post-fix-47/gosec.txt`](post-fix-47/gosec.txt) | [`post-fix-47/gosec.junit.xml`](post-fix-47/gosec.junit.xml) |
| `govulncheck ./...` | green | `No vulnerabilities found.` сохранено | [`post-fix-47/govulncheck.txt`](post-fix-47/govulncheck.txt) | [`post-fix-47/govulncheck.junit.xml`](post-fix-47/govulncheck.junit.xml) |

### Дельта

- П. 47: deterministic chaos anchor перешёл из red в green; `race-after` прошёл 10/10.
- `go test ./... -race -count=1 -timeout 15m` теперь green на post-fix графе.
- `staticcheck`, `golangci-lint run`, `gosec ./...` остались known red baseline; `gosec` не ухудшился: 55 findings до и после.
- `govulncheck ./...` остался green: `No vulnerabilities found.`
- Новых побочных пунктов реестра не добавлено.

### Файлы post-fix-47

Созданы пары логов/JUnit для `race-before`, `race-after`, `build`, `vet`, `test`, `test-race`, `test-chaos`, `staticcheck`, `golangci-lint`, `gosec`, `govulncheck` в [`post-fix-47/`](post-fix-47/).

## Post-fix Cluster A 2026-05-24

Кластер A закрыл silent error suppression пункты 1, 5 и 18 тремя отдельными коммитами. Frontend не затрагивался; ручная модификация `frontend/src/layouts/modals/Endpoint.vue` не трогалась.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| anchor 1 before: `go test -run "TestApplyState_TLSReplaceDeleteError" ./database/importxui/... -count=1 -timeout 2m` | green, `[no tests to run]` | заявленный pre-existing anchor отсутствовал в рабочем дереве | [`post-fix-cluster-A/anchor-1-before.txt`](post-fix-cluster-A/anchor-1-before.txt) |
| anchor 5 before: `go test -run "TestXUISyncJobExtraFailureSummaryKeepsLastErr" ./cronjob/... -count=1 -timeout 2m` | XFAIL/skip source, command green без `-v` | baseline XFAIL сохранён как source state | [`post-fix-cluster-A/anchor-5-before.txt`](post-fix-cluster-A/anchor-5-before.txt) |
| anchor 18 before: `go test -run "TestLoadCacheEntryFailsClosedOnClientIPReadError" ./ipmonitor/... -count=1 -timeout 2m` | XFAIL/skip source, command green без `-v` | baseline XFAIL сохранён как source state | [`post-fix-cluster-A/anchor-18-before.txt`](post-fix-cluster-A/anchor-18-before.txt) |
| `go test ./database/importxui/... -count=1` | green | без регрессии | [`post-fix-cluster-A/test-importxui.txt`](post-fix-cluster-A/test-importxui.txt) |
| `go test -run TestApplyState_TLSReplaceDeleteError ./database/importxui/... -count=10` | green | anchor 1 стал GREEN, 10/10 | [`post-fix-cluster-A/anchor-1-after.txt`](post-fix-cluster-A/anchor-1-after.txt) |
| `go test ./cronjob/... -count=1` | green | без регрессии | [`post-fix-cluster-A/test-cronjob.txt`](post-fix-cluster-A/test-cronjob.txt) |
| `go test -run "FailureSummaryKeepsLastErr" ./cronjob/... -count=10` | green | anchor 5 стал GREEN, 10/10 | [`post-fix-cluster-A/anchor-5-after.txt`](post-fix-cluster-A/anchor-5-after.txt) |
| `go test ./ipmonitor/... -count=1` | green | без регрессии | [`post-fix-cluster-A/test-ipmonitor.txt`](post-fix-cluster-A/test-ipmonitor.txt) |
| `go test -run "FailsClosedOnClientIPReadError" ./ipmonitor/... -count=10` | green | anchor 18 стал GREEN, 10/10 | [`post-fix-cluster-A/anchor-18-after.txt`](post-fix-cluster-A/anchor-18-after.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-cluster-A/build.txt`](post-fix-cluster-A/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-cluster-A/vet.txt`](post-fix-cluster-A/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-cluster-A/test.txt`](post-fix-cluster-A/test.txt) |
| `go test ./... -race -count=1 -timeout 15m` | red | residual p. 47 family reproduced; заведён п. 48, не исправлялось в Cluster A | [`post-fix-cluster-A/test-race.txt`](post-fix-cluster-A/test-race.txt), [`post-fix-cluster-A/test-race-rerun-1.txt`](post-fix-cluster-A/test-race-rerun-1.txt) |
| `go test -tags=chaos ./tests/chaos/... -count=1 -timeout 10m` | green | без регрессии | [`post-fix-cluster-A/test-chaos.txt`](post-fix-cluster-A/test-chaos.txt) |
| `staticcheck ./...` | red baseline | не хуже: post-fix-47 37 строк -> Cluster A 31 строк; no new Cluster A findings | [`post-fix-cluster-A/staticcheck.txt`](post-fix-cluster-A/staticcheck.txt) |
| `golangci-lint run` | red baseline | не хуже: post-fix-47 228 строк -> Cluster A 222 строки; no new Cluster A findings | [`post-fix-cluster-A/golangci-lint.txt`](post-fix-cluster-A/golangci-lint.txt) |
| `gosec ./...` | red baseline | не хуже: 55 -> 55 findings | [`post-fix-cluster-A/gosec.txt`](post-fix-cluster-A/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` сохранено | [`post-fix-cluster-A/govulncheck.txt`](post-fix-cluster-A/govulncheck.txt) |

### Дельта

- П. 1: `applyTLS` теперь возвращает ошибку `tx.Delete` в TLS replace path; добавлен `TestApplyState_TLSReplaceDeleteError`.
- П. 5: `xui_sync_failed` audit details теперь содержит sanitized `errorClass`; issue 4 про real `lastErr` в `last_run_summary` оставлен XFAIL отдельным тестом.
- П. 18: `loadCacheEntry` fail-closed при ошибке чтения `client_ips`; `refreshClient` не кэширует неполную запись.
- Anchors 1, 5, 18 стали GREEN и прошли 10/10.
- Побочный/остаточный signal: full-suite `-race` снова воспроизвёл token-use debouncer vs DB reinit family из п. 47; заведён п. 48 в `docs/audit/plan.md`, код не правился.

### Файлы post-fix-cluster-A

`anchor-1-before.txt`, `anchor-5-before.txt`, `anchor-18-before.txt`, `test-importxui.txt`, `anchor-1-after.txt`, `test-cronjob.txt`, `anchor-5-after.txt`, `test-ipmonitor.txt`, `anchor-18-after.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `test-race-rerun-1.txt`, `test-chaos.txt`, `staticcheck.txt`, `golangci-lint.txt`, `gosec.txt`, `govulncheck.txt`, плюс контрольные `anchor-*-after-fix*.txt`.

## Post-fix п. 48 2026-05-24

Фикс выполнен точечно для API test lifecycle: перед прямыми `database.InitDB(...)` в API test helpers добавлен pre-flight `service.StopTokenUseDebouncer(context.Background())`, а сам `StopTokenUseDebouncer` теперь использует exclusive flush gate и не пишет во время уже активного reset quiet window. Frontend не затрагивался; ручная модификация `frontend/src/layouts/modals/Endpoint.vue` не трогалась.

### Команды

| Команда | Статус | Сравнение с baseline | Лог | JUnit |
|---|---:|---|---|---|
| before: `go test ./... -race -count=1 -timeout 15m` | red before | race reproduced: `flushTokenUseUpdates` vs `api.initSessionTestDB`/`database.InitDB` | [`post-fix-48/test-race-before.txt`](post-fix-48/test-race-before.txt) | [`post-fix-48/test-race-before.junit.xml`](post-fix-48/test-race-before.junit.xml) |
| targeted API race 10/10 | green | former red API scenarios stable | [`post-fix-48/test-race-targeted-after.txt`](post-fix-48/test-race-targeted-after.txt) | [`post-fix-48/test-race-targeted-after.junit.xml`](post-fix-48/test-race-targeted-after.junit.xml) |
| `go build ./...` | green | без регрессии | [`post-fix-48/build.txt`](post-fix-48/build.txt) | [`post-fix-48/build.junit.xml`](post-fix-48/build.junit.xml) |
| `go vet ./...` | green | без регрессии | [`post-fix-48/vet.txt`](post-fix-48/vet.txt) | [`post-fix-48/vet.junit.xml`](post-fix-48/vet.junit.xml) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-48/test.txt`](post-fix-48/test.txt) | [`post-fix-48/test.junit.xml`](post-fix-48/test.junit.xml) |
| `go test ./... -race -count=1 -timeout 15m` | green | п. 48 full-suite race закрыт | [`post-fix-48/test-race.txt`](post-fix-48/test-race.txt) | [`post-fix-48/test-race.junit.xml`](post-fix-48/test-race.junit.xml) |
| race reruns 1..3 | green | full-suite race чисто 3/3 рерана | [`post-fix-48/test-race-rerun-1.txt`](post-fix-48/test-race-rerun-1.txt), [`test-race-rerun-2.txt`](post-fix-48/test-race-rerun-2.txt), [`test-race-rerun-3.txt`](post-fix-48/test-race-rerun-3.txt) | [`post-fix-48/test-race-rerun-1.junit.xml`](post-fix-48/test-race-rerun-1.junit.xml), [`test-race-rerun-2.junit.xml`](post-fix-48/test-race-rerun-2.junit.xml), [`test-race-rerun-3.junit.xml`](post-fix-48/test-race-rerun-3.junit.xml) |
| `go test -tags=chaos ./tests/chaos/... -count=1 -timeout 10m` | green | без регрессии | [`post-fix-48/test-chaos.txt`](post-fix-48/test-chaos.txt) | [`post-fix-48/test-chaos.junit.xml`](post-fix-48/test-chaos.junit.xml) |
| anchor 47: `go test -race -tags=chaos -run TestTokenUseDebouncerRaceVsDBReinitChaosIssue47 ./tests/chaos/... -count=10 -timeout 10m` | green | старый anchor остался GREEN, 10/10 | [`post-fix-48/anchor-47-after.txt`](post-fix-48/anchor-47-after.txt) | [`post-fix-48/anchor-47-after.junit.xml`](post-fix-48/anchor-47-after.junit.xml) |
| anchor 48: `go test -race -tags=chaos -run TestTokenUseDebouncerRaceVsAPITestLifecycleIssue48 ./tests/chaos/... -count=10 -timeout 10m` | green | новый API lifecycle anchor GREEN, 10/10 | [`post-fix-48/anchor-48-after.txt`](post-fix-48/anchor-48-after.txt) | [`post-fix-48/anchor-48-after.junit.xml`](post-fix-48/anchor-48-after.junit.xml) |
| `staticcheck ./...` | red baseline | known red, 31 findings + wrapper header; не хуже Cluster A | [`post-fix-48/staticcheck.txt`](post-fix-48/staticcheck.txt) | [`post-fix-48/staticcheck.junit.xml`](post-fix-48/staticcheck.junit.xml) |
| `golangci-lint run` | red baseline | known red, same 222-line finding set + wrapper header; не хуже Cluster A | [`post-fix-48/golangci-lint.txt`](post-fix-48/golangci-lint.txt) | [`post-fix-48/golangci-lint.junit.xml`](post-fix-48/golangci-lint.junit.xml) |
| `gosec ./...` | red baseline | 55 -> 55 issues | [`post-fix-48/gosec.txt`](post-fix-48/gosec.txt) | [`post-fix-48/gosec.junit.xml`](post-fix-48/gosec.junit.xml) |
| `govulncheck ./...` | green | `No vulnerabilities found.` сохранено | [`post-fix-48/govulncheck.txt`](post-fix-48/govulncheck.txt) | [`post-fix-48/govulncheck.junit.xml`](post-fix-48/govulncheck.junit.xml) |

### Дельта

- П. 48: API/perf/realtime test helpers больше не вызывают `database.InitDB(...)` без token-use pre-flight barrier.
- `StopTokenUseDebouncer` теперь ждёт уже стартовавший timer/manual flush через exclusive gate, force-flush'ит pending только если gate не находится в reset quiet window, и оставляет quiet window перед reinit.
- Новый chaos anchor `TestTokenUseDebouncerRaceVsAPITestLifecycleIssue48` прошёл 10/10 под `-race`.
- `go test ./... -race -count=1 -timeout 15m` green, плюс 3/3 full-suite race reruns green.
- `staticcheck`, `golangci-lint run`, `gosec ./...` остались known red baseline; `gosec` не ухудшился: 55 findings.
- `govulncheck ./...` остался green: `No vulnerabilities found.`

### Файлы post-fix-48

Созданы пары логов/JUnit для `test-race-before`, `test-race-targeted-after`, `build`, `vet`, `test`, `test-race`, `test-race-rerun-1`, `test-race-rerun-2`, `test-race-rerun-3`, `test-chaos`, `anchor-47-after`, `anchor-48-after`, `staticcheck`, `golangci-lint`, `gosec`, `govulncheck`. Промежуточный красный уточняющий лог старого anchor сохранён как `anchor-47-after-red-1.*`.

## Post-fix Cluster D 2026-05-24

### Коммиты

- `5b1a7ac` — fix(frontend/ws): healing reconnect from degraded fallback (registry #32)
- `58a3d3c` — fix(api/realtime): constant-time consumeWSToken match-and-delete (registry #33)

Cluster D закрыл WS hardening пункты 32 и 33 двумя отдельными коммитами. Ручная модификация `frontend/src/layouts/modals/Endpoint.vue` не трогалась.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| anchor 33 before: `go test -run "TestConsumeWSTokenTimingRegressionAnchor_XFAILIssue33" ./api/... -count=1 -timeout 2m` | green command | source XFAIL/skip зафиксирован | [`post-fix-cluster-D/anchor-33-before.txt`](post-fix-cluster-D/anchor-33-before.txt) |
| anchor 32 before: Vitest `ws.spec.ts` | green | current-behavior anchor до healing; literal `npm --prefix` path mismatch сохранён отдельно | [`post-fix-cluster-D/vitest-ws-before.txt`](post-fix-cluster-D/vitest-ws-before.txt), [`vitest-ws-before-filter-mismatch.txt`](post-fix-cluster-D/vitest-ws-before-filter-mismatch.txt) |
| anchor 32 before: Playwright chaos | green command, expected-fail scenario | XFAIL issue 32 подтверждён | [`post-fix-cluster-D/playwright-ws-reconnect-before.txt`](post-fix-cluster-D/playwright-ws-reconnect-before.txt) |
| anchor 32 after: Vitest `ws.spec.ts` | green | healing reconnect anchor GREEN, 7/7 | [`post-fix-cluster-D/vitest-ws-after.txt`](post-fix-cluster-D/vitest-ws-after.txt) |
| anchor 32 after: Playwright `ws-reconnect-chaos.spec.ts` | green | XFAIL снят, WS chaos scenario GREEN | [`post-fix-cluster-D/playwright-ws-reconnect-after.txt`](post-fix-cluster-D/playwright-ws-reconnect-after.txt) |
| `go test ./api/... -count=1` | green | без регрессии API lifecycle/capacity | [`post-fix-cluster-D/test-api.txt`](post-fix-cluster-D/test-api.txt) |
| anchor 33 after: `go test -run "TestConsumeWSTokenTimingRegressionAnchor" ./api/... -count=10 -timeout 5m` | green | timing anchor GREEN, 10/10 | [`post-fix-cluster-D/anchor-33-after.txt`](post-fix-cluster-D/anchor-33-after.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-cluster-D/build.txt`](post-fix-cluster-D/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-cluster-D/vet.txt`](post-fix-cluster-D/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-cluster-D/test.txt`](post-fix-cluster-D/test.txt) |
| `go test ./... -race -count=1 -timeout 15m` | green | п. 47/48 race fixes не регресснули | [`post-fix-cluster-D/test-race.txt`](post-fix-cluster-D/test-race.txt) |
| `go test -tags=chaos ./tests/chaos/... -count=1 -timeout 10m` | green | без регрессии | [`post-fix-cluster-D/test-chaos.txt`](post-fix-cluster-D/test-chaos.txt) |
| `staticcheck ./...` | red baseline | known red, no new Cluster D production findings | [`post-fix-cluster-D/staticcheck.txt`](post-fix-cluster-D/staticcheck.txt) |
| `golangci-lint run` | red baseline | known red, no new Cluster D production findings | [`post-fix-cluster-D/golangci-lint.txt`](post-fix-cluster-D/golangci-lint.txt) |
| `gosec ./...` | red baseline | не хуже: 55 -> 55 issues | [`post-fix-cluster-D/gosec.txt`](post-fix-cluster-D/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` сохранено | [`post-fix-cluster-D/govulncheck.txt`](post-fix-cluster-D/govulncheck.txt) |
| `npm --prefix frontend run test` | green | 15 files / 76 tests | [`post-fix-cluster-D/vitest-full.txt`](post-fix-cluster-D/vitest-full.txt) |
| `npx playwright test` | green command | 9 passed / 4 skipped; Cluster D chaos WS scenario GREEN; MigrateXui XFAIL остаются | [`post-fix-cluster-D/playwright-full.txt`](post-fix-cluster-D/playwright-full.txt) |

### Дельта

- П. 32: `WsRuntime.startFallback()` теперь на каждом `fallbackPollMs` tick делает `loadData()` и healing `connect()`, если нет активного socket или reconnect timer; fired reconnect timer очищается перед retry.
- П. 32 anchor: старый assertion "no auto-reconnect after fallback" заменён на `degraded -> reconnecting -> connected`, Playwright `ws-reconnect-chaos.spec.ts` снят с `test.fail` и проходит.
- П. 33: `consumeWSToken()` сортирует digest keys, проходит полный список без match-branch, выбирает key/expiry/user index constant-time select/copy и делает один unconditional `delete` после цикла.
- П. 33 anchor: `TestConsumeWSTokenTimingRegressionAnchor` включён вместо XFAIL и прошёл 10/10.
- `staticcheck`, `golangci-lint run`, `gosec ./...` остались known red baseline; `gosec` не ухудшился: 55 issues.
- `govulncheck ./...` остался green: `No vulnerabilities found.`
- Побочных новых пунктов реестра не добавлено. Примечание: legacy placeholder `frontend/tests/e2e/ws-reconnect.spec.ts` остаётся `test.fixme`, потому что этот файл не входил в разрешённый scope Cluster D; целевой chaos WS anchor зелёный.

### Файлы post-fix-cluster-D

`anchor-33-before.txt`, `vitest-ws-before-filter-mismatch.txt`, `vitest-ws-before.txt`, `playwright-ws-reconnect-before.txt`, `vitest-ws-after.txt`, `playwright-ws-reconnect-after-prefix-cwd.txt`, `playwright-ws-reconnect-after.txt`, `playwright-login-debug.txt`, `playwright-login-plus-ws-debug.txt`, `test-api.txt`, `anchor-33-after.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `test-chaos.txt`, `staticcheck.txt`, `golangci-lint.txt`, `gosec.txt`, `govulncheck.txt`, `vitest-full.txt`, `playwright-full.txt`, `playwright-results/`.

## Post-fix Cluster F 2026-05-24

### Коммиты

- `50631af` — fix(service/audit-writer): prioritize warn/security on overflow eviction (registry #16)
- `c9de793` — fix(service/secret-settings): suppress redundant secretbox fallback audit (registry #17)

Cluster F закрыл audit pipeline пункты 16 и 17 двумя отдельными production-коммитами. Ручная модификация `frontend/src/layouts/modals/Endpoint.vue` не трогалась; frontend не затрагивался фиксом — Vitest и Playwright пропущены как нерелевантные scope.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go build ./...` | green | без регрессии | [`post-fix-cluster-F/build.txt`](post-fix-cluster-F/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-cluster-F/vet.txt`](post-fix-cluster-F/vet.txt) |
| anchor п. 16: `go test -run "TestAuditWriterOverloadSeverityPriorityAnchorIssue16Phase5" ./service/... -count=10` | green | severity priority anchor стал GREEN, 10/10 | [`post-fix-cluster-F/anchor-16-after.txt`](post-fix-cluster-F/anchor-16-after.txt) |
| anchor п. 17: `go test -run "TestLegacySecretboxFallbackDoesNotAuditAfterFix" ./service/... -count=10` | green | XFAIL снят, anchor GREEN, 10/10 | [`post-fix-cluster-F/anchor-17-after.txt`](post-fix-cluster-F/anchor-17-after.txt) |
| `go test -race -timeout 900s ./...` | green | Cluster D + YELLOW-фикс не регресснули | [`post-fix-cluster-F/test-race.txt`](post-fix-cluster-F/test-race.txt) |
| `gosec ./...` | red baseline | 55 -> 55 issues | [`post-fix-cluster-F/gosec.txt`](post-fix-cluster-F/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` сохранено | [`post-fix-cluster-F/govulncheck.txt`](post-fix-cluster-F/govulncheck.txt) |

### Дельта

- П. 16: `auditWriter.push()` теперь вытесняет события с severity `info` раньше, чем `warn`/security; severity-priority anchor стал GREEN под post-fix инвариантом, где high-severity события удерживаются до предела `auditQueueCapacity`.
- П. 17: direct legacy decrypt path больше не пишет `settings_secretbox_key_fallback`; `TestLegacySecretboxFallbackDoesNotAuditAfterFix` снят с XFAIL и проходит 10/10.
- `gosec ./...` остался known red baseline; не ухудшился (55 -> 55).
- `govulncheck ./...` остался green: `No vulnerabilities found.`
- Побочных новых пунктов реестра не добавлено.
- Frontend не затрагивался: Vitest и Playwright пропущены как нерелевантные scope.

### Файлы post-fix-cluster-F

`pre-cluster-F-head.txt`, `pre-cluster-F-status.txt`, `post-cluster-F-status.txt`, `build.txt`, `vet.txt`, `anchor-16-after.txt`, `anchor-17-after.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Cluster B 2026-05-24

### Коммиты

- `aae3ed5` — fix(service/runtime): serialize core start cooldown access (registry #20)
- `3a14e46` — fix(service/telegram): single-flight telegram http client swap (registry #21)

Cluster B закрыл concurrency пункты 20 и 21 двумя отдельными production-коммитами. Ручная модификация `frontend/src/layouts/modals/Endpoint.vue` не трогалась; frontend не затрагивался фиксом — Vitest и Playwright пропущены как нерелевантные scope.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go build ./...` | green | без регрессии | [`post-fix-cluster-B/build.txt`](post-fix-cluster-B/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-cluster-B/vet.txt`](post-fix-cluster-B/vet.txt) |
| anchor п. 20: `go test -run "Issue20" ./service/... -count=10` | green | runtime cooldown race-anchor стал GREEN, 10/10 | [`post-fix-cluster-B/anchor-20-after.txt`](post-fix-cluster-B/anchor-20-after.txt) |
| anchor п. 21: `go test -run "Issue21" ./service/... -count=10` | green | telegram http client race-anchor стал GREEN, 10/10 | [`post-fix-cluster-B/anchor-21-after.txt`](post-fix-cluster-B/anchor-21-after.txt) |
| `go test -race -run "Issue20\|Issue21" ./service/... -count=10` | green | race anchors GREEN под `-race`, 10/10 | [`post-fix-cluster-B/test-race-anchors.txt`](post-fix-cluster-B/test-race-anchors.txt) |
| `go test -race -timeout 900s ./...` | green | Cluster F race-baseline сохранён | [`post-fix-cluster-B/test-race.txt`](post-fix-cluster-B/test-race.txt) |
| `gosec ./...` | red baseline | 55 -> 55 issues | [`post-fix-cluster-B/gosec.txt`](post-fix-cluster-B/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` сохранено | [`post-fix-cluster-B/govulncheck.txt`](post-fix-cluster-B/govulncheck.txt) |

### Дельта

- П. 20: `Runtime.startCooldownActive`/`markCoreStartFailed`/`markCoreStartSucceeded`/`coreStartCooldownDuration` синхронизированы через `r.mu`; новый race-anchor `Issue20` в `service/runtime_race_test.go` GREEN под `-race` 10/10.
- П. 21: `TelegramService.getTelegramHTTPClient` использует double-checked locking и single-flight создание клиента под `telegramHTTPClientMu`; новый race-anchor `Issue21` в `service/telegram_proxy_race_test.go` GREEN под `-race` 10/10.
- `sync.Once` для telegram default client осознанно не добавлялся: default client остаётся eager-initialized package-level значением, совместимым с `setTelegramHTTPClient`, а concurrency-риск закрыт повторной проверкой и созданием клиента под тем же mutex.
- `gosec ./...` остался known red baseline; не ухудшился (55 -> 55).
- `govulncheck ./...` остался green: `No vulnerabilities found.`
- Побочных новых пунктов реестра не добавлено.
- Frontend не затрагивался: Vitest и Playwright пропущены как нерелевантные scope.

### Файлы post-fix-cluster-B

`pre-cluster-B-head.txt`, `pre-cluster-B-status.txt`, `post-cluster-B-status.txt`, `build.txt`, `vet.txt`, `anchor-20-after.txt`, `anchor-21-after.txt`, `test-race.txt`, `test-race-anchors.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Cluster C 2026-05-24

### Коммиты

- `0464e17` — fix(cronjob/xui-sync): record sanitized lastErr in failed sync summary (registry #4)
- `c5d45bf` — fix(cronjob/xui-sync): exponential backoff between sync retries (registry #40)
- `d468daa` — fix(cronjob/xui-sync): treat success persist failure as warn-only (registry #41)

Cluster C закрыл cron observability пункты 4, 40 и 41 тремя отдельными production-коммитами в одном файле `cronjob/xuiSyncJob.go`. Ручная модификация `frontend/src/layouts/modals/Endpoint.vue` не трогалась; frontend не затрагивался фиксом — Vitest и Playwright пропущены как нерелевантные scope.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go build ./...` | green | без регрессии | [`post-fix-cluster-C/build.txt`](post-fix-cluster-C/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-cluster-C/vet.txt`](post-fix-cluster-C/vet.txt) |
| anchor п. 4: `go test -run "FailureSummary\|IncludesLastErrIssue4" ./cronjob/... -count=10` | green | failed-summary anchor’ы стали GREEN, 10/10 | [`post-fix-cluster-C/anchor-4-after.txt`](post-fix-cluster-C/anchor-4-after.txt) |
| anchor п. 40: `go test -run "Issue40" ./cronjob/... -count=10` | green | exponential backoff anchor стал GREEN, 10/10 | [`post-fix-cluster-C/anchor-40-after.txt`](post-fix-cluster-C/anchor-40-after.txt) |
| anchor п. 41: `go test -run "Issue41" ./cronjob/... -count=10` | green | success-persist-failure anchor GREEN, 10/10 | [`post-fix-cluster-C/anchor-41-after.txt`](post-fix-cluster-C/anchor-41-after.txt) |
| `go test ./cronjob/... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-cluster-C/test-cronjob.txt`](post-fix-cluster-C/test-cronjob.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-cluster-C/test.txt`](post-fix-cluster-C/test.txt) |
| `go test -race -timeout 900s ./...` | green | Cluster B race-baseline сохранён | [`post-fix-cluster-C/test-race.txt`](post-fix-cluster-C/test-race.txt) |
| `gosec ./...` | red baseline | 55 -> 55 issues | [`post-fix-cluster-C/gosec.txt`](post-fix-cluster-C/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` сохранено | [`post-fix-cluster-C/govulncheck.txt`](post-fix-cluster-C/govulncheck.txt) |

### Дельта

- П. 4: `RunProfile` теперь записывает в `last_run_summary` sanitized `lastErr.Error()` (через `redact.String`) и `errorClass` (через `classifyXUISyncError`); generic `{"error":"failed"}` больше не пишется. XFAIL-anchor `TestXUISyncJobExtraFailureSummaryIncludesLastErr_XFAILIssue4` снят и переименован в `TestXUISyncJobExtraFailureSummaryIncludesLastErrIssue4`; ригидный anchor `TestXUISyncJobExtraRunProfileFailureRecordsFailedSummary` обновлён под post-fix контракт (sanitized error + source errorClass). Дополнительно коммит #4 впервые добавил в HEAD lost Phase 3 anchor `cronjob/integration_xui_sync_test.go` (был untracked) и переписал его ассерцию в под-тесте `failure` под тот же post-fix контракт.
- П. 40: между retry-попытками `RunProfile` теперь использует экспоненциальное расписание `200ms → 1s` вместо линейного `100ms → 200ms`. Anchor `TestXUISyncJobLostNetworkBackoffAnchorIssue40Phase5` обновлён на двухграничный диапазон `>=1100ms && <=2000ms`; benchmark `BenchmarkXUISyncJobLostNetworkBackoff` теперь докладывает `expected_backoff_ms=1200`. Дополнительный anchor `TestXUISyncJobExponentialBackoffScheduleIssue40` закрепляет тот же total-backoff контракт без production hook’ов.
- П. 41: success-ветка `RunProfile` больше не пробрасывает наружу ошибку `recordRun` — она логируется как warn, а `RunProfile` возвращает `nil`. Новый anchor `TestXUISyncJobSuccessReturnsNilOnPersistFailureIssue41` GREEN 10/10.
- `go test -race -timeout 900s ./...` остался green; Cluster B race-anchors не регрессировали.
- `gosec ./...` остался known red baseline; не ухудшился (55 -> 55).
- `govulncheck ./...` остался green: `No vulnerabilities found.`
- Побочных новых пунктов реестра не добавлено.
- Frontend не затрагивался: Vitest и Playwright пропущены как нерелевантные scope.

### Файлы post-fix-cluster-C

`pre-cluster-C-head.txt`, `pre-cluster-C-status.txt`, `post-cluster-C-status.txt`, `build.txt`, `vet.txt`, `anchor-4-after.txt`, `anchor-40-after.txt`, `anchor-41-after.txt`, `test-cronjob.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Cluster G 2026-05-25

### Коммиты

- `1976a4f` — fix(database/backup): make sighup timeout configurable via env (registry #10)
- `8c7cd3b` — fix(database/backup): fallback to full WAL checkpoint on truncate failure (registry #11)
- `223c082` — fix(database/backup): warn on missing settings.config in versioned backup (registry #12)

Cluster G закрыл backup safety пункты 10, 11, 12 тремя production-коммитами в одном файле `database/backup.go` (плюс новые/обновлённые anchor-файлы). Frontend и зависимости не затрагивались. SIGHUP timeout вынесен в ENV-config (`SUI_SIGHUP_TIMEOUT_SECONDS`), без settings UI / API contract / migration — полная contract-fix отложена до Кластера E.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go build ./...` | green | без регрессии | [`post-fix-cluster-G/build.txt`](post-fix-cluster-G/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-cluster-G/vet.txt`](post-fix-cluster-G/vet.txt) |
| anchor #10: `go test -run Issue10 ./database/... -count=10` | green | sighup timeout anchor GREEN, 10/10 | [`post-fix-cluster-G/anchor-10-after.txt`](post-fix-cluster-G/anchor-10-after.txt) |
| anchor #11: `go test -run Issue11 ./database/... -count=10` | green | WAL fallback anchor GREEN, 10/10 | [`post-fix-cluster-G/anchor-11-after.txt`](post-fix-cluster-G/anchor-11-after.txt) |
| anchor #12: `go test -run "Issue12\|TestImportDBAcceptsVersionedBackupWithoutConfig" ./database/... -count=10` | green | versioned config soft-validate anchor GREEN, 10/10 | [`post-fix-cluster-G/anchor-12-after.txt`](post-fix-cluster-G/anchor-12-after.txt) |
| `go test ./database/... -count=1` | green | без регрессии | [`post-fix-cluster-G/test-database.txt`](post-fix-cluster-G/test-database.txt) |
| `go test ./... -count=1` | green | без регрессии | [`post-fix-cluster-G/test.txt`](post-fix-cluster-G/test.txt) |
| `go test -race ./... -timeout 900s` | green | Cluster C race-baseline сохранён | [`post-fix-cluster-G/test-race.txt`](post-fix-cluster-G/test-race.txt) |
| `gosec ./...` | red baseline | 55 -> 55 issues | [`post-fix-cluster-G/gosec.txt`](post-fix-cluster-G/gosec.txt) |
| `govulncheck ./...` | green | No vulnerabilities found. | [`post-fix-cluster-G/govulncheck.txt`](post-fix-cluster-G/govulncheck.txt) |

### Дельта

- П. 10: SIGHUP timeout теперь читается из `SUI_SIGHUP_TIMEOUT_SECONDS` (диапазон 1–60 секунд), при отсутствии или невалидном значении fallback на 3s. Helper `resolvedSighupTimeout()` через `sync.Once` для idempotency. Test-only override `SetSighupTimeoutForTest` рядом с уже существующим `SetSendSighupHook`. Settings UI / migration / API contract не трогались — полная contract-fix отложена до Кластера E.
- П. 11: `GetDb` WAL checkpoint теперь использует `walCheckpointWithFallback`: TRUNCATE → FULL → log warning + continue. Бэкап без checkpoint всё равно валиден.
- П. 12: `validateVersionedBackupConfig` при отсутствии `settings.config` теперь логирует warning и возвращает `nil` вместо отказа. Pre-fix anchor `TestImportDBRejectsVersionedBackupWithoutConfig` переписан в `TestImportDBAcceptsVersionedBackupWithoutConfigIssue12` под post-fix контракт. Версионные старые бэкапы (до появления `settings.config`) теперь восстановимы.
- `gosec` остался known red baseline; не ухудшился.
- `govulncheck` остался green.
- Frontend не затрагивался.

### Файлы post-fix-cluster-G

`pre-cluster-G-head.txt`, `pre-cluster-G-status.txt`, `post-cluster-G-status.txt`, `status-diff.txt`, `build.txt`, `vet.txt`, `anchor-10-after.txt`, `anchor-11-after.txt`, `anchor-12-after.txt`, `test-database.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Cluster I 2026-05-25

### Коммиты

- `c782e4b324b85e0848d5c2607473dbeba4c5e4f2` — fix(service/telegram): cancel notifier backoff on stop (registry #22)
- `6610062fa9eb5af9eac38140c191fcd7b42fea89` — fix(service/telegram-backup): harden secret zeroization paths (registry #25)
- `655d7017b92f5119411e17661af1dd897794433f` — fix(service/warp): centralize authorized client headers (registry #31)

Cluster I закрыл Telegram/WARP robustness пункты 22, 25 и 31 тремя отдельными production-коммитами. Frontend, `go.mod`, `go.sum` и frontend package manifests не затрагивались.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go build ./...` | green | без регрессии | [`post-fix-cluster-I/build.txt`](post-fix-cluster-I/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-cluster-I/vet.txt`](post-fix-cluster-I/vet.txt) |
| anchor #22: `go test -run Issue22 ./service/... -count=10 -timeout 5m` | green | stop-aware notifier backoff anchor GREEN, 10/10 | [`post-fix-cluster-I/anchor-22-after.txt`](post-fix-cluster-I/anchor-22-after.txt) |
| anchor #25: `go test -run Issue25 ./service/... -count=10 -timeout 5m` | green | secret-bag/oversize anchors GREEN, 10/10 | [`post-fix-cluster-I/anchor-25-after.txt`](post-fix-cluster-I/anchor-25-after.txt) |
| anchor #31: `go test -run Issue31 ./service/... -count=10 -timeout 5m` | green | WARP header capture anchors GREEN, 10/10 | [`post-fix-cluster-I/anchor-31-after.txt`](post-fix-cluster-I/anchor-31-after.txt) |
| `go test ./service/... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-cluster-I/test-service.txt`](post-fix-cluster-I/test-service.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-cluster-I/test.txt`](post-fix-cluster-I/test.txt) |
| `go test -race ./... -timeout 900s` | green | Cluster G race-baseline сохранён | [`post-fix-cluster-I/test-race.txt`](post-fix-cluster-I/test-race.txt) |
| `gosec ./...` | red baseline | exactly 55 issues | [`post-fix-cluster-I/gosec.txt`](post-fix-cluster-I/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` | [`post-fix-cluster-I/govulncheck.txt`](post-fix-cluster-I/govulncheck.txt) |

### Дельта

- П. 22: `telegramNotifier.deliver()` больше не использует `telegramSleep` для retry backoff; `sleepBackoff` создаёт `time.NewTimer`, выбирает между timer и `stopCh`, корректно stop/drain'ит timer и немедленно завершает retry loop при `Stop`.
- П. 25: `TelegramBackupService.RunOnce` передаёт payload/passphrase во владение `telegramBackupSecretBag`; passphrase зануляется сразу после build envelope, payload — после появления envelope, а deferred bag zeroing закрывает error paths до audit.
- П. 31: `setWarpAuthorizedHeaders` централизует WARP client headers + Bearer auth для `getWarpInfo` и `SetWarpLicense`; preferred `api_version` de-dupes against fallback list.
- `gosec` остался known red baseline с тем же счётчиком 55 issues; `govulncheck` остался green.
- Frontend и зависимости не затрагивались.

### Файлы post-fix-cluster-I

`pre-cluster-I-head.txt`, `pre-cluster-I-status.txt`, `build.txt`, `vet.txt`, `anchor-22-after.txt`, `anchor-25-after.txt`, `anchor-31-after.txt`, `test-service.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`, `post-cluster-I-status.txt`, `status-diff.txt`.

## Post-fix Cluster H 2026-05-25

### Коммиты

- `baba54e80b56df8cb4b749734b6dd3206c77f5e2` — fix(frontend/migrate-xui): show apply failure inline (registry #43)
- `ebf2b39bc9e8dbd71909ec5f4ae98ec4eb018fa8` — fix(frontend/migrate-xui): wait for rollback health before reload (registry #44)
- `e733112e0634b0f723b7e5d8ec4fa02fa0e5f4f4` — fix(frontend/migrate-xui): require reveal for generated admins (registry #45)

Cluster H закрыл MigrateXui UX пункты 43, 44, 45. Diff ограничен `frontend/src/views/MigrateXui.vue`, `frontend/tests/e2e/migrate-xui-happy.spec.ts`, `frontend/src/locales/en.ts`, `frontend/src/locales/ru.ts`. Backend/API/schema/migrations, dependencies, `Endpoint.vue`, `go.mod`, `go.sum`, package manifests и `tests/chaos/**` не затрагивались.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `npm --prefix frontend run test -- --run` | green | 15 files / 76 tests | [`post-fix-cluster-H/vitest.txt`](post-fix-cluster-H/vitest.txt) |
| `node_modules\.bin\playwright.cmd test tests/e2e/migrate-xui-happy.spec.ts --grep 'Issue4[345]'` (cwd `frontend`) | green | Issue43/44/45 anchors GREEN, 3/3 | [`post-fix-cluster-H/playwright-migrate-xui.txt`](post-fix-cluster-H/playwright-migrate-xui.txt) |
| `node_modules\.bin\playwright.cmd test` (cwd `frontend`) | red exception | unrelated `ws-reconnect-chaos.spec.ts` singleton failed with `page.evaluate: Execution context was destroyed`; MigrateXui anchors passed | [`post-fix-cluster-H/playwright-full.txt`](post-fix-cluster-H/playwright-full.txt) |
| `node_modules\.bin\playwright.cmd test tests/e2e/ws-reconnect-chaos.spec.ts` (cwd `frontend`) | red singleton | reproduced same unrelated WS chaos failure for reviewer/orchestrator triage | [`post-fix-cluster-H/playwright-ws-reconnect-chaos-rerun.txt`](post-fix-cluster-H/playwright-ws-reconnect-chaos-rerun.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-cluster-H/build.txt`](post-fix-cluster-H/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-cluster-H/vet.txt`](post-fix-cluster-H/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-cluster-H/test.txt`](post-fix-cluster-H/test.txt) |
| `go test -race ./... -timeout 900s` | green | без регрессии | [`post-fix-cluster-H/test-race.txt`](post-fix-cluster-H/test-race.txt) |
| `gosec ./...` | red baseline | exactly 55 issues | [`post-fix-cluster-H/gosec.txt`](post-fix-cluster-H/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` | [`post-fix-cluster-H/govulncheck.txt`](post-fix-cluster-H/govulncheck.txt) |

### Дельта

- П. 43: `MigrateXui.applyPlan()` теперь сбрасывает stale progress, возвращает review step с inline `migrate-xui-apply-error`, показывает backend `msg` или fallback и сохраняет выбранный plan state. Anchor `Issue43` GREEN.
- П. 44: `rollback()` больше не делает fixed 1s sleep; после успешного restore он polling'ит raw `api.get('api/status', { params: { r: 'db' } })`, reload делает только после healthy DB response, а timeout/POST failure показывает inline rollback error. Anchor `Issue44` GREEN.
- П. 45: `generatedAdmins` больше не рендерится raw JSON до user reveal; пароли скрыты до explicit reveal, can be hidden/cleared, auto-cleared after timer, timer очищается on unmount/build-plan reset. Anchor `Issue45` GREEN.
- Full Playwright не заявлен green: failure ограничен unrelated tracked `frontend/tests/e2e/ws-reconnect-chaos.spec.ts`, отдельно воспроизведён singleton rerun; Cluster H production/test diff не затрагивает WS chaos e2e или helpers/config.
- П. 46 / `reset_required` остаётся открытым для Cluster E и не менялся.

### Файлы post-fix-cluster-H

`pre-cluster-H-head.txt`, `pre-cluster-H-status.txt`, `post-cluster-H-status.txt`, `status-diff.txt`, `vitest.txt`, `playwright-migrate-xui.txt`, `playwright-full.txt`, `playwright-ws-reconnect-chaos-rerun.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Singleton #23 2026-05-25

### Коммиты

- `5bcef1ac4658bfa27986e9aec27771030739507e` — fix(service/server): harden system info interface parsing (registry #23)

Singleton #23 закрыл crash-risk в `ServerService.GetSystemInfo()` при коротких/пустых `Flags`, коротких/пустых interface addresses и пустом результате `cpu.Info()`. #24 confidentiality behavior не менялся и остаётся open.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go test ./service -run Issue23 -count=10` | green | package-local Issue23 anchor GREEN 10/10 | [`post-fix-23/anchor-23-service.txt`](post-fix-23/anchor-23-service.txt) |
| `go test -tags=chaos ./tests/chaos/... -run GetSystemInfo -count=1 -timeout 5m` | green | current-host chaos smoke GREEN; `tests/chaos` untouched | [`post-fix-23/test-chaos-system-info.txt`](post-fix-23/test-chaos-system-info.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-23/build.txt`](post-fix-23/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-23/vet.txt`](post-fix-23/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-23/test.txt`](post-fix-23/test.txt) |
| `go test -race ./... -timeout 900s` | green | без регрессии | [`post-fix-23/test-race.txt`](post-fix-23/test-race.txt) |
| `gosec ./...` | red baseline | expected baseline exactly 55 issues; ANSI-tolerant count check used | [`post-fix-23/gosec.txt`](post-fix-23/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` | [`post-fix-23/govulncheck.txt`](post-fix-23/govulncheck.txt) |

### Дельта

- П. 23 «GetSystemInfo IPv6-only crash» — closed. `systemInfoInterfaces` даёт package-local DI anchor, interface flags проверяются по содержимому, а IPv6 link-local prefix проверяется через safe `strings.HasPrefix(strings.ToLower(...), "fe80::")`.
- Короткие/пустые addresses больше не panic'ят; пустой `cpu.Info()` не приводит к индексации `cpuInfo[0]`.
- П. 24 «GetSystemInfo confidentiality» — unchanged/open; private/link-local filtering сверх существующего поведения не добавлялся.
- Frontend, dependencies, API/schema, `Endpoint.vue`, `go.mod`, `go.sum`, frontend package manifests и `tests/chaos/**` не затрагивались.

### Файлы post-fix-23

`pre-fix-23-head.txt`, `pre-fix-23-status.txt`, `post-fix-23-status.txt`, `status-diff.txt`, `anchor-23-service.txt`, `test-chaos-system-info.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Singleton #24 2026-05-25

### Коммиты

- `c208edcec7c07f3807b6a1a6af09fc59287a32ee` — fix(service/server): filter non-public system info addresses (registry #24)

Singleton #24 закрыл confidentiality gap в `ServerService.GetSystemInfo()` одним production-коммитом в `service/server.go` и `service/server_system_info_test.go`. `sys.ipv4` и `sys.ipv6` сохранили shape `[]string`; теперь в эти ключи попадают только public routable interface addresses. Frontend, dependencies, API/schema, `Endpoint.vue`, `go.mod`, `go.sum`, frontend package manifests и `tests/chaos/**` не затрагивались.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go test ./service -run "Issue23|Issue24" -count=10` | green | package-local Issue23/Issue24 anchors GREEN 10/10 | [`post-fix-24/anchor-23-24-service.txt`](post-fix-24/anchor-23-24-service.txt) |
| `go test -tags=chaos ./tests/chaos/... -run GetSystemInfo -count=1 -timeout 5m` | green | current-host chaos smoke GREEN; `tests/chaos` untouched | [`post-fix-24/test-chaos-system-info.txt`](post-fix-24/test-chaos-system-info.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-24/build.txt`](post-fix-24/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-24/vet.txt`](post-fix-24/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-24/test.txt`](post-fix-24/test.txt) |
| `go test -race ./... -timeout 900s` | green | без регрессии | [`post-fix-24/test-race.txt`](post-fix-24/test-race.txt) |
| `gosec ./...` | red baseline | expected baseline exactly 55 issues; ANSI-tolerant count check used | [`post-fix-24/gosec.txt`](post-fix-24/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` | [`post-fix-24/govulncheck.txt`](post-fix-24/govulncheck.txt) |

### Дельта

- П. 24 «GetSystemInfo confidentiality» — closed. `GetSystemInfo` now parses interface addresses with `net/netip` and filters private, link-local, loopback, unspecified, multicast and invalid values before appending to `ipv4`/`ipv6`.
- П. 23 regression anchor remains GREEN 10/10; short/empty interface data still does not panic, and invalid `"x"` no longer leaks into `ipv6`.
- `gosec` remains known red baseline with exactly 55 issues by ANSI-tolerant count check.
- `govulncheck` remains green: `No vulnerabilities found.`
- No frontend/dependency/API shape changes were made.

### Файлы post-fix-24

`pre-fix-24-head.txt`, `pre-fix-24-status.txt`, `post-fix-24-status.txt`, `status-diff.txt`, `anchor-23-24-service.txt`, `test-chaos-system-info.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Singleton #36 2026-05-25

### Коммиты

- `2ee7d7de84bae65dc5dc90ad3b9c59183007dc14` — fix(api/import-xui): bound xui rate-limit cache (registry #36)

Singleton #36 закрыл DoS-риск в `enforceXUIRateLimit()` одним production-коммитом в `api/import_xui.go` и package-local anchor `api/import_xui_rate_limit_test.go`. Frontend, dependencies, DB schema/migrations, API response shape/status/body and token-scope behavior не затрагивались.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go test ./api -run "Issue36|ImportXUIReportsRateLimit" -count=10` | green | Issue36 bounded-cache/stale-prune anchors GREEN 10/10; existing quota anchor GREEN 10/10 | [`post-fix-36/anchor-36-api.txt`](post-fix-36/anchor-36-api.txt) |
| `go test -tags=chaos ./tests/chaos/... -run XUIRateLimit -count=1 -timeout 5m` | green | chaos smoke GREEN; `tests/chaos/**` untouched | [`post-fix-36/test-chaos-xui-rate-limit.txt`](post-fix-36/test-chaos-xui-rate-limit.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-36/build.txt`](post-fix-36/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-36/vet.txt`](post-fix-36/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-36/test.txt`](post-fix-36/test.txt) |
| `go test -race ./... -timeout 900s` | green | без регрессии | [`post-fix-36/test-race.txt`](post-fix-36/test-race.txt) |
| `gosec ./...` | red baseline | expected baseline exactly 55 issues; ANSI-tolerant count check used | [`post-fix-36/gosec.txt`](post-fix-36/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` | [`post-fix-36/govulncheck.txt`](post-fix-36/govulncheck.txt) |

### Дельта

- П. 36 «xui rate-limit map unbounded» — closed. `xuiRateMaxEntries` caps the package-level `xuiRates` cache; on first insert past the cap, `pruneXUIRateLimitLocked` removes expired buckets and evicts oldest remaining buckets until the map is below the cap.
- Existing per-actor behavior is preserved: same actor/IP still gets `xuiRequestMax` allowed requests per `xuiRequestWindow`, and existing 429 response behavior remains covered by `TestAPIImportXUIReportsRateLimitPhase5`.
- Package-local Issue36 anchors cover unique anonymous/IP pressure and stale bucket pruning without using DB-backed 429 audit paths.
- `tests/chaos/xui_rate_limit_unbounded_test.go` не менялся; deferred chaos/XFAIL remains a separate task.
- Frontend, dependencies, schema/migrations, `Endpoint.vue`, `go.mod`, `go.sum`, frontend package manifests and `tests/chaos/**` не затрагивались.

### Файлы post-fix-36

`pre-fix-36-head.txt`, `pre-fix-36-status.txt`, `post-fix-36-status.txt`, `status-diff.txt`, `anchor-36-api.txt`, `test-chaos-xui-rate-limit.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Singleton #19 2026-05-25

### Коммиты

- `14c4f8d3d3f81f43e0f7f2614fc5f00832798b63` — fix(service/settings): make default initialization idempotent (registry #19)

Singleton #19 закрыл startup race в `SettingService.GetAllSetting()` одним production-коммитом в `service/setting.go` и package-local anchor `service/setting_test.go`. Defaults now initialize through DB-level idempotent inserts inside a transaction before readback; existing values and the returned settings shape are preserved. Frontend, dependencies, DB schema/migrations, API contract, `Endpoint.vue`, `go.mod`, `go.sum`, frontend package manifests and `tests/chaos/**` не затрагивались.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go test ./service -run "Issue19|DefaultSetting|GetAllSetting" -count=10` | green | Issue19 concurrent default initialization anchor GREEN 10/10; default-setting anchors GREEN | [`post-fix-19/anchor-19-service.txt`](post-fix-19/anchor-19-service.txt) |
| `go test ./service -race -run "Issue19|DefaultSetting|GetAllSetting" -count=5` | green | race anchor GREEN 5/5 | [`post-fix-19/anchor-19-service-race.txt`](post-fix-19/anchor-19-service-race.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-19/build.txt`](post-fix-19/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-19/vet.txt`](post-fix-19/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-19/test.txt`](post-fix-19/test.txt) |
| `go test -race ./... -timeout 900s` | green | без регрессии | [`post-fix-19/test-race.txt`](post-fix-19/test-race.txt) |
| `gosec ./...` | red baseline | expected baseline exactly 55 issues; ANSI-tolerant count check used | [`post-fix-19/gosec.txt`](post-fix-19/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` | [`post-fix-19/govulncheck.txt`](post-fix-19/govulncheck.txt) |

### Дельта

- П. 19 «SettingService.GetAllSetting startup race» — closed. `GetAllSetting` now calls `ensureDefaultSettings` before final readback; defaults are inserted with one-statement `INSERT ... SELECT ... WHERE NOT EXISTS` operations inside a DB transaction, sorted for deterministic order.
- Package-local Issue19 anchor empties the file-backed SQLite settings table after `InitDB`, starts 16 concurrent callers behind a barrier, verifies no errors, asserts `len(defaultValueMap)` rows, and checks no duplicate keys.
- The returned settings contract is unchanged: `secret`, `installSalt`, `sessionGeneration`, `config`, and `version` remain omitted, while normal defaults such as `webPort` are still returned.
- No frontend/dependency/schema/API contract changes were made; blacklist paths were untouched.
- `gosec` remains known red baseline with exactly 55 issues by ANSI-tolerant count check; `govulncheck` remains green.

### Файлы post-fix-19

`pre-fix-19-head.txt`, `pre-fix-19-status.txt`, `post-fix-19-status.txt`, `status-diff.txt`, `anchor-19-service.txt`, `anchor-19-service-race.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Singleton #34 2026-05-25

### Коммиты

- `6c4ed85685f73d7cc3933541c879d71eea26a698` — fix(api/auth): enforce legacy token header sunset (registry #34)

Singleton #34 закрыл enforcement gap в legacy API token header одним production-коммитом в `api/apiV2Handler.go`, `api/security_token_test.go` и `api/api_v2_token_test.go`. Legacy `Token` остается accepted before `Sat, 15 Aug 2026 00:00:00 GMT` with `Deprecation`/`Sunset` headers, but is rejected with a targeted 401 at/after Sunset; `Authorization: Bearer` precedence remains accepted after Sunset.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go test ./api -run "Issue34|APIV2LegacyTokenHeader|APITokenFromRequest|SecurityAuthZAPIV2Invalid" -count=10` | green | Issue34 API anchors GREEN 10/10; generic invalid/expired token contract preserved outside expired legacy header case | [`post-fix-34/anchor-34-api.txt`](post-fix-34/anchor-34-api.txt) |
| `go test ./api -race -run "Issue34|APIV2LegacyTokenHeader|APITokenFromRequest" -count=5` | green | race anchor GREEN 5/5 | [`post-fix-34/anchor-34-api-race.txt`](post-fix-34/anchor-34-api-race.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-34/build.txt`](post-fix-34/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-34/vet.txt`](post-fix-34/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-34/test.txt`](post-fix-34/test.txt) |
| `go test -race ./... -timeout 900s` | green | без регрессии | [`post-fix-34/test-race.txt`](post-fix-34/test-race.txt) |
| `gosec ./...` | red baseline | expected baseline exactly 55 issues; ANSI-tolerant count check used | [`post-fix-34/gosec.txt`](post-fix-34/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` | [`post-fix-34/govulncheck.txt`](post-fix-34/govulncheck.txt) |

### Дельта

- П. 34 «apiTokenFromRequest legacy Token header» — closed. `apiTokenFromRequest` keeps Bearer precedence, marks expired legacy header use after Sunset, and `checkToken` returns the targeted legacy-expired 401 while keeping generic invalid-token behavior unchanged.
- Pre-Sunset legacy `Token` behavior is preserved: accepted valid legacy header still emits `Deprecation: true` and the published `Sunset` header.
- Bearer path remains canonical and accepted after legacy Sunset even when both headers are present.
- No frontend/dependency/schema changes were made; blacklist paths (`Endpoint.vue`, `go.mod`, `go.sum`, frontend package manifests, `tests/chaos/**`, frontend files and DB schema/model/migration files) were untouched.
- `gosec` remains known red baseline with exactly 55 issues by ANSI-tolerant count check; `govulncheck` remains green.

### Файлы post-fix-34

`pre-fix-34-head.txt`, `pre-fix-34-status.txt`, `post-fix-34-status.txt`, `status-diff.txt`, `anchor-34-api.txt`, `anchor-34-api-race.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Singleton #35 2026-05-25

### Коммиты

- `f97a993c18c6a39b044031be79e88c55494fe7d5` — fix(api/routes): share import-xui route registration (registry #35)

Singleton #35 закрыл route drift risk для import-xui endpoints одним production-коммитом в `api/apiHandler.go`, `api/apiV2Handler.go`, `api/import_xui_routes.go`, `api/apiHandler_routes_test.go` и `api/security_authz_test.go`. v1 `/api` and v2 `/apiv2` now register the same package-local route spec, including explicit `POST /apiv2/import-xui`; session/CSRF and token middleware surfaces remain distinct.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go test ./api -run "Issue35|ImportXUIRoutes|ImportXUIDuplicateRoute|APIHandlerRegistersLegacyActionRoutesExplicitly|SecurityAuthZScopeMatrixRows" -count=10` | green | Issue35 shared-registry anchor GREEN 10/10; legacy route list and authz matrix anchors GREEN | [`post-fix-35/anchor-35-api.txt`](post-fix-35/anchor-35-api.txt) |
| `go test ./api -race -run "Issue35|ImportXUIRoutes|SecurityAuthZScopeMatrixRows" -count=5` | green | race anchors GREEN 5/5 | [`post-fix-35/anchor-35-api-race.txt`](post-fix-35/anchor-35-api-race.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-35/build.txt`](post-fix-35/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-35/vet.txt`](post-fix-35/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-35/test.txt`](post-fix-35/test.txt) |
| `go test -race ./... -timeout 900s` | green | без регрессии | [`post-fix-35/test-race.txt`](post-fix-35/test-race.txt) |
| `gosec ./...` | red baseline | expected baseline exactly 55 issues; ANSI-tolerant count check used | [`post-fix-35/gosec.txt`](post-fix-35/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` | [`post-fix-35/govulncheck.txt`](post-fix-35/govulncheck.txt) |

### Дельта

- П. 35 «duplicate import-xui route registration» — closed. `importXUIRouteSpecs` is the single source for all twelve import-xui routes and is used by both `APIHandler.registerGroupedRoutes()` and `APIv2Handler.initRouter()`.
- `POST /apiv2/import-xui` is explicitly registered from the shared registry; the generic `postHandler` no longer owns the `import-xui` case.
- Existing v1/v2 auth-surface distinction remains covered: unauthenticated `/api/import-xui/plan` still hits the session surface, while unauthenticated `/apiv2/import-xui/plan` keeps the current HTTP 200 `invalid token` contract.
- No frontend/dependency/schema changes were made; blacklist paths (`Endpoint.vue`, `go.mod`, `go.sum`, frontend package manifests, `tests/chaos/**`, frontend files and DB schema/model/migration files) were untouched.
- `gosec` remains known red baseline with exactly 55 issues by ANSI-tolerant count check; `govulncheck` remains green.

### Файлы post-fix-35

`pre-fix-35-head.txt`, `pre-fix-35-status.txt`, `post-fix-35-status.txt`, `status-diff.txt`, `anchor-35-api.txt`, `anchor-35-api-race.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Singleton #30 2026-05-25

### Коммиты

- `e9711e5e9a1b630e95f7dfb5a44f47f03c40233e` — fix(service/settings): reject unsafe optional URLs (registry #30)
- `1720f6c6253ad6aa5f7203dfcf49123254b38759` — fix(service/settings): reject edge control chars in optional URLs (registry #30)

Singleton #30 закрыл validation gap in `validateOptionalHTTPURL()` в `service/setting.go` и package-local anchor `service/setting_extra_test.go`. Continuation `1720f6c6253ad6aa5f7203dfcf49123254b38759` closes the reviewer edge case by checking the raw input for control characters before `strings.TrimSpace`, so leading newlines and trailing CRLF/tab bytes cannot pass validation while the original raw setting value is still stored.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go test ./service -run "Issue30|ValidateOptionalHTTPURL|SubscriptionSettingsDefaultsAndValidation" -count=10` | green | Issue30 helper anchor GREEN 10/10, including leading newline and trailing CRLF/tab rejects; existing optional URL/default validation remains covered | [`post-fix-30/anchor-30-service.txt`](post-fix-30/anchor-30-service.txt) |
| `go test ./service -race -run "Issue30|ValidateOptionalHTTPURL" -count=5` | green | race anchor GREEN 5/5, including continuation edge controls | [`post-fix-30/anchor-30-service-race.txt`](post-fix-30/anchor-30-service-race.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-30/build.txt`](post-fix-30/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-30/vet.txt`](post-fix-30/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-30/test.txt`](post-fix-30/test.txt) |
| `go test -race ./... -timeout 900s` | green | без регрессии | [`post-fix-30/test-race.txt`](post-fix-30/test-race.txt) |
| `gosec ./...` | red baseline | expected baseline exactly 55 issues; ANSI-tolerant count check used | [`post-fix-30/gosec.txt`](post-fix-30/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` | [`post-fix-30/govulncheck.txt`](post-fix-30/govulncheck.txt) |

### Дельта

- П. 30 «validateOptionalHTTPURL fragments/control chars» — closed. `validateOptionalHTTPURL` now rejects URL fragments, raw control characters before trimming, decoded path controls, raw query controls, and decoded query controls.
- Continuation anchors cover leading raw newline and trailing raw CRLF/tab values, matching the Save-path risk where the original untrimmed value may be persisted.
- Query URLs remain accepted: `https://example.com/profile?from=sub` is covered by the Issue30 anchor.
- Existing `http`/`https` scheme restriction, required host behavior, non-http rejection and userinfo rejection remain covered.
- No frontend/dependency/schema/tests/chaos changes were made by the continuation; blacklist paths (`Endpoint.vue`, `go.mod`, `go.sum`, frontend package manifests, `tests/chaos/**`, frontend files and DB schema/model/migration files) were not staged.
- `gosec` remains known red baseline with exactly 55 issues by ANSI-tolerant count check; `govulncheck` remains green.

### Файлы post-fix-30

`pre-fix-30-head.txt`, `pre-fix-30-status.txt`, `post-fix-30-status.txt`, `status-diff.txt`, `anchor-30-service.txt`, `anchor-30-service-race.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.

## Post-fix Singleton #29 2026-05-25

### Коммиты

- `4067f4989156cc0ac402e6f0225b306832d48bd8` — fix(service/update): use etag for release checks (registry #29)

Singleton #29 закрыл update-check cache gap в `service/update.go` и package-local anchor `service/update_test.go`. Version checks now store the GitHub release ETag in memory, send `If-None-Match` after hourly cache expiry, and preserve cached release metadata when GitHub returns `304 Not Modified`.

### Команды

| Команда | Статус | Сравнение с baseline | Лог |
|---|---:|---|---|
| `go test ./service -run "Issue29|VersionInfo|VersionIsNewer" -count=10` | green | Issue29 ETag anchor GREEN 10/10; existing VersionInfo and version comparison anchors remain covered | [`post-fix-29/anchor-29-service.txt`](post-fix-29/anchor-29-service.txt) |
| `go test ./service -race -run "Issue29|VersionInfo" -count=5` | green | race anchor GREEN 5/5 | [`post-fix-29/anchor-29-service-race.txt`](post-fix-29/anchor-29-service-race.txt) |
| `go build ./...` | green | без регрессии | [`post-fix-29/build.txt`](post-fix-29/build.txt) |
| `go vet ./...` | green | без регрессии | [`post-fix-29/vet.txt`](post-fix-29/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | без регрессии | [`post-fix-29/test.txt`](post-fix-29/test.txt) |
| `go test -race ./... -timeout 900s` | green | без регрессии | [`post-fix-29/test-race.txt`](post-fix-29/test-race.txt) |
| `gosec ./...` | red baseline | expected baseline exactly 55 issues; ANSI-tolerant count check used | [`post-fix-29/gosec.txt`](post-fix-29/gosec.txt) |
| `govulncheck ./...` | green | `No vulnerabilities found.` | [`post-fix-29/govulncheck.txt`](post-fix-29/govulncheck.txt) |

### Дельта

- П. 29 «fetchLatestRelease ETag» — closed. `latestReleaseCached` keeps an in-memory ETag, passes it to `fetchLatestRelease`, and refreshes `checkedAt` on both 200 and 304 responses.
- Issue29 anchor verifies first fetch sends no validator, stores `ETag: "release-v1"`, sends `If-None-Match` after hourly cache expiry, and keeps the prior latest/releaseURL on 304 without a third request while the cache is fresh.
- Existing fail-soft behavior remains covered: network/server/decode failures do not break callers and still cache the failure interval.
- No frontend/dependency/schema changes were made; blacklist paths (`Endpoint.vue`, `go.mod`, `go.sum`, frontend package manifests, `tests/chaos/**`, frontend files and DB schema/model/migration files) were untouched.
- `gosec` remains known red baseline with exactly 55 issues by ANSI-tolerant count check; `govulncheck` remains green.

### Файлы post-fix-29

`pre-fix-29-head.txt`, `pre-fix-29-status.txt`, `post-fix-29-status.txt`, `status-diff.txt`, `anchor-29-service.txt`, `anchor-29-service-race.txt`, `build.txt`, `vet.txt`, `test.txt`, `test-race.txt`, `gosec.txt`, `govulncheck.txt`.
