# Solovey UI

<p align="center">
  <b>Personal sing-box panel with a modular runtime, component-aware installer, and built-in operational tools.</b>
</p>

<p align="center">
  <a href="https://github.com/MalenkiySolovey/solovey-ui/releases"><img alt="Release" src="https://img.shields.io/github/v/release/MalenkiySolovey/solovey-ui?include_prereleases&label=release"></a>
  <a href="https://github.com/MalenkiySolovey/solovey-ui/actions"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/MalenkiySolovey/solovey-ui/ci.yml?branch=main&label=ci"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/MalenkiySolovey/solovey-ui"></a>
</p>

Current version: `2026.3.0`

Solovey UI is a GPL-3.0 panel for managing a `sing-box` server through a web
interface and command-line tool. The core panel includes the required runtime
parts: users, inbounds, outbounds, routing, DNS, TLS, services, backups,
diagnostics, and basic client subscriptions. Larger features are delivered as
optional components, so the same project can be installed as a full panel, a
compact core panel, or a custom profile with only the features you need.

The project is experimental operational software. Keep backups, test upgrades
before production use, and review generated configuration when changing network
behavior.

## Highlights

- Manage `sing-box` objects: inbounds, outbounds, endpoints, DNS, route rules,
  TLS settings, services, and client subscriptions.
- Choose between two interface modes: Nexus and classic.
- Install optional components only when needed: their routes, background jobs,
  settings, tables, frontend entries, and runtime hooks are registered only when
  the component is installed and enabled.
- Use component-aware install profiles: `full`, `minimal`/`core`, `--with`, and
  `--without`.
- Import remote outbound subscriptions, normalize profile data, group entries,
  run delay checks, and synchronize selected entries into panel outbounds.
- Export clients as URI links, sing-box JSON, Clash/Mihomo YAML, and Xray JSON.
- Use built-in backup, restore, rollback, doctor checks, diagnostics, reports,
  audit events, and release checksums.
- Build releases with full binaries, core binaries, a shared component bundle, a
  release manifest, checksums, and Docker/GHCR images.

## Install

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh)
sudo solovey-ui doctor
sudo solovey-ui status
```

Install a specific release:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --version v2026.3.0
```

Default paths:

| Item | Path |
|---|---|
| Install directory | `/usr/local/solovey-ui` |
| Hardened database | `/var/lib/solovey-ui/db/solovey-ui.db` |
| Legacy-root database | `/usr/local/solovey-ui/db/solovey-ui.db` |
| Secret environment file | `/etc/solovey-ui/secretbox.env` |
| systemd service | `solovey-ui` |
| Privileged broker sockets | `/run/solovey-ui/privileged-broker.sock`, `/run/solovey-ui/privileged-proof.sock` |
| CLI command | `solovey-ui` |
| Backups | `/var/backups/solovey-ui` |

### Deployment security profiles

New compatible native installs use `native-hardened`: the panel runs as the
`solovey-ui` service account without capabilities, while one socket-activated
root broker accepts only fixed semantic operations. Existing installs retain
the explicit `native-legacy-root` compatibility profile until an operator runs
the revision-bound migration workflow. `native-network-advanced` is generated
but deliberately unavailable until the network runtime can be isolated from
the panel; do not grant the panel `CAP_NET_ADMIN` as a workaround.

Docker has separate host, explicit-bridge, and experimental advanced-network
profiles. Every profile uses an immutable image digest, a non-root process, a
read-only root filesystem, bounded resources/logs/tmpfs, and no Docker or
broker socket. The panel does not control the Docker daemon and does not call a
non-root container a rootless daemon without engine evidence.

See [Deployment profiles and privileged broker](docs/deployment-profiles.md)
for profile selection, migration, rollback, Docker commands, and verification.
See [Operations and data lifecycle](docs/operations.md) for signed updates,
resource pressure, SQLite/migrations, backup/restore, Drop Data, and recovery
boundaries.

## Optional Components

The default install profile is `full`. It installs the full binary and all
optional components. Use `minimal` or `core` for the base panel only, or choose a
custom component set.

```bash
# Core panel only, no optional components
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --profile minimal

# Full binary, but without selected components
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --without telegram,paid-subscriptions

# Full binary with only selected optional components
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --with remote-outbound-subscriptions,telegram
```

`--with` and `--without` accept comma-separated component IDs. The installer
chooses the smallest binary profile that can satisfy the selected component set.

### Component Catalog

| Component ID | Purpose | Typical use |
|---|---|---|
| `panel-update-ui` | Web interface for checking panel/component updates, applying updates, enabling or disabling components, and removing optional components. | Manage updates from the panel instead of using only the CLI. |
| `remote-outbound-subscriptions` | External outbound subscriptions, normalized profile data, groups, conversion policies, delay checks, bulk groups, and synchronization into outbounds. | Pull remote proxy lists and convert selected entries into sing-box outbounds. |
| `import-xui` | Migration tools for compatible panel databases, including preview, conflict handling, and selective import. | Move existing panel data into Solovey UI with review before applying changes. |
| `paid-subscriptions` | Paid client subscription records, tariffs, payment orders, bindings, and administration UI. | Sell or manage paid access to client subscriptions. |
| `telegram` | Telegram notifications, bot transport, backup delivery, and related settings. | Receive operational alerts and backups outside the panel. |
| `observability-extra` | Additional runtime sampling, observability views, and related metrics. | Inspect runtime behavior beyond the base diagnostics. |
| `fallback-html` | Default-disabled managed fallback HTML sites, pages, redirects, assets, and publication records. | Prepare an explicit fallback site without enabling it automatically. |
| `server-protection` | Default-disabled host-bound protection, fronting, firewall-composition, UDP guard, and recovery workflows. | Inspect or stage experimental protection capabilities; external Live acceptance remains separate and is not implied by installation. |

Installed components can be disabled without deleting data. Removing a component
removes its runtime files and unregisters its routes, jobs, and hooks. Data
deletion is a separate explicit action when the component supports it.

The update UI component protects itself: it cannot remove or disable its own
management surface from inside that same surface.

## Update

```bash
sudo solovey-ui update
sudo solovey-ui doctor
sudo systemctl status solovey-ui --no-pager
```

Update to a specific tag:

```bash
sudo solovey-ui update --version v2026.3.0
```

## Backup And Restore

```bash
sudo solovey-ui backup
sudo solovey-ui rollback latest
sudo solovey-ui doctor
```

Use `uninstall --purge` only when you intentionally want to remove panel data.

```bash
sudo solovey-ui uninstall
sudo solovey-ui uninstall --purge
```

## Useful CLI Commands

```bash
sudo solovey-ui status
sudo solovey-ui restart
sudo solovey-ui log
sudo solovey-ui version
sudo solovey-ui build-info
sudo solovey-ui doctor
sudo solovey-ui doctor --full
sudo solovey-ui diagnose
sudo solovey-ui report
```

## Local Development On Windows

From the repository worktree:

```powershell
.\scripts\dev\start-panel.ps1 -Build -OpenBrowser
```

Useful component examples:

```powershell
# Core/minimal local panel
.\scripts\dev\start-panel.ps1 -Build -OpenBrowser -Profile minimal

# Custom local panel
.\scripts\dev\start-panel.ps1 -Build -OpenBrowser -With remote-outbound-subscriptions,telegram

# Exclude selected components
.\scripts\dev\start-panel.ps1 -Build -OpenBrowser -Without import-xui,observability-extra,paid-subscriptions,telegram
```

Clean local runtime state:

```powershell
.\scripts\dev\stop-panel.ps1 -Clean
```

## Release Artifacts

GitHub Releases publish:

- full Linux archives: `solovey-ui-linux-<arch>.tar.gz`
- core Linux archives: `solovey-ui-core-linux-<arch>.tar.gz`
- component bundle: `solovey-ui-components.tar.gz`
- signed release manifest: `solovey-ui-release.json`
- Windows archives: `solovey-ui-windows-<arch>.zip`
- checksums for every archive

Docker images are published to GHCR for release tags. The Compose contracts
require `SOLOVEY_UI_IMAGE_DIGEST` to pin the exact release image; review the
[deployment guide](docs/deployment-profiles.md) before selecting bridge or
advanced networking.

## Related Projects And Credits

Solovey UI is based on the S-UI family and manually adapts selected fixes and
ideas from related open-source projects while keeping its own structure,
component model, installer, and release flow:

- [alireza0/s-ui](https://github.com/alireza0/s-ui)
- [deposist/s-ui-x](https://github.com/deposist/s-ui-x)
- [admin8800/s-ui](https://github.com/admin8800/s-ui)
- [shenaba/2s-ui](https://github.com/shenaba/2s-ui)
- [printfer/v2sing](https://github.com/printfer/v2sing)
- [sub-store-org/Sub-Store](https://github.com/sub-store-org/Sub-Store)

The project remains licensed under GNU GPL v3.0.

---

# Solovey UI (Русский)

<p align="center">
  <b>Персональная панель для sing-box с модульной средой выполнения, установщиком с учётом компонентов и встроенными инструментами администрирования.</b>
</p>

Текущая версия: `2026.3.0`

Solovey UI — панель GPL-3.0 для управления сервером `sing-box` через веб-интерфейс
и командную строку. Базовое ядро включает обязательные части среды выполнения:
пользователей, входящие и исходящие подключения, маршрутизацию, DNS, TLS, службы,
резервное копирование, диагностику и базовые клиентские подписки. Крупные
возможности поставляются как дополнительные компоненты, поэтому один и тот же
проект можно установить как полную панель, компактное ядро или собственный
профиль только с нужными функциями.

Проект является экспериментальным программным обеспечением для эксплуатации
серверов. Делайте резервные копии, проверяйте обновления до использования в
рабочей среде и просматривайте сгенерированную конфигурацию при изменении
сетевой логики.

## Возможности

- Управление объектами `sing-box`: входящими и исходящими подключениями,
  конечными точками, DNS, правилами маршрутизации, параметрами TLS, службами и
  клиентскими подписками.
- Два режима интерфейса на выбор: Nexus и классический.
- Установка дополнительных компонентов только при необходимости: их маршруты,
  фоновые задания, настройки, таблицы, элементы интерфейса и обработчики среды
  выполнения регистрируются только тогда, когда компонент установлен и включён.
- Профили установки с учётом компонентов: `full`, `minimal`/`core`, `--with` и
  `--without`.
- Импорт удалённых подписок исходящих подключений, нормализация данных профиля,
  группировка записей, проверки задержки и синхронизация выбранных записей с
  исходящими подключениями панели.
- Экспорт клиентов в виде URI-ссылок, sing-box JSON, Clash/Mihomo YAML и Xray
  JSON.
- Встроенное резервное копирование, восстановление, откат, проверки состояния,
  диагностика, отчёты, события аудита и контрольные суммы релизов.
- Сборка релизов с полной версией бинарных файлов, базовой версией бинарных
  файлов, общим архивом компонентов, манифестом релиза, контрольными суммами и
  образами Docker/GHCR.

## Установка

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh)
sudo solovey-ui doctor
sudo solovey-ui status
```

Установка конкретного релиза:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --version v2026.3.0
```

Пути по умолчанию:

| Что | Путь |
|---|---|
| Каталог установки | `/usr/local/solovey-ui` |
| База данных защищённого профиля | `/var/lib/solovey-ui/db/solovey-ui.db` |
| База данных совместимого root-профиля | `/usr/local/solovey-ui/db/solovey-ui.db` |
| Файл секретного окружения | `/etc/solovey-ui/secretbox.env` |
| Служба systemd | `solovey-ui` |
| Сокеты привилегированного брокера | `/run/solovey-ui/privileged-broker.sock`, `/run/solovey-ui/privileged-proof.sock` |
| CLI команда | `solovey-ui` |
| Резервные копии | `/var/backups/solovey-ui` |

### Профили безопасности развёртывания

Новые совместимые нативные установки используют `native-hardened`: панель
работает от служебной учётной записи `solovey-ui` без capabilities, а один
активируемый сокетами root-брокер принимает только фиксированные семантические
операции. Существующие установки сохраняют явный совместимый профиль
`native-legacy-root`, пока оператор не выполнит привязанную к ревизиям
миграцию. `native-network-advanced` генерируется, но намеренно недоступен до
изоляции сетевой среды от панели; выдавать панели `CAP_NET_ADMIN` в качестве
обхода нельзя.

Для Docker предусмотрены отдельные профили host, explicit bridge и
экспериментальный advanced network. Каждый профиль использует неизменяемый
digest образа, непривилегированный процесс, файловую систему root только для
чтения, ограниченные ресурсы, журналы и tmpfs, без сокета Docker или брокера.
Панель не управляет Docker daemon и не называет контейнер rootless без фактов о
движке.

Выбор профиля, миграция, откат, команды Docker и проверки описаны в
[руководстве по профилям развёртывания](docs/deployment-profiles.md).
[Operations and data lifecycle](docs/operations.md) описывает подписанные
обновления, нагрузку, SQLite/миграции, backup/restore, Drop Data и границы
восстановления.

## Дополнительные компоненты

Профиль установки по умолчанию — `full`. Он устанавливает полную версию бинарного
файла и все дополнительные компоненты. Используйте `minimal` или `core`, если
нужна только базовая панель, либо выберите собственный набор компонентов.

```bash
# Только ядро панели, без дополнительных компонентов
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --profile minimal

# Полная версия бинарного файла, но без выбранных компонентов
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --without telegram,paid-subscriptions

# Полная версия бинарного файла только с выбранными дополнительными компонентами
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --with remote-outbound-subscriptions,telegram
```

`--with` и `--without` принимают идентификаторы компонентов через запятую.
Установщик выбирает самый маленький бинарный профиль, который может обеспечить
выбранный набор компонентов.

### Каталог компонентов

| Идентификатор компонента | Назначение | Когда использовать |
|---|---|---|
| `panel-update-ui` | Веб-интерфейс для проверки обновлений панели и компонентов, применения обновлений, включения или отключения компонентов и удаления дополнительных компонентов. | Управлять обновлениями из панели, а не только через командную строку. |
| `remote-outbound-subscriptions` | Внешние подписки исходящих подключений, нормализованные данные профиля, группы, правила преобразования, проверки задержки, массовые группы и синхронизация с исходящими подключениями. | Загружать удалённые списки прокси и преобразовывать выбранные записи в исходящие подключения sing-box. |
| `import-xui` | Инструменты миграции совместимых баз данных панелей, включая предварительный просмотр, обработку конфликтов и выборочный импорт. | Переносить существующие данные панели в Solovey UI с проверкой перед применением изменений. |
| `paid-subscriptions` | Записи платных клиентских подписок, тарифы, платёжные заказы, привязки и интерфейс администрирования. | Продавать или управлять платным доступом к клиентским подпискам. |
| `telegram` | Уведомления Telegram, транспорт бота, доставка резервных копий и связанные настройки. | Получать эксплуатационные оповещения и резервные копии вне панели. |
| `observability-extra` | Дополнительная выборка данных среды выполнения, представления наблюдаемости и связанные метрики. | Изучать поведение среды выполнения глубже базовой диагностики. |
| `fallback-html` | Отключённые по умолчанию управляемые fallback-сайты, страницы, перенаправления, ресурсы и записи публикации. | Подготовить явный резервный сайт без его автоматического включения. |
| `server-protection` | Отключённые по умолчанию и привязанные к хосту процессы защиты, fronting, композиции firewall, UDP guard и восстановления. | Проверять или подготавливать экспериментальные возможности защиты; установка не означает отдельную внешнюю Live-приёмку. |

Установленные компоненты можно отключать без удаления данных. Удаление компонента
удаляет его файлы среды выполнения и снимает регистрацию его маршрутов, заданий
и обработчиков. Удаление данных — отдельное явное действие, если компонент его
поддерживает.

Компонент интерфейса обновлений защищает сам себя: его нельзя удалить или
отключить из его же поверхности управления.

## Обновление

```bash
sudo solovey-ui update
sudo solovey-ui doctor
sudo systemctl status solovey-ui --no-pager
```

Обновление до конкретного тега:

```bash
sudo solovey-ui update --version v2026.3.0
```

## Резервное копирование и восстановление

```bash
sudo solovey-ui backup
sudo solovey-ui rollback latest
sudo solovey-ui doctor
```

Используйте `uninstall --purge` только если действительно хотите удалить данные
панели.

```bash
sudo solovey-ui uninstall
sudo solovey-ui uninstall --purge
```

## Полезные команды командной строки

```bash
sudo solovey-ui status
sudo solovey-ui restart
sudo solovey-ui log
sudo solovey-ui version
sudo solovey-ui build-info
sudo solovey-ui doctor
sudo solovey-ui doctor --full
sudo solovey-ui diagnose
sudo solovey-ui report
```

## Локальная разработка на Windows

Из рабочей папки репозитория:

```powershell
.\scripts\dev\start-panel.ps1 -Build -OpenBrowser
```

Примеры с компонентами:

```powershell
# Локальная панель с профилем core/minimal
.\scripts\dev\start-panel.ps1 -Build -OpenBrowser -Profile minimal

# Локальная панель с выбранными компонентами
.\scripts\dev\start-panel.ps1 -Build -OpenBrowser -With remote-outbound-subscriptions,telegram

# Исключить выбранные компоненты
.\scripts\dev\start-panel.ps1 -Build -OpenBrowser -Without import-xui,observability-extra,paid-subscriptions,telegram
```

Очистить локальное состояние среды выполнения:

```powershell
.\scripts\dev\stop-panel.ps1 -Clean
```

## Артефакты релиза

GitHub Releases публикует:

- полные Linux-архивы: `solovey-ui-linux-<arch>.tar.gz`
- базовые Linux-архивы: `solovey-ui-core-linux-<arch>.tar.gz`
- архив компонентов: `solovey-ui-components.tar.gz`
- подписанный манифест релиза: `solovey-ui-release.json`
- архивы Windows: `solovey-ui-windows-<arch>.zip`
- контрольные суммы для каждого архива

Образы Docker публикуются в GHCR для релизных тегов. Compose-контракты требуют
`SOLOVEY_UI_IMAGE_DIGEST` с digest точного релизного образа; перед выбором
bridge или advanced networking изучите
[руководство по развёртыванию](docs/deployment-profiles.md).

## Связанные проекты и благодарности

Solovey UI основан на семействе S-UI и вручную адаптирует отдельные исправления
и идеи из связанных открытых проектов, сохраняя собственную структуру, модель
компонентов, установщик и процесс выпуска релизов:

- [alireza0/s-ui](https://github.com/alireza0/s-ui)
- [deposist/s-ui-x](https://github.com/deposist/s-ui-x)
- [admin8800/s-ui](https://github.com/admin8800/s-ui)
- [shenaba/2s-ui](https://github.com/shenaba/2s-ui)
- [printfer/v2sing](https://github.com/printfer/v2sing)
- [sub-store-org/Sub-Store](https://github.com/sub-store-org/Sub-Store)

Проект остаётся под лицензией GNU GPL v3.0.
