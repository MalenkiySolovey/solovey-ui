package maintenance

import (
	"context"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

// CertificateRenewalJob re-issues the managed IP certificate when it nears expiry. It is
// a cheap no-op when auto-renew is disabled or the certificate is still fresh,
// so it is safe to run on a fixed schedule.
type CertificateRenewalJob struct {
	service.IpCertificateService
}

func NewCertificateRenewalJob() *CertificateRenewalJob {
	return &CertificateRenewalJob{
		IpCertificateService: service.IpCertificateService{
			Runtime:  service.DefaultRuntime(),
			Settings: &service.SettingService{},
		},
	}
}

func (j *CertificateRenewalJob) Run() {
	renewed, err := j.IpCertificateService.RenewIfNeeded(context.Background())
	if err != nil {
		logger.Warning("ip cert renew failed: ", err)
		return
	}
	if renewed {
		logger.Info("ip cert renewed")
	}
}
