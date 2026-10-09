package singboxconfig

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"

	capabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
)

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
