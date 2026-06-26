package ipcertcmd

import "fmt"

func printIpCertUsage() {
	fmt.Println("Usage:")
	fmt.Println("    solovey-ui ip-cert issue -ip <ip> -email <email> [-port 80] [-no-renew]")
	fmt.Println("        issue a Let's Encrypt certificate for a bare IP and apply it to the panel HTTPS listener")
	fmt.Println("    solovey-ui ip-cert renew")
	fmt.Println("        re-issue now using the stored IP/email/port")
	fmt.Println("    solovey-ui ip-cert status")
	fmt.Println("        show the managed certificate state")
	fmt.Println("    solovey-ui ip-cert disable")
	fmt.Println("        turn off 12h auto-renewal")
}
