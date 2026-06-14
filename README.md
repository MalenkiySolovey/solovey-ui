# Solovey UI

Solovey UI is a personal web panel for managing a bundled `sing-box` core. It
is based on the original S-UI ecosystem and is being adapted for private use,
safer updates, cleaner maintenance, and custom panel features.

Current version: `1.5.7-solovey.1`

## Русская Версия

### Важно

Этот проект делается для личного использования. Он не является официальным
продуктом S-UI или sing-box, не предоставляет никаких гарантий и не снимает с
пользователя ответственность за установку, настройку, обновление, безопасность,
резервные копии и соблюдение законов своей страны.

Если вы используете этот код, вы делаете это на свой риск. Перед установкой на
рабочий сервер обязательно делайте backup и проверяйте обновления на тестовой
машине.

### Что Это

Solovey UI - панель управления для `sing-box` с веб-интерфейсом, systemd
service, установщиком, обновлениями через GitHub Releases, локальными
backup/rollback командами и заделом под миграцию с оригинальной S-UI.

Основной целевой сервер: Debian 12. Другие Linux-системы могут работать, но их
нужно проверять отдельно.

### Установка

После публикации первого GitHub Release установите панель так:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh)
sudo solovey-ui doctor
sudo solovey-ui status
```

Установка конкретной версии:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --version v1.5.7-solovey.1
```

По умолчанию используются:

- каталог установки: `/usr/local/solovey-ui`
- база данных: `/usr/local/solovey-ui/db/solovey-ui.db`
- файл секретов окружения: `/etc/solovey-ui/secretbox.env`
- systemd service: `solovey-ui`
- CLI-команда: `solovey-ui`

### Обновление

```bash
sudo solovey-ui update
sudo solovey-ui doctor
sudo systemctl status solovey-ui --no-pager
```

Обновление на конкретный тег:

```bash
sudo solovey-ui update --version v1.5.7-solovey.1
```

### Backup И Rollback

Создать локальный backup:

```bash
sudo solovey-ui backup
```

Откатиться на последний backup:

```bash
sudo solovey-ui rollback latest
sudo solovey-ui doctor
```

Backup'и хранятся в `/var/backups/solovey-ui`.

### Миграция С Оригинальной S-UI

Миграция пока считается дополнительной возможностью, а не основным сценарием.
Перед использованием обязательно сделайте ручную копию старой панели.

```bash
sudo solovey-ui migrate-from-sui
```

Команда пытается перенести данные из `/usr/local/s-ui` и `/etc/s-ui`, включая
базу, `secretbox.env`, сертификаты и service-метаданные.

### Полезные Команды

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

`doctor --full`, `diagnose` и `report` показывают расширенный отчёт по базе данных, настройкам, портам, systemd-сервису, системе, сети и последним логам.

Удаление панели без удаления данных:

```bash
sudo solovey-ui uninstall
```

Полное удаление вместе с данными:

```bash
sudo solovey-ui uninstall --purge
```

### Локальная Проверка В Браузере

На Windows из корня репозитория:

```powershell
.\scripts\dev\start-panel.cmd -Fresh -Build -OpenBrowser
type .runtime\local-panel\startup-summary.txt
```

Тестовая база, логи, PID и секреты будут лежать в `.runtime/local-panel/`.

Остановить локальную панель:

```powershell
.\scripts\dev\stop-panel.cmd
```

Остановить и удалить тестовые данные:

```powershell
.\scripts\dev\stop-panel.cmd -Clean
```

### Сборка Релиза

Linux-архив для GitHub Release собирается workflow'ом
`.github/workflows/release.yml`. Локально контракт архива можно проверить так:

```bash
bash tests/installer/release-package.sh
```

Релизный архив должен содержать:

- `solovey-ui/solovey-ui`
- `solovey-ui/solovey-ui.sh`
- `solovey-ui/solovey-ui.service`
- `solovey-ui/BUILD_INFO.txt`

### Благодарности

Solovey UI основан на идеях и коде оригинального S-UI и его форков, включая
`alireza0/s-ui`, `admin8800/s-ui`, `deposist/s-ui-x`, `shenaba/2s-ui` и другие
родственные проекты.

---

## English Version

### Important

This project is maintained for personal use. It is not an official S-UI or
sing-box product, comes with no warranty, and does not take responsibility for
your deployment, configuration, updates, backups, security, or legal compliance.

If you use this code, you do so at your own risk. Always make a backup and test
updates on a non-production machine before touching a real server.

### What It Is

Solovey UI is a web panel for managing a bundled `sing-box` core. It includes a
web UI, systemd service, GitHub Release based installer/update flow, local
backup/rollback commands, and early migration support from legacy S-UI installs.

The primary target is Debian 12. Other Linux systems may work, but should be
tested separately.

### Install

After the first GitHub Release is published:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh)
sudo solovey-ui doctor
sudo solovey-ui status
```

Install a specific version:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh) --version v1.5.7-solovey.1
```

Default paths:

- install directory: `/usr/local/solovey-ui`
- database: `/usr/local/solovey-ui/db/solovey-ui.db`
- secret environment file: `/etc/solovey-ui/secretbox.env`
- systemd service: `solovey-ui`
- CLI command: `solovey-ui`

### Update

```bash
sudo solovey-ui update
sudo solovey-ui doctor
sudo systemctl status solovey-ui --no-pager
```

Update to a specific tag:

```bash
sudo solovey-ui update --version v1.5.7-solovey.1
```

### Backup And Rollback

Create a local backup:

```bash
sudo solovey-ui backup
```

Rollback to the latest backup:

```bash
sudo solovey-ui rollback latest
sudo solovey-ui doctor
```

Backups are stored under `/var/backups/solovey-ui`.

### Legacy S-UI Migration

Migration is an optional path, not the primary install flow. Make a manual copy
of the old panel before using it.

```bash
sudo solovey-ui migrate-from-sui
```

The command attempts to copy data from `/usr/local/s-ui` and `/etc/s-ui`,
including the database, `secretbox.env`, certificates, and service metadata.

### Useful Commands

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

`doctor --full`, `diagnose`, and `report` print the extended diagnostics report: database, settings, ports, systemd service, system, network, and recent logs.

Uninstall the panel without removing data:

```bash
sudo solovey-ui uninstall
```

Remove the panel and its data:

```bash
sudo solovey-ui uninstall --purge
```

### Local Browser Check

On Windows, from the repository root:

```powershell
.\scripts\dev\start-panel.cmd -Fresh -Build -OpenBrowser
type .runtime\local-panel\startup-summary.txt
```

The test database, logs, PID, and secrets are stored under
`.runtime/local-panel/`.

Stop the local panel:

```powershell
.\scripts\dev\stop-panel.cmd
```

Stop it and remove test data:

```powershell
.\scripts\dev\stop-panel.cmd -Clean
```

### Release Build

The Linux archive for GitHub Release is built by `.github/workflows/release.yml`.
The archive contract can be checked locally with:

```bash
bash tests/installer/release-package.sh
```

The release archive must contain:

- `solovey-ui/solovey-ui`
- `solovey-ui/solovey-ui.sh`
- `solovey-ui/solovey-ui.service`
- `solovey-ui/BUILD_INFO.txt`

### Credits

Solovey UI is based on ideas and code from the original S-UI ecosystem and its
forks, including `alireza0/s-ui`, `admin8800/s-ui`, `deposist/s-ui-x`,
`shenaba/2s-ui`, and related projects.
