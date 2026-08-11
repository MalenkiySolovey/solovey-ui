//go:build with_utls

package coreinboundcontrol

import "testing"

func TestCompiledCapabilityWithUTLSTag(t *testing.T) {
	if !compiledWithUTLS {
		t.Fatal("with_utls build did not report the capability")
	}
}
