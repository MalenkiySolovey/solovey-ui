package server

import (
	"fmt"
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
// the database at most once per DisplaySettingsTTL. A failed refresh is never
// cached: callers must not silently replace persisted presentation settings
// with zero values for the lifetime of the cache.
func CachedDisplaySettings(ss DisplaySettingsReader, now time.Time) (DisplaySettings, error) {
	displaySettingsCache.Lock()
	defer displaySettingsCache.Unlock()
	if now.Before(displaySettingsCache.expiresAt) {
		return displaySettingsCache.value, nil
	}
	var v DisplaySettings
	var err error
	if v.ShowInfo, err = ss.GetSubShowInfo(); err != nil {
		return DisplaySettings{}, fmt.Errorf("read subShowInfo: %w", err)
	}
	if v.NameInRemark, err = ss.GetSubNameInRemark(); err != nil {
		return DisplaySettings{}, fmt.Errorf("read subNameInRemark: %w", err)
	}
	if v.Updates, err = ss.GetSubUpdates(); err != nil {
		return DisplaySettings{}, fmt.Errorf("read subUpdates: %w", err)
	}
	if v.Title, err = ss.GetSubTitle(); err != nil {
		return DisplaySettings{}, fmt.Errorf("read subTitle: %w", err)
	}
	if v.SupportURL, err = ss.GetSubSupportUrl(); err != nil {
		return DisplaySettings{}, fmt.Errorf("read subSupportUrl: %w", err)
	}
	if v.ProfileURL, err = ss.GetSubProfileUrl(); err != nil {
		return DisplaySettings{}, fmt.Errorf("read subProfileUrl: %w", err)
	}
	if v.Announce, err = ss.GetSubAnnounce(); err != nil {
		return DisplaySettings{}, fmt.Errorf("read subAnnounce: %w", err)
	}
	if v.Encode, err = ss.GetSubEncode(); err != nil {
		return DisplaySettings{}, fmt.Errorf("read subEncode: %w", err)
	}
	displaySettingsCache.value = v
	displaySettingsCache.expiresAt = now.Add(DisplaySettingsTTL)
	return v, nil
}

// ResetDisplaySettingsCache invalidates the presentation snapshot after a
// committed settings change or database restore.
func ResetDisplaySettingsCache() {
	displaySettingsCache.Lock()
	defer displaySettingsCache.Unlock()
	displaySettingsCache.value = DisplaySettings{}
	displaySettingsCache.expiresAt = time.Time{}
}
