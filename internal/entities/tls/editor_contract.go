package entitytls

import (
	"reflect"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/util/jsonfields"
	"github.com/sagernet/sing-box/option"
)

type EditorFacts struct {
	Fields               map[string][]string `json:"fields"`
	ProviderFields       map[string][]string `json:"providerFields"`
	ProviderTypes        []string            `json:"providerTypes"`
	ProviderUnavailable  map[string]string   `json:"providerUnavailable"`
	ProviderModes        []string            `json:"providerModes"`
	ClientAuthentication []string            `json:"clientAuthentication"`
	Engines              []string            `json:"engines"`
}

func EditorContract() EditorFacts {
	facts := EditorFacts{Fields: map[string][]string{"server": jsonfields.Names(reflect.TypeFor[option.InboundTLSOptions](), false), "client": jsonfields.Names(reflect.TypeFor[option.OutboundTLSOptions](), false)}, ProviderFields: map[string][]string{LegacyInlineMode: jsonfields.Names(reflect.TypeFor[option.InboundACMEOptions](), true), NativeProviderMode: jsonfields.Names(reflect.TypeFor[option.ACMECertificateProviderOptions](), false)}, ProviderTypes: []string{}, ProviderUnavailable: map[string]string{"origin_ca": "TLS_PROVIDER_UNAVAILABLE", "tailscale": "TLS_PROVIDER_UNAVAILABLE"}, ProviderModes: []string{LegacyInlineMode, NativeProviderMode}, ClientAuthentication: append([]string(nil), clientAuthenticationModes...), Engines: []string{"go"}}
	if registry.Resolve("certificateProviders", "acme").Available() {
		facts.ProviderTypes = append(facts.ProviderTypes, "acme")
	} else {
		facts.ProviderUnavailable["acme"] = "TLS_PROVIDER_UNAVAILABLE"
	}
	return facts
}
