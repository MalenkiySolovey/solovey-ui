package singboxconfig

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	capabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
)

func TestNativeDNSReferencesUseSelectedProvider(t *testing.T) {
	for _, test := range []struct{ source, code string }{
		{`{"dns":{"servers":[{"type":"openconnect","endpoint":"vpn","accept_search_domain":true}]},"endpoints":[{"type":"openconnect","tag":"vpn"}]}`, ""},
		{`{"dns":{"servers":[{"type":"openconnect"}]},"endpoints":[]}`, "dns_openconnect_endpoint_required"},
		{`{"dns":{"servers":[{"type":"openconnect","endpoint":"vpn"}]},"endpoints":[{"type":"wireguard","tag":"vpn"}]}`, "dns_openconnect_endpoint_missing"},
	} {
		code := test.code
		if !registry.Resolve("dns", "openconnect").Available() {
			code = "dns_native_transport_unavailable"
		}
		findings := DNSSelectedTransportFindings([]byte(test.source))
		if code == "" && len(findings) != 0 || code != "" && !hasDNSCode(findings, code) {
			t.Fatalf("OpenConnect DNS result: %+v", findings)
		}
	}
	for _, test := range []struct{ source, code string }{
		{`{"dns":{"servers":[{"type":"fallback","servers":["1"]},{"type":"udp","server":"127.0.0.1"}]}}`, ""},
		{`{"dns":{"servers":[{"type":"fallback","tag":"group","servers":["later"],"strategy":"parallel","timeout":"3s"},{"type":"udp","tag":"later","server":"127.0.0.1"}]}}`, ""},
		{`{"dns":{"servers":[{"type":"fallback","tag":"group","servers":[]}]}}`, "dns_fallback_servers_required"},
		{`{"dns":{"servers":[{"type":"fallback","tag":"group","servers":["missing"]}]}}`, "dns_fallback_reference_invalid"},
		{`{"dns":{"servers":[{"type":"fallback","tag":"group","servers":["group"]}]}}`, "dns_fallback_reference_invalid"},
		{`{"dns":{"servers":[{"type":"fallback","tag":"a","servers":["b"]},{"type":"fallback","tag":"b","servers":["a"]}]}}`, "dns_fallback_dependency_cycle"},
		{`{"dns":{"servers":[{"type":"fallback","tag":"a","servers":["b"]},{"type":"udp","tag":"b","server":"example.invalid","domain_resolver":"a"}]}}`, "dns_fallback_dependency_cycle"},
		{`{"dns":{"servers":[{"type":"fallback","tag":"a","servers":["b"],"unexpected":true},{"type":"udp","tag":"b","server":"127.0.0.1"}]}}`, "dns_transport_schema_rejected"},
	} {
		code := test.code
		if !registry.Resolve("dns", "fallback").Available() {
			code = "dns_native_transport_unavailable"
		}
		findings := DNSSelectedTransportFindings([]byte(test.source))
		if code == "" && len(findings) != 0 || code != "" && !hasDNSCode(findings, code) {
			t.Fatalf("native DNS result: %+v", findings)
		}
		if code != "" && registry.Resolve("dns", "fallback").Available() {
			result, err := PrepareDNSUpgrade([]byte(test.source))
			if err == nil || !bytes.Equal(result.Candidate, []byte(test.source)) {
				t.Fatal("rejected native DNS changed the captured desired state")
			}
		}
	}
}

func TestNativeDNSHistoryProjectsOnlyEffectiveState(t *testing.T) {
	source := []byte(`{"dns":{"servers":[{"type":"udp","tag":"active","server":"127.0.0.1"},{"type":"fallback","tag":"history","servers":["active"],"strategy":"parallel","timeout":"3s"},{"type":"udp","server":"127.0.0.2"}],"final":"active"}}`)
	result, findings, err := ProjectDNSCapabilities(source)
	if err != nil {
		t.Fatal(err)
	}
	servers, _ := dnsTransportSections(result)
	if registry.Resolve("dns", "fallback").Available() {
		if !bytes.Equal(result, source) || len(findings) != 0 {
			t.Fatal("supporting provider changed available DNS state")
		}
		return
	}
	if len(servers) != 2 || dnsString(servers[1], "tag") != "2" || !hasDNSCode(findings, "dns_resource_inactive") {
		t.Fatal("inactive DNS projection changed implicit identities or retained unavailable transport")
	}
	for _, extra := range []string{
		`"final":"history"`,
		`"rules":[{"type":"logical","mode":"and","rules":[{"domain":["example.invalid"],"server":"history"}]}],"final":"active"`,
	} {
		var root, section dnsObject
		_ = json.Unmarshal(source, &root)
		_ = json.Unmarshal(root["dns"], &section)
		var fields dnsObject
		_ = json.Unmarshal([]byte("{"+extra+"}"), &fields)
		for key, value := range fields {
			section[key] = value
		}
		root["dns"] = dnsJSON(section)
		candidate := dnsJSON(root)
		result, _, err := ProjectDNSCapabilities(candidate)
		if err == nil || !bytes.Equal(result, candidate) {
			t.Fatal("inactive resolver reference changed DNS policy")
		}
	}
	implicit := []byte(`{"dns":{"servers":[{"type":"fallback","servers":["active"]},{"type":"udp","tag":"active","server":"127.0.0.1"}]}}`)
	bootstrap := append(bytes.Clone(source[:len(source)-1]), []byte(`,"outbounds":[{"type":"socks","tag":"proxy","server":"example.invalid","server_port":1080,"domain_resolver":"history"}]}`)...)
	if result, _, err := ProjectDNSCapabilities(bootstrap); err == nil || !bytes.Equal(result, bootstrap) {
		t.Fatal("inactive bootstrap reference escaped projection or changed desired state")
	}
	if _, _, err := ProjectDNSCapabilities(implicit); err == nil {
		t.Fatal("unavailable implicit default silently selected another resolver")
	}
}

func TestNativeDNSHistoryRejectsEditsAndAllowsCanonicalRemoval(t *testing.T) {
	previous := []byte(`{"dns":{"servers":[{"type":"fallback","tag":"history","servers":["active"],"timeout":"3s","strategy":"parallel"},{"type":"udp","tag":"active","server":"127.0.0.1"}],"final":"active"}}`)
	if err := ValidateDNSCapabilityEdits(previous, previous); err != nil {
		t.Fatal(err)
	}
	removed := []byte(`{"dns":{"servers":[{"type":"udp","tag":"active","server":"127.0.0.1"}],"final":"active"}}`)
	if err := ValidateDNSCapabilityEdits(previous, removed); err != nil {
		t.Fatal("canonical removal rejected")
	}
	for _, candidate := range [][]byte{
		bytes.ReplaceAll(previous, []byte("3s"), []byte("5s")),
		[]byte(`{"dns":{"servers":[{"type":"udp","tag":"history","server":"127.0.0.1"},{"type":"udp","tag":"active","server":"127.0.0.1"}],"final":"active"}}`),
	} {
		err := ValidateDNSCapabilityEdits(previous, candidate)
		if (err == nil) != registry.Resolve("dns", "fallback").Available() {
			t.Fatal("provider capability did not control historical edits/conversion")
		}
	}
	err := ValidateDNSCapabilityEdits(nil, previous)
	if (err == nil) != registry.Resolve("dns", "fallback").Available() {
		t.Fatal("unavailable creation was admitted")
	}
}

func TestSelectedDNSPortableContracts(t *testing.T) {
	for _, test := range []struct{ source, code string }{
		{`{"dns":{"servers":[{"type":"mdns","interface":["a","b"]}]}}`, ""},
		{`{"dns":{"servers":[{"type":"mdns","interface":"a"}]}}`, ""},
		{`{"dns":{"servers":[{"type":"mdns","interface":["a",""]}]}}`, "dns_mdns_interface_invalid"},
		{`{"dns":{"servers":[{"type":"mdns","interface":["a","a"]}]}}`, "dns_mdns_interface_invalid"},
		{`{"dns":{"servers":[{"type":"mdns","interface":[" a"]}]}}`, "dns_mdns_interface_invalid"},
		{`{"dns":{"servers":[{"type":"mdns","interface":7}]}}`, "dns_transport_schema_rejected"},
		{`{"dns":{"servers":[{"type":"mdns","neighbor_domain":["private.invalid"]}]}}`, "dns_mdns_local_option_ignored"},
		{`{"dns":{"servers":[{"type":"resolved"}]},"services":[]}`, "dns_resolved_service_required"},
		{`{"dns":{"servers":[{"type":"resolved","service":"resolver"}]},"services":[]}`, "dns_resolved_service_missing"},
		{`{"dns":{"servers":[{"type":"resolved","service":"resolver"}]},"services":[{"type":"ssm-api","tag":"resolver"}]}`, "dns_resolved_service_missing"},
		{`{"dns":{"servers":[{"type":"resolved","service":"resolver"}]},"services":[{"type":"resolved","tag":"resolver","enabled":false}]}`, "dns_resolved_service_missing"},
		{`{"dns":{"servers":[{"type":"resolved","service":"resolver"}]},"services":[{"type":"resolved","tag":"resolver"}]}`, ""},
		{`{"services":[{"type":"resolved","tag":"a"},{"type":"resolved","tag":"b"}]}`, "dns_resolved_service_duplicate"},
		{`{"dns":{"servers":[{"type":"resolved","service":"resolver"},{"type":"resolved","service":"resolver"}]},"services":[{"type":"resolved","tag":"resolver"}]}`, "dns_resolved_transport_duplicate"},
	} {
		findings := DNSSelectedTransportFindings([]byte(test.source))
		if test.code == "" && len(findings) != 0 || test.code != "" && !hasDNSCode(findings, test.code) {
			t.Fatalf("portable findings for %s: %+v", test.source, findings)
		}
		if test.code != "" {
			projection, err := PrepareDNSUpgrade([]byte(test.source))
			if err == nil || !bytes.Equal(projection.Candidate, []byte(test.source)) {
				t.Fatal("rejected DNS changed original")
			}
		}
	}
}

func TestDNSRuntimeChecksEverySelectedInterfaceAndBusOwner(t *testing.T) {
	environment := capabilities.Environment{InterfacesAvailable: true, Interfaces: []capabilities.Interface{{Name: "good", Eligible: true}, {Name: "bad"}}, SystemBusAvailable: true, Resolve1Name: capabilities.Resolve1NameUnclaimed}
	for _, interfaces := range []any{nil, "good", []string{"good"}} {
		source, _ := json.Marshal(map[string]any{"dns": map[string]any{"servers": []any{map[string]any{"type": "mdns", "interface": interfaces}}}})
		if findings := DNSRuntimeFindings(source, environment); len(findings) != 0 {
			t.Fatalf("valid interfaces: %+v", findings)
		}
	}
	if !hasDNSCode(DNSRuntimeFindings([]byte(`{"dns":{"servers":[{"type":"mdns","interface":["good","bad"]}]}}`), environment), "dns_mdns_interface_unavailable") {
		t.Fatal("partly unusable explicit selection accepted")
	}
	if !hasDNSCode(DNSRuntimeFindings([]byte(`{"dns":{"servers":[{"type":"mdns","interface":"bad"}]}}`), environment), "dns_mdns_interface_unavailable") {
		t.Fatal("unusable scalar interface accepted")
	}
	source := []byte(`{"dns":{"servers":[{"type":"resolved","service":"resolver"}]},"services":[{"type":"resolved","tag":"resolver"}]}`)
	if findings := DNSRuntimeFindings(source, environment); runtime.GOOS == "linux" && len(findings) != 0 || runtime.GOOS != "linux" && len(findings) == 0 {
		t.Fatalf("platform facts: %+v", findings)
	}
	environment.Resolve1Name = capabilities.Resolve1NameOtherProcess
	if len(DNSRuntimeFindings(source, environment)) == 0 {
		t.Fatal("other resolve1 owner authorized")
	}
	if findings := DNSSelectedTransportFindings(source); len(findings) != 0 {
		t.Fatal("host environment contaminated portable validation")
	}
}
