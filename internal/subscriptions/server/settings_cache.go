package server

import (
	"sync"
	"time"
)

// DisplaySettingsTTL bounds how long the per-process snapshot of the
// display/format subscription settings is reused before being re-read.
const DisplaySettingsTTL = time.Minute

// DisplaySettingsReader exposes only the subscription-presentation getters this
// hot path needs. Accepting this narrow interface instead of the concrete
// service type keeps the pure subscription-server package free of a dependency
// on the service layer; *service.SettingService satisfies it structurally.
type DisplaySettingsReader interface {
	GetSubShowInfo() (bool, error)
	GetSubNameInRemark() (bool, error)
	GetSubUpdates() (int, error)
	GetSubTitle() (string, error)
	GetSubSupportUrl() (string, error)
	GetSubProfileUrl() (string, error)
	GetSubAnnounce() (string, error)
	GetSubEncode() (bool, error)
}

// DisplaySettings is the read-mostly subscription presentation snapshot used
// on the public subscription hot path. Security-relevant gates such as
// subSecretRequired and subLinkEnable are deliberately not included here.
type DisplaySettings struct {
	ShowInfo     bool
	NameInRemark bool
	Updates      int
	Title        string
	SupportURL   string
	ProfileURL   string
	Announce     string
	Encode       bool
}

var displaySettingsCache = struct {
	sync.Mutex
	value     DisplaySettings
	expiresAt time.Time
}{}

// CachedDisplaySettings returns the display settings snapshot, reading it from
// the database at most once per DisplaySettingsTTL. Getter errors keep the same
// zero-value behavior as the legacy inline subscription path.
func CachedDisplaySettings(ss DisplaySettingsReader, now time.Time) DisplaySettings {
	displaySettingsCache.Lock()
	defer displaySettingsCache.Unlock()
	if now.Before(displaySettingsCache.expiresAt) {
		return displaySettingsCache.value
	}
	var v DisplaySettings
	v.ShowInfo, _ = ss.GetSubShowInfo()
	v.NameInRemark, _ = ss.GetSubNameInRemark()
	v.Updates, _ = ss.GetSubUpdates()
	v.Title, _ = ss.GetSubTitle()
	v.SupportURL, _ = ss.GetSubSupportUrl()
	v.ProfileURL, _ = ss.GetSubProfileUrl()
	v.Announce, _ = ss.GetSubAnnounce()
	v.Encode, _ = ss.GetSubEncode()
	displaySettingsCache.value = v
	displaySettingsCache.expiresAt = now.Add(DisplaySettingsTTL)
	return v
}

func ResetDisplaySettingsCacheForTest() {
	displaySettingsCache.Lock()
	defer displaySettingsCache.Unlock()
	displaySettingsCache.value = DisplaySettings{}
	displaySettingsCache.expiresAt = time.Time{}
}
