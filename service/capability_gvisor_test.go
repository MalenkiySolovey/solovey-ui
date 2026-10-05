//go:build with_gvisor

package service

import "testing"

func assertCapabilityWarpDryCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("official dry construction rejected mapped final: %v", err)
	}
}
