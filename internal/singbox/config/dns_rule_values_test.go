package singboxconfig

import (
	"bytes"
	"fmt"
	"testing"
)

func TestDNSRuleAdmissionRejectsInactiveReferenceAndECSConflict(t *testing.T) {
	for _, test := range []struct{ rule, code string }{
		{`{"domain":"example.invalid","server":"hosts","client_subnet":"192.0.2.0/24","remove_client_subnet":true}`, "dns_client_subnet_conflict"},
		{`{"preferred_by":"missing","server":"hosts"}`, "dns_preferred_server_missing"},
		{`{"preferred_by":"udp","server":"hosts"}`, "dns_preferred_server_incompatible"},
		{`{"type":"logical","mode":"and","rules":[{"preferred_by":"missing"}],"server":"hosts"}`, "dns_preferred_server_missing"},
	} {
		source := []byte(fmt.Sprintf(`{"dns":{"servers":[{"type":"hosts","tag":"hosts"},{"type":"udp","tag":"udp","server":"127.0.0.1"}],"rules":[%s]}}`, test.rule))
		preimage := bytes.Clone(source)
		findings, err := ValidateDNSConfig(source)
		if err == nil || !hasDNSCode(findings, test.code) {
			t.Fatalf("invalid DNS candidate admitted; expected %s", test.code)
		}
		if !bytes.Equal(source, preimage) {
			t.Fatal("DNS admission rewrote rejected desired state")
		}
	}
	if _, err := ValidateDNSConfig([]byte(`{"dns":{"servers":[{"type":"hosts","tag":"hosts"}],"rules":[{"preferred_by":"hosts","server":"hosts","client_subnet":"192.0.2.0/24","remove_client_subnet":false}]}}`)); err != nil {
		t.Fatalf("valid DNS references and explicit false rejected: %v", err)
	}
}
