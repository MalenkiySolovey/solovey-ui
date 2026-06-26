package service

import (
	"context"
	"time"

	ipcertops "github.com/MalenkiySolovey/solovey-ui/internal/ops/ipcert"
	ipcertsvc "github.com/MalenkiySolovey/solovey-ui/service/ipcert"
)

// IpCertificateService binds certificate issuance to settings and runtime actions.
type IpCertificateService struct {
	Runtime  *Runtime
	Settings *SettingService
	acme     ipcertops.ACMEIssuer
	now      func() time.Time
}

func (s *IpCertificateService) settings() *SettingService {
	if s != nil && s.Settings != nil {
		return s.Settings
	}
	return &SettingService{}
}

func (s *IpCertificateService) backend() ipcertsvc.Service {
	return ipcertsvc.New(
		ipCertificateSettingsAdapter{s.settings()},
		s.acme,
		s.now,
		s.applyToTarget,
		s.setPanelCertSettings,
	)
}

func (s *IpCertificateService) IssueNow(ctx context.Context, ip, email string, port int, applyTarget, hostname string) (ipcertops.Status, error) {
	backend := s.backend()
	return backend.IssueNow(ctx, ip, email, port, applyTarget, hostname)
}

func (s *IpCertificateService) IssueForCLI(ctx context.Context, ip, email string, port int) (ipcertops.Status, error) {
	backend := s.backend()
	return backend.IssueForCLI(ctx, ip, email, port)
}

func (s *IpCertificateService) RenewIfNeeded(ctx context.Context) (bool, error) {
	backend := s.backend()
	return backend.RenewIfNeeded(ctx)
}

func (s *IpCertificateService) GetStatus() (ipcertops.Status, error) {
	backend := s.backend()
	return backend.GetStatus()
}

type ipCertificateSettingsAdapter struct {
	settings *SettingService
}

func (a ipCertificateSettingsAdapter) GetIpCertEnabled() (bool, error) {
	return a.settings.GetIpCertEnabled()
}
func (a ipCertificateSettingsAdapter) GetIpCertTargetIP() (string, error) {
	return a.settings.GetIpCertTargetIP()
}
func (a ipCertificateSettingsAdapter) GetIpCertEmail() (string, error) {
	return a.settings.GetIpCertEmail()
}
func (a ipCertificateSettingsAdapter) GetIpCertChallengePort() (int, error) {
	return a.settings.GetIpCertChallengePort()
}
func (a ipCertificateSettingsAdapter) GetIpCertApplyTarget() (string, error) {
	return a.settings.GetIpCertApplyTarget()
}
func (a ipCertificateSettingsAdapter) GetIpCertCertPath() (string, error) {
	return a.settings.GetIpCertCertPath()
}
func (a ipCertificateSettingsAdapter) GetIpCertNotAfter() (string, error) {
	return a.settings.GetIpCertNotAfter()
}
func (a ipCertificateSettingsAdapter) GetIpCertLastIssue() (string, error) {
	return a.settings.GetIpCertLastIssue()
}
func (a ipCertificateSettingsAdapter) AccountKey() (string, error) {
	return a.settings.getIpCertAccountKey()
}
func (a ipCertificateSettingsAdapter) SetAccountKey(value string) error {
	return a.settings.setIpCertAccountKey(value)
}
func (a ipCertificateSettingsAdapter) AccountRegistration() (string, error) {
	return a.settings.getIpCertAccountRegistration()
}
func (a ipCertificateSettingsAdapter) SetAccountRegistration(value string) error {
	return a.settings.setIpCertAccountRegistration(value)
}
func (a ipCertificateSettingsAdapter) LastIP() (string, error) {
	return a.settings.getIpCertLastIP()
}
func (a ipCertificateSettingsAdapter) SetTargetIP(value string) error {
	return a.settings.setIpCertTargetIP(value)
}
func (a ipCertificateSettingsAdapter) SetEmail(value string) error {
	return a.settings.setIpCertEmail(value)
}
func (a ipCertificateSettingsAdapter) SetChallengePort(value int) error {
	return a.settings.setIpCertChallengePort(value)
}
func (a ipCertificateSettingsAdapter) SetApplyTarget(value string) error {
	return a.settings.setIpCertApplyTarget(value)
}
func (a ipCertificateSettingsAdapter) SetLastIP(value string) error {
	return a.settings.setIpCertLastIP(value)
}
func (a ipCertificateSettingsAdapter) SetCertPath(value string) error {
	return a.settings.setIpCertCertPath(value)
}
func (a ipCertificateSettingsAdapter) SetKeyPath(value string) error {
	return a.settings.setIpCertKeyPath(value)
}
func (a ipCertificateSettingsAdapter) SetNotAfter(value string) error {
	return a.settings.setIpCertNotAfter(value)
}
func (a ipCertificateSettingsAdapter) SetLastIssue(value string) error {
	return a.settings.setIpCertLastIssue(value)
}
