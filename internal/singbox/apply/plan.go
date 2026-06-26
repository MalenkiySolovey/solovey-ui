package apply

type Plan struct {
	objects           []string
	requiresCoreReset bool
	restartReason     string

	inboundIDs  []uint
	serviceIDs  []uint
	outboundIDs []uint
	endpointIDs []uint

	removedInboundTags  []string
	removedServiceTags  []string
	removedOutboundTags []string
	removedEndpointTags []string
}

type Change struct {
	NeedsRestart      bool
	RestartReason     string
	ReloadIDs         []uint
	RemoveTags        []string
	CascadeServiceIDs []uint
}

func NewPlan(primaryObject string) Plan {
	return Plan{objects: []string{primaryObject}}
}

func (p *Plan) IncludeObjects(objects ...string) {
	p.objects = append(p.objects, objects...)
}

func (p *Plan) RequireCoreRestart(reason ...string) {
	p.requiresCoreReset = true
	if len(reason) > 0 && reason[0] != "" {
		p.restartReason = reason[0]
	}
}

func (p Plan) Objects() []string {
	return copyStrings(p.objects)
}

func (p Plan) RequiresCoreRestart() bool {
	return p.requiresCoreReset
}

func (p Plan) RestartReason() string {
	return p.restartReason
}

func (p Plan) HasObjectChanges() bool {
	return len(p.inboundIDs) > 0 || len(p.serviceIDs) > 0 ||
		len(p.outboundIDs) > 0 || len(p.endpointIDs) > 0 ||
		len(p.removedInboundTags) > 0 || len(p.removedServiceTags) > 0 ||
		len(p.removedOutboundTags) > 0 || len(p.removedEndpointTags) > 0
}

func (p Plan) InboundIDs() []uint {
	return copyUints(p.inboundIDs)
}

func (p Plan) ServiceIDs() []uint {
	return copyUints(p.serviceIDs)
}

func (p Plan) OutboundIDs() []uint {
	return copyUints(p.outboundIDs)
}

func (p Plan) EndpointIDs() []uint {
	return copyUints(p.endpointIDs)
}

func (p Plan) RemovedInboundTags() []string {
	return copyStrings(p.removedInboundTags)
}

func (p Plan) RemovedServiceTags() []string {
	return copyStrings(p.removedServiceTags)
}

func (p Plan) RemovedOutboundTags() []string {
	return copyStrings(p.removedOutboundTags)
}

func (p Plan) RemovedEndpointTags() []string {
	return copyStrings(p.removedEndpointTags)
}

func (p *Plan) MergeInboundChange(change *Change) {
	if change == nil {
		return
	}
	p.mergeCoreRestart(change)
	p.inboundIDs = appendUniqueUint(p.inboundIDs, change.ReloadIDs...)
	p.removedInboundTags = appendUniqueString(p.removedInboundTags, change.RemoveTags...)
	p.serviceIDs = appendUniqueUint(p.serviceIDs, change.CascadeServiceIDs...)
}

func (p *Plan) MergeOutboundChange(change *Change) {
	if change == nil {
		return
	}
	p.mergeCoreRestart(change)
	p.outboundIDs = appendUniqueUint(p.outboundIDs, change.ReloadIDs...)
	p.removedOutboundTags = appendUniqueString(p.removedOutboundTags, change.RemoveTags...)
}

func (p *Plan) MergeEndpointChange(change *Change) {
	if change == nil {
		return
	}
	p.mergeCoreRestart(change)
	p.endpointIDs = appendUniqueUint(p.endpointIDs, change.ReloadIDs...)
	p.removedEndpointTags = appendUniqueString(p.removedEndpointTags, change.RemoveTags...)
}

func (p *Plan) MergeServiceChange(change *Change) {
	if change == nil {
		return
	}
	p.mergeCoreRestart(change)
	p.serviceIDs = appendUniqueUint(p.serviceIDs, change.ReloadIDs...)
	p.removedServiceTags = appendUniqueString(p.removedServiceTags, change.RemoveTags...)
}

func (p *Plan) mergeCoreRestart(change *Change) {
	if change.NeedsRestart {
		p.RequireCoreRestart(change.RestartReason)
	}
}

func appendUniqueUint(dst []uint, values ...uint) []uint {
	if len(values) == 0 {
		return dst
	}
	seen := make(map[uint]struct{}, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func appendUniqueString(dst []string, values ...string) []string {
	if len(values) == 0 {
		return dst
	}
	seen := make(map[string]struct{}, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func copyUints(values []uint) []uint {
	return append([]uint(nil), values...)
}

func copyStrings(values []string) []string {
	return append([]string(nil), values...)
}
