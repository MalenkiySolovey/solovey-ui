package validation

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestValidateConfigRejectsMalformedConfig(t *testing.T) {
	if err := ValidateConfig([]byte("{ this is not json")); err == nil {
		t.Fatal("ValidateConfig must reject malformed config")
	}
}

func TestValidateConfigAcceptsMinimalConfig(t *testing.T) {
	config := []byte(`{"log":{"disabled":true},"dns":{"servers":[],"rules":[]},"route":{"rules":[]}}`)
	if err := ValidateConfig(config); err != nil {
		t.Fatalf("ValidateConfig rejected minimal config: %v", err)
	}
}

func TestValidateConfigDoesNotBindOrDownload(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	config := []byte(fmt.Sprintf(`{
		"log":{"disabled":true},
		"dns":{"servers":[],"rules":[]},
		"inbounds":[{"type":"mixed","tag":"in","listen":"127.0.0.1","listen_port":%d}],
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"rules":[{"rule_set":"remote-rs","outbound":"direct"}],"rule_set":[{"type":"remote","tag":"remote-rs","format":"binary","url":"https://10.255.255.1/never.srs","download_detour":"direct"}]}
	}`, port))

	done := make(chan error, 1)
	go func() { done <- ValidateConfig(config) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ValidateConfig must accept config without binding/downloading: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ValidateConfig blocked; it appears to start listeners or download rule-sets")
	}

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
	if err != nil {
		t.Fatalf("inbound port %d still bound after ValidateConfig: %v", port, err)
	}
	_ = ln.Close()
}
