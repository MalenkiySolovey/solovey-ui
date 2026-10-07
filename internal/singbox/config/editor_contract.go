package singboxconfig

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/rulepolicy"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// CoreEditorContract is presentation data from the pinned semantic schema.
// Editors use it for field presence and action switching; validation remains
// server-side and never relies on this catalogue as authorization.
type CoreEditorContract struct {
	DNSActions              map[string][]string `json:"dnsActions"`
	DNSConditions           []string            `json:"dnsConditions"`
	DNSCacheFields          []string            `json:"dnsCacheFields"`
	TUNDNSModes             []string            `json:"tunDnsModes"`
	TUNDNSUnavailableModes  map[string]string   `json:"tunDnsUnavailableModes,omitempty"`
	MaxRuleDepth            int                 `json:"maxRuleDepth"`
	MaxRuleNodes            int                 `json:"maxRuleNodes"`
	HTTPClientFields        map[string][]string `json:"httpClientFields"`
	HTTPEngines             []string            `json:"httpEngines"`
	HTTPVersions            []string            `json:"httpVersions"`
	HTTPUnavailableEngines  map[string]string   `json:"httpUnavailableEngines"`
	HTTPUnavailableVersions map[string]string   `json:"httpUnavailableVersions"`
	DirectHTTPClient        json.RawMessage     `json:"directHttpClient"`
}

func EditorContract() CoreEditorContract {
	actions := map[string][]string{
		C.RuleActionTypeRoute:        jsonFields(reflect.TypeFor[option.DNSRouteActionOptions]()),
		C.RuleActionTypeEvaluate:     jsonFields(reflect.TypeFor[option.DNSEvaluateActionOptions]()),
		C.RuleActionTypeRespond:      {},
		C.RuleActionTypeRouteOptions: jsonFields(reflect.TypeFor[option.DNSRouteOptionsActionOptions]()),
		C.RuleActionTypeReject:       jsonFields(reflect.TypeFor[option.RejectActionOptions]()),
		C.RuleActionTypePredefined:   jsonFields(reflect.TypeFor[option.DNSRouteActionPredefined]()),
	}
	for _, action := range []string{C.RuleActionTypeRoute, C.RuleActionTypeRespond, C.RuleActionTypeReject, C.RuleActionTypePredefined} {
		actions[action] = append(actions[action], "race")
	}
	dnsMode, _ := reflect.TypeFor[option.TunInboundOptions]().FieldByName("DNSMode")
	httpType := reflect.TypeFor[option.HTTPClient]()
	engine, _ := httpType.FieldByName("Engine")
	version, _ := httpType.FieldByName("Version")
	base := jsonFields(httpType)
	httpFields := map[string][]string{"1": base, "2": append(append([]string{}, base...), jsonFields(reflect.TypeFor[option.HTTP2Options]())...), "3": append(append([]string{}, base...), jsonFields(reflect.TypeFor[option.QUICOptions]())...)}
	return CoreEditorContract{DNSActions: actions, DNSConditions: jsonFields(reflect.TypeFor[option.DefaultDNSRule]()), DNSCacheFields: jsonFields(reflect.TypeFor[option.CacheFileOptions]()), TUNDNSModes: strings.Split(dnsMode.Tag.Get("enum"), ","), MaxRuleDepth: rulepolicy.MaxDepth, MaxRuleNodes: rulepolicy.MaxNodes, HTTPClientFields: httpFields, HTTPEngines: strings.Split(engine.Tag.Get("enum"), ","), HTTPVersions: strings.Split(version.Tag.Get("enum"), ","), HTTPUnavailableEngines: HTTPUnavailableEngines(), HTTPUnavailableVersions: HTTPUnavailableVersions(), DirectHTTPClient: ExplicitDirectHTTPClient()}
}

func jsonFields(t reflect.Type) []string {
	var fields []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			fields = append(fields, jsonFields(field.Type)...)
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields = append(fields, name)
		}
	}
	return fields
}
