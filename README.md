# Solovey UI

<p align="center">
  <b>Personal sing-box panel with a modular runtime, component-aware installer, and built-in operational tooling.</b>
</p>

<p align="center">
  <a href="https://github.com/MalenkiySolovey/solovey-ui/releases"><img alt="Release" src="https://img.shields.io/github/v/release/MalenkiySolovey/solovey-ui?include_prereleases&label=release"></a>
  <a href="https://github.com/MalenkiySolovey/solovey-ui/actions"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/MalenkiySolovey/solovey-ui/ci.yml?branch=main&label=ci"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/MalenkiySolovey/solovey-ui"></a>
</p>

Current version: `2026.2.0`

Solovey UI is a GPL-3.0 panel for managing a `sing-box` server through a web
interface and CLI. The core panel contains the required runtime: users,
inbounds, outbounds, routing, DNS, TLS, services, backups, diagnostics, and
basic subscriptions. Larger features are shipped as optional components so a
server can be installed as a full panel, a compact core panel, or a custom
profile.

This project is experimental operational software. Keep backups, test upgrades
before production use, and review generated configuration when changing network
behavior.

## Highlights

- `sing-box` management with inbounds, outbounds, endpoints, DNS, route rules,
  TLS settings, services, and client subscriptions.
- Two UI modes: Nexus and classic.
- Real optional components: routes, jobs, settings, tables, frontend entries,
  and runtime hooks are registered only when the component is installed and
  enabled.
- Component-aware install profiles: `full`, `minimal`/`core`, `--with`, and
  `--without`.
- Remote outbound subscriptions with normalized profile data, group handling,
  delay checks, and synchronization into panel outbounds.
- Client exports in URI, sing-box JSON, Clash/Mihomo YAML, and Xray JSON.
- Backup, restore, rollback, doctor checks, diagnostics, reports, audit events,
  and release checksums.
- Release packaging with full binaries, core binaries, one component bundle,
  release manifest, and Docker/GHCR image publishing.

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

| Item | Path |
|---|---|
| Install directory | `/usr/local/solovey-ui` |
| Database | `/usr/local/solovey-ui/db/solovey-ui.db` |
| Secret environment | `/etc/solovey-ui/secretbox.env` |
| systemd service | `solovey-ui` |
| CLI command | `solovey-ui` |
| Backups | `/var/backups/solovey-ui` |

## Optional Components

The default install profile is `full`. It installs the full binary and all
optional components. Use `minimal` or `core` for the base panel only, or select a
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
| `panel-update-ui` | Web UI for checking available panel/component updates, applying updates, enabling/disabling components, and removing optional components. | Manage updates from the panel instead of the CLI. |
| `remote-outbound-subscriptions` | External outbound subscriptions, normalized collected profile data, groups, conversion policies, delay checks, bulk groups, and synchronization into outbounds. | Pull remote proxy lists and convert selected entries into sing-box outbounds. |
| `import-xui` | Migration tools for compatible panel databases, including preview, conflict handling, and selective import. | Move existing panel data into Solovey UI with review before applying. |
| `paid-subscriptions` | Paid client subscription entities, tariffs, payment orders, bindings, and admin UI. | Sell or manage paid client subscription access. |
| `telegram` | Telegram notifications, bot transport, backup delivery, and related settings. | Receive operational alerts and backups outside the panel. |
| `observability-extra` | Additional runtime sampling, observability views, and related metrics. | Inspect runtime behavior beyond the base diagnostics. |

Installed components can be disabled without deleting data. Removing a component
removes its runtime files and unregisters routes/jobs/hooks. Data deletion is a
separate explicit action when the component supports it.

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
sudo solovey-ui update --version v2026.2.0
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
- release manifest: `release-manifest.json`
- checksums for every archive

Docker images are published to GHCR for release tags.

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
