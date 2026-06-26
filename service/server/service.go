// Package server provides host, runtime, database, and log diagnostics.
package server

import (
	"net/netip"
	"os"
	"runtime"
	"strings"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

type ServerService struct {
	coreStatus func() (running bool, uptime uint32)
}

var systemInfoInterfaces = net.Interfaces

func New(coreStatus func() (running bool, uptime uint32)) ServerService {
	return ServerService{coreStatus: coreStatus}
}

func (s *ServerService) GetStatus(request string) *map[string]interface{} {
	status := make(map[string]interface{}, 0)
	requests := strings.Split(request, ",")
	for _, req := range requests {
		switch req {
		case "cpu":
			status["cpu"] = s.GetCpuPercent()
		case "mem":
			status["mem"] = s.GetMemInfo()
		case "dsk":
			status["dsk"] = s.GetDiskInfo()
		case "dio":
			status["dio"] = s.GetDiskIO()
		case "swp":
			status["swp"] = s.GetSwapInfo()
		case "net":
			status["net"] = s.GetNetInfo()
		case "sys":
			status["sys"] = s.GetSystemInfo()
		case "sbd":
			status["sbd"] = s.GetSingboxInfo()
		case "db":
			status["db"] = s.GetDatabaseInfo()
		}
	}
	return &status
}

func (s *ServerService) GetCpuPercent() float64 {
	percents, err := cpu.Percent(0, false)
	if err != nil {
		logger.Warning("get cpu percent failed:", err)
		return 0
	} else {
		return percents[0]
	}
}

func (s *ServerService) GetMemInfo() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		logger.Warning("get virtual memory failed:", err)
	} else {
		info["current"] = memInfo.Used
		info["total"] = memInfo.Total
	}
	return info
}

func (s *ServerService) GetDiskInfo() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	diskInfo, err := disk.Usage("/")
	if err != nil {
		logger.Warning("get disk usage failed:", err)
	} else {
		info["current"] = diskInfo.Used
		info["total"] = diskInfo.Total
	}
	return info
}

func (s *ServerService) GetDiskIO() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	ioStats, err := disk.IOCounters()
	if err != nil {
		logger.Warning("get disk io counters failed:", err)
	} else if len(ioStats) > 0 {
		infoR, infoW := uint64(0), uint64(0)
		for _, ioStat := range ioStats {
			infoR += ioStat.ReadBytes
			infoW += ioStat.WriteBytes
		}
		info["read"] = infoR
		info["write"] = infoW
	} else {
		logger.Warning("can not find disk io counters")
	}
	return info
}

func (s *ServerService) GetSwapInfo() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	swapInfo, err := mem.SwapMemory()
	if err != nil {
		logger.Warning("get swap memory failed:", err)
	} else {
		info["current"] = swapInfo.Used
		info["total"] = swapInfo.Total
	}
	return info
}

func (s *ServerService) GetNetInfo() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	ioStats, err := net.IOCounters(false)
	if err != nil {
		logger.Warning("get io counters failed:", err)
	} else if len(ioStats) > 0 {
		ioStat := ioStats[0]
		info["sent"] = ioStat.BytesSent
		info["recv"] = ioStat.BytesRecv
		info["psent"] = ioStat.PacketsSent
		info["precv"] = ioStat.PacketsRecv
	} else {
		logger.Warning("can not find io counters")
	}
	return info
}

func (s *ServerService) GetSingboxInfo() map[string]interface{} {
	var rtm runtime.MemStats
	runtime.ReadMemStats(&rtm)
	isRunning, uptime := false, uint32(0)
	if s != nil && s.coreStatus != nil {
		isRunning, uptime = s.coreStatus()
	}
	return map[string]interface{}{
		"running": isRunning,
		"stats": map[string]interface{}{
			"NumGoroutine": runtime.NumGoroutine(),
			"Alloc":        rtm.Alloc,
			"Uptime":       uptime,
		},
	}
}

func (s *ServerService) GetSystemInfo() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	var rtm runtime.MemStats
	runtime.ReadMemStats(&rtm)

	info["appMem"] = rtm.Sys
	info["appThreads"] = runtime.NumGoroutine()
	cpuInfo, err := cpu.Info()
	if err == nil && len(cpuInfo) > 0 {
		info["cpuType"] = cpuInfo[0].ModelName
	}
	info["cpuCount"] = runtime.NumCPU()
	info["hostName"], _ = os.Hostname()
	info["appVersion"] = configidentity.GetVersion()
	ipv4 := make([]string, 0)
	ipv6 := make([]string, 0)
	// get ip address
	netInterfaces, err := systemInfoInterfaces()
	if err != nil {
		logger.Warning("get net interfaces failed:", err)
	} else {
		for i := 0; i < len(netInterfaces); i++ {
			if !systemInterfaceUsable(netInterfaces[i].Flags) {
				continue
			}
			addrs := netInterfaces[i].Addrs

			for _, address := range addrs {
				publicAddress, ok := systemInfoPublicAddress(address.Addr)
				if !ok {
					continue
				}
				if strings.Contains(publicAddress, ".") {
					ipv4 = append(ipv4, publicAddress)
				} else {
					ipv6 = append(ipv6, publicAddress)
				}
			}
		}
	}
	info["ipv4"] = ipv4
	info["ipv6"] = ipv6
	info["bootTime"], _ = host.BootTime()

	return info
}

func systemInfoPublicAddress(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}
	var addr netip.Addr
	if prefix, err := netip.ParsePrefix(value); err == nil {
		addr = prefix.Addr()
	} else if parsed, err := netip.ParseAddr(value); err == nil {
		addr = parsed
	} else {
		return "", false
	}
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() ||
		addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() ||
		addr.IsUnspecified() {
		return "", false
	}
	return value, true
}

func systemInterfaceUsable(flags []string) bool {
	up := false
	for _, flag := range flags {
		switch strings.ToLower(flag) {
		case "up":
			up = true
		case "loopback":
			return false
		}
	}
	return up
}

func (s *ServerService) GetDatabaseInfo() map[string]int64 {
	info := make(map[string]int64, 0)
	db := dbsqlite.DB()
	if db == nil {
		return nil
	}

	var clientsCount, inboundsCount, outboundsCount, servicesCount, endpointsCount, clientUp, clientDown int64

	db.Model(&model.Client{}).Count(&clientsCount)
	db.Model(&model.Inbound{}).Count(&inboundsCount)
	db.Model(&model.Outbound{}).Count(&outboundsCount)
	db.Model(&model.Service{}).Count(&servicesCount)
	db.Model(&model.Endpoint{}).Count(&endpointsCount)
	db.Model(&model.Client{}).Select("COALESCE(SUM(up+total_up),0)").Scan(&clientUp)
	db.Model(&model.Client{}).Select("COALESCE(SUM(down+total_down),0)").Scan(&clientDown)

	info["clients"] = clientsCount
	info["inbounds"] = inboundsCount
	info["outbounds"] = outboundsCount
	info["services"] = servicesCount
	info["endpoints"] = endpointsCount
	info["clientUp"] = clientUp
	info["clientDown"] = clientDown

	return info
}
