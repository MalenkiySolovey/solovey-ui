// Package ipcert orchestrates managed IP certificate issuance and renewal.
package ipcert

import (
	"context"
	"time"

	ipcertops "github.com/MalenkiySolovey/solovey-ui/internal/ops/ipcert"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type Settings interface {
	GetIpCertEnabled() (bool, error)
	GetIpCertTargetIP() (string, error)
	GetIpCertEmail() (string, error)
	GetIpCertChallengePort() (int, error)
	GetIpCertApplyTarget() (string, error)
	GetIpCertCertPath() (string, error)
	GetIpCertNotAfter() (string, error)
	GetIpCertLastIssue() (string, error)
	AccountKey() (string, error)
	SetAccountKey(string) error
	AccountRegistration() (string, error)
	SetAccountRegistration(string) error
	LastIP() (string, error)
	SetTargetIP(string) error
	SetEmail(string) error
	SetChallengePort(int) error
	SetApplyTarget(string) error
	SetLastIP(string) error
	SetCertPath(string) error
	SetKeyPath(string) error
	SetNotAfter(string) error
	SetLastIssue(string) error
}

// Service issues and renews a Let's Encrypt TLS certificate for a
// bare IP address (RFC 8738 / shortlived profile), then applies it to the panel
// HTTPS listener or an inbound TLS profile. ACME/network code sits behind the
// internal ipcert.ACMEIssuer seam so orchestration stays unit-testable without
// touching Let's Encrypt.
type Service struct {
	settings       Settings
	issuer         ipcertops.ACMEIssuer
	now            func() time.Time
	applyTarget    func(target, certPath, keyPath, hostname string) error
	setPanelTarget func(certPath, keyPath string) error
}

func New(settings Settings, issuer ipcertops.ACMEIssuer, now func() time.Time, applyTarget func(string, string, string, string) error, setPanelTarget func(string, string) error) Service {
	return Service{settings: settings, issuer: issuer, now: now, applyTarget: applyTarget, setPanelTarget: setPanelTarget}
}

func (s *Service) acmeIssuer() ipcertops.ACMEIssuer {
	if s.issuer != nil {
		return s.issuer
	}
	return ipcertops.LegoIssuer{}
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// IssueNow obtains a fresh certificate for ip and applies it to applyTarget.
// hostname is the request host used for inbound link regeneration ("" is fine
// for the panel target and for cron-driven renewals). Used by the renewal cron
// running inside the live panel, so a "panel" target also triggers a panel
// restart to reload the web certificate.
func (s *Service) IssueNow(ctx context.Context, ip, email string, port int, applyTarget, hostname string) (ipcertops.Status, error) {
	certPath, keyPath, err := s.obtainAndPersist(ctx, ip, email, port, applyTarget)
	if err != nil {
		return ipcertops.Status{}, err
	}

	if s.applyTarget == nil {
		return ipcertops.Status{}, common.NewError("ip cert: apply target is not configured")
	}
	if err := s.applyTarget(applyTarget, certPath, keyPath, hostname); err != nil {
		// The certificate is issued and persisted; surface the apply failure
		// but keep the stored state so a later retry/renewal can re-apply.
		status, _ := s.GetStatus()
		return status, common.NewError("ip cert: issued but apply failed: ", err.Error())
	}

	return s.GetStatus()
}

// IssueForCLI obtains a certificate from a one-shot CLI process and points the
// panel HTTPS cert settings at the new files. Unlike IssueNow it never restarts
// the panel because a CLI invocation has no live runtime. Restart the panel
// after issuance to reload the web certificate. The apply target is always the
// panel.
func (s *Service) IssueForCLI(ctx context.Context, ip, email string, port int) (ipcertops.Status, error) {
	certPath, keyPath, err := s.obtainAndPersist(ctx, ip, email, port, "panel")
	if err != nil {
		return ipcertops.Status{}, err
	}

	if s.setPanelTarget == nil {
		return ipcertops.Status{}, common.NewError("ip cert: panel target is not configured")
	}
	if err := s.setPanelTarget(certPath, keyPath); err != nil {
		// Certificate is issued and persisted; report the apply failure but keep
		// the stored state so the cron/CLI can re-apply later.
		status, _ := s.GetStatus()
		return status, common.NewError("ip cert: issued but apply failed: ", err.Error())
	}

	return s.GetStatus()
}

// obtainAndPersist validates the request, obtains the certificate through the
// ACME issuer, writes the cert/key files, and persists the issued state plus the
// (possibly newly created) ACME account. It returns the on-disk cert/key paths
// so the caller can apply them. It performs no apply itself.
func (s *Service) obtainAndPersist(ctx context.Context, ip, email string, port int, applyTarget string) (certPath, keyPath string, err error) {
	if err := ipcertops.ValidateIssuableIP(ip); err != nil {
		return "", "", err
	}
	if err := ipcertops.ValidateApplyTarget(applyTarget); err != nil {
		return "", "", err
	}
	if err := ipcertops.ValidateEmail(email, false); err != nil {
		return "", "", err
	}
	if port <= 0 {
		port = 80
	}
	if err := ipcertops.ValidatePort(port); err != nil {
		return "", "", err
	}

	set := s.settings
	accountKey, err := set.AccountKey()
	if err != nil {
		return "", "", err
	}
	registrationJSON, err := set.AccountRegistration()
	if err != nil {
		return "", "", err
	}

	result, err := s.acmeIssuer().Obtain(ctx, ipcertops.ACMERequest{
		IP:               ip,
		Email:            email,
		ChallengePort:    port,
		AccountKeyPEM:    accountKey,
		RegistrationJSON: registrationJSON,
	})
	if err != nil {
		return "", "", common.NewError("ip cert: issuance failed: ", err.Error())
	}

	certPath, keyPath, err = ipcertops.WriteCertFiles(ip, result.CertPEM, result.KeyPEM)
	if err != nil {
		return "", "", err
	}
	notAfter, err := ipcertops.ParseCertNotAfter(result.CertPEM)
	if err != nil {
		return "", "", err
	}

	if err := s.persistIssued(ipCertIssued{
		ip: ip, email: email, port: port, applyTarget: applyTarget,
		result: result, certPath: certPath, keyPath: keyPath, notAfter: notAfter,
	}); err != nil {
		return "", "", err
	}

	return certPath, keyPath, nil
}

// ipCertIssued bundles everything persisted after a successful issuance.
type ipCertIssued struct {
	ip          string
	email       string
	port        int
	applyTarget string
	result      ipcertops.ACMEResult
	certPath    string
	keyPath     string
	notAfter    time.Time
}

// persistIssued stores the issued certificate state, the request parameters
// (so a direct-API issue stays renewable), and the (possibly newly created)
// ACME account so renewals reuse it.
func (s *Service) persistIssued(d ipCertIssued) error {
	set := s.settings
	if d.result.AccountKeyPEM != "" {
		if err := set.SetAccountKey(d.result.AccountKeyPEM); err != nil {
			return err
		}
	}
	if d.result.RegistrationJSON != "" {
		if err := set.SetAccountRegistration(d.result.RegistrationJSON); err != nil {
			return err
		}
	}
	writes := []func() error{
		func() error { return set.SetTargetIP(d.ip) },
		func() error { return set.SetEmail(d.email) },
		func() error { return set.SetChallengePort(d.port) },
		func() error { return set.SetApplyTarget(d.applyTarget) },
		func() error { return set.SetLastIP(d.ip) },
		func() error { return set.SetCertPath(d.certPath) },
		func() error { return set.SetKeyPath(d.keyPath) },
		func() error { return set.SetNotAfter(d.notAfter.UTC().Format(time.RFC3339)) },
		func() error { return set.SetLastIssue(s.clock().UTC().Format(time.RFC3339)) },
	}
	for _, w := range writes {
		if err := w(); err != nil {
			return err
		}
	}
	return nil
}

// RenewIfNeeded re-issues the managed certificate when auto-renew is enabled
// and the remaining validity has dropped below the threshold. It reuses the
// stored target/email/port/apply-target and the persisted ACME account.
func (s *Service) RenewIfNeeded(ctx context.Context) (bool, error) {
	set := s.settings
	enabled, err := set.GetIpCertEnabled()
	if err != nil {
		return false, err
	}
	if !enabled {
		return false, nil
	}
	ip, err := set.GetIpCertTargetIP()
	if err != nil {
		return false, err
	}
	if ip == "" {
		return false, nil
	}

	// Force a re-issue when the configured target IP no longer matches the IP
	// the stored certificate was issued for (the operator changed ipCertTargetIP
	// in settings): the on-disk cert's SAN would otherwise be wrong until expiry.
	// A blank lastIP means "no prior issue on record" and falls back to the
	// expiry-only decision.
	lastIP, err := set.LastIP()
	if err != nil {
		return false, err
	}
	ipChanged := lastIP != "" && lastIP != ip

	notAfter := s.storedNotAfter()
	if !ipChanged && !ipcertops.ShouldRenew(notAfter, s.clock()) {
		return false, nil
	}

	email, err := set.GetIpCertEmail()
	if err != nil {
		return false, err
	}
	port, err := set.GetIpCertChallengePort()
	if err != nil {
		return false, err
	}
	applyTarget, err := set.GetIpCertApplyTarget()
	if err != nil {
		return false, err
	}

	if _, err := s.IssueNow(ctx, ip, email, port, applyTarget, ""); err != nil {
		return false, err
	}
	return true, nil
}

// GetStatus reports the current managed-certificate state for the UI.
func (s *Service) GetStatus() (ipcertops.Status, error) {
	set := s.settings
	status := ipcertops.Status{}
	var err error
	if status.Enabled, err = set.GetIpCertEnabled(); err != nil {
		return ipcertops.Status{}, err
	}
	if status.TargetIP, err = set.GetIpCertTargetIP(); err != nil {
		return ipcertops.Status{}, err
	}
	if status.ApplyTarget, err = set.GetIpCertApplyTarget(); err != nil {
		return ipcertops.Status{}, err
	}
	if status.CertPath, err = set.GetIpCertCertPath(); err != nil {
		return ipcertops.Status{}, err
	}
	if status.LastIssue, err = set.GetIpCertLastIssue(); err != nil {
		return ipcertops.Status{}, err
	}
	notAfter := s.storedNotAfter()
	if !notAfter.IsZero() {
		status.Issued = true
		status.NotAfter = notAfter.UTC().Format(time.RFC3339)
		status.DaysRemaining = notAfter.Sub(s.clock()).Hours() / 24
	}
	return status, nil
}

// storedNotAfter parses the persisted expiry; an unparseable/empty value yields
// the zero time (treated as "renew/unknown").
func (s *Service) storedNotAfter() time.Time {
	raw, err := s.settings.GetIpCertNotAfter()
	if err != nil || raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		logger.Warning("ip cert: stored notAfter is unparseable: ", raw)
		return time.Time{}
	}
	return parsed
}
