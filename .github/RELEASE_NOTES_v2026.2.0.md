# Solovey UI 2026.2.0

Compared with `v2026.1.0`.

## English

- Introduced the optional component install model for release builds: full/core
  binaries, a compact component bundle, installer profiles (`full`,
  `minimal/core`) and custom `--with` / `--without` component selection.
- Moved optional feature areas behind component boundaries: panel import,
  remote outbound subscriptions, paid subscriptions, Telegram, observability
  extras, and panel update UI now register their routes, settings, jobs,
  database tables, CLI entries, and frontend entries through the component host.
- Added generated backend component composition and frontend component registry
  loading so the core panel does not need direct edits for every optional
  feature.
- Synced selected upstream fixes from `deposist/s-ui-x v1.5.10-beta9`: stale
  settings rows no longer round-trip into saves, and RU/ZH regional presets now
  use country-direct behavior with managed RU `geosite-ru-smart` `.srs`
  preparation.
- Synced the `alireza0/s-ui v1.5.1` security direction by updating sing-box
  integration to `v1.13.14` and auditing the TLS certificate pin conflict fix
  against Solovey UI's TLS/outbound generation path.
- Updated public documentation and release defaults for the `2026.2.0` version
  scheme.

## Русский

- Добавлена компонентная модель установки для релизных сборок: full/core
  бинарники, один компактный component bundle, профили установщика (`full`,
  `minimal/core`) и выбор компонентов через `--with` / `--without`.
- Optional-функции вынесены за компонентные границы: импорт панелей, remote
  outbound-подписки, платные подписки, Telegram, расширенная observability и UI
  обновлений регистрируют routes, settings, jobs, database tables, CLI и
  frontend-входы через component host.
- Добавлена генерация backend component composition и загрузка frontend
  component registry, чтобы ядро панели не требовало прямых правок под каждый
  optional-компонент.
- Перенесены выбранные исправления из `deposist/s-ui-x v1.5.10-beta9`: stale
  settings rows больше не возвращаются в Settings save, а RU/ZH regional presets
  работают в режиме country-direct с подготовкой managed RU
  `geosite-ru-smart` `.srs`.
- Учтено направление безопасности `alireza0/s-ui v1.5.1`: интеграция sing-box
  обновлена до `v1.13.14`, а исправление конфликта TLS certificate pin сверено с
  текущей логикой TLS/outbound в Solovey UI.
- Обновлены публичная документация и релизные defaults под версию `2026.2.0`.
