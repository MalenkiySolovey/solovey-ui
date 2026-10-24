package validation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	panelidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func TestTargetCheckBindsExactInputAndDoesNotStartListenerOrWriteAssets(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	logPath := filepath.Join(t.TempDir(), "not-created.log")
	quotedPath, _ := json.Marshal(logPath)
	raw := []byte(fmt.Sprintf(`{
  "log":{"output":%s},
  "inbounds":[{"type":"mixed","tag":"occupied-port","listen":"127.0.0.1","listen_port":%d}]
}`, quotedPath, listener.Addr().(*net.TCPAddr).Port))
	before := bytes.Clone(raw)
	report := checkTargetConfig(raw, providerfixture.Identity(t, coreprovider.DefaultID()))
	digest := sha256.Sum256(raw)
	if report.Schema != TargetCheckSchema || report.State != "VALID" || len(report.ReasonCodes) != 0 ||
		report.ConfigurationDigest != hex.EncodeToString(digest[:]) || !bytes.Equal(raw, before) ||
		report.PanelVersion != panelidentity.GetVersion() || report.Capabilities.Validate(report.CoreProvider, report.Capabilities.ComponentProfile) != nil ||
		report.CapabilityRevision != report.Capabilities.Revision() {
		t.Fatalf("target report is not bound to unchanged input: %+v", report)
	}
	if report.CoreProvider.ArtifactState != coreprovider.Unknown || report.CoreProvider.ArtifactDigest != "" {
		t.Fatal("target validation invented artifact trust")
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("target validation wrote a runtime asset")
	}
}

func TestTargetCheckRejectsUnsupportedFieldsWithoutDroppingOrEchoingThem(t *testing.T) {
	for _, raw := range []string{
		`{"log":{"disabled":true},"credential-sentinel-unknown":true}`,
		`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"d","credential-sentinel-unknown":true}]}`,
		`{"log":{"disabled":true},"outbounds":[{"type":"http","tag":"fixture","server":"127.0.0.1","server_port":8080,"username":"fixture","password":"credential-sentinel","tls":{"enabled":true,"insecure":true,"credential-sentinel-unknown":null}}]}`,
		`{"log":{"disabled":true},"inbounds":[{"type":"credential-sentinel-protocol"}]}`,
	} {
		input := []byte(raw)
		before := bytes.Clone(input)
		report := checkTargetConfig(input, providerfixture.Identity(t, coreprovider.DefaultID()))
		encoded, err := json.Marshal(report)
		if err != nil || report.State != "REJECTED" || len(report.ReasonCodes) != 1 ||
			report.ReasonCodes[0] != "TARGET_CONFIGURATION_REJECTED" || report.ConfigurationDigest == "" ||
			strings.Contains(string(encoded), "credential-sentinel") || !bytes.Equal(input, before) {
			t.Fatalf("unsupported configuration was accepted, transformed or echoed: %+v %v", report, err)
		}
	}
}

func TestTargetCheckRequiresSourceAndExactBoundedJSONObject(t *testing.T) {
	for _, identity := range []coreprovider.Identity{{}, coreprovider.Resolve(nil)} {
		if report := checkTargetConfig([]byte(`{}`), identity); report.State != "UNAVAILABLE" || report.ConfigurationDigest != "" {
			t.Fatal("unverified source supplied target validation")
		}
	}
	identity := providerfixture.Identity(t, coreprovider.DefaultID())
	for _, raw := range []string{``, `null`, `[]`, `{} {}`, `{"log":{},"log":{}}`, strings.Repeat(" ", MaxTargetConfigBytes+1)} {
		report := checkTargetConfig([]byte(raw), identity)
		if report.State != "REJECTED" || report.ConfigurationDigest != "" || report.ReasonCodes[0] != "CONFIGURATION_INPUT_INVALID" {
			t.Fatal("invalid or ambiguous input reached target decoder")
		}
	}
}
