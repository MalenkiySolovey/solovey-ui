# Solovey UI 2026.2.0

Compared with `v2026.1.0`.

## English

- Added a real optional component system: components register backend routes,
  jobs, settings, database hooks, frontend entries, and lifecycle behavior only
  when installed and enabled.
- Added component-aware release packaging: full binary, core binary, one
  component bundle, release manifest, and compatibility metadata.
- Added update UI support for component availability, required panel versions,
  install/remove, enable/disable, and explicit component data deletion.
- Hardened component boundaries: core packages and core tests no longer import
  concrete component packages; generic host behavior is tested with fixtures.
- Improved remote outbound subscriptions: normalized collected profile data,
  group conversion, delay checks, bulk groups, and safer synchronization into
  panel outbounds and client exports.
- Improved frontend drag-and-drop selection and placement indicators across
  Nexus and classic layouts.
- Updated Docker and release automation for multi-platform images and modular
  release artifacts.
- Fixed CI regressions from the public documentation cleanup: policy tests no
  longer depend on ignored local docs, and frontend E2E startup now has longer
  CI readiness budget plus managed-server logs in artifacts.
- Cleaned public documentation and release defaults for the `2026.2.0` line.

## Русский

- Добавлена настоящая система optional components: компоненты регистрируют
  backend routes, jobs, settings, database hooks, frontend entries и lifecycle
  только когда они установлены и включены.
- Добавлена компонентная релизная упаковка: full binary, core binary, единый
  component bundle, release manifest и metadata совместимости.
- Добавлен update UI для просмотра доступности компонентов, требуемой версии
  панели, установки/удаления, включения/отключения и явного удаления данных
  компонента.
- Усилены границы компонентов: core-пакеты и core-тесты больше не импортируют
  конкретные component-пакеты; generic host behavior проверяется фикстурами.
- Улучшены remote outbound subscriptions: нормализованный профиль данных,
  конвертация групп, delay checks, bulk groups и более безопасная синхронизация
  в panel outbounds и client exports.
- Улучшены frontend drag-and-drop selection и placement indicators для Nexus и
  classic layouts.
- Обновлены Docker и release automation под multi-platform images и модульные
  release artifacts.
- Исправлены CI-регрессии после очистки публичной документации: policy-тесты
  больше не зависят от игнорируемых локальных docs, а frontend E2E startup
  получил увеличенный CI readiness budget и managed-server logs в артефактах.
- Очищены публичная документация и release defaults для ветки `2026.2.0`.
