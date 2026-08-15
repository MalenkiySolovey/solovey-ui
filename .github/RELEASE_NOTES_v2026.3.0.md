# Solovey UI v2026.3.0

Compared with `v2026.2.3`.

## English

- Added the disabled-by-default Server Protection component with endpoint and host-surface inventory, capability-aware planning, fronting, firewall composition, UDP guard, recovery, and auditable workflows.
- Added hardened native deployment profiles, a constrained privileged broker boundary, deployment and SSH-management surfaces, and explicit Docker networking contracts.
- Strengthened panel security with step-up verification, session and realtime protections, stricter request validation and budgets, safer secret handling, and expanded audit reporting.
- Added signed update metadata, stronger update and rollback coordination, streamed backup protection, restore rehearsal, durable ownership checks, and explicit component data-lifecycle operations.
- Consolidated component registration, manifests, commands, settings, backup codecs, health, and resource contracts into one deterministic full/core composition model.
- Expanded the frontend for security, deployment, operations, SSH management, and component-owned routes and locales, with additional accessibility and profile checks.
- Preserved upgrade compatibility and existing core behavior with broader migration, installer, packaging, architecture, and regression tests.
- Windows release archives are published for x64 and ARM64 on native runners with the required CGO-backed SQLite runtime; Linux and Docker architecture coverage is unchanged.
- Kept host-bound advanced protection modes experimental or inspection-only where separate external acceptance is still required.

## Русский

- Добавлен отключённый по умолчанию компонент Server Protection: инвентаризация endpoint- и host-поверхностей, планирование с учётом capabilities, fronting, композиция firewall, UDP guard, восстановление и аудит операций.
- Добавлены защищённые профили нативного развёртывания, ограниченная граница привилегированного брокера, поверхности управления развёртыванием и SSH, а также явные сетевые контракты Docker.
- Усилена безопасность панели: step-up-проверка, защита сессий и realtime, более строгая валидация и бюджеты запросов, безопасная работа с секретами и расширенный аудит.
- Добавлены подписанные метаданные обновлений, более надёжная координация обновления и отката, потоковая защита резервных копий, репетиция восстановления, проверки владельцев и явный жизненный цикл данных компонентов.
- Регистрация компонентов, манифесты, команды, настройки, backup-кодеки, health- и resource-контракты сведены в единую детерминированную модель full/core.
- Интерфейс расширен для безопасности, развёртывания, эксплуатации, управления SSH и маршрутов и локалей компонентов; добавлены проверки доступности и профилей.
- Совместимость обновлений и поведение ядра сохранены и покрыты расширенными тестами миграции, установщика, упаковки, архитектур и регрессий.
- Архивы Windows публикуются для x64 и ARM64 на нативных runners с обязательным CGO-backed SQLite runtime; набор архитектур Linux и Docker не изменился.
- Продвинутые host-bound режимы защиты остаются экспериментальными или inspection-only там, где требуется отдельная внешняя приёмка.
