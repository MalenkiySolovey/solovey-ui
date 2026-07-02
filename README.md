# Solovey UI

<p align="center">
  <b>Personal sing-box panel with modular optional features</b>
</p>

<p align="center">
  <a href="https://github.com/MalenkiySolovey/solovey-ui/releases"><img alt="Release" src="https://img.shields.io/github/v/release/MalenkiySolovey/solovey-ui?include_prereleases&label=release"></a>
  <a href="https://github.com/MalenkiySolovey/solovey-ui/actions"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/MalenkiySolovey/solovey-ui/ci.yml?branch=main&label=ci"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/MalenkiySolovey/solovey-ui"></a>
</p>

Current version: `2026.2.0`

Solovey UI is a modified GPL-3.0 derivative of the S-UI ecosystem. It keeps the
core panel focused on `sing-box` management and moves larger features into
installable optional components.

> This is not an official S-UI, S-UI-X, sing-box, or SagerNet product. Use it at
> your own risk, keep backups, and test updates before touching production.

## Highlights

- Bundled `sing-box` core with web UI and CLI management.
- Nexus and classic UI modes.
- Optional component system with `full`, `minimal/core`, and custom installs.
- Remote outbound subscriptions with canonical connection data, group handling,
  delay checks, and sing-box conversion.
- Client subscriptions in URI, sing-box JSON, Clash/Mihomo YAML, and Xray JSON.
- Backup, restore, rollback, diagnostics, doctor checks, and audit logs.
- Debian-oriented installer with release checksums and component metadata.
- Upstream fixes adapted from `alireza0/s-ui` and `deposist/s-ui-x` without
  reverting to their older flat project structure.

## Install

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh)
sudo solovey-ui doctor
sudo solovey-ui status
```

Install a specific release:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --version v2026.2.0
```

Default paths:

- install directory: `/usr/local/solovey-ui`
- database: `/usr/local/solovey-ui/db/solovey-ui.db`
- secret environment file: `/etc/solovey-ui/secretbox.env`
- systemd service: `solovey-ui`
- CLI command: `solovey-ui`

## Optional Components

The default install is `full`: it installs the full binary and all optional
components. Use `minimal` or `core` when you want only the base panel.

```bash
# Core panel only, no optional components
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --profile minimal

# Full binary, but without selected components
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --without telegram,paid-subscriptions

# Custom install with only selected optional components
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --with remote-outbound-subscriptions,telegram
```

Available component IDs:

| ID | What it adds |
|---|---|
| `import-xui` | Import and migration tools for compatible panel databases. |
| `remote-outbound-subscriptions` | External outbound subscriptions, groups, conversion, delay checks, and synchronization into outbounds. |
| `paid-subscriptions` | Paid client subscriptions, tariffs, orders, and related admin UI. |
| `telegram` | Telegram notifications, bot transport, and backup delivery. |
| `observability-extra` | Extra runtime sampling and observability views. |
| `panel-update-ui` | Web UI for checking and applying panel updates. |

Component choices are real install choices, not just visual hiding. Disabled
components are not installed into the runtime component directory and their
routes, jobs, settings, tables, and frontend entries are not activated.

## Update

```bash
sudo solovey-ui update
sudo solovey-ui doctor
sudo systemctl status solovey-ui --no-pager
```

Update to a specific tag:

```bash
sudo solovey-ui update --version v2026.2.0
```

## Backup And Restore

```bash
sudo solovey-ui backup
sudo solovey-ui rollback latest
sudo solovey-ui doctor
```

Backups are stored under `/var/backups/solovey-ui`.

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
sudo solovey-ui uninstall
sudo solovey-ui uninstall --purge
```

## Local Development On Windows

From the repository worktree:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\start-panel.ps1 -Build -OpenBrowser
```

Useful component examples:

```powershell
# Core/minimal local panel
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\start-panel.ps1 -Build -OpenBrowser -Profile minimal

# Custom local panel
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\start-panel.ps1 -Build -OpenBrowser -With remote-outbound-subscriptions,telegram
```

Clean local runtime state:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\stop-panel.ps1 -Clean
```

## Release Artifacts

GitHub Releases publish:

- full Linux archives: `solovey-ui-linux-<arch>.tar.gz`
- core Linux archives: `solovey-ui-core-linux-<arch>.tar.gz`
- component bundle: `solovey-ui-components.tar.gz`
- checksums for every archive

The installer chooses the smallest binary profile that can satisfy the selected
components. Optional component files are shipped as one component bundle to keep
the release page compact.

## Credits

Solovey UI is based on the S-UI family and manually adapts selected fixes and
ideas from related projects:

- [alireza0/s-ui](https://github.com/alireza0/s-ui)
- [deposist/s-ui-x](https://github.com/deposist/s-ui-x)
- [admin8800/s-ui](https://github.com/admin8800/s-ui)
- [shenaba/2s-ui](https://github.com/shenaba/2s-ui)
- [printfer/v2sing](https://github.com/printfer/v2sing)
- [sub-store-org/Sub-Store](https://github.com/sub-store-org/Sub-Store)

See `NOTICE.md` for attribution details.

---

## Русская Версия

Solovey UI - модифицированная панель из экосистемы S-UI для управления
`sing-box`. Основная панель остаётся компактной, а крупные возможности вынесены
в optional components, которые можно реально не устанавливать.

> Проект не является официальным продуктом S-UI, S-UI-X, sing-box или SagerNet.
> Используйте его на свой риск, делайте backup и проверяйте обновления на
> тестовой машине.

### Возможности

- встроенный `sing-box` core, web UI и CLI;
- режимы Nexus и classic;
- компонентная установка: `full`, `minimal/core` или свой набор компонентов;
- ремоут outbound-подписки, группы, delay-проверки и синхронизация в outbounds;
- клиентские подписки URI, sing-box JSON, Clash/Mihomo YAML и Xray JSON;
- backup, restore, rollback, doctor, diagnostics и audit logs;
- установщик для Debian-серверов с checksum-проверкой релизных архивов.

### Установка

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh)
sudo solovey-ui doctor
sudo solovey-ui status
```

Конкретная версия:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --version v2026.2.0
```

### Компоненты

```bash
# Только базовая панель
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --profile minimal

# Полная панель без выбранных компонентов
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --without telegram,paid-subscriptions

# Только нужные optional components
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --with remote-outbound-subscriptions,telegram
```

Доступные ID компонентов: `import-xui`, `remote-outbound-subscriptions`,
`paid-subscriptions`, `telegram`, `observability-extra`, `panel-update-ui`.

### Обновление

```bash
sudo solovey-ui update
sudo solovey-ui doctor
sudo systemctl status solovey-ui --no-pager
```

Обновление до конкретного тега:

```bash
sudo solovey-ui update --version v2026.2.0
```

### Backup И Rollback

```bash
sudo solovey-ui backup
sudo solovey-ui rollback latest
sudo solovey-ui doctor
```

Backup хранится в `/var/backups/solovey-ui`.

### Локальный Запуск На Windows

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\start-panel.ps1 -Build -OpenBrowser
```

С компонентными аргументами:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\start-panel.ps1 -Build -OpenBrowser -Profile minimal
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\start-panel.ps1 -Build -OpenBrowser -With remote-outbound-subscriptions,telegram
```

Очистить локальную тестовую базу и конфиг:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\stop-panel.ps1 -Clean
```
