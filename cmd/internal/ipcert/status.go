package ipcertcmd

import (
	"fmt"

	ipcertops "github.com/MalenkiySolovey/solovey-ui/internal/ops/ipcert"
)

func ipCertStatus() int {
	svc, err := newIpCertService()
	if err != nil {
		fmt.Println("ip-cert:", err)
		return 1
	}
	status, err := svc.GetStatus()
	if err != nil {
		fmt.Println("ip-cert:", err)
		return 1
	}
	printIpCertStatus(status)
	return 0
}

func ipCertDisable() int {
	svc, err := newIpCertService()
	if err != nil {
		fmt.Println("ip-cert:", err)
		return 1
	}
	if err := svc.Settings.SetIpCertEnabled(false); err != nil {
		fmt.Println("ip-cert:", err)
		return 1
	}
	fmt.Println("ip-cert: auto-renewal disabled")
	return 0
}

func printIpCertStatus(s ipcertops.Status) {
	fmt.Println("IP certificate status:")
	fmt.Println("\tAuto-renew:\t", s.Enabled)
	if s.TargetIP != "" {
		fmt.Println("\tTarget IP:\t", s.TargetIP)
	}
	fmt.Println("\tIssued:   \t", s.Issued)
	if s.Issued {
		fmt.Println("\tExpires:  \t", s.NotAfter)
		fmt.Printf("\tDays left:\t %.1f\n", s.DaysRemaining)
	}
	if s.LastIssue != "" {
		fmt.Println("\tLast issue:\t", s.LastIssue)
	}
	if s.CertPath != "" {
		fmt.Println("\tCert path:\t", s.CertPath)
	}
}
