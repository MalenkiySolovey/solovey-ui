export default {
  "nav": {
    "groups": {
      "proxy": "Proxy",
      "network": "Network",
      "integrations": "Integrations",
      "system": "System"
    }
  },
  "table": {
    "search": "Search",
    "rowsPerPage": "Rows per page",
    "rowNumber": "#",
    "showingRange": "{from}–{to} of {total}",
    "selectAll": "Select all",
    "selectRow": "Select row",
    "expandRow": "Toggle row details",
    "clearFilters": "Clear filters",
    "noData": "No data",
    "moveUp": "Move up",
    "moveDown": "Move down"
  },
  "form": {
    "unsavedChanges": "Unsaved changes",
    "leaveTitle": "Discard changes?",
    "leaveConfirm": "You have unsaved changes. Discard them?",
    "discard": "Discard",
    "sections": {
      "basic": "Basic",
      "configuration": "Configuration"
    }
  },
  "success": "success",
  "warning": "warning",
  "failed": "failed",
  "enable": "Enable",
  "disable": "Disable",
  "none": "None",
  "all": "All",
  "loading": "Loading...",
  "confirm": "Are you sure ?",
  "yes": "yes",
  "no": "no",
  "unlimited": "infinite",
  "type": "Type",
  "protocol": "Protocol",
  "submit": "Submit",
  "reset": "Reset",
  "now": "Now",
  "network": "Network",
  "copyToClipboard": "Copy to clipboard",
  "noData": "No data!",
  "invalidLogin": "Invalid Login!",
  "online": "Online",
  "status": "Status",
  "version": "Version",
  "email": "Email",
  "commaSeparated": "(comma separated)",
  "count": "Count",
  "template": "Template",
  "editor": "Editor",
  "error": {
    "dplData": "Duplicate Data",
    "core": "Sing-Box Error",
    "invalidData": "Invalid Data"
  },
  "theme": {
    "light": "Light",
    "dark": "Dark",
    "system": "System"
  },
  "pages": {
    "login": "Login",
    "home": "Dashboard",
    "inbounds": "Inbounds",
    "outbounds": "Outbounds",
    "remoteOutboundSubscriptions": "Remote Subscriptions",
    "services": "Services",
    "endpoints": "Endpoints",
    "clients": "Clients",
    "rules": "Rules",
    "tls": "TLS Settings",
    "basics": "Basics",
    "dns": "DNS",
    "admins": "Admins",
    "telegram": "Telegram",
    "audit": "Audit",
    "diagnostics": "Diagnostics",
    "migrateXui": "3x-ui Migration",
    "paidSub": "Paid Subscriptions",
    "singBoxConfig": "Sing-box Config",
    "settings": "Settings",
    "support": "Support"
  },
  "support": {
    "title": "Support Solovey UI",
    "intro": "Solovey UI is an independent open-source project. You can help with reproducible issue reports, documentation, release testing, or code contributions.",
    "noPaymentDetails": "No official payment addresses are published for Solovey UI. This page never displays third-party donation wallets.",
    "github": "Open project on GitHub",
    "issues": "Report an issue",
    "imageAlt": "Solovey UI logo"
  },
  "main": {
    "tiles": "Tiles",
    "gauges": "Gauges",
    "charts": "Charts",
    "infos": "Information",
    "gauge": {
      "cpu": "CPU Gauge",
      "mem": "RAM Gauge",
      "dsk": "Disk Gauge",
      "swp": "Swap Gauge"
    },
    "chart": {
      "cpu": "CPU Monitor",
      "mem": "RAM Monitor",
      "net": "Network Bandwidth",
      "pnet": "Network Packets",
      "dio": "Disk I/O"
    },
    "info": {
      "sys": "System Info",
      "sbd": "Sing-Box Info",
      "host": "Host",
      "cpu": "CPU",
      "core": "Core",
      "uptime": "Uptime",
      "startupTime": "Startup time",
      "threads": "Threads",
      "memory": "Memory",
      "running": "Running"
    },
    "backup": {
      "title": "Backup & Restore",
      "backup": "Download Backup",
      "restore": "Restore Backup",
      "restoreHint": "Restores an s-ui backup (.db). To import a 3x-ui database, use \"Migrate from 3x-ui\" below — not this.",
      "exclStats": "Exclude graphs",
      "exclChanges": "Exclude changes",
      "encryptTelegram": "Encrypt with Telegram backup passphrase",
      "encryptDisabledHint": "Set Backup passphrase in the Telegram tab",
      "restorePassphrase": "Backup passphrase",
      "sbConfig": "Download Sing-Box Config",
      "xui": {
        "title": "Migrate from 3x-ui",
        "hint": "Quick import applies immediately. The full wizard lets you preview, review conflicts and choose exactly what to migrate.",
        "button": "Quick import — choose 3x-ui .db…",
        "dryRun": "Dry-run (preview only)",
        "strategy": "Conflict strategy",
        "merge": "Merge",
        "replace": "Replace",
        "skip": "Skip",
        "summary": "Import summary",
        "warnings": "Warnings",
        "openFull": "Full wizard (review & select)"
      }
    },
    "stats": {
      "title": "Usage & Counts",
      "totalUsage": "Total Usage"
    }
  },
  "nexus": {
    "on": "On",
    "off": "Off",
    "summary": {
      "inbounds": "{total} inbounds • {online} online",
      "outbounds": "{total} outbounds • {online} online",
      "clients": "{total} clients • {online} online",
      "endpoints": "{total} endpoints • {online} online",
      "services": "{total} services",
      "tls": "{total} certificates • {acme} ACME • {reality} Reality",
      "rules": "{rulesets} rulesets • {rules} rules",
      "dns": "{servers} servers • {rules} rules",
      "admins": "{total} admins"
    },
    "palette": {
      "label": "Palette",
      "options": {
        "technical": "Technical",
        "navy": "Navy",
          "emerald": "Emerald Minimal",
          "dracula": "Dracula Cyberpunk"
      }
    },
    "mode": {
      "label": "Interface mode",
      "switchTo": "Switch to {mode} mode",
      "options": {
        "classic": "Classic",
        "nexus": "Nexus"
      }
    },
    "status": {
      "online": "Online",
      "offline": "Offline",
      "loading": "Loading",
      "failed": "Failed",
      "unavailable": "Unavailable",
      "running": "Running",
      "notRunning": "Not running",
      "statusMissing": "Status missing",
      "coreDown": "Core down",
      "connected": "Connected",
      "reconnecting": "Reconnecting",
      "pollFallback": "Poll fallback",
      "realtime": "realtime",
      "healthy": "Healthy",
      "degraded": "Degraded",
      "down": "Down",
      "historyReady": "History ready",
      "noHistory": "No history",
      "idle": "Idle"
    },
    "overview": {
      "kpi": {
        "liveTraffic": "Live traffic",
        "liveTrafficDelta": "down + up",
        "trafficStats": "Traffic statistics",
        "trafficStatsDelta": "Down {download} · Up {upload}",
        "trafficTrend": "Traffic history trend",
        "onlineClients": "Online clients",
        "clientSignal": "Current client presence signal.",
        "enabledInbounds": "Enabled inbounds",
        "activeInbounds": "{count} active",
        "inboundOnlineTags": "{count} inbound tags report online.",
        "health": "Health",
        "healthWaiting": "Waiting for status payload.",
        "healthHealthy": "Status and sing-box checks are online.",
        "healthDown": "An online or sing-box check is down.",
        "healthDegraded": "A health signal is still missing."
      },
      "traffic": {
        "title": "Traffic overview",
        "range24h": "24h inbound history",
        "loading": "Loading traffic history.",
        "chartAria": "Inbound upload and download history",
        "emptyOffline": "Traffic history is unavailable while the browser is offline.",
        "emptyUnavailable": "Traffic history could not be loaded from current inbound stats.",
        "emptyNoHistory": "No inbound traffic history is available yet."
      },
      "system": {
        "title": "System status",
        "hostUptime": "Host uptime",
        "singboxUptime": "sing-box uptime",
        "cpu": "CPU",
        "memory": "Memory",
        "disk": "Disk",
        "realtime": "Realtime",
        "noAddress": "No address reported."
      },
      "clients": {
        "title": "Top clients",
        "shown": "{count} shown",
        "loading": "Loading clients.",
        "empty": "No client traffic is available yet.",
        "state": "State",
        "total": "Total",
        "viewAll": "View all clients"
      },
      "events": {
        "title": "Recent events",
        "loading": "Loading audit events.",
        "rows": "{count} rows",
        "emptyOffline": "Recent audit events are unavailable while offline.",
        "emptyUnavailable": "Recent audit events could not be loaded.",
        "empty": "No recent audit events were returned."
      },
      "protocols": {
        "title": "Protocol summaries",
        "groups": "{count} protocol groups",
        "type": "Type",
        "activeShort": "Active",
        "totalShort": "Total",
        "tags": "Tags",
        "loading": "Loading inbounds.",
        "empty": "No inbounds are configured.",
        "inboundTags": "{count} inbound tags",
        "noTag": "No tag reported."
      }
    }
  },
  "objects": {
    "inbound": "Inbound",
    "client": "Client",
    "outbound": "Outbound",
    "endpoint": "Endpoint",
    "config": "Config",
    "rule": "Rule",
    "ruleset": "Ruleset",
    "service": "Service",
    "dnsserver": "DNS Server",
    "dnsrule": "DNS Rule",
    "user": "User",
    "tag": "Tag",
    "listen": "Listen",
    "dial": "Dial",
    "tls": "TLS",
    "multiplex": "Multiplex",
    "transport": "Transport",
    "headers": "Headers",
    "key": "Key",
    "value": "Value"
  },
  "actions": {
    "action": "Action",
    "add": "Add",
    "addbulk": "Add Bulk",
    "editbulk": "Edit Bulk",
    "delbulk": "Delete Bulk",
    "new": "New",
    "edit": "Edit",
    "del": "Delete",
    "clone": "Clone",
    "test": "Test",
    "testAll": "Test all",
    "save": "Save",
    "saveOrder": "Save order",
    "cancelOrder": "Cancel order",
    "update": "Update",
    "sortByNameAsc": "Name A-Z",
    "sortByNameDesc": "Name Z-A",
    "submit": "Submit",
    "set": "Set",
    "generate": "Generate",
    "disable": "Disable",
    "close": "Close",
    "cancel": "Cancel",
    "refresh": "Refresh",
    "diagnose": "Diagnose",
    "restartApp": "Restart App",
    "restartSb": "Restart Singbox",
    "logoutAllAdmins": "Log out all admins"
  },
  "unsavedOrder": "Manual order has not been saved. Leave this page anyway?",
  "presets": {
    "title": "RU/ZH Routing and DNS Presets",
    "subtitle": "Apply rule-set, routing, DNS, and cache-file changes to the local unsaved config.",
    "preset": "Preset",
    "proxyOutbound": "Proxy outbound",
    "directOutbound": "Direct outbound",
    "apply": "Apply to local config",
    "preview": "Preview diff",
    "sources": "Sources",
    "source": "Source",
    "presetManaged": "Preset",
    "custom": "Custom",
    "selectOutbounds": "Choose proxy and direct outbound tags before applying.",
    "sameOutboundWarning": "Proxy and direct outbound are the same. Split routing stays inactive until you pick a distinct proxy outbound.",
    "zhMainlandDirect": "ZH: mainland direct",
    "zhMainlandDirectDesc": "Routes CN geosite/geoip direct and non-CN domains through the selected proxy outbound.",
    "zhNonCnProxy": "ZH: non-CN proxy",
    "zhNonCnProxyDesc": "Adds a smaller split where non-CN domains use proxy and CN domains use direct.",
    "ruBlockedProxy": "RU: blocked IP proxy",
    "ruBlockedProxyDesc": "Routes runetfreedom blocked ranges through proxy and private ranges through direct."
  },
  "regionalPresets": {
    "title": "Regional presets",
    "subtitle": "Configure routing and DNS for Russian and Chinese domains in one place.",
    "open": "Regional presets",
    "cancel": "Cancel",
    "preview": "Preview changes",
    "apply": "Apply presets",
    "applied": "Regional presets applied",
    "back": "Back",
    "done": "Done",
    "needFullControl": "Need full control?",
    "editRulesManually": "Edit rules manually.",
    "proxyOutbound": "Proxy outbound",
    "directOutbound": "Direct outbound",
    "selectOutbounds": "Choose proxy and direct outbound tags before previewing changes.",
    "sameOutboundWarning": "Proxy and direct outbound are the same. Split routing will not change traffic until you pick distinct outbounds.",
    "region": {
      "ru": {
        "title": "RU routing and DNS",
        "description": "Use a ready-made setup for Russian domains and DNS behavior."
      },
      "zh": {
        "title": "ZH routing and DNS",
        "description": "Use a ready-made setup for Chinese domains and DNS behavior."
      },
      "status": {
        "notConfigured": "Not configured",
        "enabled": "Enabled",
        "pendingChange": "Enabled, pending change",
        "customDetected": "Custom changes detected",
        "cannotApply": "Cannot apply preset"
      }
    },
    "direction": {
      "title": "Direction",
      "direct": {
        "title": "Direct",
        "description": "Regional domains bypass proxy. Good for local services that work better from your server's location."
      },
      "proxy": {
        "title": "Through proxy",
        "description": "Regional domains use proxy. Good when you want this region to follow your proxy route."
      }
    },
    "dns": {
      "behavior": "DNS will match {mode} mode for {region} domains."
    },
    "previewGroups": {
      "willAdd": "Will add",
      "willChange": "Will change",
      "willKeep": "Will keep",
      "willRemove": "Will remove",
      "noChanges": "No changes",
      "securityNote": "Custom rules and DNS entries will be kept. Preset changes apply only after you confirm.",
      "securityWarnings": "Security warnings",
      "noWarnings": "No warnings detected"
    },
    "security": {
      "note": "Review changes before applying. Presets can affect DNS privacy and traffic paths.",
      "dnsLeakRisk": "This choice may resolve regional domains through a DNS path that differs from the selected route. Review before applying.",
      "routeExposureRisk": "This choice may send regional traffic outside the path you expected. Review before applying.",
      "partialApplyBlocked": "Routing and DNS changes must be saved together. Nothing was changed.",
      "customItemsKept": "Custom rules and DNS entries will be kept."
    },
    "advanced": {
      "title": "Advanced options",
      "exceptions": "Exceptions",
      "exceptionsHelp": "Domains listed here will not follow this regional preset.",
      "addDomain": "Add domain",
      "removeDomain": "Remove domain",
      "noExceptions": "No exceptions added.",
      "invalidDomain": "Enter a valid domain."
    },
    "result": {
      "customItemsKept": "Preset-managed rules and DNS entries were updated. Custom items were kept.",
      "failed": "Preset was not applied",
      "regionalDataUnavailable": "Required regional domain data is not available. Update regional data and try again."
    }
  },
  "delivery": {
    "title": "Delivery",
    "rawLinks": "Raw links",
    "subscriptionUrl": "Subscription URL",
    "importUrl": "Import URL",
    "testUrl": "Test URL",
    "testOk": "Subscription URL is reachable. This only checks reachability, not that the response is a valid subscription.",
    "testFailed": "Subscription URL test failed.",
    "noRawLinks": "No raw client links are stored for this client."
  },
  "login": {
    "title": "Login",
    "username": "Username",
    "unRules": "Username can not be empty",
    "password": "Password",
    "pwRules": "Password can not be empty",
    "invalidCredentials": "Invalid username or password."
  },
  "menu": {
    "logout": "Logout",
    "language": "Language",
    "theme": "Theme",
    "navigation": "Toggle navigation"
  },
  "admin": {
    "addAdmin": "Add admin",
    "deleteAdmin": "Delete admin",
    "username": "Username",
    "changeCred": "Change credentials",
    "oldPass": "Current Password",
    "newUname": "New Username",
    "newPass": "New Password",
    "confirmPass": "Confirm Password",
    "addValidation": "Fill in current password, username, and both password fields.",
    "deleteValidation": "Current password is required.",
    "passwordMismatch": "Passwords do not match.",
    "deleteConfirm": "This admin and their API tokens will be deleted. Continue?",
    "deleteSelfDisabled": "You cannot delete your own admin account.",
    "lastLogin": "Last login",
    "date": "Date",
    "time": "Time",
    "changes": "Changes",
    "logoutAll": "Log out all admins",
    "logoutAllConfirm": "All admin web sessions will be invalidated, including your current session. API tokens are not affected. Continue?",
    "actor": "Actor",
    "key": "Key",
    "action": "Action",
    "api": {
      "title": "API Tokens",
      "msg": "Please copy the token below and store it somewhere safe. It will not be shown again.",
      "token": "Token",
      "scope": "Scope",
      "enabled": "Enabled"
    }
  },
  "types": {
    "un": "Username",
    "pw": "Password",
    "direct": {
      "overrideAddr": "Override Address",
      "overridePort": "Override Port"
    },
    "hy": {
      "obfs": "Obfuscated Password",
      "auth": "Authentication Password",
      "hyOptions": "Hysteria Options",
      "hy2Options": "Hysteria2 Options",
      "ignoreBw": "Ignore Client Bandwidth"
    },
    "shdwTls": {
      "hs": "Handshake Server",
      "addHS": "Add Handshake Server"
    },
    "ssh": {
      "passphrase": "Passphrase",
      "hostKey": "Host Keys",
      "algorithm": "Key Algorithms",
      "clientVer": "Client Version",
      "options": "SSH Options"
    },
    "tor": {
      "execPath": "Executable File Path",
      "dataDir": "Data Directory",
      "extArgs": "Extra Args"
    },
    "tuic": {
      "congControl": "Congestion Control",
      "authTimeout": "Authentication Timeout",
      "hb": "Heartbeat"
    },
    "tun": {
      "addr": "Addresses",
      "ifName": "Interface Name",
      "excludeMptcp": "Exclude MPTCP",
      "fallbackRuleIndex": "iproute2 Fallback Rule Index",
      "resetMark": "Auto Redirect Reset Mark",
      "nfqueue": "Auto Redirect NFQUEUE"
    },
    "vless": {
      "flow": "Flow",
      "udpEnc": "UDP Packet Encoding"
    },
    "vmess": {
      "security": "Security",
      "globalPadding": "Global Padding",
      "authLen": "Encryptrd Length"
    },
    "wg": {
      "privKey": "Private Key",
      "pubKey": "Peer Public Key",
      "psk": "Pre-Shared Key",
      "localIp": "Local IPs",
      "worker": "Workers",
      "ifName": "Interface Name",
      "sysIf": "System Interface",
      "options": "Wireguard Options",
      "allowedIp": "Allowed IPs",
      "peer": "Peer",
      "peers": "Peers"
    },
    "lb": {
      "defaultOut": "Default Outbound",
      "interruptConn": "Interrupt exist connections",
      "testUrl": "Test URL",
      "interval": "Interval",
      "tolerance": "Tolerance",
      "urlTestOptions": "URLTest Options"
    },
    "failover": {
      "title": "Failover Group",
      "members": "Priority Outbounds",
      "primary": "Primary",
      "backup": "Backup",
      "addMember": "Add Outbound",
      "probeTarget": "Probe URL",
      "interval": "Probe Interval",
      "hysteresis": "Recovery Threshold",
      "enabled": "Automatic Switching",
      "allDown": "All outbounds are unavailable"
    },
    "ts": {
      "options": "Tailscale Options",
      "stateDir": "State Directory",
      "authKey": "Authentication Key",
      "relayServer": "Relay Server",
      "relayServerPort": "Relay Server Port",
      "relayEndpoints": "Relay Static Endpoints",
      "systemInterface": "System Interface",
      "sysIfName": "Interface Name",
      "sysIfMtu": "Interface MTU",
      "controlUrl": "Control URL",
      "ephemeral": "Ephemeral Mode",
      "hostname": "Hostname",
      "acceptRoutes": "Accept Routes",
      "exitNode": "Exit Node",
      "allowLanAccess": "Allow LAN Access",
      "advRoutes": "Advertise Routes",
      "advTags": "Advertise Tags",
      "advExitNode": "Advertise Exit Node",
      "udpTimeout": "UDP Timeout"
    },
    "oom": {
      "memoryLimit": "Memory Limit",
      "safetyMargin": "Safety Margin",
      "minInterval": "Minimum Interval",
      "maxInterval": "Maximum Interval",
      "checksBeforeLimit": "Checks Before Limit"
    },
    "derp": {
      "configPath": "Config Path",
      "verifyClientEndpoint": "Verify Client Endpoint",
      "verifyClientUrl": "Verify Client URL",
      "meshWith": "Mesh With",
      "meshPsk": "Mesh PSK",
      "meshPskFile": "Mesh PSK File",
      "stun": "STUN Server",
      "options": "DERP Options"
    },
    "anytls": {
      "idleInterval": "Idle Session Check Interval",
      "idleTimeout": "Idle Session Timeout",
      "minIdle": "Minimum Idle Session"
    },
    "naive": {
      "insecureConcurrency": "Insecure Concurrency",
      "quic": "QUIC",
      "quicCongestion": "QUIC Congestion Control",
      "udpOverTcp": "UDP over TCP",
      "streamReceiveWindow": "Stream Receive Window",
      "quicSessionReceiveWindow": "QUIC Session Receive Window"
    }
  },
  "basic": {
    "log": {
      "title": "Logs",
      "level": "Level",
      "output": "Output",
      "timestamp": "Enable Timestamp"
    },
    "routing": {
      "title": "Routing",
      "defaultOut": "Default Outbound",
      "defaultIf": "Default NIC",
      "defaultRm": "Default Routing Mark",
      "defaultDns": "Default DNS Resolver",
      "autoBind": "Auto Bind NIC"
    },
    "exp": {
      "storeFakeIp": "Store Fake IP",
      "extController": "External Controller",
      "extUi": "External UI",
      "extUiDownloadUrl": "UI Download URL",
      "extUiDownloadDetour": "UI Download detour",
      "secret": "Secret",
      "defaultMode": "Default Mode",
      "allowOrigin": "Allow Origin",
      "allowPrivate": "Allow Private Network"
    },
    "hint": {
      "logOutput": "File the sing-box core writes logs to. Leave empty for stderr. Example: box.log.",
      "ntpServer": "NTP server used to sync the clock. Default: time.apple.com.",
      "ntpPort": "NTP server port. Default: 123.",
      "ntpInterval": "How often the clock is synchronized, in minutes. Default: 30.",
      "cachePath": "File used to cache DNS, FakeIP, and rejected-domain data. Default: cache.db.",
      "cacheId": "Optional namespace used to isolate this profile's cache.",
      "debugListen": "Address for the Go pprof debug server, for example 127.0.0.1:8080.",
      "gcPercent": "Go garbage-collector target percentage. Empty uses the Go default (100).",
      "memoryLimit": "Soft memory limit for the core, for example 256MiB. Empty means no limit.",
      "maxStack": "Maximum goroutine stack size in bytes. Empty uses the Go default.",
      "maxThreads": "Maximum OS threads available to the core. Empty uses the Go default.",
      "clashController": "Address the Clash API listens on, for example 127.0.0.1:9090.",
      "clashSecret": "Bearer token for the Clash API. Use a strong value when exposed.",
      "clashExtUi": "Local directory serving the Clash dashboard. Optional.",
      "clashExtUiUrl": "URL used to download the Clash dashboard. Optional.",
      "clashDefaultMode": "Default Clash routing mode. Optional.",
      "clashAllowOrigin": "Allowed CORS origins for the Clash API, comma-separated.",
      "v2rayListen": "Address the V2Ray gRPC stats API listens on, for example 127.0.0.1:8080."
    }
  },
  "date": {
    "expiry": "Expiry",
    "expired": "Expired",
    "d": "d",
    "h": "h",
    "m": "m",
    "s": "s",
    "ms": "ms"
  }
}
