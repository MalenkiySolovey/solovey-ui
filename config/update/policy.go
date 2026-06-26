package update

import "runtime"

const (
	ChannelMain = "main"
	ChannelBeta = "beta"
)

// ArtifactPlatform is injected by release builds. It is required for ARM,
// whose GOARM variant cannot be recovered at runtime.
var ArtifactPlatform string

func NormalizeChannel(channel string) string {
	if channel == ChannelBeta {
		return ChannelBeta
	}
	return ChannelMain
}

func ResolveArtifactPlatform() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if ArtifactPlatform != "" {
		return ArtifactPlatform
	}
	switch runtime.GOARCH {
	case "amd64", "arm64", "386", "s390x":
		return runtime.GOARCH
	default:
		return ""
	}
}
