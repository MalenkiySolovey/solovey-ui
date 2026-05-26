# S-UI v1.5.5-beta4

Prerelease focused on runtime stability, data safety, security hardening,
3x-ui import correctness and smoother admin workflows.

## Highlights

- Runtime stability: Telegram client refresh, notifier retry timers, core
  restart cooldowns and token-use flushing are synchronized to avoid data races
  during load, restarts and database lifecycle changes.
- Security and confidentiality: WebSocket token consumption is hardened against
  timing leaks, the legacy `Token` API header now has an enforced Sunset,
  system info hides private/link-local addresses, Telegram backup secrets are
  zeroed in memory, and optional URL settings reject unsafe input.
- Data integrity: startup default insertion is idempotent, migration/adapt
  failures stop startup, TLS delete/read/commit failures are surfaced, and
  backup/restore is more tolerant of slow disks and older unusual databases.
- 3x-ui import and cron sync: `reset_required` is durable through
  `users.force_password_reset`, sync profiles now honor saved import policies,
  large apply plans stream from temp storage, stale import temp directories are
  cleaned up, and rollback publishes realtime invalidation.
- Admin UI: MigrateXui keeps apply errors visible, waits for rollback health
  before reload and hides generated admin passwords until explicit reveal.
  Endpoint save now blocks double-submit attempts and clears loading state after
  failed saves.

## Packaging

- Version metadata and Release, Windows and Docker workflow defaults now target
  `v1.5.5-beta4`.

## Validation

- `go build ./...` - PASS
- `go vet ./...` - PASS
- `go test ./...` - PASS
- `go test -race ./... -timeout 900s` - PASS
- `govulncheck ./...` - PASS, no vulnerabilities found
- `gosec ./...` remains the known classified baseline with 55 issues

## Install

```sh
bash <(curl -Ls https://raw.githubusercontent.com/deposist/s-ui-x/main/install.sh) v1.5.5-beta4
```

## Русский

Предрелиз про стабильность runtime, безопасность, целостность данных, корректный
импорт 3x-ui и более спокойный admin workflow.

- Синхронизированы Telegram client refresh, retry-таймеры notifier, cooldown
  перезапуска ядра и token-use flush, чтобы убрать гонки при нагрузке,
  рестартах и переинициализации БД.
- Усилены токены и приватность: WS-token consumption защищён от timing leaks,
  legacy `Token` header получил enforced Sunset, system info скрывает
  private/link-local адреса, секреты Telegram backup зануляются в памяти, а
  optional URL settings отбрасывают unsafe input.
- Улучшена целостность данных: дефолтные settings вставляются идемпотентно,
  ошибки migration/adapt останавливают startup, тихие DB/read/commit ошибки
  стали видимыми, backup/restore устойчивее к медленным дискам и старым БД.
- 3x-ui import и cron sync теперь исполняют сохранённые политики:
  `reset_required` хранится через `users.force_password_reset`, большие
  apply-планы читаются потоково, stale temp import directories очищаются, а
  rollback публикует realtime invalidation.
- MigrateXui показывает apply errors inline, ждёт health после rollback и
  скрывает сгенерированные admin-пароли до явного reveal. Endpoint save
  защищён от double-submit и корректно сбрасывает loading state после ошибки.
