package ipcertcmd

import "fmt"

// Run handles `solovey-ui ip-cert <issue|renew|status|disable>`.
// Issuance happens in-process via go-acme/lego (the panel binary embeds it).
// The panel's cron runner performs auto-renewal; loading a freshly applied
// panel certificate still requires a panel restart.
func Run(args []string) int {
	if len(args) == 0 {
		printIpCertUsage()
		return 2
	}
	switch args[0] {
	case "issue":
		return ipCertIssue(args[1:])
	case "renew":
		return ipCertRenew(args[1:])
	case "status":
		return ipCertStatus()
	case "disable":
		return ipCertDisable()
	default:
		fmt.Println("ip-cert: unknown sub-command:", args[0])
		printIpCertUsage()
		return 2
	}
}
