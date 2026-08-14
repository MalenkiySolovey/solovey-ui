package settingscmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/shirou/gopsutil/v4/net"
)

func PanelURI() error {
	err := dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		return err
	}
	settingService := service.SettingService{}
	Port, err := settingService.GetPort()
	if err != nil {
		return fmt.Errorf("read panel port: %w", err)
	}
	BasePath, err := settingService.GetWebPath()
	if err != nil {
		return fmt.Errorf("read panel path: %w", err)
	}
	Listen, err := settingService.GetListen()
	if err != nil {
		return fmt.Errorf("read panel listen address: %w", err)
	}
	Domain, err := settingService.GetWebDomain()
	if err != nil {
		return fmt.Errorf("read panel domain: %w", err)
	}
	KeyFile, err := settingService.GetKeyFile()
	if err != nil {
		return fmt.Errorf("read panel TLS key setting: %w", err)
	}
	CertFile, err := settingService.GetCertFile()
	if err != nil {
		return fmt.Errorf("read panel TLS certificate setting: %w", err)
	}
	TLS := false
	if KeyFile != "" && CertFile != "" {
		TLS = true
	}
	Proto := ""
	if TLS {
		Proto = "https://"
	} else {
		Proto = "http://"
	}
	PortText := fmt.Sprintf(":%d", Port)
	if (Port == 443 && TLS) || (Port == 80 && !TLS) {
		PortText = ""
	}
	if len(Domain) > 0 {
		fmt.Println(Proto + Domain + PortText + BasePath)
		return nil
	}
	if len(Listen) > 0 {
		fmt.Println(Proto + Listen + PortText + BasePath)
		return nil
	}
	fmt.Println("Local address:")
	netInterfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("list local interfaces: %w", err)
	}
	for i := 0; i < len(netInterfaces); i++ {
		if slices.Contains(netInterfaces[i].Flags, "up") && !slices.Contains(netInterfaces[i].Flags, "loopback") {
			addrs := netInterfaces[i].Addrs
			for _, address := range addrs {
				prefix, parseErr := netip.ParsePrefix(address.Addr)
				if parseErr != nil || prefix.Addr().IsLoopback() || prefix.Addr().IsLinkLocalUnicast() {
					continue
				}
				ip := prefix.Addr()
				if ip.Is4() {
					fmt.Println(Proto + ip.String() + PortText + BasePath)
				} else {
					fmt.Println(Proto + "[" + ip.String() + "]" + PortText + BasePath)
				}
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pubIP := getPublicIP(ctx)
	if pubIP != "" {
		fmt.Printf("\nGlobal address:\n%s%s%s\n", Proto, pubIP, PortText+BasePath)
	}
	return nil
}

func getPublicIP(ctx context.Context) string {
	apis := []string{
		"https://api64.ipify.org",
		"https://ip.sb",
		"https://icanhazip.com",
		"https://ipinfo.io/ip",
		"https://checkip.amazonaws.com",
	}
	type result struct {
		ip  string
		err error
	}
	ch := make(chan result, len(apis))
	client := &http.Client{Timeout: 3 * time.Second}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, api := range apis {
		go func(url string) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				ch <- result{"", err}
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				ch <- result{"", err}
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				ch <- result{"", fmt.Errorf("unexpected status %d", resp.StatusCode)}
				return
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
			if err != nil {
				ch <- result{"", err}
				return
			}
			ip, err := netip.ParseAddr(strings.TrimSpace(string(body)))
			if err != nil {
				ch <- result{"", err}
				return
			}
			ch <- result{ip.String(), nil}
		}(api)
	}

	for range apis {
		select {
		case res := <-ch:
			if res.err == nil && res.ip != "" {
				cancel()
				return res.ip
			}
		case <-ctx.Done():
			return ""
		}
	}
	return ""
}
