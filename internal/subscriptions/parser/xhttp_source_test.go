package parser

import (
	"fmt"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func TestXHTTPUnmappedSourceRemainsInspectOnly(t *testing.T) {
	for _, settings := range []string{
		`{"path":"/","mode":{"unsupported":true}}`,
		`{"path":"/","headers":{"X-Test":["one","two"]}}`,
		`{"path":"/","extra":{"downloadSettings":{"address":"other.example"}}}`,
	} {
		source := fmt.Sprintf(`{"outbounds":[{"tag":"native","protocol":"vless","settings":{"vnext":[{"address":"edge.example","port":443,"users":[{"id":"00000000-0000-4000-8000-000000000001"}]}]},"streamSettings":{"network":"xhttp","xhttpSettings":%s}}]}`, settings)
		outbounds, err := ParseXrayOutbounds(source)
		if err != nil || len(outbounds) != 1 {
			t.Fatal("unmapped XHTTP source was lost", err)
		}
		endpoint := canonical.ObserveOutbound(canonical.FormatXray, outbounds[0], 0)
		if _, err := canonical.CheckedEndpointOutbound(endpoint); err == nil {
			t.Fatal("unmapped XHTTP source became executable")
		}
		if len(endpoint.Observations) == 0 || len(endpoint.Observations[0].Source) == 0 {
			t.Fatal("unmapped source lost its inspectable provenance")
		}
	}
}
