package deploymentadapter

import (
	"context"
	"errors"

	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

// FirewallRuntime retains the installed authority chosen by composition. It
// does not discover an OS or turn a process restart into a host reboot.
type FirewallRuntime struct {
	Authority protectionruntime.RuntimeRootAuthority
}

func (p FirewallRuntime) Available() bool {
	return p.Authority.Installed() && (p.Authority.Backend() == protectionruntime.DeploymentBackendSystemd || p.Authority.Backend() == protectionruntime.DeploymentBackendProcd)
}

func (p FirewallRuntime) Generation(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !p.Authority.Installed() {
		return "", errors.New("installed lifecycle authority unavailable")
	}
	if err := p.Authority.Recheck(); err != nil {
		return "", err
	}
	switch p.Authority.Backend() {
	case protectionruntime.DeploymentBackendSystemd, protectionruntime.DeploymentBackendProcd:
		return broker.HostBootGeneration()
	default:
		return "", errors.New("host runtime restoration unavailable")
	}
}

func (p FirewallRuntime) RequiresBaseFirewall() bool {
	return p.Authority.Backend() == protectionruntime.DeploymentBackendProcd
}
