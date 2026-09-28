package deploymentbroker

import _ "embed"

// systemdPanelAddressFamilies owns the native main-runtime socket allow-list.
// INET/INET6 serve proxy and management traffic; UNIX serves local broker IPC.
// NETLINK serves sing-tun's unprivileged route/link subscriptions and interface
// discovery. It does not grant network mutation authority or CAP_NET_ADMIN.
// Unit directives are projected by scripts/generate-systemd-policy.mjs.
//
//go:embed systemd-address-families.txt
var systemdPanelAddressFamilies string
