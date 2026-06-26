package settingscmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/shirou/gopsutil/v4/net"
)

func PanelURI() {
	err := dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}
	settingService := service.SettingService{}
	Port, _ := settingService.GetPort()
	BasePath, _ := settingService.GetWebPath()
	Listen, _ := settingService.GetListen()
	Domain, _ := settingService.GetWebDomain()
	KeyFile, _ := settingService.GetKeyFile()
	CertFile, _ := settingService.GetCertFile()
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
		return
	}
	if len(Listen) > 0 {
		fmt.Println(Proto + Listen + PortText + BasePath)
		return
	}
	fmt.Println("Local address:")
	netInterfaces, _ := net.Interfaces()
	for i := 0; i < len(netInterfaces); i++ {
		if len(netInterfaces[i].Flags) > 2 && netInterfaces[i].Flags[0] == "up" && netInterfaces[i].Flags[1] != "loopback" {
			addrs := netInterfaces[i].Addrs
			for _, address := range addrs {
				IP := strings.Split(address.Addr, "/")[0]
				if strings.Contains(address.Addr, ".") {
					fmt.Println(Proto + IP + PortText + BasePath)
				} else if address.Addr[0:6] != "fe80::" {
					fmt.Println(Proto + "[" + IP + "]" + PortText + BasePath)
				}
			}
		}
	}
	pubIP := getPublicIP()
	if pubIP != "" {
		fmt.Printf("\nGlobal address:\n%s%s%s\n", Proto, pubIP, PortText+BasePath)
	}
}

func getPublicIP() string {
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
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 3 * time.Second}

	for _, api := range apis {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
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
			body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if err != nil {
				ch <- result{"", err}
				return
			}
			ch <- result{string(body), nil}
		}(api)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for res := range ch {
		if res.err == nil && res.ip != "" {
			return strings.TrimSpace(res.ip)
		}
	}
	return ""
}
