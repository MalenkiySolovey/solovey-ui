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
	ctx context.Context
}

func NewCertificateRenewalJob(contexts ...context.Context) *CertificateRenewalJob {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	return &CertificateRenewalJob{
		ctx: ctx,
		IpCertificateService: service.IpCertificateService{
			Runtime:  service.DefaultRuntime(),
			Settings: &service.SettingService{},
		},
	}
}

func (j *CertificateRenewalJob) Run() {
	ctx := j.context()
	if err := ctx.Err(); err != nil {
		return
	}
	renewed, err := j.IpCertificateService.RenewIfNeeded(ctx)
	if err != nil {
		logger.Warning("ip cert renew failed: ", err)
		return
	}
	if renewed {
		logger.Info("ip cert renewed")
	}
}

func (j *CertificateRenewalJob) context() context.Context {
	if j != nil && j.ctx != nil {
		return j.ctx
	}
	return context.Background()
}
