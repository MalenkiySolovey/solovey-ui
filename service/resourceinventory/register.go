package resourceinventory

import (
	"errors"
	"sync"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"gorm.io/gorm"
)

func RegisterCoreContributors(settings *coreservice.SettingService, db *gorm.DB, control *coreinboundcontrol.Service) (func(), error) {
	if control == nil {
		return nil, errors.New("register core contributors: inbound control is required")
	}
	if settings == nil {
		settings = &coreservice.SettingService{}
	}
	unregisterFronting, err := hostresources.RegisterFrontingBackendProviderV1(NewCoreFrontingBackendProviderV1(db, control))
	if err != nil {
		return nil, errors.Join(errors.New("register core fronting provider"), err)
	}
	unregisterLocalProxy, err := hostresources.RegisterLocalProxyProviderV1(NewCoreLocalProxyProviderV1(db, control))
	if err != nil {
		unregisterFronting()
		return nil, errors.Join(errors.New("register core local proxy provider"), err)
	}
	unregisterInterception, err := hostresources.RegisterInterceptionProviderV1(NewCoreInterceptionProviderV1(db, control))
	if err != nil {
		unregisterLocalProxy()
		unregisterFronting()
		return nil, errors.Join(errors.New("register core interception provider"), err)
	}
	unregisterIngress, err := hostresources.RegisterForwardedIngressScopeProviderV1(NewHostIngressScopeProviderV1())
	if err != nil {
		unregisterInterception()
		unregisterLocalProxy()
		unregisterFronting()
		return nil, errors.Join(errors.New("register host ingress-scope provider"), err)
	}
	unregisterTransport, err := hostresources.RegisterInboundTransportCapabilityProviderV2(NewCoreInboundTransportProviderV2(db, control))
	if err != nil {
		unregisterIngress()
		unregisterInterception()
		unregisterLocalProxy()
		unregisterFronting()
		return nil, errors.Join(errors.New("register core inbound transport provider"), err)
	}
	unregisterUDPProbe, err := componenthealth.DefaultProtocolProbesV1.Register(coreinboundcontrol.NewPlainUDPProbeProviderV1(control))
	if err != nil {
		unregisterTransport()
		unregisterIngress()
		unregisterInterception()
		unregisterLocalProxy()
		unregisterFronting()
		return nil, errors.Join(errors.New("register core UDP probe provider"), err)
	}
	unregisterLocalProxyProbe, err := componenthealth.DefaultLocalProxyProbesV1.Register(coreinboundcontrol.NewLocalProxyProbeProviderV1(control))
	if err != nil {
		unregisterUDPProbe()
		unregisterTransport()
		unregisterIngress()
		unregisterInterception()
		unregisterLocalProxy()
		unregisterFronting()
		return nil, errors.Join(errors.New("register core local proxy probe provider"), err)
	}
	unregister := []func(){
		hostresources.Register(panelContributor{settings: settings}),
		hostresources.Register(subscriptionContributor{settings: settings}),
		hostresources.Register(inboundContributor{db: db, control: control}),
		unregisterFronting,
		unregisterLocalProxy,
		unregisterInterception,
		unregisterIngress,
		unregisterTransport,
		unregisterUDPProbe,
		unregisterLocalProxyProbe,
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for index := len(unregister) - 1; index >= 0; index-- {
				unregister[index]()
			}
		})
	}, nil
}
