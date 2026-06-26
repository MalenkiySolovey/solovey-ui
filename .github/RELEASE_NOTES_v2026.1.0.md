# Solovey UI 2026.1.0

Compared with `v1.5.7-solovey.1`.

## English

- Reworked remote subscriptions: normalized collected profile data, improved Xray and Mihomo group parsing, fixed group delay checks, repaired group synchronization into sing-box outbounds, and added bulk group operations.
- Added cleaner subscription group handling, including an internal `All` group, add/remove/invert selection controls, clearer profile text, and better status/type display for converted entries.
- Improved outbound, inbound, client, endpoint, service, TLS, DNS, rule, and ruleset ordering with shared drag-and-drop selection logic, multi-row moves, and clearer insertion indicators in both Nexus and Classic layouts.
- Refined the panel UI: dashboard cards, subscription actions, conversion/profile controls, and inbound transport selection now present the active configuration more directly.
- Reorganized backend packages across API handlers, services, database import/backup, paid subscriptions, settings validation, subscription parsing, realtime, logging, and core runtime boundaries.
- Hardened release and update paths: stricter frontend install/build checks, pinned GitHub Actions, improved Linux artifact metadata, backup/rollback installer tests, cookie-key rotation coverage, and version identity moved to the year-based `2026.1.0` scheme.
- Updated sing-box integration to `v1.13.13` with a fixed commit pin for reproducible Windows/Linux builds, while trimming unsupported experimental service editors from the public build.
- Removed stale wrappers, compatibility aliases, generated frontend artifacts, and local-only development notes from the tracked release tree.

## Русский

- Переработаны remote-подписки: нормализовано внутреннее представление данных, улучшен разбор групп Xray и Mihomo, исправлены проверки задержки групп, синхронизация групп в sing-box outbounds и массовые операции с группами.
- Улучшена работа с группами подписок: добавлена внутренняя группа `All`, кнопки добавить/удалить/инвертировать выбор, понятный профиль подписки и более ясное отображение исходного типа и результата конвертации.
- Обновлено упорядочивание outbounds, inbounds, clients, endpoints, services, TLS, DNS, rules и rulesets: общая логика drag-and-drop, выбор нескольких строк, перенос группой и понятные индикаторы места вставки в Nexus и Classic.
- Улучшен интерфейс панели: карточки dashboard, действия подписок, профиль/настройки конвертации и выбор транспорта inbound теперь показывают состояние конфигурации прямее и понятнее.
- Перестроена структура backend-пакетов: API handlers, services, database import/backup, paid subscriptions, settings validation, subscription parsing, realtime, logging и core runtime разделены по ответственности.
- Усилены установка, обновление и релиз: строгие frontend-проверки, закрепленные GitHub Actions, metadata Linux-артефактов, тесты backup/rollback установщика, проверка ротации cookie-key и переход на версию `2026.1.0`.
- Обновлена интеграция sing-box до `v1.13.13` с закреплением исправленного commit для воспроизводимых Windows/Linux сборок; неподдерживаемые экспериментальные service-редакторы убраны из публичной сборки.
- Удалены старые wrappers, compatibility aliases, сгенерированные frontend-артефакты и локальные development-заметки из отслеживаемого дерева релиза.
