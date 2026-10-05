//go:build !with_gvisor

package service

import (
	"strings"
	"testing"

	"github.com/sagernet/sing-tun"
)

func assertCapabilityWarpDryCheck(t *testing.T, err error) {
	t.Helper()
	// The existing corebox constructor flattens errors; match the exact pinned
	// official option-dependent error, rather than asserting an absent wrapper.
	if err == nil || !strings.HasSuffix(err.Error(), tun.ErrGVisorNotIncluded.Error()) {
		t.Fatalf("userspace device must truthfully require gVisor in this profile: %v", err)
	}
}
