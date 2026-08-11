//go:build !with_utls

package coreinboundcontrol

import "testing"

func TestCompiledCapabilityWithoutUTLSTag(t *testing.T) {
	if compiledWithUTLS {
		t.Fatal("untagged build reported with_utls")
	}
}
