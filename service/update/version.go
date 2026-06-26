package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configupdate "github.com/MalenkiySolovey/solovey-ui/config/update"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

const (
	githubAPIBase       = "https://api.github.com/repos/MalenkiySolovey/solovey-ui"
	githubDownloadBase  = "https://github.com/MalenkiySolovey/solovey-ui/releases/download"
	versionCheckCache   = time.Hour
	versionCheckTimeout = 3 * time.Second
)

var (
	ErrNoRelease = errors.New("no release found for channel")
	ErrNotNewer  = errors.New("installed version is up to date")
	ErrNoAsset   = errors.New("no installable artifact for this platform")
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type VersionInfo struct {
	Current         string `json:"current"`
	Version         string `json:"version"`
	Channel         string `json:"channel,omitempty"`
	Latest          string `json:"latest,omitempty"`
	Prerelease      bool   `json:"prerelease,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable,omitempty"`
	AssetAvailable  bool   `json:"assetAvailable,omitempty"`
	ReleaseURL      string `json:"releaseURL,omitempty"`
	ReleaseNotes    string `json:"releaseNotes,omitempty"`
	CheckedAt       int64  `json:"checkedAt,omitempty"`
	CheckError      string `json:"checkError,omitempty"`
}

type ReleaseTarget struct {
	Channel      string
	Tag          string
	Version      string
	Prerelease   bool
	ReleaseNotes string
	ReleaseURL   string
	Platform     string
	AssetURL     string
	ChecksumURL  string
}

type resolvedRelease struct {
	tag, version, notes, htmlURL string
	prerelease                   bool
	platform                     string
	assetURL, checksumURL        string
	assetAvailable               bool
}

type channelState struct {
	checkedAt time.Time
	etag      string
	release   *resolvedRelease
	lastErr   string
}

var versionState = struct {
	sync.Mutex
	client   HTTPDoer
	baseURL  string
	channels map[string]*channelState
}{
	client:   &http.Client{Timeout: versionCheckTimeout},
	baseURL:  githubAPIBase,
	channels: make(map[string]*channelState),
}

var artifactPlatform = configupdate.ResolveArtifactPlatform

func init() {
	dbhooks.RegisterResetHook("service.update.version_check", ResetVersionCache)
}

func ResetVersionCache() {
	versionState.Lock()
	versionState.channels = make(map[string]*channelState)
	versionState.Unlock()
}

func GetVersionInfo() VersionInfo {
	return CheckForChannel(configupdate.ChannelMain, false)
}

func CheckForChannel(channel string, force bool) VersionInfo {
	channel = configupdate.NormalizeChannel(channel)
	current := configidentity.GetVersion()
	info := VersionInfo{Current: current, Version: current, Channel: channel}
	release, checkedAt, checkError := cachedRelease(channel, force)
	if !checkedAt.IsZero() {
		info.CheckedAt = checkedAt.Unix()
	}
	info.CheckError = checkError
	if release == nil {
		return info
	}
	info.Latest = release.tag
	info.Prerelease = release.prerelease
	info.ReleaseURL = release.htmlURL
	info.ReleaseNotes = release.notes
	info.AssetAvailable = release.assetAvailable
	info.UpdateAvailable = versionIsNewer(release.tag, current)
	return info
}

func ResolveTarget(channel string) (ReleaseTarget, error) {
	channel = configupdate.NormalizeChannel(channel)
	release, _, checkError := cachedRelease(channel, true)
	if release == nil {
		if checkError != "" {
			return ReleaseTarget{}, fmt.Errorf("update check: %s", checkError)
		}
		return ReleaseTarget{}, ErrNoRelease
	}
	if !versionIsNewer(release.tag, configidentity.GetVersion()) {
		return ReleaseTarget{}, ErrNotNewer
	}
	if !release.assetAvailable {
		return ReleaseTarget{}, ErrNoAsset
	}
	return ReleaseTarget{
		Channel: channel, Tag: release.tag, Version: release.version,
		Prerelease: release.prerelease, ReleaseNotes: release.notes,
		ReleaseURL: release.htmlURL, Platform: release.platform,
		AssetURL: release.assetURL, ChecksumURL: release.checksumURL,
	}, nil
}

func cachedRelease(channel string, force bool) (*resolvedRelease, time.Time, string) {
	versionState.Lock()
	state := channelStateLocked(channel)
	now := time.Now()
	if !force && !state.checkedAt.IsZero() && now.Sub(state.checkedAt) < versionCheckCache {
		release, at, lastErr := state.release, state.checkedAt, state.lastErr
		versionState.Unlock()
		return release, at, lastErr
	}
	client, baseURL, etag := versionState.client, versionState.baseURL, state.etag
	versionState.Unlock()

	release, responseETag, notModified, err := fetchChannelRelease(client, baseURL, channel, etag)
	versionState.Lock()
	defer versionState.Unlock()
	state = channelStateLocked(channel)
	state.checkedAt = now
	if err != nil {
		logger.Warning("version check failed: ", err)
		state.lastErr = "version check failed"
		return state.release, state.checkedAt, state.lastErr
	}
	state.lastErr = ""
	if responseETag != "" {
		state.etag = responseETag
	}
	if !notModified {
		state.release = release
	}
	return state.release, state.checkedAt, ""
}

func channelStateLocked(channel string) *channelState {
	state := versionState.channels[channel]
	if state == nil {
		state = &channelState{}
		versionState.channels[channel] = state
	}
	return state
}

type githubAsset struct {
	Name string `json:"name"`
}
type githubRelease struct {
	TagName    string        `json:"tag_name"`
	HTMLURL    string        `json:"html_url"`
	Body       string        `json:"body"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

func fetchChannelRelease(client HTTPDoer, baseURL, channel, etag string) (*resolvedRelease, string, bool, error) {
	requestURL := baseURL + "/releases/latest"
	if channel == configupdate.ChannelBeta {
		requestURL = baseURL + "/releases?per_page=20"
	}
	body, responseETag, notModified, err := doReleaseRequest(client, requestURL, etag)
	if err != nil || notModified {
		return nil, responseETag, notModified, err
	}
	if channel == configupdate.ChannelBeta {
		var releases []githubRelease
		if err := json.Unmarshal(body, &releases); err != nil {
			return nil, "", false, err
		}
		return resolveRelease(selectBetaRelease(releases)), responseETag, false, nil
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, "", false, err
	}
	return resolveRelease(&release), responseETag, false, nil
}

func doReleaseRequest(client HTTPDoer, requestURL, etag string) ([]byte, string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionCheckTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, "", false, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "solovey-ui-version-check")
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", false, err
	}
	defer response.Body.Close()
	responseETag := strings.TrimSpace(response.Header.Get("ETag"))
	if response.StatusCode == http.StatusNotModified {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		if responseETag == "" {
			responseETag = etag
		}
		return nil, responseETag, true, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return nil, "", false, fmt.Errorf("unexpected status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return body, responseETag, false, err
}

func selectBetaRelease(releases []githubRelease) *githubRelease {
	var best *githubRelease
	for index := range releases {
		release := &releases[index]
		if release.Draft || strings.TrimSpace(release.TagName) == "" {
			continue
		}
		if _, ok := versionpolicy.ParseSemver(strings.TrimPrefix(strings.TrimPrefix(release.TagName, "v"), "V")); !ok {
			continue
		}
		if best == nil {
			best = release
			continue
		}
		if comparison, ok := versionpolicy.CompareVersions(release.TagName, best.TagName); ok && comparison > 0 {
			best = release
		}
	}
	return best
}

func resolveRelease(release *githubRelease) *resolvedRelease {
	if release == nil {
		return nil
	}
	tag := strings.TrimSpace(release.TagName)
	version := versionpolicy.NormalizeVersion(tag)
	if version == "" {
		return nil
	}
	platform := artifactPlatform()
	resolved := &resolvedRelease{
		tag: tag, version: version, prerelease: release.Prerelease || isPrereleaseTag(tag),
		notes: strings.TrimSpace(release.Body), htmlURL: strings.TrimSpace(release.HTMLURL), platform: platform,
	}
	if platform != "" {
		assetName := fmt.Sprintf("solovey-ui-linux-%s.tar.gz", platform)
		resolved.assetURL = fmt.Sprintf("%s/%s/%s", githubDownloadBase, tag, assetName)
		resolved.checksumURL = resolved.assetURL + ".sha256"
		resolved.assetAvailable = hasAsset(release.Assets, assetName) && hasAsset(release.Assets, assetName+".sha256")
	}
	return resolved
}

func hasAsset(assets []githubAsset, name string) bool {
	for _, asset := range assets {
		if asset.Name == name {
			return true
		}
	}
	return false
}

func isPrereleaseTag(tag string) bool {
	semver, ok := versionpolicy.ParseSemver(strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "V"))
	return ok && len(semver.Prerelease) > 0
}

func versionIsNewer(candidate, current string) bool {
	comparison, ok := versionpolicy.CompareVersions(candidate, current)
	return ok && comparison > 0
}

func VersionIsNewer(candidate, current string) bool {
	return versionIsNewer(candidate, current)
}
