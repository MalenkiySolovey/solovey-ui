package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	ipcertops "github.com/MalenkiySolovey/solovey-ui/internal/ops/ipcert"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

// ipCertPanelRestartDelay gives the HTTP response time to flush before the
// panel restarts to pick up the new certificate.
const ipCertPanelRestartDelay = 3 * time.Second

// applyToTarget installs the freshly issued cert/key at the requested target:
// either the panel HTTPS listener (settings + restart) or an inbound TLS
// profile (hot-reload, no core restart).
func (s *IpCertificateService) applyToTarget(target, certPath, keyPath, hostname string) error {
	if target == "" || target == "panel" {
		return s.applyToPanel(certPath, keyPath)
	}
	rest, ok := strings.CutPrefix(target, "inbound:")
	if !ok {
		return common.NewError("ip cert: unknown apply target: ", target)
	}
	tlsID, err := strconv.Atoi(rest)
	if err != nil || tlsID <= 0 {
		return common.NewError("ip cert: invalid inbound tls id: ", rest)
	}
	return s.applyToInboundTls(uint(tlsID), certPath, keyPath, hostname)
}

// applyToPanel points the panel cert settings at the managed files and
// schedules a panel restart so web.go reloads them. Used from inside the live
// panel (the renewal cron); the CLI path uses setPanelCertSettings without the
// restart.
func (s *IpCertificateService) applyToPanel(certPath, keyPath string) error {
	if err := s.setPanelCertSettings(certPath, keyPath); err != nil {
		return err
	}
	manager := runtimeOrDefault(s.Runtime).restart()
	if manager == nil {
		return common.NewError("ip cert: panel restart manager is not configured")
	}
	return manager.ScheduleRestartBlocking(ipCertPanelRestartDelay)
}

// setPanelCertSettings points the panel HTTPS cert settings at the managed
// files without restarting anything. The panel re-reads webCertFile/webKeyFile
// only on a full restart, so the caller is responsible for restarting the panel.
func (s *IpCertificateService) setPanelCertSettings(certPath, keyPath string) error {
	set := s.settings()
	if err := set.setString("webCertFile", certPath); err != nil {
		return err
	}
	return set.setString("webKeyFile", keyPath)
}

// applyToInboundTls patches the chosen TLS row's server block with the managed
// certificate_path/key_path and routes the change through ConfigService.Save so
// only the affected inbounds/services hot-reload.
func (s *IpCertificateService) applyToInboundTls(tlsID uint, certPath, keyPath, hostname string) error {
	db := dbsqlite.DB()
	if db == nil {
		return common.NewError("ip cert: database is not initialized")
	}
	var tls model.Tls
	if err := db.Model(model.Tls{}).Where("id = ?", tlsID).First(&tls).Error; err != nil {
		return common.NewError("ip cert: tls profile not found: ", err.Error())
	}

	patchedServer, err := patchTlsServerBlock(tls.Server, certPath, keyPath)
	if err != nil {
		return err
	}
	tls.Server = patchedServer

	payload, err := json.Marshal(tls)
	if err != nil {
		return err
	}

	config := NewConfigServiceWithRuntime(s.Runtime)
	_, err = config.Save("tls", "edit", payload, "", "", hostname)
	return err
}

// patchTlsServerBlock points a sing-box TLS server block at the managed
// certificate/key files. Pure (no I/O): it parses the existing server JSON,
// sets certificate_path/key_path, and drops any inline certificate/key bytes
// that would otherwise shadow the file paths in sing-box.
func patchTlsServerBlock(serverJSON json.RawMessage, certPath, keyPath string) (json.RawMessage, error) {
	return ipcertops.PatchTLSServerBlock(serverJSON, certPath, keyPath)
}
