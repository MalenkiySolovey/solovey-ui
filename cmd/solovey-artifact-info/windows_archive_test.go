package main

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func TestWindowsPackageMetadataCannotRelabelObservedProviderProfileOrMachine(t *testing.T) {
	build := release.PanelBuildIdentity{CoreProvider: providerfixture.Identity(t, "shtorm-extended").Source,
		Platform: "windows", Arch: "arm64", BinaryProfile: "core"}
	raw := "app=solovey-ui\ncore_provider=shtorm-extended\nsing_box=v1.14.3\nplatform=windows/arm64\nprofile=core\ncommit=" +
		strings.Repeat("a", 40) + "\nsource_fingerprint=" + strings.Repeat("b", 64) + "\n"
	if !windowsBuildInfoMatches(raw, build) || !windowsBuildInfoMatches(strings.ReplaceAll(raw, "\n", "\r\n"), build) {
		t.Fatal("canonical Windows metadata rejected")
	}
	for _, invalid := range []string{
		strings.Replace(raw, "shtorm-extended", "sing-box", 1), strings.Replace(raw, "windows/arm64", "windows/amd64", 1),
		strings.Replace(raw, "profile=core", "profile=full", 1), strings.Replace(raw, "v1.14.3", "v1.14.2", 1),
		strings.Replace(raw, strings.Repeat("a", 40), "moving-main", 1), raw + "profile=core\n",
		strings.Replace(raw, "source_fingerprint=", "missing=", 1),
	} {
		if windowsBuildInfoMatches(invalid, build) {
			t.Fatal("ambiguous or mislabeled Windows package metadata accepted")
		}
	}
}
