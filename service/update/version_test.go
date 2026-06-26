package update

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func withVersionServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	versionState.Lock()
	oldClient, oldBase, oldChannels := versionState.client, versionState.baseURL, versionState.channels
	versionState.client, versionState.baseURL, versionState.channels = server.Client(), server.URL, make(map[string]*channelState)
	versionState.Unlock()
	t.Cleanup(func() {
		versionState.Lock()
		versionState.client, versionState.baseURL, versionState.channels = oldClient, oldBase, oldChannels
		versionState.Unlock()
	})
}

func TestVersionCheckCachesAndUsesETag(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch calls.Add(1) {
		case 1:
			writer.Header().Set("ETag", `"release-1"`)
			_, _ = writer.Write([]byte(`{"tag_name":"v9999.0.0","html_url":"https://example.test/release","assets":[]}`))
		case 2:
			if request.Header.Get("If-None-Match") != `"release-1"` {
				t.Fatalf("If-None-Match = %q", request.Header.Get("If-None-Match"))
			}
			writer.WriteHeader(http.StatusNotModified)
		}
	}))
	defer server.Close()
	withVersionServer(t, server)
	first := GetVersionInfo()
	if first.Latest != "v9999.0.0" || !first.UpdateAvailable {
		t.Fatalf("first = %#v", first)
	}
	_ = GetVersionInfo()
	if calls.Load() != 1 {
		t.Fatalf("cache calls = %d", calls.Load())
	}
	versionState.Lock()
	versionState.channels["main"].checkedAt = time.Now().Add(-2 * versionCheckCache)
	versionState.Unlock()
	second := GetVersionInfo()
	if second.Latest != first.Latest || calls.Load() != 2 {
		t.Fatalf("304 lost cached release: %#v calls=%d", second, calls.Load())
	}
}

func TestBetaSelectsHighestSemverAndBuildsSoloveyArtifactURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`[
			{"tag_name":"v2.0.0-beta.1","prerelease":true,"assets":[]},
			{"tag_name":"v2.0.0","assets":[{"name":"solovey-ui-linux-amd64.tar.gz"},{"name":"solovey-ui-linux-amd64.tar.gz.sha256"}]},
			{"tag_name":"invalid","assets":[]}
		]`))
	}))
	defer server.Close()
	withVersionServer(t, server)
	oldPlatform := artifactPlatform
	artifactPlatform = func() string { return "amd64" }
	t.Cleanup(func() { artifactPlatform = oldPlatform })

	release, _, _, err := fetchChannelRelease(server.Client(), server.URL, "beta", "")
	if err != nil {
		t.Fatal(err)
	}
	if release.tag != "v2.0.0" || !release.assetAvailable {
		t.Fatalf("release = %#v", release)
	}
	want := "https://github.com/MalenkiySolovey/solovey-ui/releases/download/v2.0.0/solovey-ui-linux-amd64.tar.gz"
	if release.assetURL != want || release.checksumURL != want+".sha256" {
		t.Fatalf("urls = %q %q", release.assetURL, release.checksumURL)
	}
}

func TestMalformedReleaseIsNeverConsideredNewer(t *testing.T) {
	if VersionIsNewer("nightly", "1.0.0") {
		t.Fatal("malformed release must not be executable update target")
	}
}
