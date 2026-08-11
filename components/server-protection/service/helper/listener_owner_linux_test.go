//go:build linux

package helper

import (
	"net/netip"
	"testing"
)

func TestProcAddressEncodingAndOwnerClassification(t *testing.T) {
	cases := []struct {
		address string
		want    string
	}{
		{"127.0.0.1", "0100007F"},
		{"203.0.113.7", "077100CB"},
		{"::1", "00000000000000000000000001000000"},
	}
	for _, test := range cases {
		got, err := procAddress(netip.MustParseAddr(test.address))
		if err != nil || got != test.want {
			t.Fatalf("procAddress(%s)=%q err=%v want=%q", test.address, got, err, test.want)
		}
	}
	if classifyListenerProcess("sing-box\n") != ListenerOwnerSingBox || classifyListenerProcess("solovey-ui\n") != ListenerOwnerPanel || classifyListenerProcess("nginx\n") != "" {
		t.Fatal("listener process classification is not fail-closed")
	}
}
