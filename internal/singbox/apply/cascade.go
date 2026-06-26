package apply

type CascadePolicy struct {
	ReloadObjects []Object
	CoreRestart   bool
}

var (
	CascadeClientInboundMembershipChanged = CascadePolicy{
		ReloadObjects: []Object{ObjectInbounds},
	}
	CascadeTLSChanged = CascadePolicy{
		ReloadObjects: []Object{ObjectClients, ObjectInbounds},
		CoreRestart:   true,
	}
	CascadeInboundChanged = CascadePolicy{
		ReloadObjects: []Object{ObjectClients},
	}
	CascadeCoreRuntimeChanged = CascadePolicy{
		CoreRestart: true,
	}
)

func (p *Plan) IncludeSaveObjects(objects ...Object) {
	for _, object := range objects {
		p.IncludeObjects(object.String())
	}
}

func (p *Plan) ApplyCascade(policy CascadePolicy) {
	p.IncludeSaveObjects(policy.ReloadObjects...)
	if policy.CoreRestart {
		p.RequireCoreRestart()
	}
}
