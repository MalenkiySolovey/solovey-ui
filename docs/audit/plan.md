# s-ui-x — план аудита, тестирования и фиксации проблем

Документ зафиксирован 2026‑05‑23. Источник — полный аудит ветки на момент чтения. Правки в продукционный код пока не вносились.

Условные обозначения severity:
- P1 — критическая (целостность данных, безопасность, потеря данных, скрытые отказы).
- P2 — заметный риск (наблюдаемость, корректность контракта, стабильность).
- P3 — низкий (точность отчётов, мелкие дефекты UX/тестов).

Все ссылки на код кликабельны и ведут в нужную строку.

---

## 1. Реестр найденных проблем

Каждая запись: severity / категория / точная ссылка на код / impact / repro / fix.

### 1.1. Импорт 3x-ui и связанная логика

1. **P1 / Data integrity** — игнор ошибки удаления TLS в replace‑ветке: [`applyState.applyTLS()`](../../database/importxui/plan.go:477) на строке [`_ = tx.Delete(&existing).Error`](../../database/importxui/plan.go:516). Если delete упадёт (FK / DB ошибка), последующий `Create` может выполниться поверх несогласованного состояния.
   - Repro: эмулировать ошибку удаления TLS с действием `replace`.
   - Fix: проверять и возвращать ошибку до Create, прерывать транзакцию.
   - Status 2026-05-24: closed by Cluster A; `Apply` now returns the delete error and transaction rollback path is covered by `TestApplyState_TLSReplaceDeleteError`.

2. **P1 / Security+Contract** — режим [`AdminModeResetRequired`](../../database/importxui/options.go:89) объявлен и выбирается в UI [`adminModeItems`](../../frontend/src/views/MigrateXui.vue:355), но в [`applyState.applyAdmins()`](../../database/importxui/plan.go:663) и [`upsertUser()`](../../database/importxui/plan.go:922) нет отдельной ветки: `new_password` и `reset_required` всегда генерируют новый пароль и кладут его в [`Report.GeneratedAdmins`](../../database/importxui/report.go:8).
   - Repro: построить план с `reset_required`, выполнить apply — пароль придёт в отчёте.
   - Fix: либо ввести семантику (поле `force_password_reset` на `User`), либо удалить `reset_required` из контракта.

3. **P2 / Functional correctness** — игнорируется поле [`XUISyncProfile.OnlyNew`](../../database/model/model.go:118) в cron‑синке: в [`xuiSyncJob.runProfileOnce()`](../../cronjob/xuiSyncJob.go:90) жёстко прошиты `OnlyNew: true` для [`Plan(...)`](../../cronjob/xuiSyncJob.go:110) и [`Apply(...)`](../../cronjob/xuiSyncJob.go:118).
   - Fix: пробросить `profile.OnlyNew` в обе опции.

4. **P2 / Observability** — обобщённый summary при ошибке sync: [`recordRun(profile, "failed", map[string]any{"error":"failed"})`](../../cronjob/xuiSyncJob.go:85) теряет реальный `lastErr`.
   - Fix: положить sanitized `lastErr.Error()` и класс ошибки.
   - Status 2026-05-24: closed by Cluster C; failed sync summary now records sanitized lastErr and errorClass.

5. **P2 / Audit trail** — [`recordSyncAudit("xui_sync_failed", profile, nil)`](../../cronjob/xuiSyncJob.go:86) пишет audit без error details (`report=nil` → нет summary).
   - Fix: добавить error class в details.
   - Status 2026-05-24: closed by Cluster A; `xui_sync_failed` audit details now include sanitized `errorClass`.

6. **P3 / Reporting correctness** — wireguard endpoint skip учтён как inbound‑skip в [`applyState.applyInboundsEndpoints()`](../../database/importxui/plan.go:528) на строке [`s.report.Summary.Inbounds.Skipped++`](../../database/importxui/plan.go:541).
   - Fix: отдельный счётчик endpoint skipped.

7. **P3 / Profile config drift** — `IncludeSettings/IncludeHistory/IncludeRouting/AdminMode` нельзя задать из профиля в cron‑синке: cron собирает `PlanOptions` без них в [`xuiSyncJob.runProfileOnce()`](../../cronjob/xuiSyncJob.go:90), хотя UI и API их поддерживают.
   - Fix: расширить модель профиля и пробросить флаги в Plan/Apply.

8. **P3 / API/UI contract** — `adminMode` пробрасывается из [`MigrateXui.buildPlan()`](../../frontend/src/views/MigrateXui.vue:413) в [`ImportXuiPlan()`](../../api/import_xui.go:125), но в plan‑item этот режим не закодирован как исполнимое правило для apply (см. п. 2).

9. **P3 / Test coverage gap** — в [`plan_test.go`](../../database/importxui/plan_test.go:1) есть тест только на `new_password` ([`TestApply_ImportsSettingsAndNewPasswordAdmins()`](../../database/importxui/plan_test.go:137)); ветка `reset_required`, ошибка delete в TLS replace и sync‑fail запись не покрыты.

### 1.2. Слой данных, бэкап, миграции

10. **P2 / Reliability** — таймаут SIGHUP захардкожен в [`SendSighup()`](../../database/backup.go:438) на 3 секунды через [`time.AfterFunc(3*time.Second, ...)`](../../database/backup.go:449). На медленной машине процесс может не успеть завершить пишущую операцию.
    - Fix: вынести в конфиг и согласовать с graceful‑shutdown.
    - Status 2026-05-25: closed by Cluster G; SIGHUP timeout configurable via SUI_SIGHUP_TIMEOUT_SECONDS env var (1–60s, fallback 3s). Settings UI deferred to Кластер E.

11. **P2 / Backup robustness** — [`copyBackupTable()`](../../database/backup.go:158) копирует данные в одной транзакции; ошибка `WAL checkpoint` через [`PRAGMA wal_checkpoint(TRUNCATE)`](../../database/backup.go:107) фатальна и роняет весь бэкап.
    - Fix: retry / fallback на не‑truncate checkpoint, продолжать при не‑критичной ошибке checkpoint.
    - Status 2026-05-25: closed by Cluster G; WAL checkpoint TRUNCATE→FULL→warning fallback in GetDb.

12. **P2 / Backup**: [`validateVersionedBackupConfig()`](../../database/backup.go:381) считает «версионным» любой бэкап с непустым `version` и требует наличие `settings.config`.
    - Fix: сделать проверку конфигурируемой/мягче либо логировать предупреждение вместо отказа.
    - Status 2026-05-25: closed by Cluster G; missing settings.config in versioned backup logged as warning, restore continues.

13. **P3 / Schema** — [`type User`](../../database/model/model.go:18) не имеет поля `force_password_reset` или эквивалента; нельзя реализовать AdminModeResetRequired без доработки схемы (связано с п. 2).

14. **P3 / Migrations** — [`AdaptToCurrentVersion()`](../../database/adapt.go:28) вызывается всегда; ошибки логируются как warning в [`db.go`](../../database/db.go:144).
    - Fix: эскалировать ошибку adapt в начале запуска до явного отказа, если индекс/настройки критичны.

15. **P3 / DB pool** — [`OpenDB()`](../../database/db.go:53) фиксированно ставит `SetMaxOpenConns(8)`/`SetMaxIdleConns(4)`. Не настраивается.

### 1.3. Сервисный слой

16. **P1 / Audit integrity** — [`auditWriter.push()`](../../service/audit_writer.go:85) при переполнении сдвигает очередь и вытесняет самые старые события без приоритета severity (`warn`/security‑события уравнены с `info`).
    - Fix: приоритезация `warn`+ при вытеснении / отдельная очередь для security.
    - Status 2026-05-24: closed by Cluster F; overflow eviction now drops `info` before `warn`/security and the severity-priority anchor is green.

17. **P1 / Audit noise** — [`SettingService.recordSecretboxFallback()`](../../service/secret_settings.go:296) пишет audit при _успешной_ legacy‑расшифровке; первый «правильный» кандидат в [`getSecretboxCandidates()`](../../service/secret_settings.go:76) и так должен побеждать.
    - Fix: отдельный путь для legacy ключа без логирования каждого нормального decrypt.
    - Status 2026-05-24: closed by Cluster F; direct decrypt fallback no longer writes per-decrypt audit noise and the XFAIL anchor is green.

18. **P1 / DB enforcement** — [`ipmonitor.loadCacheEntry()`](../../ipmonitor/ipmonitor.go:331) на строке [`_ = db.Model(model.ClientIP{}).Select("ip, ip_hash").Where("client_name = ?", clientName).Find(&rows).Error`](../../ipmonitor/ipmonitor.go:350) глотает ошибку чтения. enforce‑mode пропустит «новый IP» при transient DB error.
    - Fix: возвращать `(allowCacheEntry{}, false)` и fail‑closed для enforce.
    - Status 2026-05-24: closed by Cluster A; `loadCacheEntry` now returns `ok=false` on `client_ips` read error and refresh does not cache an incomplete entry.

19. **P2 / Startup race** — [`SettingService.GetAllSetting()`](../../service/setting.go:119) при первом запуске вставляет дефолты без транзакции; параллельный старт двух процессов на одной БД может породить дубликаты до срабатывания UNIQUE.
    - Fix: транзакция + ON CONFLICT; либо явный single‑initializer.
    - Status 2026-05-25: closed by singleton #19; GetAllSetting now initializes defaults through DB-level idempotent inserts before reading settings, preventing duplicate rows during concurrent first-start calls.

20. **P2 / Concurrency** — поля [`Runtime.lastStartFailTime`](../../service/runtime.go:60) читаются/пишутся без синхронизации в [`startCooldownActive()`](../../service/runtime.go:215) и [`markCoreStartFailed()`](../../service/runtime.go:222).
    - Fix: защитить `r.mu` или хранить unixNano в `atomic.Int64`.
    - Status 2026-05-24: closed by Cluster B; core start cooldown reads/writes are serialized with `r.mu` and the Issue20 race anchor is green.

21. **P2 / Concurrency** — [`telegramHTTPClient`](../../service/telegram.go:51) обновляется под `Lock`, но проверка соответствия конфига идёт под `RLock`, затем повторно под `Lock` без double‑check.
    - Fix: атомарная замена клиента + `sync.Once` инициализация.
    - Status 2026-05-24: closed by Cluster B; `getTelegramHTTPClient` now double-checks under `telegramHTTPClientMu` and single-flights client creation.

22. **P2 / Telegram retry** — [`telegramNotifier.deliver()`](../../service/telegram.go:196) использует `telegramSleep(delay)` без `context`. Stop ждёт через [`done`](../../service/telegram.go:242), но текущая отправка всё равно проспит до конца.
    - Fix: `time.NewTimer` с select на `stopCh`.
    - Status 2026-05-25: closed by Cluster I; notifier retry backoff is stop-aware and `Stop` cancels pending retry sleeps.

23. **P2 / Crash risk** — в [`ServerService.GetSystemInfo()`](../../service/server.go:168) код полагается на `netInterfaces[i].Flags[0]`, `Flags[1]`, и `address.Addr[0:6]` без проверок длины.
    - Fix: безопасное сравнение по содержанию слайса; `len(address.Addr) >= 6`.
    - Status 2026-05-25: closed by singleton #23; GetSystemInfo now uses content-based interface flag checks and safe IPv6 prefix handling with package-local DI anchor.

24. **P2 / Confidentiality** — [`ServerService.GetSystemInfo()`](../../service/server.go:168) возвращает все IPv4/IPv6 интерфейсов без фильтрации, в том числе приватные/линк‑локальные.
    - Fix: фильтр или явный токеновый scope для системной инфы.
    - Status 2026-05-25: closed by singleton #24; GetSystemInfo now filters non-public/private/link-local interface addresses while preserving ipv4/ipv6 response shape.

25. **P2 / Backup leak risk** — [`telegram_backup.RunOnce()`](../../service/telegram_backup.go:46) после ошибки шифрования `defer zeroBytes(payload)` корректен, но при ошибке загрузки passphrase (строки 123‑127) логика хрупкая.
    - Fix: явный pattern «secret bag» вокруг payload.
    - Status 2026-05-25: closed by Cluster I; Telegram backup payload/passphrase zeroization uses explicit secret-bag ownership.

26. **P3 / Logging** — [`StatsService.SaveStats()`](../../service/stats.go:51) при `commitErr` шлёт `realtime.Publish` с предупреждением, но не пишет audit и не возвращает ошибку наружу.
    - Status 2026-05-25: closed by singleton #26; stats transaction commit failures now return the commit error, publish the existing warning event, and write a `stats_commit_failed` audit event without emitting normal stats realtime updates.

27. **P3 / Token migration** — [`UserService.migrateLegacyTokens()`](../../service/user.go:286) перезаписывает `enabled=true` для всех старых токенов независимо от исходного состояния.
    - Status 2026-05-25: closed by singleton #27; legacy API token migration preserves the stored `enabled` flag while still hashing plaintext tokens and normalizing scope/prefix metadata.

28. **P3 / Token use** — [`tokenUseDebouncer.flushTimer()`](../../service/token_use_debouncer.go:81) после ошибки записи продолжает по таймеру, без circuit‑breaker.
    - Status 2026-05-25: closed by singleton #28; token-use timer flush failures now requeue pending updates, open a backoff circuit, and retry after cooldown while manual flushes can still bypass the circuit.

29. **P3 / Update** — [`fetchLatestRelease()`](../../service/update.go:109) не использует `If‑None‑Match`. Ежечасный hit GitHub без кеша.
    - Status 2026-05-25: closed by singleton #29; version checks now cache GitHub ETags and send `If-None-Match` after hourly cache expiry, preserving cached release info on 304.

30. **P3 / Validation** — [`validateOptionalHTTPURL()`](../../service/setting.go:1015) запрещает `parsed.User`, но не запрещает `?fragment` или встраивание управляющих символов.
   - Status 2026-05-25: closed by singleton #30; optional HTTP URL validation now rejects fragments and control characters while preserving http/https query URLs and existing userinfo rejection.
   - Continuation 2026-05-25: reviewer edge case closed; raw input is checked for control characters before `strings.TrimSpace`, so leading newline and trailing CRLF/tab values are rejected before any raw Save value can be persisted.

31. **P3 / Endpoint warp** — порядок вызовов в [`WarpService.SetWarpLicense()`](../../service/warp.go:311) фрагильный (Authorization до setWarpHeaders).
    - Status 2026-05-25: closed by Cluster I; WARP authorized headers are centralized and covered by request-capture tests.

### 1.4. API / handlers / realtime

32. **P1 / WS reliability** — [`WsRuntime.startFallback()`](../../frontend/src/store/ws.ts:129) переводит UI в `degraded` и НИКОГДА сам не пробует повторное `connect()`.
    - Repro: сеть моргнула 3 раза подряд → realtime висит в polling до перезагрузки страницы.
    - Fix: периодический «healing» reconnect внутри fallback‑таймера.
    - Status 2026-05-24: closed by Cluster D; fallback polling now attempts a cooldown-bounded healing `connect()` and the WS chaos anchor is green.

33. **P1 / Realtime token** — [`consumeWSToken()`](../../api/realtime.go:404) делает constant‑time только частично; цикл проходит по всем токенам, но `delete` выполняется внутри вычисления, временная разница detectable.
    - Fix: завершать после уверенного match‑and‑delete, не переписывать matchedKey по последнему совпадению.
    - Status 2026-05-24: closed by Cluster D; token scan uses sorted full iteration, constant-time key/data selection, and one post-loop delete.

34. **P2 / Validation** — [`apiTokenFromRequest()`](../../api/apiV2Handler.go:204) принимает оба формата (Authorization Bearer и устаревший заголовок `Token`); enforcement отсутствует.
    - Fix: HARD‑disable legacy header после `Sunset` даты.
    - Status 2026-05-25: closed by singleton #34; legacy `Token` header remains accepted only before its published Sunset and is rejected with 401 after Sunset, while Bearer precedence and generic invalid-token behavior are preserved.

35. **P2 / Concurrency / route registration** — в [`api/apiV2Handler.initRouter()`](../../api/apiV2Handler.go:48) и [`api/apiHandler.registerGroupedRoutes()`](../../api/apiHandler.go:35) дублируются маршруты `/import-xui/*`. Контракт расходится тонко.
    - Fix: единый источник истины для `import-xui` маршрутов.
    - Status 2026-05-25: closed by singleton #35; `/api` and `/apiv2` import-xui routes now register from a shared route spec, including explicit `POST /apiv2/import-xui`, while preserving distinct session/CSRF vs token auth surfaces.

36. **P2 / DoS** — [`enforceXUIRateLimit()`](../../api/import_xui.go:266) держит rate‑state в `xuiRates` map; ключ под анонимом — IP, нет верхней границы на размер мапы.
    - Fix: bounded map (LRU).
    - Status 2026-05-25: closed by singleton #36; xui rate-limit cache now prunes expired buckets and evicts oldest entries to keep the in-memory map bounded while preserving per-actor quota behavior.

37. **P2 / API contract** — [`ImportXuiApply()`](../../api/import_xui.go:166) принимает план как `Fields["plan"]` (строка JSON в форме). Размер ограничен `maxXUIFieldBytes=8MiB`.
    - Fix: chunked endpoint или stream‑декодирование plan через body.
    - Status 2026-05-26: closed by singleton #37; import-xui apply streams the multipart `plan` part to temp storage so valid plans larger than 8MiB can be decoded under the aggregate request cap while other form fields keep the 8MiB limit.

38. **P3 / API** — [`saveXUIUpload()`](../../api/import_xui.go:289) пишет временный файл в `os.TempDir()`, не имеет фоновой чистки остатков.
    - Status 2026-05-25: closed by singleton #38; import-xui upload handling now opportunistically removes stale `xui-import-*` temp directories older than 24h while preserving active uploads and fail-soft request behavior.

39. **P3 / API** — [`ImportXuiRollback()`](../../api/import_xui.go:204) логирует только audit, не публикует realtime событие.
    - Status 2026-05-26: closed by singleton #39; successful import-xui rollback now publishes `config_invalidated` realtime after restoring the backup and recording rollback audit.

### 1.5. Cronjob и фон

40. **P2 / Cron policy** — [`xuiSyncJob.RunProfile()`](../../cronjob/xuiSyncJob.go:55) использует короткий backoff `attempt * 100ms`, до 3 попыток.
   - Fix: экспоненциальный backoff `200ms → 1s → 5s` или из конфига.
   - Status 2026-05-24: closed by Cluster C; sync retry backoff is exponential 200ms→1s via xuiSyncBackoff table.

41. **P3 / Cron audit** — [`recordRun(profile, "success", report)`](../../cronjob/xuiSyncJob.go:70) возвращает ошибку наружу, импорт уже выполнен; функция вернёт error, хотя данные на месте.
   - Fix: успех всегда возвращать `nil`, ошибку persist писать как warn.
   - Status 2026-05-24: closed by Cluster C; RunProfile returns nil on success even when recordRun persist fails.

### 1.6. Frontend

42. **P2 / WS** — см. п. 32.

43. **P2 / UX** — [`MigrateXui.applyPlan()`](../../frontend/src/views/MigrateXui.vue:437) при `!msg.success` возврат на step 2 без сообщения об ошибке.
    - Status 2026-05-25: closed by Cluster H; failed apply returns to review with inline error preserving selected plan state.

44. **P2 / Race** — [`MigrateXui.rollback()`](../../frontend/src/views/MigrateXui.vue:456) после успеха ждёт фиксированную 1 секунду через `setTimeout` и делает `location.reload()`.
    - Fix: ожидание health‑check эндпоинта.
    - Status 2026-05-25: closed by Cluster H; rollback waits for api/status?r=db health before reload instead of fixed sleep.

45. **P2 / Token leakage in logs** — `report.generatedAdmins` рендерится как `JSON.stringify(report.generatedAdmins, null, 2)` в [`MigrateXui.vue`](../../frontend/src/views/MigrateXui.vue:283).
    - Fix: «click to reveal» pattern, авто‑очистка по таймеру.
    - Status 2026-05-25: closed by Cluster H; generated admin passwords are hidden until reveal and auto-cleared.

46. **P3 / API contract** — `MigrateXui.adminModeItems` всегда показывает `reset_required`, даже когда backend этой семантикой не управляет.

47. **Closed / P2 / Concurrency / Phase 3 race finding** — full-suite race detector показал гонку между фоновым [`tokenUseDebouncer.flushTimer()`](../../service/token_use_debouncer.go:81) / batch flush через [`flushTokenUseBatch()`](../../service/token_use_debouncer.go:163) и переинициализацией тестовой БД через [`api.initSessionTestDB()`](../../api/session_test.go:28), вызванной из [`TestSaveSettingsRejectsProtectedKeyAndAudits()`](../../api/settings_save_test.go:111).
    - Impact: потенциальный partial write через `UPDATE tokens` во время drop/recreate/InitDB; в тестовом full-suite это проявляется как `no such table: tokens`, в production-паттерне риск похож на запись в устаревший/пересоздаваемый handle.
    - Repro: `go test ./... -race -count=1 -timeout 15m`.
    - Fix: синхронизировать debouncer с `InitDB` / reset DB lifecycle или выполнять явный `Flush`/`StopTokenUseDebouncer` перед reset.
    - Status 2026-05-24: закрыт reset hook + token-use flush gate; regression anchor `go test -race -tags=chaos -run TestTokenUseDebouncerRaceVsDBReinitChaosIssue47 ./tests/chaos/... -count=10` green.

48. **Closed / P2 / Concurrency / Post-fix Cluster A race signal** — full-suite `go test ./... -race -count=1 -timeout 15m` после Cluster A снова воспроизвёл семейство п. 47: `service.tokenUseDebouncer.flushTimer()` / `flushTokenUseBatch()` читает текущий GORM handle одновременно с `database.InitDB()` в API test lifecycle.
    - Repro: [`tests/baseline/post-fix-cluster-A/test-race.txt`](../../tests/baseline/post-fix-cluster-A/test-race.txt) падает в `TestIssueWSTokenExtraRateLimit`; rerun [`test-race-rerun-1.txt`](../../tests/baseline/post-fix-cluster-A/test-race-rerun-1.txt) падает в `TestRealtimeWSCapacityAnchorPhase5/max_per_ip`.
    - Scope: не связан с файлами Cluster A (`database/importxui/plan.go`, `cronjob/xuiSyncJob.go`, `ipmonitor/ipmonitor.go`); закрыт отдельным API test lifecycle hardening.
    - Fix: переоткрыть/расширить singleton п. 47 для perf/API realtime test lifecycle или синхронизировать `InitDB` с timer flush в этих сценариях.
    - Status 2026-05-24: закрыт вариантом 2; API test helpers вызывают `service.StopTokenUseDebouncer(context.Background())` перед `database.InitDB(...)`, а `StopTokenUseDebouncer` стал exclusive barrier, который ждёт in-flight timer flush и не пишет во время уже активного reset quiet window. Full-suite race green 4/4, anchor п. 48 green 10/10.

---

## 2. Системные паттерны

- Молчащие ошибки: `_ = ...` в критичных местах (п. 1, 5, 18).
- Контракт UI/Backend опережает реализацию: `reset_required` (п. 2), `OnlyNew` в профиле (п. 3).
- Наблюдаемость падений недостаточно детальна: cron‑sync, audit writer overflow, stats commit failure пишут только warn без структурированной диагностики.
- Гонки в `Runtime` полях времени и `telegramHTTPClient` (п. 20–21).
- Жёстко прошитые таймауты: 3s SIGHUP, 100ms backoff cron, 1s reload в UI.
- Тесты сосредоточены на счастливом пути.

---

## 3. Пробелы аудита (gaps)

Файлы, которые я НЕ читал в рамках этого прохода и которые требуется покрыть в следующем заходе для статуса «полноценная программа»:

### Тесты Go, не читанные
- `service/audit_test.go`, `service/inbounds_test.go`, `service/inbounds_vless_flow_test.go`, `service/observability_test.go`, `service/secret_settings_test.go`, `service/restart_manager_test.go`, `service/setting_test.go`, `service/setting_realtime_test.go`, `service/server_logs_test.go`, `service/stats_realtime_test.go`, `service/telegram_backup_envelope_test.go`, `service/telegram_backup_restore_test.go`, `service/telegram_backup_test.go`, `service/telegram_test.go`, `service/token_test.go`, `service/update_test.go`, `service/client_test.go`, `service/client_secret_test.go`, `service/config_changes_test.go`, `service/config_core_test.go`.
- Все `_test.go` в `api/` (csrf_test, session_test, ratelimit_test, realtime_test, observability_test, settings_save_test, sub_secret_test, telegram_*test).

### Cronjob, кроме [`xuiSyncJob.go`](../../cronjob/xuiSyncJob.go:1)
- `auditGCJob.go`, `checkCoreJob.go`, `cpuHysteresisJob.go`, `cronJob.go`, `delStatsJob.go`, `depleteJob.go`, `observabilitySamplerJob.go`, `statsJob.go`, `telegramBackupJob.go`, `telegramReportJob.go`, `WALCheckpointJob.go` и их тесты.

### Core, network, middleware, realtime, sub, util, logger
- `core/`: `main.go`, `log.go`, `endpoint.go`, `outbound_check.go`, `register.go` целиком (только частично смотрел `box.go`).
- `network/`: `auto_https_listener.go`, `auto_https_conn.go`, `listen.go`, `listen_test.go`, `listen_errno_*.go`.
- `middleware/`: `securityHeaders.go`, `domainValidator.go`.
- `realtime/`: `event.go`, `hub.go`, `hub_test.go`.
- `sub/` весь поддиректорий (subService, clashService, jsonService, linkService, rate_limit, sub.go).
- `util/`: `genLink.go`, `linkToJson.go`, `outJson.go`, `header_sanitize.go`, `path_validate.go`, `redact/redact.go`, `secretbox/secretbox.go`, `ssrf/*`.
- `logger/`.
- `web/`.
- `app/app.go` (только бегло).
- `cmd/` (вероятно migration/CLI).

### Frontend
- `frontend/src/components/*`, `frontend/src/router/index.ts`, `frontend/src/plugins/*`, остальные `views/*.vue`, `frontend/src/store/csrf.ts`, `frontend/src/plugins/httputil.ts`.

### Внешние проверки, которые НЕ запускались
- `go test ./... -race`, `go vet`, `staticcheck`, `golangci-lint`, `govulncheck`.
- `npm run typecheck`, `npm run lint`, `npm run build`.
- E2E на реальной панели.

---

## 4. План тестирования всей системы (baseline до фиксов)

Цель: получить воспроизводимую baseline‑картину «что зелёное, что красное», чтобы дельта от каждого фикса измерялась объективно.

### Фаза 0. Инфраструктура
- Локально: `go build ./...`, `go test ./...`, `go vet ./...`, `staticcheck`, `golangci-lint`.
- Frontend: `npm ci && npm run typecheck && npm run lint && npm run build`.
- Каталог `test-db/` с фикстурами 3x-ui/s-ui для тестов из [`importer_test.setupImportTestDB()`](../../database/importxui/importer_test.go:211).
- `Makefile` или `Taskfile.yml`: `audit:lint`, `audit:vet`, `audit:test-go`, `audit:test-fe`, `audit:e2e`.
- Каталог `tests/baseline/` для отчётов прогона (junit/xml/json) до правок.

### Фаза 1. Статический анализ Go
- Кросс‑сборка linux/windows/darwin.
- `govulncheck ./...`.
- `staticcheck` + `golangci-lint` (rule‑set: `errcheck`, `ineffassign`, `gosimple`, `nilness`, `unparam`, `bodyclose`, `noctx`, `gosec`).
- `gosec` baseline отдельно.

Особое внимание под отчёт:
- `errcheck` — пункты 1, 5, 18.
- `gosec G304/G306` — п. 38.
- `gosec G101` — жёстко прошитые ключи/префиксы.

### Фаза 2. Юнит‑тесты Go (per package)
- `go test ./... -count=1`, `-race`, `-coverprofile`.
- Минимум по покрытию ≥ 60% для `service/`, `api/`, `database/importxui/`, `ipmonitor/`, `realtime/`, `cronjob/`.
- Темы покрытия:
  - Auth/token: [`UserService.Login()`](../../service/user.go:81), [`UserService.AddToken()`](../../service/user.go:214), [`UserService.migrateLegacyTokens()`](../../service/user.go:286), [`apiV2Handler.findUsername()`](../../api/apiV2Handler.go:135), [`UserService.HashAPIToken()`](../../service/user.go:274).
  - Settings: [`SettingService.Save()`](../../service/setting.go:605), [`validateAll()`](../../service/setting.go:694), [`validateSubscriptionPathSettings()`](../../service/setting.go:908), [`validateTelegramSettingInput()`](../../service/setting.go:1033).
  - Secrets: [`encryptSettingValue()`](../../service/secret_settings.go:242)/[`decryptSettingValue()`](../../service/secret_settings.go:253), legacy fallback, ENV‑override.
  - Telegram backup envelope: [`BuildTelegramBackupEnvelope()`](../../service/telegram_backup_envelope.go:78), [`OpenTelegramBackupEnvelope()`](../../service/telegram_backup_envelope.go:82).
  - Telegram notifier: overflow, deliver retry, error classes.
  - Audit writer: переполнение, batched flush, Stop.
  - Restart manager: повторный вызов в полёте, отмена.
  - Stats: пустой stats, ошибка commit, ipmonitor flush.
  - Client/Inbound/Tls Save все ветки `act`.
  - Backup/ImportDB happy/негатив с rollback.
  - Importxui: Plan/Apply, ErrPlanStale, ErrBusy, wireguard, reality dedup.
  - Importxui sources: [file](../../database/importxui/source/file/file.go:1), [ssh host‑key](../../database/importxui/source/ssh/ssh.go:1), [xuihttp](../../database/importxui/source/xuihttp/xuihttp.go:1).
  - ipmonitor: hashing, fail‑closed (фикс п. 18).
  - Cronjob: все джобы, не только xuiSyncJob.
  - API rate‑limit + realtime token + ws origin.

### Фаза 3. Интеграционные тесты (in-process)
- Login → CSRF → Save → Restart core.
- WS lifecycle (ws‑token → connect → publish → close → reconnect).
- Session rotation → ws disconnect + token invalidation + audit `ws_tokens_invalidated`.
- Backup → Telegram envelope → restore.
- xui import full (plan + apply, разные strategy/adminMode).
- xui sync (профиль → cron → last_run).
- Stats pipeline.
- Sub‑secret rotate → realtime publish.

### Фаза 4. Безопасность
- AuthZ матрица по всем endpoint’ам v2.
- CSRF matrix v1.
- Login lockout / rate‑limit.
- SSRF: `telegramProxyURL`, `validateRollbackPath`.
- Token: legacy header sunset (п. 34), `consumeWSToken` constant‑time (п. 33).
- Cookie flags + session rotation.
- WS origin enforcement + audit.
- Backup confidentiality, secret zeroization.
- Тулинг: `gosec`, `nuclei`/`zap`.

### Фаза 5. Производительность и надёжность
- Backup при 1M `stats`/`client_ips`/`changes`, проверка [`SafeSQLiteBatchSize()`](../../database/bulk.go:31).
- Stats save throughput.
- Audit writer 10k/s 5s.
- Telegram queue overflow.
- xui import 200MB.
- WS reconnect 50× (фикс п. 32).
- Cron sync под потерянной сетью.
- ipmonitor под нагрузкой.
- Инструменты: `go test -bench`, `pprof`, `k6`/`vegeta`, `wrk2`.

### Фаза 6. Frontend
- `npm run typecheck`/`lint`/`build`.
- vitest для `frontend/src/store/ws.ts`, csrf, http util.
- E2E (Cypress/Playwright):
  - Login + CSRF.
  - `MigrateXui.vue` happy path.
  - Поведение `adminMode` (после фикса п. 2).
  - WS reconnect (после фикса п. 32).
  - Settings save с конфликтом subPath/subJsonPath/subClashPath.
  - Audit/observability страницы.
  - Token rotate UI обновляет линки.
- Accessibility `axe`.
- Проверка security‑headers smoke.

### Фаза 7. Системные / chaos
- Docker‑compose холодный старт, миграции, SIGHUP, upgrade.
- Kill во время xui import → консистентность DB и rollback.
- Kill во время backup → нет частичных файлов.
- SQLite locked → graceful retry.
- Telegram down → notifier не падает.
- Cloudflare WARP API down → retry в [`RegisterWarp()`](../../service/warp.go:152) и [`SetWarpLicense()`](../../service/warp.go:311).
- IPv6‑only хост → [`ServerService.GetSystemInfo()`](../../service/server.go:168) не падает (п. 23).

### Фаза 8. Регрессионная упаковка и CI
- Объединить всё в `.github/workflows/audit.yml`.
- Артефакты: junit XML, coverage.html, perf.md, chaos.md, gosec/govulncheck JSON.
- Critical gates: vet+staticcheck чисто после P1, `-race` зелёный, frontend build/typecheck зелёный, E2E happy path зелёный.

### Как мерить прогресс
После каждой пачки фиксов:
1. Перезапуск pipeline.
2. Сравнение с `tests/baseline/`: red→green, coverage, perf delta, race detector, новые audit‑события.
3. PR указывает, какой пункт отчёта закрыт и какие тесты добавлены.

### Матрица «проблема → тип теста»

| # | Где ловить | Какой тест |
|---|---|---|
| 1 | unit + fault‑injection | wrap GORM, заставить `Delete` вернуть error → ожидать ошибку Apply |
| 2 | unit + e2e | `reset_required` НЕ возвращает пароль; e2e в MigrateXui |
| 3 | unit cronjob | моки на Plan/Apply, проверить переданные options |
| 4 | unit cronjob | проверить `last_run_summary` JSON |
| 5 | unit cronjob | проверить `detail.errorClass` |
| 6 | unit importxui | new test wireguard skip → endpoints.skipped |
| 7 | unit cronjob | profile с разными флагами → опции совпадают |
| 8 | e2e + integration | согласованность плана и итогового действия |
| 9 | unit | новый тест |
| 10 | integration | mock `signalCurrentProcess` |
| 11 | integration | держать write‑lock, ожидать корректную обработку |
| 12 | unit backup | подделка settings без config |
| 13 | unit migration | при добавлении поля — миграция и обратное чтение |
| 14 | integration | падение adapt не должен молча пройти |
| 15 | bench | конкурентный read‑hammer без timeouts |
| 16 | unit + bench | под перегрузкой warn‑события не теряются |
| 17 | unit | при правильном candidate — нет audit |
| 18 | unit + integration | заставить query упасть, enforce не пропускает |
| 19 | integration | две go‑рутины параллельно стартуют сервис |
| 20 | `-race` integration | стресс‑тест coreStartFailed |
| 21 | `-race` | concurrent setting changes |
| 22 | unit | Stop в середине backoff |
| 23 | unit на IPv6‑only | моки net.Interfaces |
| 24 | api test | scope `read` не должен видеть чувствительные поля |
| 25 | unit | при ошибке passphrase payload должен быть 0 |
| 26 | unit | mock commit error → audit вместо warn‑only |
| 27 | unit | legacy disabled токен остаётся disabled |
| 28 | unit | поток ошибок → один лог в N секунд |
| 29 | unit | повторный запрос без `If‑None‑Match` запрещён через стаб |
| 30 | unit | URL с фрагментом отбивается |
| 31 | unit | проверить итоговые headers |
| 32 | e2e + unit (jest/vitest) | drop 4 раза → состояние `connected` после X секунд |
| 33 | unit | timing test (best‑effort) |
| 34 | api test | после Sunset — 401 |
| 35 | e2e | оба пути одинаково защищены |
| 36 | unit | 10000 уникальных IP → map не растёт бесконечно |
| 37 | api test | план >8MB → ожидаемое поведение |
| 38 | integration | старт с грязным tmp → чистка |
| 39 | e2e | UI получает событие после rollback |
| 40 | unit | вычисление intervals |
| 41 | unit | recordRun fail после success → не делает retry |
| 42 | — | см. 32 |
| 43 | e2e | UI показывает причину отказа |
| 44 | e2e | health‑check polling вместо setTimeout |
| 45 | e2e + a11y | пароль скрыт по умолчанию |
| 46 | e2e | при отсутствии backend семантики — кнопка disabled или предупреждение |

---

## 5. Этапы исправлений (после baseline) — кластерная модель

С 2026-05-24 фиксы группируются в **кластеры**: один диалог закрывает один кластер. Внутри кластера фиксы тематически связаны и проверяются одним общим прогоном; каждый фикс — отдельный коммит, чтобы `git bisect` остался возможным. Singleton-фиксы (несовместимые правки, ломающие контракт) идут отдельным диалогом.

Артефакты прогона: `tests/baseline/post-fix-cluster-<X>/`. В этом разделе плана появляется секция «Fix Cluster <X> YYYY-MM-DD» под последней секцией «Phase N run».

Правила батча:
- Все anchor’ы внутри кластера должны существовать ДО начала фикса (созданы в Фазах 2/3/4/7).
- Внутри кластера — атомарные коммиты по одному пункту реестра.
- Если хотя бы один фикс из кластера ломает baseline — откатывается ИМЕННО он, остальные остаются.
- Severity внутри кластера однородна (не смешивать P1 c P3, кроме случаев когда P3 — побочный мелкий фикс в том же файле).
- Контрактные правки (см. ниже Кластер E) требуют подтверждения пользователя через `ask_followup_question`.

### Кластеры

- **Кластер A. «Silent error suppression»** (P1). Пункты: **1, 5, 18**. Один паттерн `_ = ...Error` в трёх разных файлах: [`database/importxui/plan.go:516`](../../database/importxui/plan.go:516), [`cronjob/xuiSyncJob.go:85`](../../cronjob/xuiSyncJob.go:85), [`ipmonitor/ipmonitor.go:350`](../../ipmonitor/ipmonitor.go:350). Anchor’ы: XFAIL в plan_extra_test, xuiSyncJob_extra_test, ipmonitor_extra_test.
- **Кластер B. «Concurrency hardening»** (P1/P2). Пункты: **20, 21**. (П. 47 уже закрыт singleton-диалогом.) [`service/runtime.go:60`](../../service/runtime.go:60), [`service/telegram.go:51`](../../service/telegram.go:51). Anchor’ы: race-стресс‑тесты в Phase 5/7.
- **Кластер C. «Cron observability»** (P2/P3). Пункты: **4, 5, 40, 41**. Один файл [`cronjob/xuiSyncJob.go`](../../cronjob/xuiSyncJob.go:1). Anchor’ы: xuiSyncJob_extra_test.go (Phase 2), integration_xui_sync_test.go (Phase 3), xuiSyncJob_bench_test.go (Phase 5).
- **Кластер D. «WS hardening»** (P1). Пункты: **32, 33**. [`frontend/src/store/ws.ts`](../../frontend/src/store/ws.ts:1) и [`api/realtime.go:404`](../../api/realtime.go:404). Anchor’ы: Vitest ws.spec.ts, Playwright ws-reconnect-chaos.spec.ts (XFAIL), TestConsumeWSTokenTimingRegressionAnchor_XFAILIssue33.
- **Кластер E. «xui-import contract drift»** (P1/P3, **требует подтверждения**). Пункты: **2, 3, 7, 8, 46**. Меняет контракт adminMode и onlyNew. Затрагивает [`database/importxui/plan.go`](../../database/importxui/plan.go:1), [`database/importxui/options.go`](../../database/importxui/options.go:1), [`cronjob/xuiSyncJob.go`](../../cronjob/xuiSyncJob.go:1), [`api/import_xui.go`](../../api/import_xui.go:1), [`frontend/src/views/MigrateXui.vue`](../../frontend/src/views/MigrateXui.vue:1), [`database/model/model.go`](../../database/model/model.go:1) (поле `force_password_reset`).
- **Кластер F. «Audit pipeline»** (P1/P2). Пункты: **16, 17**. [`service/audit_writer.go`](../../service/audit_writer.go:1), [`service/secret_settings.go`](../../service/secret_settings.go:1). Anchor’ы: audit_writer_extra_test.go, secret_settings_extra_test.go.
- **Кластер G. «Backup safety»** (P2). Пункты: **10, 11, 12**. Один файл [`database/backup.go`](../../database/backup.go:1). Anchor’ы: integration_backup_restore_test.go, security_rollback_path_test.go, sqlite_locked_test.go (chaos).
- **Кластер H. «MigrateXui UX»** (P2/P3). Пункты: **43, 44, 45**. Один файл [`frontend/src/views/MigrateXui.vue`](../../frontend/src/views/MigrateXui.vue:1). Anchor’ы: Playwright migrate-xui-happy.spec.ts (XFAIL), a11y-spec.
- **Кластер I. «Telegram/WARP robustness»** (P2/P3). Пункты: **22, 25, 31**. [`service/telegram.go`](../../service/telegram.go:1), [`service/telegram_backup.go`](../../service/telegram_backup.go:1), [`service/warp.go`](../../service/warp.go:1). Anchor’ы: chaos/telegram_down_test.go, chaos/warp_down_test.go.

### Singletons (вне кластеров)

- **П. 47** «token_use_debouncer race vs DB reinit» — закрыт отдельно как первый production-фикс.
- **П. 23** «GetSystemInfo IPv6-only crash» — singleton, нужен DI net.Interfaces (требует продакшен-hook).
- **П. 36** «xui rate-limit map unbounded» — singleton, может стать частью Кластера C при желании.
- **П. 14, 19** — singleton’ы про адаптеры миграции и стартап-гонки, отдельный риск.
- **П. 26, 27, 28, 29, 30, 34, 35, 37, 38, 39** — singletons по UX/observability/contract; решаются точечно.

### Порядок прохождения кластеров (предлагаемый)

1. **Кластер A** «Silent error suppression» — самый изолированный P1, эффективная разминка.
2. **Кластер D** «WS hardening» — критично для realtime UX.
3. **Кластер F** «Audit pipeline» — критично для security observability.
4. **Кластер B** «Concurrency hardening» — race-detector clean‑up.
5. **Кластер G** «Backup safety» — стабильность хранилища.
6. **Кластер C** «Cron observability» — диагностика sync.
7. **Кластер E** «xui-import contract drift» — контрактные правки, отдельное согласование.
8. **Кластер H** «MigrateXui UX» — frontend domain.
9. **Кластер I** «Telegram/WARP robustness».
10. Singletons по убыванию severity.
11. Финальный диалог «Recheck audit baseline»: повторить все 8 фаз, пересчитать дельту, обновить top‑10 risks.

---

## Baseline run 2026-05-23

Полный отчёт baseline: [`tests/baseline/SUMMARY.md`](../../tests/baseline/SUMMARY.md). Окружение и версии: [`tests/baseline/env.md`](../../tests/baseline/env.md).

Что зелёное:
- `go build ./...`
- `go vet ./...`
- `go test ./...`
- `go test ./... -race -count=1`
- установка и version‑check инструментов `staticcheck`, `golangci-lint`, `gosec`, `govulncheck`
- `golangci-lint config verify -v`

Что красное:
- `staticcheck ./...`: unused/deprecated/simplification findings.
- `golangci-lint run`: findings по `errcheck`, `gosec`, `bodyclose`, `noctx`, `nilness` через `govet`, `unparam`, `ineffassign`, `gosimple`.
- `gosec ./...`: 55 issues, включая `G104`, `G115`, `G304`, `G101`, `G703`; отдельного `G306` в baseline не найдено.
- `govulncheck ./...`: 12 called vulnerabilities в `golang.org/x/net`, `golang.org/x/crypto` и Go stdlib `go1.26.2`.
- `npm ci`: Windows `EPERM unlink` на `frontend/node_modules/@rolldown/binding-win32-x64-msvc/rolldown-binding.win32-x64-msvc.node`; последующие `npm run lint`, `npm run build`, `npm run test` тоже red после неполной установки зависимостей.

Что skipped / требует фикстур:
- `frontend/package.json` не содержит отдельный `typecheck` script; typecheck находится внутри `npm run build`.
- `e2e` оставлен заглушкой TODO.
- importxui fixture‑тесты требуют локальные `test-db/x-ui.db` и `test-db/s-ui.db`; каталог `test-db/` создан только с `.gitkeep`, реальные `.db` файлы не добавлялись.

Какие пункты отчёта подтверждены прогоном:
- Системный паттерн «молчащие ошибки» подтверждён `golangci-lint`/`errcheck` и `gosec G104`.
- Специальная проверка Фазы 1 по `gosec G304/G101` дала baseline findings; `G304` есть в путях import/backup/rollback, `G101` есть как secret‑pattern findings.
- Пробел «внешние проверки не запускались» закрыт для `go test -race`, `go vet`, `staticcheck`, `golangci-lint`, `govulncheck`, `gosec`.
- Frontend checks пока зафиксированы как red baseline из-за failed `npm ci`; production frontend код в этом диалоге не менялся.

## Phase 2 run 2026-05-24

Полный отчёт Phase 2 добавлен в [`tests/baseline/SUMMARY.md`](../../tests/baseline/SUMMARY.md#phase-2-per-package-unit-tests). Артефакты команд лежат в [`tests/baseline/phase2/`](../../tests/baseline/phase2/), coverage function report — [`tests/baseline/phase2/coverage/coverage-func.txt`](../../tests/baseline/phase2/coverage/coverage-func.txt).

Что добавлено без правок production-кода:
- новые per-package unit anchors в `service`, `api`, `cronjob`, `database/importxui`, `ipmonitor`, `realtime`;
- негативные проверки для auth/token/settings/secrets/telegram envelope/audit writer/restart manager/stats/realtime token/importxui plan/cron sync;
- current-behavior и `XFAIL` anchors для известных пунктов реестра, которые требуют production-фикса в отдельном диалоге.

Командная дельта к baseline 2026-05-23:
- `go build ./...`, `go vet ./...`, `go test ./... -count=1 -timeout 5m`, `go test ./... -race -count=1 -timeout 10m` остались green.
- Frontend/npm и static-analysis red-команды из Фазы 0/1 не запускались и не исправлялись.
- `gocov`/`gocov-html` не запускались: инструменты не найдены в локальном `PATH`.

Coverage:
- новый абсолютный baseline total: 42.1% statements.
- целевые пакеты: `api` 53.9%, `cronjob` 61.4%, `database/importxui` 24.6%, `ipmonitor` 81.4%, `realtime` 92.5%, `service` 47.5%.
- Порог 60% достигнут для `cronjob`, `ipmonitor`, `realtime`; не достигнут для `service`, `api`, `database/importxui`. Для `database/importxui` существенная часть более широких сценариев остаётся skipped без локальных `test-db/x-ui.db` и `test-db/s-ui.db`.

Regression anchors по пунктам реестра:
- п. 4 — `XFAIL` на сохранение real `lastErr` в `last_run_summary`.
- п. 6 — current-behavior anchor: wireguard endpoint skip сейчас попадает в `Inbounds.Skipped`, не в `Endpoints.Skipped`.
- п. 16 — audit writer overflow/drop counter, batch flush, stop flush.
- п. 17 — primary secretbox candidate no-audit green; legacy fallback no-audit оставлен `XFAIL`.
- п. 18 — ipmonitor DB read error fail-closed оставлен `XFAIL`.
- п. 27 — legacy disabled token migration оставлен `XFAIL`.
- п. 30 — user-info URL rejection green; fragment rejection оставлен `XFAIL`.
- п. 33 — websocket token double-spend, expiry, capacity и IssueWSToken rate-limit green; timing-sensitive anchor оставлен `XFAIL`.

Skipped/XFAIL в новых тестах:
- `TestXUISyncJobExtraFailureSummaryKeepsLastErr_XFAILIssue4`
- `TestLegacySecretboxFallbackDoesNotAuditAfterFix_XFAILIssue17`
- `TestLoadCacheEntryFailsClosedOnClientIPReadError_XFAILIssue18`
- `TestUserServiceMigrateLegacyTokensKeepsDisabled_XFAILIssue27`
- `TestValidateOptionalHTTPURLRejectsFragment_XFAILIssue30`
- `TestConsumeWSTokenTimingRegressionAnchor_XFAILIssue33`
- `TestUserServiceLoginLockedDocumentedAtAPILayer` — documented skip: lockout реализован на API rate-limit слое, не в `UserService`.

## Phase 3 run 2026-05-24

Полный отчёт Phase 3 добавлен в [`tests/baseline/SUMMARY.md`](../../tests/baseline/SUMMARY.md#phase-3-integration-tests). Артефакты команд лежат в [`tests/baseline/phase3/`](../../tests/baseline/phase3/), coverage function report — [`tests/baseline/phase3/coverage/coverage-func.txt`](../../tests/baseline/phase3/coverage/coverage-func.txt).

Что добавлено без правок production-кода:
- `api/integration_auth_flow_test.go`: login -> CSRF -> save settings -> realtime config invalidation; audit anchors для `login_success`, `sub_path_changed`, `settings_save_rejected_key`.
- `api/integration_ws_lifecycle_test.go`: ws-token issue/connect, событие `connected`, publish через realtime hub, close/reconnect, multiple clients, capacity по user/IP.
- `service/integration_session_rotation_test.go`: `RotateSessionGeneration()` закрывает WS кодом 4401 `session_rotated`, инвалидирует ws-token и пишет audit `ws_tokens_invalidated`.
- `database/integration_backup_restore_test.go`: backup envelope -> open -> `ImportDB`, плюс fallback restore при падении migration после rename.
- `database/importxui/integration_import_full_test.go`: full fixture Plan/Apply path для strategies/adminMode/includes, skipped без локальных `test-db`.
- `cronjob/integration_xui_sync_test.go`: `RunProfile` success/fail/min-interval обновляет `last_run_status` и `last_run_summary`.
- `service/integration_stats_pipeline_test.go`: smoke `SaveStats` без core не падает; test-core realtime publish оставлен XFAIL.
- `service/integration_subsecret_rotate_test.go`: rotate `sub_secret` -> APIv2 reload -> realtime `TopicConfigInvalidated`.
- `ipmonitor/integration_enforce_path_test.go`: enforce path `Allow` на наполненной DB с разными limit/mode.

Командная дельта к Phase 2:
- `go build ./...`, `go vet ./...`, `go test ./... -count=1 -timeout 5m` остались green.
- `go test ./... -race -count=1 -timeout 15m` стал red на full-suite race: фоновый `service/token_use_debouncer.go` flush пересекается с переинициализацией test DB в `api/settings_save_test.go:111`. Изолированные повторы `go test ./api -race -count=1 -timeout 5m` прошли 5/5 green, поэтому это зафиксировано как flaky/cross-package race, без фикса в этом диалоге.
- Frontend/npm и static-analysis red-команды из baseline не запускались и не исправлялись.
- Интеграционные тесты не изолировались build-tag `integration`; отдельный `-tags=integration` прогон не нужен.

Coverage:
- total: 42.1% -> 42.5% statements.
- целевые пакеты: `api` 53.9% -> 56.0%, `service` 47.5% -> 48.0%, `database` 66.3% -> 66.5%, `database/importxui` 24.6% -> 24.6%, `cronjob` 61.4% -> 61.4%, `ipmonitor` 81.4% -> 82.4%, `realtime` 92.5% -> 92.5%.
- Порог 60% по-прежнему не достигнут для `api`, `service`, `database/importxui`; importxui остаётся ограничен отсутствием fixture DB.

Новые skipped/XFAIL:
- `TestIntegrationAuthFlowSettingsSaveSuccessAudit_XFAILPhase3`: успешный settings save пока не пишет `settings_save_*` audit event; привязка к п. 26 и observability gap.
- `TestIntegrationRealtimeWSSlowClientDrop_XFAILPhase3`: требуется hook для детерминированного slow writer / заполнения WS send queue; привязка к п. 32.
- `TestIntegrationBackupEnvelopeRestorePreservesBackupTableCounts`: XFAIL на mismatch `tls` count после restore из-за no-TLS sentinel; привязка к п. 11.
- `TestIntegrationImportXUIFullFixturePlanApply`: skipped без `test-db/x-ui.db` и `test-db/s-ui.db`.
- `TestIntegrationStatsPipelineRealtimeWithTestCore_XFAILPhase3`: требуется test-core или hook для подмены `core.Core`/`StatsTracker`; привязка к п. 26.

Пункты реестра с integration-anchor:
- п. 2, 8: importxui full fixture Plan/Apply path закреплён skipped anchor до появления `test-db`.
- п. 4, 5, 7, 41: xui sync success/fail/min-interval и run fields.
- п. 11: backup restore consistency/fallback; `tls` sentinel mismatch оставлен XFAIL.
- п. 18: ipmonitor enforce path green, Phase 2 XFAIL на DB read error не дублировался.
- п. 26: stats nil-core green, settings success audit и test-core realtime publish оставлены XFAIL.
- п. 32, 33: websocket lifecycle/reconnect/multiple/capacity/session rotation покрыты in-process; slow-client drop оставлен XFAIL.
- Дополнительно закреплён sub-secret rotation path: DB/APIv2 reload/realtime invalidation.

## Phase 4 run 2026-05-24

Полный отчёт Phase 4 добавлен в [`tests/baseline/SUMMARY.md`](../../tests/baseline/SUMMARY.md#phase-4-security). Артефакты команд лежат в [`tests/baseline/phase4/`](../../tests/baseline/phase4/), coverage function report - [`tests/baseline/phase4/coverage/coverage-func.txt`](../../tests/baseline/phase4/coverage/coverage-func.txt).

Security review docs:
- gosec triage: [`docs/audit/security/gosec-triage.md`](security/gosec-triage.md).
- AuthZ matrix: [`docs/audit/security/authz-matrix.md`](security/authz-matrix.md).
- CSRF matrix: [`docs/audit/security/csrf-matrix.md`](security/csrf-matrix.md).

Что добавлено без правок production-кода:
- `api/security_authz_test.go`: scope matrix для v2 middleware, current-contract anchor для v2 invalid/expired token HTTP 200 + `success:false`, duplicate `/import-xui/*` v1/v2 contract anchor.
- `api/security_csrf_test.go`: protected POST matrix, missing/expired CSRF reject, rotated session reject before handler, явные исключения login/logout/csrf.
- `api/security_login_lockout_test.go`: 10+ wrong logins, `login_blocked` audit, recovery after window и `resetLoginFailures`.
- `service/security_ssrf_test.go`: Telegram proxy и optional HTTP URL validation для private/link-local/multicast/file/scheme/user-info случаев.
- `api/security_rollback_path_test.go` и `database/security_rollback_path_test.go`: rollback path traversal/symlink/outside/missing anchors.
- `api/security_token_test.go` и `service/security_token_test.go`: bearer vs legacy token, constant-time scope helper, WS token double-spend/expiry/capacity.
- `api/security_session_test.go`: cookie flags, session max age, rotation invalidates old cookie.
- `api/security_ws_origin_test.go`: host mismatch, invalid scheme/origin, canonical web domain и `ws_origin_rejected` audit.
- `service/security_backup_confidentiality_test.go`: backup audit confidentiality, config passphrase redaction, zeroization XFAIL hook.

Командная дельта к Phase 3:
- `go build ./...`, `go vet ./...`, `go test ./... -count=1 -timeout 5m`, `go test ./... -run Security -count=1`, `go test ./... -race -count=1 -timeout 15m` green.
- `gosec ./...` red с теми же 55 baseline findings, теперь расклассифицировано: true positive 16, nosec 18, mitigated_in_review 21. `//nosec` / `//nolint` в production-код не добавлялись.
- DAST (`nuclei`/`zap`/Burp/live instance) не запускался по ограничению Фазы 4.
- Govulncheck triage не выполнялся и оставлен на отдельный диалог.

Coverage:
- total: 42.5% -> 42.7% statements.
- целевые пакеты: `api` 56.0% -> 57.5%, `service` 48.0% -> 48.1%, `database` 66.5% -> 66.5%, `util/ssrf` 0.0% -> 59.7%, `ipmonitor` 82.4% -> 82.4%, `realtime` 92.5% -> 92.5%.

Пункты реестра с security-anchor:
- п. 25: backup audit confidentiality green, deterministic zeroization оставлен XFAIL до production/test hook.
- п. 30: SSRF scheme/user-info/private-network coverage, optional HTTP private-host gap оставлен XFAIL.
- п. 33: WS token double-spend/expiry/capacity green; Phase 2 timing XFAIL не дублировался.
- п. 34: v2 token auth HTTP-status gap закреплён current-contract + XFAIL desired 401/403.
- п. 35: duplicate `/import-xui/*` v1/v2 route contract закреплён test-anchor.
- п. 38: все `G304` findings классифицированы, rollback path tests добавлены.
- п. 47: Phase 3 race finding добавлен в реестр как отдельный P2 Concurrency issue.

Gosec true positives связаны прежде всего с п. 14 (migration ignored errors), п. 16 (observability/log write errors), п. 19 (startup error handling), session security/logout, app restart, entropy fallback и integer conversion import gap. `G101` классифицирован как false positive для marker/session-key/date значений; SQL injection false-positive вида `tx.Where` в текущем `gosec.txt` не найден.

Следующий шаг по плану: Фаза 5 «Производительность и надёжность», параллельно отдельный диалог triage `govulncheck`.

## Phase 5 run 2026-05-24

Полный perf baseline: [`docs/audit/perf/baseline.md`](perf/baseline.md). Артефакты команд лежат в [`tests/baseline/phase5/`](../../tests/baseline/phase5/), coverage function report - [`tests/baseline/phase5/coverage/coverage-func.txt`](../../tests/baseline/phase5/coverage/coverage-func.txt).

Что покрыто без правок production-кода:
- backup pipeline: `GetDb` full/exclude-heavy на 100k и 1M строк `stats`, `client_ips`, `changes`; отдельный anchor для `SafeSQLiteBatchSize` по `model.Client`.
- stats pipeline: `StatsService.SaveStats` на 100/1000 clients, `updateClientTrafficDeltas` с 0/50/90% пустых deltas.
- audit writer и telegram notifier: throughput, overload, overflow; зафиксировано отсутствие severity priority при FIFO overflow.
- token use debouncer: concurrent `Record` и batch flush.
- importxui Plan/Apply: synthetic wireguard source на 100/1000 inbounds, dry-run и real apply.
- ipmonitor allow path: cache load, warmup, known IP allow, reject over enforce limit.
- realtime: hub publish to 10/100/1000 subscribers, WS connect/disconnect и max per user/IP capacity через `httptest`.
- cron sync под потерянной сетью: текущий retry/backoff shape 3 attempts, 100ms + 200ms.
- HTTP API через `httptest`: `/load`, `/stats`, `/onlines`, `/save`, `/import-xui/reports` под нагрузкой/rate-limit.
- pprof snapshots: backup CPU/mem и API `/load` CPU, top-10 сохранены в `tests/baseline/phase5/profiles/`.

Командная дельта:
- `go build ./...`, `go vet ./...`, `go test ./... -count=1 -timeout 5m`, `go test ./... -race -count=1 -timeout 15m`, `go test ./... -bench=. -benchmem -run=^$ -benchtime=2s`, coverage - green.
- Coverage total: 42.7% -> 43.0%; `api`: 57.5% -> 59.5%.
- Phase 3 race finding #47 в Phase 5 `-race` не воспроизвёлся, но остаётся отдельным P2 concurrency issue для отдельного фикса.
- Full backup benches на Windows шумят из-за temp/SQLite I/O: dedicated `bench-backup` 1M full 84.8s/op, общий `bench-all` 128.6s/op. Для gating нужен Linux runner/re-run.

Пункты реестра с performance-anchor:
- п. 16: audit writer overload 10,000 events, `dropped=5904`, `lost_warn_security=5000`, `lost_info=904`; severity priority отсутствует.
- п. 18: ipmonitor warmup 1000 clients x 5 IP 126.9ms, known allow ~2.1us/op, reject ~3.0us/op.
- п. 32: realtime hub publish 1000 subscribers 361.7us/op, WS connect/disconnect 20 clients 35.3ms/op, capacity 429.
- п. 36: `/import-xui/reports` rate-limit 100 requests -> 5x 200, 95x 429; benchmark 3.23ms/op.
- п. 40: cron lost-network retry/backoff anchor: 3 attempts, 300ms expected, 303.4ms observed.
- п. 44: server-side import report/rollback-adjacent latency/rate-limit anchor зафиксирован для Phase 6 frontend health-check/polling work; реальный frontend e2e остаётся Phase 6.

Следующий шаг по плану: Фаза 6 «Frontend»; параллельно отдельные диалоги для triage `govulncheck` и фикса #47.

## Phase 6 run 2026-05-24

Полный отчёт Phase 6 добавлен в [`tests/baseline/SUMMARY.md`](../../tests/baseline/SUMMARY.md#phase-6-frontend). Frontend code-path baseline: [`docs/audit/frontend/baseline.md`](frontend/baseline.md). Accessibility/security baseline: [`docs/audit/frontend/accessibility.md`](frontend/accessibility.md). Артефакты команд лежат в [`tests/baseline/phase6/`](../../tests/baseline/phase6/).

Что сделано без правок production-кода:
- восстановлен рабочий `frontend/node_modules`: Windows EPERM на `@rolldown/binding-win32-x64-msvc` ушёл после закрытия локальных Node/VS Code держателей файла, удаления `frontend/node_modules` и повторного `npm ci`;
- frontend static baseline green: `npm run lint --prefix frontend`, `npm run build --prefix frontend`, финальный Vitest baseline 15 files / 76 tests;
- добавлены test-only regression anchors для `WsRuntime`, `csrfStore`, `HttpUtils`, API CSRF/AbortController contract;
- добавлены Playwright smoke/e2e сценарии login, settings path conflicts, API tokens, observability, security headers, accessibility и XFAIL anchors для MigrateXui/WS reconnect;
- добавлен test-only e2e server helper `tests/e2e/panel-server/main.go` и runner `tests/e2e/run-server.js`, чтобы проверять локальную панель без sing-box/core и без внешних сервисов.

Командная дельта к Phase 5:
- `npm ci`, `npm run lint --prefix frontend`, `npm run build --prefix frontend`, финальный `vitest`, `npx playwright install chromium`, `npx playwright test` — green;
- отдельного `typecheck` script нет, `vue-tsc --noEmit` покрыт `npm run build`;
- Playwright: 8 passed, 4 skipped/XFAIL; HTML/JUnit отчёт сохранён в `tests/baseline/phase6/playwright/` и `tests/baseline/phase6/playwright.junit.xml`;
- полный `sui` server не стартовал в Phase 6 окружении: no-tag сборка падает на disabled naive outbound, `with_naive_outbound` на Windows падает из-за `cronet-go/all` build constraints, `SUI_DISABLE_CORE=1` production-кодом не поддержан. Поэтому e2e использует API-only helper с реальными session/security/API handlers и test SQLite DB.

Accessibility findings:
- Axe baseline собран по login, dashboard, migrate-xui, settings, audit.
- Violations: login 6, dashboard 5, migrate-xui 5, settings 5, audit 6.
- Основные классы debt: `button-name`, `label`, `aria-required-children`, `image-alt`, `color-contrast`, landmarks/H1, `aria-tooltip-name`, `empty-table-header`.

Security headers findings:
- Green на HTTP dev-инстанции: `Content-Security-Policy` с `frame-ancestors 'none'`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`.
- `Strict-Transport-Security` условно skipped на HTTP и остаётся HTTPS-only проверкой.

Пункты реестра с frontend-anchor:
- п. 32: `WsRuntime` unit anchors для reconnect/fallback/degraded polling; Playwright `ws-reconnect.spec.ts` оставлен `test.fixme` до фикса авто-возврата из degraded в connected.
- п. 43: MigrateXui apply error UX задокументирован; e2e apply UX оставлен `test.fixme`.
- п. 44: rollback reload race `setTimeout + location.reload` задокументирован; e2e rollback оставлен `test.fixme`.
- п. 45: password reveal через `JSON.stringify(generatedAdmins)` задокументирован; e2e предупреждение/report-download оставлены `test.fixme`.
- п. 46: `adminModeItems` и fallback/reset-required contract задокументированы; migration contract e2e оставлен `test.fixme`.
- Дополнительно: realtime `applyRealtimeEvent` unknown-type pitfalls, CSRF retry/clear semantics, `HttpUtils` abort/JSON-failure behavior, API CSRF idempotent abort, security headers.

Следующий шаг по плану: Фаза 7 «Системные / chaos». Параллельно отдельные диалоги: triage `govulncheck`, фикс п. 47, фикс п. 32; после фикса п. 32 `frontend/tests/e2e/ws-reconnect.spec.ts` должен перейти из XFAIL в green.

## Phase 7 run 2026-05-24

Полный отчёт Phase 7 добавлен в [`tests/baseline/SUMMARY.md`](../../tests/baseline/SUMMARY.md#phase-7-chaos). Runbook: [`docs/audit/chaos/runbook.md`](chaos/runbook.md). Артефакты команд лежат в [`tests/baseline/phase7/`](../../tests/baseline/phase7/).

Что добавлено без правок production-кода:
- `tests/chaos/` build-tag `chaos` набор для SIGHUP/ImportDB, SQLite lock, xui import cancel, cron lost-source, rate-limit, system info smoke, token-use race и XFAIL anchors для недоступных fault-injection случаев;
- `frontend/tests/e2e/ws-reconnect-chaos.spec.ts` — Playwright sequence 5 offline/online циклов, expected-fail до фикса п. 32;
- `docs/audit/chaos/runbook.md` — воспроизводимые команды, red-baseline правила, связь сценариев с реестром;
- `tests/chaos/docker-compose.chaos.yml` и helper scripts как опциональный Docker-chaos каркас.

Командная дельта к Phase 6:
- `go build ./...`, `go vet ./...`, `go test ./... -count=1 -timeout 5m` остались green.
- `go test -tags=chaos ./tests/chaos/... -count=1 -timeout 10m` green: executable anchors проходят, XFAIL/skips явно задокументированы.
- `go test ./... -race -count=1 -timeout 15m` red на п. 47; отдельный `go test -race -tags=chaos ./tests/chaos -run TestTokenUseDebouncerRaceVsDBReinitChaosIssue47` детерминированно воспроизводит ту же гонку.
- Playwright WS chaos command green как expected-fail scenario; sequence сохранён в `tests/baseline/phase7/playwright-ws-chaos/`.
- Docker chaos skipped: Docker CLI недоступен в локальном окружении.

Пункты реестра с chaos-anchor:
- п. 10: `ImportDB` SIGHUP hook + Linux-only real 3s timer anchor; Windows skipped.
- п. 11: SQLite lock для `GetDb`, rollback после foreign-key failure, stage-read cancel cleanup.
- п. 12: versioned backup без `settings.config` отклоняется без изменения live DB.
- п. 14: post-rename migration/adapt failure возвращает fallback и live DB остаётся валидной.
- п. 18: XFAIL до DB fault-injection hook для `ipmonitor.loadCacheEntry`.
- п. 22: XFAIL до Telegram HTTP/notifier hook для cancel-aware backoff.
- п. 23: current-host smoke green, IPv6-only XFAIL до DI `net.Interfaces`.
- п. 31: XFAIL до WARP HTTP client hook; внешние Cloudflare вызовы не делались.
- п. 32: Playwright offline/online chaos expected-fail до healing reconnect.
- п. 36: 10,000 unique IP current-behavior anchor; bounded map length XFAIL до helper/API фикса.
- п. 40: 50 lost-source `RunProfile` запусков закрепляют текущий backoff 100ms+200ms, run fields и bounded memory.
- п. 47: race red anchor для token-use debouncer vs DB reinit.

Платформенные и testability blockers:
- Linux runner нужен для настоящего SIGHUP timer.
- Docker Engine/CLI отсутствует локально.
- `test-db/x-ui.db` и `test-db/s-ui.db` отсутствуют; xui chaos использует синтетическую SQLite.
- Для п. 18/22/23/31/36 нужны DI/test hooks или package-local chaos tests в отдельных фикс-диалогах.

Следующий шаг по плану: Фаза 8 «Регрессионная упаковка и CI». Параллельно отдельные диалоги: triage `govulncheck`, фикс п. 47, фикс п. 32.

## Phase 8 run 2026-05-24

Полный отчёт Phase 8 добавлен в [`tests/baseline/SUMMARY.md`](../../tests/baseline/SUMMARY.md#phase-8-ci). CI docs: [`docs/audit/ci/design.md`](ci/design.md), [`docs/audit/ci/required-checks.md`](ci/required-checks.md), [`docs/audit/ci/runbook.md`](ci/runbook.md). Артефакты команд лежат в [`tests/baseline/phase8/`](../../tests/baseline/phase8/).

Что добавлено без правок production-кода:
- `.github/workflows/audit.yml` — dashboard-only manual/nightly workflow.
- `.github/workflows/audit-go.yml` — Go build/vet/test required matrix Linux+Windows, race/static/security soft checks, dashboard artifact.
- `.github/workflows/audit-frontend.yml` — npm install, lint, build/typecheck, Vitest required; Playwright e2e и accessibility soft.
- `.github/workflows/audit-chaos.yml` — nightly/manual chaos tests и opt-in docker chaos через `vars.RUN_DOCKER_CHAOS` или `workflow_dispatch`.
- `.github/workflows/audit-perf.yml` — weekly/manual benchmark run, benchstat comparison, >20% regression as soft warning/optional PR comment.
- `scripts/audit/aggregate.sh` и `scripts/audit/aggregate.ps1` — сбор всех JUnit в `summary.html`/`summary.json`.
- `docs/audit/ci/*`, `CONTRIBUTING.md`, README секция `How to read CI status`.

Critical gates:
- REQUIRED: `build`, `vet`, `test-go`, `fe-lint`, `fe-build`, `fe-vitest`.
- SOFT: `test-go-race`, `gosec`, `govulncheck`, `staticcheck`, `golangci-lint`, `chaos`, `perf-bench`, `fe-e2e`, `accessibility`.

Командная дельта к Phase 7:
- `go build ./...`, `go vet ./...`, `go test ./...`, `npm run lint`, `npm run build`, `npm run test` — green.
- `go test ./... -race -count=1` — red на п. 47; не исправлялось в Phase 8 и оформлено soft check.
- `staticcheck`/`golangci-lint` — red baseline; не исправлялось.
- `act` dry-run skipped: `act` не установлен локально, проверка `audit-go.yml` остаётся на push/PR.
- `aggregate.sh` локально не запускался из-за отсутствия WSL-дистрибутива для `bash`; dashboard сгенерирован через `aggregate.ps1`, а `aggregate.sh` будет выполняться на Ubuntu CI.

Blocked для отдельных диалогов:
- `govulncheck` triage и dependency upgrade policy.
- фиксы P1/P2, начиная с 1, 16, 17, 18, 32, 33, 47.

## Vuln triage 2026-05-24

Govulncheck triage закрыт dependency-only изменениями без правок production-кода, без npm/frontend изменений и без мажорных bump'ов. Детали: [`docs/audit/security/govulncheck-triage.md`](security/govulncheck-triage.md). Повторный результат: [`tests/baseline/phase1/govulncheck-after.txt`](../../tests/baseline/phase1/govulncheck-after.txt).

Что поднято:
- добавлен `toolchain go1.26.3` при сохранении `go 1.25.7`;
- `golang.org/x/crypto` v0.48.0 -> v0.52.0;
- `golang.org/x/net` v0.51.0 -> v0.55.0;
- транзитивно обновлены `golang.org/x/term` v0.40.0 -> v0.43.0, `x/sys` v0.41.0 -> v0.45.0, `x/text` v0.34.0 -> v0.37.0, `x/mod` v0.33.0 -> v0.35.0, `x/sync` v0.19.0 -> v0.20.0, `x/tools` v0.42.0 -> v0.44.0.

Дельта:
- `govulncheck ./...`: 12 called vulnerabilities -> 0, итог `No vulnerabilities found.`
- verbose inventory: 7 imported-package uncalled и 9 module-only uncalled -> 0.
- `go build ./...`, `go vet ./...`, `go test ./... -count=1 -timeout 5m`, `go test ./... -race -count=1` green на финальном графе; п. 47 в этом прогоне не воспроизвёлся, но остаётся отдельным зарегистрированным concurrency issue.
- `gosec ./...` остался baseline red без ухудшения: 55 -> 55 findings.

Реестр:
- новых пунктов не добавлено;
- Phase 8 blocker `govulncheck triage и dependency upgrade policy` закрыт;
- оставшиеся следующие фиксы P1/P2: 1, 16, 17, 18, 32, 33.

## Fix Cluster A 2026-05-24

### Design

П. 1, `database/importxui/plan.go`: replace-ветка TLS уже находится внутри `applyTLS()` и поднимает ошибки наружу через `src.eachInbound`. Минимальная правка: заменить `_ = tx.Delete(&existing).Error` на локальную проверку `if err := tx.Delete(&existing).Error; err != nil { return err }`. Это сохраняет существующую семантику `Apply`: ошибка прерывает `state.run`, транзакция откатывается существующим `defer`. Альтернатива с оборачиванием ошибки отвергнута, чтобы не менять contract и строки ошибок. Альтернатива с retry/delete policy отвергнута как более широкий transaction-safety дизайн.

П. 5, `cronjob/xuiSyncJob.go`: failure audit сейчас вызывается как `recordSyncAudit("xui_sync_failed", profile, nil)`, поэтому details не получает диагностику. Минимальная правка: расширить helper опциональным `error` и добавить в details только sanitized `errorClass`, не меняя имя события, severity, actor/resource и не вмешиваясь в `recordRun`. Для классификации используем небольшой локальный classifier: `success`, `failed`, `cancelled`, `disabled`, `source`, `db`. Альтернатива `recordSyncAuditFailure(profile, lastErr)` отвергнута как дополнительная обвязка при одном call site.

П. 18, `ipmonitor/ipmonitor.go`: `loadCacheEntry` уже возвращает `(allowCacheEntry, bool)`, а `refreshClient` при `ok=false` удаляет entry из cache. Минимальная правка: проверить ошибку `Find(&rows).Error` и вернуть `(allowCacheEntry{}, false)` без заполнения cache entry. Это fail-closed для transient DB error: `Allow` при cache miss сохраняет текущий fallback путь, а refresh path не записывает неполную запись. Альтернатива логировать/публиковать событие отвергнута как observability change вне кластера silent error suppression.

### Results

Артефакты: [`tests/baseline/post-fix-cluster-A/`](../../tests/baseline/post-fix-cluster-A/).

| Команда | Статус | Лог |
|---|---:|---|
| anchor 1 before: `go test -run "TestApplyState_TLSReplaceDeleteError" ./database/importxui/... -count=1 -timeout 2m` | green, `[no tests to run]`; заявленный pre-existing anchor отсутствовал в рабочем дереве | [`anchor-1-before.txt`](../../tests/baseline/post-fix-cluster-A/anchor-1-before.txt) |
| anchor 5 before: `go test -run "TestXUISyncJobExtraFailureSummaryKeepsLastErr" ./cronjob/... -count=1 -timeout 2m` | XFAIL/skip source, command green без `-v` | [`anchor-5-before.txt`](../../tests/baseline/post-fix-cluster-A/anchor-5-before.txt) |
| anchor 18 before: `go test -run "TestLoadCacheEntryFailsClosedOnClientIPReadError" ./ipmonitor/... -count=1 -timeout 2m` | XFAIL/skip source, command green без `-v` | [`anchor-18-before.txt`](../../tests/baseline/post-fix-cluster-A/anchor-18-before.txt) |
| `go test -run TestApplyState_TLSReplaceDeleteError ./database/importxui/... -count=10` | green, 10/10 | [`anchor-1-after.txt`](../../tests/baseline/post-fix-cluster-A/anchor-1-after.txt) |
| `go test -run "FailureSummaryKeepsLastErr" ./cronjob/... -count=10` | green, 10/10 | [`anchor-5-after.txt`](../../tests/baseline/post-fix-cluster-A/anchor-5-after.txt) |
| `go test -run "FailsClosedOnClientIPReadError" ./ipmonitor/... -count=10` | green, 10/10 | [`anchor-18-after.txt`](../../tests/baseline/post-fix-cluster-A/anchor-18-after.txt) |
| `go build ./...` | green | [`build.txt`](../../tests/baseline/post-fix-cluster-A/build.txt) |
| `go vet ./...` | green | [`vet.txt`](../../tests/baseline/post-fix-cluster-A/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | [`test.txt`](../../tests/baseline/post-fix-cluster-A/test.txt) |
| `go test ./... -race -count=1 -timeout 15m` | red, residual p. 47 family; see new p. 48 | [`test-race.txt`](../../tests/baseline/post-fix-cluster-A/test-race.txt), [`test-race-rerun-1.txt`](../../tests/baseline/post-fix-cluster-A/test-race-rerun-1.txt) |
| `go test -tags=chaos ./tests/chaos/... -count=1 -timeout 10m` | green | [`test-chaos.txt`](../../tests/baseline/post-fix-cluster-A/test-chaos.txt) |
| `staticcheck ./...` | red baseline, findings 37 -> 31 lines vs post-fix-47; no new Cluster A findings | [`staticcheck.txt`](../../tests/baseline/post-fix-cluster-A/staticcheck.txt) |
| `golangci-lint run` | red baseline, output 228 -> 222 lines vs post-fix-47; no new Cluster A findings | [`golangci-lint.txt`](../../tests/baseline/post-fix-cluster-A/golangci-lint.txt) |
| `gosec ./...` | red baseline, 55 -> 55 findings | [`gosec.txt`](../../tests/baseline/post-fix-cluster-A/gosec.txt) |
| `govulncheck ./...` | green, `No vulnerabilities found.` | [`govulncheck.txt`](../../tests/baseline/post-fix-cluster-A/govulncheck.txt) |

Коммиты:
- `b34875a` `fix(importxui): handle TLS replace delete error in applyTLS (registry #1)`
- `c027338` `fix(cronjob): include sanitized error class in xui_sync_failed audit (registry #5)`
- `3189bcd` `fix(ipmonitor): fail closed on client_ips read error in loadCacheEntry (registry #18)`

## Fix п. 47 2026-05-24

### Fix п. 47 design

- Воспроизведение Step A: `go test -race -tags=chaos -run TestTokenUseDebouncerRaceVsDBReinitChaosIssue47 ./tests/chaos/... -count=1 -timeout 5m` red; лог: [`tests/baseline/post-fix-47/race-before.txt`](../../tests/baseline/post-fix-47/race-before.txt).
- Выбран минимальный путь: `service/runtime.go` регистрирует `database.RegisterResetHook("service.token_use_debouncer", ...)`, без новых публичных API и без правки DB lifecycle contract.
- Hook вызывает `Runtime.resetTokenUseDebouncer()`: под reset gate дожидается уже начатых non-force flush, force-flush'ит текущий debouncer через `context.Background()`, затем заменяет экземпляр под `r.mu`.
- `service/token_use_debouncer.go` получил приватный `tokenUseFlushGate`: timer/manual `Flush` берут read lock, а reset hook берёт write lock, ставит `suspended=true` и оставляет короткое quiet window, чтобы flush не стартовал между `ResetCaches` и следующим `InitDB`.
- `Flush(ctx)` пишет синхронно и использует `writeMu`, чтобы reset hook мог дождаться in-flight batch write; прежняя goroutine внутри `Flush` убрана, чтобы write не переживал gate.
- П. 28 token-use circuit-breaker и dispatch loop не менялись: bounded pending map, batch size и skip semantics сохранены.
- Альтернатива с прямым вызовом service из `database.ResetCaches` отвергнута из-за import cycle; блокировка всего `InitDB` потребовала бы расширять DB lifecycle API и была шире нужного scope.
- Chaos anchor усилен только cleanup-частью (`closeChaosDB()` вокруг reinit на Windows), смысл гонки `RecordTokenUse`/`StopTokenUseDebouncer` vs `ResetCaches`/`InitDB` не ослаблялся.

### Fix п. 47 results

Артефакты: [`tests/baseline/post-fix-47/`](../../tests/baseline/post-fix-47/).

| Команда | Статус | Лог |
|---|---:|---|
| anchor before: `go test -race -tags=chaos -run TestTokenUseDebouncerRaceVsDBReinitChaosIssue47 ./tests/chaos/... -count=1 -timeout 5m` | red, data race reproduced | [`race-before.txt`](../../tests/baseline/post-fix-47/race-before.txt) |
| anchor after: `go test -race -tags=chaos -run TestTokenUseDebouncerRaceVsDBReinitChaosIssue47 ./tests/chaos/... -count=10 -timeout 10m` | green, 10/10 | [`race-after.txt`](../../tests/baseline/post-fix-47/race-after.txt) |
| `go build ./...` | green | [`build.txt`](../../tests/baseline/post-fix-47/build.txt) |
| `go vet ./...` | green | [`vet.txt`](../../tests/baseline/post-fix-47/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | [`test.txt`](../../tests/baseline/post-fix-47/test.txt) |
| `go test ./... -race -count=1 -timeout 15m` | green | [`test-race.txt`](../../tests/baseline/post-fix-47/test-race.txt) |
| `go test -tags=chaos ./tests/chaos/... -count=1 -timeout 10m` | green | [`test-chaos.txt`](../../tests/baseline/post-fix-47/test-chaos.txt) |
| `staticcheck ./...` | red baseline, без новых findings по fix-файлам | [`staticcheck.txt`](../../tests/baseline/post-fix-47/staticcheck.txt) |
| `golangci-lint run` | red baseline, не хуже Phase 8 | [`golangci-lint.txt`](../../tests/baseline/post-fix-47/golangci-lint.txt) |
| `gosec ./...` | red baseline, 55 -> 55 findings | [`gosec.txt`](../../tests/baseline/post-fix-47/gosec.txt) |
| `govulncheck ./...` | green, `No vulnerabilities found.` | [`govulncheck.txt`](../../tests/baseline/post-fix-47/govulncheck.txt) |

Статус реестра: п. 47 закрыт regression anchor; новых побочных пунктов не добавлено. Следующий отдельный фикс: п. 18 `ipmonitor fail-closed`.

## Fix п. 48 2026-05-24

### Fix п. 48 design

- Воспроизведение Step A: `go test ./... -race -count=1 -timeout 15m` red; лог: [`tests/baseline/post-fix-48/test-race-before.txt`](../../tests/baseline/post-fix-48/test-race-before.txt).
- Конкретный stack: `service.flushTokenUseUpdates()` / `flushTokenUseBatch()` из timer goroutine читает `database.GetDB()` и GORM state, пока `api.initSessionTestDB()` вызывает `database.InitDB()` и `database.OpenDB()` присваивает новый package-level `db`.
- Root cause: API/perf test helpers вызывают `database.InitDB()` напрямую, минуя `database.ResetCaches(ctx)`, поэтому hook `service.token_use_debouncer` из п. 47 не входит в этот lifecycle.
- Выбран вариант 2: test-helper hardening. Перед API `InitDB` выполняется `service.StopTokenUseDebouncer(context.Background())`, затем открывается новый test DB handle.
- Дополнительно минимально усилен сам `StopTokenUseDebouncer`: теперь он использует exclusive flush gate, как reset hook п. 47, чтобы дождаться уже стартовавшего timer flush, force-flush'ить pending и оставить quiet window на время DB reinit.
- Вариант 1 с production `InitDB` hook отвергнут как более широкий contract/lifecycle change: он запускал бы все `ResetCaches` hooks на каждом `InitDB`, а проблема локализована в API test lifecycle.
- Вариант 3 с GetDB-aware batch drop отвергнут как изменение semantics debouncer write path; batch loss при handle mismatch не нужен для закрытия текущей гонки.

### Fix п. 48 results

Артефакты: [`tests/baseline/post-fix-48/`](../../tests/baseline/post-fix-48/).

| Команда | Статус | Лог |
|---|---:|---|
| before: `go test ./... -race -count=1 -timeout 15m` | red, race reproduced in `api.initSessionTestDB()` | [`test-race-before.txt`](../../tests/baseline/post-fix-48/test-race-before.txt) |
| targeted API race 10/10 | green | [`test-race-targeted-after.txt`](../../tests/baseline/post-fix-48/test-race-targeted-after.txt) |
| anchor 47 after: `go test -race -tags=chaos -run TestTokenUseDebouncerRaceVsDBReinitChaosIssue47 ./tests/chaos/... -count=10` | green, 10/10 | [`anchor-47-after.txt`](../../tests/baseline/post-fix-48/anchor-47-after.txt) |
| anchor 48 after: `go test -race -tags=chaos -run TestTokenUseDebouncerRaceVsAPITestLifecycleIssue48 ./tests/chaos/... -count=10` | green, 10/10 | [`anchor-48-after.txt`](../../tests/baseline/post-fix-48/anchor-48-after.txt) |
| `go build ./...` | green | [`build.txt`](../../tests/baseline/post-fix-48/build.txt) |
| `go vet ./...` | green | [`vet.txt`](../../tests/baseline/post-fix-48/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | [`test.txt`](../../tests/baseline/post-fix-48/test.txt) |
| `go test ./... -race -count=1 -timeout 15m` | green | [`test-race.txt`](../../tests/baseline/post-fix-48/test-race.txt) |
| race reruns 1..3 | green, 3/3 | [`test-race-rerun-1.txt`](../../tests/baseline/post-fix-48/test-race-rerun-1.txt), [`test-race-rerun-2.txt`](../../tests/baseline/post-fix-48/test-race-rerun-2.txt), [`test-race-rerun-3.txt`](../../tests/baseline/post-fix-48/test-race-rerun-3.txt) |
| `go test -tags=chaos ./tests/chaos/... -count=1 -timeout 10m` | green | [`test-chaos.txt`](../../tests/baseline/post-fix-48/test-chaos.txt) |
| `staticcheck ./...` | red baseline, 31 findings + wrapper header; no new effective findings | [`staticcheck.txt`](../../tests/baseline/post-fix-48/staticcheck.txt) |
| `golangci-lint run` | red baseline, same 222-line finding set + wrapper header | [`golangci-lint.txt`](../../tests/baseline/post-fix-48/golangci-lint.txt) |
| `gosec ./...` | red baseline, 55 -> 55 issues | [`gosec.txt`](../../tests/baseline/post-fix-48/gosec.txt) |
| `govulncheck ./...` | green, `No vulnerabilities found.` | [`govulncheck.txt`](../../tests/baseline/post-fix-48/govulncheck.txt) |

Примечание: промежуточный уточняющий прогон старого anchor п. 47 сохранён как [`anchor-47-after-red-1.txt`](../../tests/baseline/post-fix-48/anchor-47-after-red-1.txt); он показал, что `StopTokenUseDebouncer` не должен force-flush'ить во время уже активного reset quiet window. Финальный `anchor-47-after.txt` green.

## Fix Cluster D 2026-05-24

### Design

П. 32, `frontend/src/store/ws.ts`: `startFallback()` уже является единственной точкой входа в degraded polling после отсутствия token, open-timeout и серии close. Минимальная правка: в существующем fallback interval оставить `loadData()` и добавить healing-попытку `connect()` в том же ритме `fallbackPollMs`. Попытка не стартует, если уже есть `reconnectTimer` или активный `ws`, чтобы не создавать параллельные dial'ы. Отдельный public API и новый timer не нужны: cooldown получается естественно через один interval tick, а успешный `onopen` уже вызывает `setState('connected')` и `stopFallback()`. Альтернатива с отдельным exponential backoff отвергнута как более широкое изменение runtime policy. Альтернатива с немедленным reconnect при входе в fallback отвергнута, чтобы сохранить текущий degraded-polling backpressure после серии отказов.

П. 33, `api/realtime.go`: `consumeWSToken()` должен пройти по всем token digest без match-зависимых ветвлений и удалить ровно один выбранный ключ после цикла. Минимальная правка: собрать keys, отсортировать их по digest bytes для стабильного порядка, затем в цикле считать `eq := subtle.ConstantTimeCompare`, копировать `matchedKey` через `subtle.ConstantTimeCopy` и выбирать `matchedData` через `subtle.ConstantTimeSelect` для unix-nano expiry и user index. `matched` накапливается через `ConstantTimeSelect`, а `delete(wsTokens.tokens, matchedKey)` выполняется один раз безусловно после цикла; zero key безопасен при miss. Проверка expiry остаётся обычной после match, потому что expired-token timing не является целью п. 33. Альтернатива с delete внутри match или ранним break отвергнута как источник position-dependent timing.

### Results

Артефакты: [`tests/baseline/post-fix-cluster-D/`](../../tests/baseline/post-fix-cluster-D/).

| Команда | Статус | Лог |
|---|---:|---|
| anchor 33 before: `go test -run "TestConsumeWSTokenTimingRegressionAnchor_XFAILIssue33" ./api/... -count=1 -timeout 2m` | green command, source XFAIL/skip | [`anchor-33-before.txt`](../../tests/baseline/post-fix-cluster-D/anchor-33-before.txt) |
| anchor 32 before: Vitest `ws.spec.ts` | green current-behavior anchor; literal `npm --prefix` path mismatch also saved | [`vitest-ws-before.txt`](../../tests/baseline/post-fix-cluster-D/vitest-ws-before.txt), [`vitest-ws-before-filter-mismatch.txt`](../../tests/baseline/post-fix-cluster-D/vitest-ws-before-filter-mismatch.txt) |
| anchor 32 before: Playwright `ws-reconnect-chaos.spec.ts` | expected-fail counted as passed | [`playwright-ws-reconnect-before.txt`](../../tests/baseline/post-fix-cluster-D/playwright-ws-reconnect-before.txt) |
| anchor 32 after: Vitest `ws.spec.ts` | green, 7/7 | [`vitest-ws-after.txt`](../../tests/baseline/post-fix-cluster-D/vitest-ws-after.txt) |
| anchor 32 after: Playwright chaos | green, 1/1 | [`playwright-ws-reconnect-after.txt`](../../tests/baseline/post-fix-cluster-D/playwright-ws-reconnect-after.txt) |
| API package after: `go test ./api/... -count=1` | green | [`test-api.txt`](../../tests/baseline/post-fix-cluster-D/test-api.txt) |
| anchor 33 after: `go test -run "TestConsumeWSTokenTimingRegressionAnchor" ./api/... -count=10 -timeout 5m` | green, 10/10 | [`anchor-33-after.txt`](../../tests/baseline/post-fix-cluster-D/anchor-33-after.txt) |
| `go build ./...` | green | [`build.txt`](../../tests/baseline/post-fix-cluster-D/build.txt) |
| `go vet ./...` | green | [`vet.txt`](../../tests/baseline/post-fix-cluster-D/vet.txt) |
| `go test ./... -count=1 -timeout 5m` | green | [`test.txt`](../../tests/baseline/post-fix-cluster-D/test.txt) |
| `go test ./... -race -count=1 -timeout 15m` | green | [`test-race.txt`](../../tests/baseline/post-fix-cluster-D/test-race.txt) |
| `go test -tags=chaos ./tests/chaos/... -count=1 -timeout 10m` | green | [`test-chaos.txt`](../../tests/baseline/post-fix-cluster-D/test-chaos.txt) |
| `staticcheck ./...` | red baseline, no new Cluster D production findings | [`staticcheck.txt`](../../tests/baseline/post-fix-cluster-D/staticcheck.txt) |
| `golangci-lint run` | red baseline, no new Cluster D production findings | [`golangci-lint.txt`](../../tests/baseline/post-fix-cluster-D/golangci-lint.txt) |
| `gosec ./...` | red baseline, 55 -> 55 issues | [`gosec.txt`](../../tests/baseline/post-fix-cluster-D/gosec.txt) |
| `govulncheck ./...` | green, `No vulnerabilities found.` | [`govulncheck.txt`](../../tests/baseline/post-fix-cluster-D/govulncheck.txt) |
| full Vitest: `npm --prefix frontend run test` | green, 15 files / 76 tests | [`vitest-full.txt`](../../tests/baseline/post-fix-cluster-D/vitest-full.txt) |
| full Playwright e2e | green command, 9 passed / 4 skipped; Cluster D chaos WS scenario green | [`playwright-full.txt`](../../tests/baseline/post-fix-cluster-D/playwright-full.txt) |

Коммиты:
- `5b1a7ac` `fix(frontend/ws): healing reconnect from degraded fallback (registry #32)`
- `58a3d3c` `fix(api/realtime): constant-time consumeWSToken match-and-delete (registry #33)`

Примечание: старый Phase 6 placeholder `frontend/tests/e2e/ws-reconnect.spec.ts` остаётся `test.fixme`, потому что этот файл не входил в разрешённый scope Cluster D. Targeted WS chaos scenario из этого кластера снят с XFAIL и проходит green.

## Post-fix Cluster F 2026-05-24

### Коммиты

- `50631af` — fix(service/audit-writer): prioritize warn/security on overflow eviction (registry #16)
- `c9de793` — fix(service/secret-settings): suppress redundant secretbox fallback audit (registry #17)

Cluster F закрыл audit pipeline пункты 16 и 17 двумя production-коммитами. Frontend и зависимости не затрагивались.

### Дельта по реестру

- П. 16 «Audit integrity» — closed by Cluster F. `auditWriter.push()` приоритизирует `warn`/security при overflow eviction; `info` дропается первым, а high-severity события удерживаются до предела `auditQueueCapacity`. Severity priority anchor `TestAuditWriterOverloadSeverityPriorityAnchorIssue16Phase5` переписан под post-fix инвариант и проходит 10/10.
- П. 17 «Audit noise» — closed by Cluster F. `recordSecretboxFallback()` больше не пишет audit на прямом legacy decrypt path; primary candidate decrypt по-прежнему не порождает `settings_secretbox_key_fallback`. XFAIL anchor `TestLegacySecretboxFallbackDoesNotAuditAfterFix_XFAILIssue17` снят и проходит 10/10.

### Команды и логи

См. секцию `## Post-fix Cluster F 2026-05-24` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-cluster-F/`.

## Post-fix Cluster B 2026-05-24

### Коммиты

- `aae3ed5` — fix(service/runtime): serialize core start cooldown access (registry #20)
- `3a14e46` — fix(service/telegram): single-flight telegram http client swap (registry #21)

Cluster B закрыл concurrency пункты 20 и 21 двумя production-коммитами. Frontend и зависимости не затрагивались.

### Дельта по реестру

- П. 20 «Concurrency / Runtime core start cooldown» — closed by Cluster B. Чтения/записи `lastStartFailTime` синхронизированы через `r.mu`. Новый race-anchor с подстрокой `Issue20` в имени GREEN под `-race` 10/10.
- П. 21 «Concurrency / Telegram HTTP client swap» — closed by Cluster B. `getTelegramHTTPClient` использует double-checked locking при подмене и создаёт новый client под `telegramHTTPClientMu`, чтобы concurrent miss возвращал один опубликованный client. `sync.Once` не добавлялся: default client остаётся eager-initialized package-level значением, совместимым с `setTelegramHTTPClient`, а риск гонки закрыт single-flight mutex path. Новый race-anchor с подстрокой `Issue21` в имени GREEN под `-race` 10/10.

### Команды и логи

См. секцию `## Post-fix Cluster B 2026-05-24` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-cluster-B/`.

## Post-fix Cluster C 2026-05-24

### Коммиты

- `0464e17` — fix(cronjob/xui-sync): record sanitized lastErr in failed sync summary (registry #4)
- `c5d45bf` — fix(cronjob/xui-sync): exponential backoff between sync retries (registry #40)
- `d468daa` — fix(cronjob/xui-sync): treat success persist failure as warn-only (registry #41)

Cluster C закрыл cron observability пункты 4, 40 и 41 тремя production-коммитами в `cronjob/xuiSyncJob.go`. Frontend и зависимости не затрагивались.

### Дельта по реестру

- П. 4 «Observability / xui sync failed summary» — closed by Cluster C. `last_run_summary` для failed-runs теперь содержит sanitized `lastErr.Error()` и `errorClass` вместо generic `{"error":"failed"}`. Anchor `TestXUISyncJobExtraFailureSummaryIncludesLastErrIssue4` снят с XFAIL и GREEN 10/10.
- П. 40 «Cron policy / sync retry backoff» — closed by Cluster C. Расписание sleep’ов между попытками заменено с линейного `attempt * 100ms` на экспоненциальное `200ms → 1s` через package-level таблицу `xuiSyncBackoff`. Anchor `TestXUISyncJobLostNetworkBackoffAnchorIssue40Phase5` обновлён под новый контракт и GREEN 10/10.
- П. 41 «Cron audit / success persist failure» — closed by Cluster C. Success-ветка `RunProfile` логирует ошибку persist как warn и возвращает `nil`; новый anchor `TestXUISyncJobSuccessReturnsNilOnPersistFailureIssue41` GREEN 10/10.

### Команды и логи

См. секцию `## Post-fix Cluster C 2026-05-24` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-cluster-C/`.

## Post-fix Cluster G 2026-05-25

### Коммиты

- `1976a4f` — fix(database/backup): make sighup timeout configurable via env (registry #10)
- `8c7cd3b` — fix(database/backup): fallback to full WAL checkpoint on truncate failure (registry #11)
- `223c082` — fix(database/backup): warn on missing settings.config in versioned backup (registry #12)

Cluster G закрыл backup safety пункты 10, 11, 12 тремя production-коммитами в `database/backup.go`. Frontend и зависимости не затрагивались.

### Дельта по реестру

- П. 10 «Reliability / SIGHUP timeout» — closed by Cluster G. Timeout теперь конфигурируется через `SUI_SIGHUP_TIMEOUT_SECONDS` env var (1–60s, fallback 3s). Anchor `TestSendSighupRespectsConfiguredTimeoutIssue10` GREEN 10/10.
- П. 11 «Backup robustness / WAL checkpoint» — closed by Cluster G. `GetDb` теперь делает TRUNCATE → FULL → warning fallback вместо фатальной ошибки на TRUNCATE. Anchor `TestWALCheckpointFallback*Issue11` GREEN 10/10.
- П. 12 «Backup / versioned config validation» — closed by Cluster G. `validateVersionedBackupConfig` логирует warning и продолжает при отсутствии `settings.config` вместо отказа. Pre-fix anchor `TestImportDBRejectsVersionedBackupWithoutConfig` переименован в `TestImportDBAcceptsVersionedBackupWithoutConfigIssue12` и переписан под post-fix контракт. Anchor `TestValidateVersionedBackupConfigSoftensMissingConfigIssue12` GREEN 10/10.

### Команды и логи

См. секцию `## Post-fix Cluster G 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-cluster-G/`.

## Post-fix Cluster I 2026-05-25

### Коммиты

- `c782e4b324b85e0848d5c2607473dbeba4c5e4f2` — fix(service/telegram): cancel notifier backoff on stop (registry #22)
- `6610062fa9eb5af9eac38140c191fcd7b42fea89` — fix(service/telegram-backup): harden secret zeroization paths (registry #25)
- `655d7017b92f5119411e17661af1dd897794433f` — fix(service/warp): centralize authorized client headers (registry #31)

Cluster I закрыл Telegram/WARP robustness пункты 22, 25 и 31 тремя production-коммитами в `service/`. Frontend и зависимости не затрагивались.

### Дельта по реестру

- П. 22 «Telegram retry» — closed by Cluster I. `telegramNotifier` получил stop-aware backoff на `time.NewTimer` и `stopCh`; `Stop` закрывает stop channel один раз и прерывает pending retry sleep. Anchor `TestTelegramNotifierStopCancelsBackoffIssue22` GREEN 10/10.
- П. 25 «Backup leak risk» — closed by Cluster I. `RunOnce` передаёт payload/passphrase во владение private `telegramBackupSecretBag`, зануляет passphrase сразу после build envelope и payload после появления envelope. Secret-bag и oversize audit anchors GREEN 10/10.
- П. 31 «Endpoint warp» — closed by Cluster I. WARP authorized headers централизованы в `setWarpAuthorizedHeaders`, `SetWarpLicense` de-dupes preferred API version, request-capture tests проверяют headers, Authorization и JSON body. Anchors GREEN 10/10.

### Команды и логи

См. секцию `## Post-fix Cluster I 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-cluster-I/`.

## Post-fix Cluster H 2026-05-25

### Коммиты

- `baba54e80b56df8cb4b749734b6dd3206c77f5e2` — fix(frontend/migrate-xui): show apply failure inline (registry #43)
- `ebf2b39bc9e8dbd71909ec5f4ae98ec4eb018fa8` — fix(frontend/migrate-xui): wait for rollback health before reload (registry #44)
- `e733112e0634b0f723b7e5d8ec4fa02fa0e5f4f4` — fix(frontend/migrate-xui): require reveal for generated admins (registry #45)

Cluster H закрыл MigrateXui UX пункты 43, 44, 45. Backend contract, dependencies, `Endpoint.vue`, `go.mod`, `go.sum` и package manifests не затрагивались.

### Дельта по реестру

- П. 43 «MigrateXui apply error UX» — closed by Cluster H. Apply failure now returns to review with inline error details and preserves selected plan state. Playwright Issue43 anchor GREEN.
- П. 44 «MigrateXui rollback reload race» — closed by Cluster H. Rollback success now polls `api/status?r=db` before reload instead of fixed 1s sleep. Playwright Issue44 anchor GREEN.
- П. 45 «Generated admins leakage» — closed by Cluster H. Generated admin passwords are hidden until explicit reveal and auto-cleared after a timer. Playwright Issue45 anchor GREEN.

### Команды и логи

См. секцию `## Post-fix Cluster H 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-cluster-H/`. Targeted Cluster H Playwright GREEN; full Playwright сохранён как red exception на unrelated `frontend/tests/e2e/ws-reconnect-chaos.spec.ts` (`page.evaluate: Execution context was destroyed` during offline toggle), подтверждён отдельным singleton rerun artifact.

## Post-fix Singleton #23 2026-05-25

### Коммиты

- `5bcef1ac4658bfa27986e9aec27771030739507e` — fix(service/server): harden system info interface parsing (registry #23)

Singleton #23 закрыл crash-risk в `ServerService.GetSystemInfo()` при коротких flags/address данных. #24 confidentiality filtering не менялся и остаётся open.

### Дельта по реестру

- П. 23 «GetSystemInfo IPv6-only crash» — closed. Interface flags теперь проверяются по содержимому, а не по позициям; короткие/пустые addresses больше не panic'ят. Package-local Issue23 anchor GREEN 10/10.
- П. 24 «GetSystemInfo confidentiality» — unchanged/open; private address filtering не входил в singleton #23.

### Команды и логи

См. секцию `## Post-fix Singleton #23 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-23/`.

## Post-fix Singleton #24 2026-05-25

### Коммиты

- `c208edcec7c07f3807b6a1a6af09fc59287a32ee` — fix(service/server): filter non-public system info addresses (registry #24)

Singleton #24 закрыл confidentiality gap в `ServerService.GetSystemInfo()`: `ipv4`/`ipv6` сохраняют shape `[]string`, но private/link-local/non-routable interface addresses больше не возвращаются.

### Дельта по реестру

- П. 24 «GetSystemInfo confidentiality» — closed. `GetSystemInfo` фильтрует private, link-local, loopback, unspecified, multicast и invalid interface addresses; public IPv4/IPv6 остаются в прежних keys. Package-local Issue24 anchor GREEN 10/10.
- П. 23 regression anchor остаётся GREEN 10/10.

### Команды и логи

См. секцию `## Post-fix Singleton #24 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-24/`.

## Post-fix Singleton #36 2026-05-25

### Коммиты

- `2ee7d7de84bae65dc5dc90ad3b9c59183007dc14` — fix(api/import-xui): bound xui rate-limit cache (registry #36)

Singleton #36 закрыл DoS-риск в `enforceXUIRateLimit()`: `xuiRates` больше не может расти без верхней границы от потока уникальных anonymous/IP keys. Existing 5-per-minute quota and 429 behavior preserved.

### Дельта по реестру

- П. 36 «xui rate-limit map unbounded» — closed. Package-local Issue36 anchor verifies bounded map length under unique-IP pressure and stale bucket pruning. Existing `ImportXUIReportsRateLimit` anchor remains GREEN 10/10.
- `tests/chaos/xui_rate_limit_unbounded_test.go` не менялся; deferred chaos/XFAIL остается отдельной задачей.

### Команды и логи

См. секцию `## Post-fix Singleton #36 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-36/`.

## Post-fix Singleton #19 2026-05-25

### Коммиты

- `14c4f8d3d3f81f43e0f7f2614fc5f00832798b63` — fix(service/settings): make default initialization idempotent (registry #19)

Singleton #19 закрыл startup race в `SettingService.GetAllSetting()`: default settings теперь вставляются через atomic `INSERT ... WHERE NOT EXISTS` statements внутри transaction before readback. Existing values are preserved and the returned settings shape is unchanged.

### Дельта по реестру

- П. 19 «SettingService.GetAllSetting startup race» — closed. Package-local Issue19 anchor runs concurrent first-start initialization and verifies one row per default key.
- No DB schema/index/dependency/frontend changes.

### Команды и логи

См. секцию `## Post-fix Singleton #19 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-19/`.

## Post-fix Singleton #34 2026-05-25

### Коммиты

- `6c4ed85685f73d7cc3933541c879d71eea26a698` — fix(api/auth): enforce legacy token header sunset (registry #34)

Singleton #34 закрыл enforcement gap для legacy API token header: `Authorization: Bearer` остаётся canonical path, legacy `Token` продолжает работать до published Sunset with warnings, and after `Sat, 15 Aug 2026 00:00:00 GMT` it is rejected with a targeted 401.

### Дельта по реестру

- П. 34 «apiTokenFromRequest legacy Token header» — closed. Issue34 anchors cover post-Sunset rejection and Bearer precedence after Sunset.
- Generic invalid/expired token HTTP behavior remains unchanged outside the expired legacy header case.
- No frontend/dependency/schema changes.

### Команды и логи

См. секцию `## Post-fix Singleton #34 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-34/`.

## Post-fix Singleton #35 2026-05-25

### Коммиты

- `f97a993c18c6a39b044031be79e88c55494fe7d5` — fix(api/routes): share import-xui route registration (registry #35)

Singleton #35 закрыл route drift risk для import-xui endpoints: v1 `/api` and v2 `/apiv2` now use one package-local route registry. Auth middleware semantics are unchanged; the fix only centralizes endpoint registration.

### Дельта по реестру

- П. 35 «duplicate import-xui route registration» — closed. Issue35 anchor verifies every shared route exists under both `/api` and `/apiv2`, and `POST /apiv2/import-xui` is explicit rather than catch-all driven.
- Existing v1/v2 auth-surface distinction remains covered by updated security authz anchor.
- No frontend/dependency/schema changes.

### Команды и логи

См. секцию `## Post-fix Singleton #35 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-35/`.

## Post-fix Singleton #30 2026-05-25

### Коммиты

- `e9711e5e9a1b630e95f7dfb5a44f47f03c40233e` — fix(service/settings): reject unsafe optional URLs (registry #30)
- `1720f6c6253ad6aa5f7203dfcf49123254b38759` — fix(service/settings): reject edge control chars in optional URLs (registry #30)

Singleton #30 закрыл validation gap in `validateOptionalHTTPURL()`: optional subscription/support/profile URLs reject fragments and control characters, while ordinary http/https URLs with query strings remain accepted. Continuation `1720f6c6253ad6aa5f7203dfcf49123254b38759` moves the raw control-character check before trimming and adds leading newline plus trailing CRLF/tab anchors.

### Дельта по реестру

- П. 30 «validateOptionalHTTPURL fragments/control chars» — closed. Issue30 anchor no longer XFAIL and covers fragments/control-character rejects plus query acceptance.
- Reviewer continuation covered leading/trailing raw control characters that `strings.TrimSpace` could previously remove before validation.
- Existing userinfo and non-http scheme rejects remain covered.
- No frontend/dependency/schema changes.

### Команды и логи

См. секцию `## Post-fix Singleton #30 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-30/`.

## Post-fix Singleton #29 2026-05-25

### Коммиты

- `4067f4989156cc0ac402e6f0225b306832d48bd8` — fix(service/update): use etag for release checks (registry #29)

Singleton #29 закрыл update-check cache gap: after the existing hourly cache expires, release checks send `If-None-Match` when an ETag is known and preserve cached release metadata on `304 Not Modified`.

### Дельта по реестру

- П. 29 «fetchLatestRelease ETag» — closed. Issue29 anchor verifies first 200 response stores ETag, next expired-cache request sends `If-None-Match`, and 304 keeps previous latest/releaseURL without an extra full fetch.
- Existing fail-soft and in-memory hourly cache behavior remains covered.
- No frontend/dependency/schema changes.

### Команды и логи

См. секцию `## Post-fix Singleton #29 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-29/`.

## Post-fix Singleton #27 2026-05-25

### Коммиты

- `4904b5a52fbf57c50cdb6cb22fa018d69c5c7cad` — fix(service/tokens): preserve legacy token enabled state (registry #27)

Singleton #27 закрыл token migration regression: plaintext legacy API token migration no longer force-enables rows and instead preserves the stored `enabled` flag.

### Дельта по реестру

- П. 27 «legacy token enabled state» — closed. Issue27 anchor verifies a disabled plaintext legacy token remains disabled while hash/prefix/scope metadata are populated.
- Existing enabled legacy migration behavior remains covered by `TestLoadTokensMigratesLegacyPlaintextToken`.
- No frontend/dependency/schema changes.

### Команды и логи

См. секцию `## Post-fix Singleton #27 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-27/`.

## Post-fix Singleton #26 2026-05-25

### Коммиты

- `c8794a8d8fab38eeb8109b6332152786cf1cc11a` — fix(service/stats): audit stats commit failures (registry #26)

Singleton #26 закрыл observability gap in `StatsService.SaveStats()`: commit failures now create an audit record while preserving the existing returned error and realtime warning behavior.

### Дельта по реестру

- П. 26 «StatsService commit failure observability» — closed. Issue26 anchor verifies returned commit error, `stats_commit_failed` audit event, existing `core_state` warning realtime event, no normal stats realtime events, and no committed stats rows on failed commit.
- No frontend/dependency/schema changes.

### Команды и логи

См. секцию `## Post-fix Singleton #26 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-26/`.

## Post-fix Singleton #28 2026-05-25

### Коммиты

- `322095ae7497976e792cd78eff77954c1adab7d9` — fix(service/tokens): add token-use flush circuit breaker (registry #28)

Singleton #28 закрыл token-use flush resilience gap: timer write failures no longer retry on the normal cadence, failed updates stay pending, and newer per-token usage wins before retry.

### Дельта по реестру

- П. 28 «tokenUseDebouncer circuit breaker» — closed. Issue28 anchors verify timer failure opens a circuit/backoff, blocks immediate retry, retries after cooldown, and preserves the latest update for each token id.
- Existing token-use latest-value flush and #47/#48 race lifecycle anchors remain green.
- No frontend/dependency/schema changes; `tests/chaos/**` was not modified.

### Команды и логи

См. секцию `## Post-fix Singleton #28 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-28/`.

## Post-fix Singleton #38 2026-05-25

### Коммиты

- `fb760200ce68c00ae3357ddc1515fa1346897f0e` — fix(api/import-xui): clean stale upload temp dirs (registry #38)

Singleton #38 закрыл temp lifecycle gap in `saveXUIUpload()`: old leftover `xui-import-*` directories are cleaned from the temp root before new upload staging, while fresh/active uploads and unrelated temp entries are preserved.

### Дельта по реестру

- П. 38 «x-ui upload temp cleanup» — closed. Issue38 anchors verify stale import upload dirs are removed, fresh/import-unrelated entries are preserved, and `saveXUIUpload()` triggers cleanup while still accepting a valid SQLite upload.
- No frontend/dependency/schema/route changes.

### Команды и логи

См. секцию `## Post-fix Singleton #38 2026-05-25` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-38/`.

## Post-fix Singleton #39 2026-05-26

### Коммиты

- `c409532b2d198acbe52f6b96f1c963ea295149fa` — fix(api/import-xui): publish rollback realtime event (registry #39)

Singleton #39 закрыл rollback realtime gap in `ImportXuiRollback()`: successful rollback now preserves the existing `xui_import_rollback` audit record and publishes `config_invalidated` before returning the unchanged `import-xui` success response.

### Дельта по реестру

- П. 39 «ImportXuiRollback realtime event» — closed. Issue39 anchors verify successful rollback emits `config_invalidated`, keeps the warn audit with backup basename, and invalid backup failure does not publish a realtime event.
- New anchor lives in `api/import_xui_rollback_realtime_test.go`; pre-existing dirty `api/import_xui_test.go` lifecycle diff was intentionally not edited, staged, or committed.
- No frontend/dependency/schema/route changes.

### Команды и логи

См. секцию `## Post-fix Singleton #39 2026-05-26` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-39/`.

## Post-fix Singleton #37 2026-05-26

### Коммиты

- `a4127632509083c98cd32250663bd6484df682d9` — fix(api/import-xui): stream large apply plans (registry #37)

Singleton #37 закрыл apply-plan size contract gap in `ImportXuiApply()`: multipart `plan` parts are streamed to per-request temp storage and decoded from file, while the existing 200MiB aggregate import cap and 8MiB limit for other text fields remain in force.

### Дельта по реестру

- П. 37 «ImportXuiApply streamed plan» — closed. Issue37 anchor verifies a valid plan larger than 8MiB is accepted through the existing multipart `plan` field and applied successfully.
- Existing compatibility behavior remains covered: malformed 9MiB plan fields still return 413 `payload_too_large`, and stale plans still return the existing bad-request path.
- New anchor lives in `api/import_xui_plan_stream_test.go`; pre-existing dirty `api/import_xui_test.go` lifecycle diff was intentionally not edited, staged, or committed.
- No frontend/dependency/schema/route changes.

### Команды и логи

См. секцию `## Post-fix Singleton #37 2026-05-26` в `tests/baseline/SUMMARY.md` и артефакты в `tests/baseline/post-fix-37/`.
