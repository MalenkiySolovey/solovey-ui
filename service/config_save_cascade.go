package service

type configSaveCascadePolicy struct {
	reloadObjects []configSaveObject
	coreRestart   bool
}

var (
	configSaveCascadeClientInboundMembershipChanged = configSaveCascadePolicy{
		reloadObjects: []configSaveObject{configSaveObjectInbounds},
		coreRestart:   true,
	}
	configSaveCascadeTLSChanged = configSaveCascadePolicy{
		reloadObjects: []configSaveObject{configSaveObjectClients, configSaveObjectInbounds},
		coreRestart:   true,
	}
	configSaveCascadeInboundChanged = configSaveCascadePolicy{
		reloadObjects: []configSaveObject{configSaveObjectClients},
		coreRestart:   true,
	}
	configSaveCascadeCoreRuntimeChanged = configSaveCascadePolicy{
		coreRestart: true,
	}
)

func (p *configSavePlan) ApplyCascade(policy configSaveCascadePolicy) {
	p.IncludeSaveObjects(policy.reloadObjects...)
	if policy.coreRestart {
		p.RequireCoreRestart()
	}
}
