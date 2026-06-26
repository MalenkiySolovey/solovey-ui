package update

import (
	"runtime"
	"testing"
)

func TestNormalizeChannelAllowlist(t *testing.T) {
	if NormalizeChannel(ChannelBeta) != ChannelBeta || NormalizeChannel("nightly") != ChannelMain {
		t.Fatal("channel allowlist is not enforced")
	}
}

func TestResolveArtifactPlatformDoesNotOfferLinuxBinaryOnOtherOS(t *testing.T) {
	old := ArtifactPlatform
	ArtifactPlatform = "amd64"
	t.Cleanup(func() { ArtifactPlatform = old })
	got := ResolveArtifactPlatform()
	if runtime.GOOS != "linux" && got != "" {
		t.Fatalf("platform = %q on %s; self-update must be disabled", got, runtime.GOOS)
	}
}
