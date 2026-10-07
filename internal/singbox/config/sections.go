package singboxconfig

import (
	"encoding/json"
)

const (
	SectionInbounds  = "inbounds"
	SectionOutbounds = "outbounds"
	SectionServices  = "services"
	SectionEndpoints = "endpoints"
)

type RuntimeSections struct {
	Inbounds  any
	Outbounds any
	Services  any
	Endpoints any
}

func BuildRuntimeConfig(base json.RawMessage, sections RuntimeSections) ([]byte, error) {
	merged, err := MergeRuntimeConfig(base, sections)
	if err != nil {
		return nil, err
	}
	return PrepareRuntimeAssets(merged)
}

// MergeRuntimeConfig is read-only. Migration previews and preflight must inspect
// the complete candidate before any managed asset download or file publication.
func MergeRuntimeConfig(base json.RawMessage, sections RuntimeSections) ([]byte, error) {
	doc, err := ParseBaseConfig(base)
	if err != nil {
		return nil, err
	}
	if err := doc.SetSection(SectionInbounds, sections.Inbounds); err != nil {
		return nil, err
	}
	if err := doc.SetSection(SectionOutbounds, sections.Outbounds); err != nil {
		return nil, err
	}
	if err := doc.SetSection(SectionServices, sections.Services); err != nil {
		return nil, err
	}
	if err := doc.SetSection(SectionEndpoints, sections.Endpoints); err != nil {
		return nil, err
	}
	merged, err := doc.MarshalIndented()
	if err != nil {
		return nil, err
	}
	return []byte(merged), nil
}

func PrepareRuntimeAssets(mergedBytes json.RawMessage) ([]byte, error) {
	if err := EnsureManagedRuleSetsForConfig(mergedBytes); err != nil {
		return nil, err
	}
	runtimeConfig, err := RewriteManagedRuleSetsForRuntime(mergedBytes)
	if err != nil {
		return nil, err
	}
	return runtimeConfig, nil
}

func (d *Document) SetSection(section string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if d.sections == nil {
		d.sections = map[string]json.RawMessage{}
	}
	d.sections[section] = append(json.RawMessage(nil), raw...)
	merged, err := json.Marshal(d.sections)
	if err != nil {
		return err
	}
	d.raw = append(json.RawMessage(nil), merged...)
	return nil
}
