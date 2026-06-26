package ipcertcmd

import (
	"context"
	"flag"
	"fmt"
	"strings"
)

func ipCertIssue(args []string) int {
	fs := flag.NewFlagSet("ip-cert issue", flag.ContinueOnError)
	var ip, email string
	var port int
	var noRenew bool
	fs.StringVar(&ip, "ip", "", "public IP address to certify")
	fs.StringVar(&email, "email", "", "ACME account email")
	fs.IntVar(&port, "port", 80, "HTTP-01 challenge port")
	fs.BoolVar(&noRenew, "no-renew", false, "do not enable 12h auto-renewal")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ip = strings.TrimSpace(ip)
	if ip == "" {
		fmt.Println("ip-cert: -ip is required")
		printIpCertUsage()
		return 2
	}

	svc, err := newIpCertService()
	if err != nil {
		fmt.Println("ip-cert:", err)
		return 1
	}

	fmt.Printf("ip-cert: issuing certificate for %s (HTTP-01 challenge on port %d)...\n", ip, port)
	status, err := svc.IssueForCLI(context.Background(), ip, strings.TrimSpace(email), port)
	if err != nil {
		fmt.Println("ip-cert: issue failed:", err)
		return 1
	}

	if !noRenew {
		if err := svc.Settings.SetIpCertEnabled(true); err != nil {
			fmt.Println("ip-cert: warning: could not enable auto-renew:", err)
		} else {
			status.Enabled = true
		}
	}

	printIpCertStatus(status)
	fmt.Println("ip-cert: applied to the panel HTTPS listener; restart the panel to load it.")
	return 0
}

func ipCertRenew(args []string) int {
	fs := flag.NewFlagSet("ip-cert renew", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	svc, err := newIpCertService()
	if err != nil {
		fmt.Println("ip-cert:", err)
		return 1
	}

	ip, err := svc.Settings.GetIpCertTargetIP()
	if err != nil {
		fmt.Println("ip-cert:", err)
		return 1
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		fmt.Println("ip-cert: no stored IP; run `solovey-ui ip-cert issue` first")
		return 1
	}
	email, _ := svc.Settings.GetIpCertEmail()
	port, _ := svc.Settings.GetIpCertChallengePort()

	fmt.Printf("ip-cert: re-issuing certificate for %s...\n", ip)
	status, err := svc.IssueForCLI(context.Background(), ip, strings.TrimSpace(email), port)
	if err != nil {
		fmt.Println("ip-cert: renew failed:", err)
		return 1
	}

	printIpCertStatus(status)
	fmt.Println("ip-cert: applied to the panel HTTPS listener; restart the panel to load it.")
	return 0
}
