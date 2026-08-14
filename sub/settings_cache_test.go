package sub

import (
	"errors"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func setSubTitle(t *testing.T, value string) {
	t.Helper()
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "subTitle").Update("value", value).Error; err != nil {
		t.Fatal(err)
	}
}

// TestCachedSubDisplaySettingsReusesSnapshotWithinTTL proves the O2 cache serves
// a snapshot for DisplaySettingsTTL before re-reading the database, so the hot
// subscription path does not issue the display-setting SELECTs on every request.
func TestCachedDisplaySettingsReusesSnapshotWithinTTL(t *testing.T) {
	initSubTestDB(t)
	ss := &service.SettingService{}
	if _, err := ss.GetAllSetting(); err != nil {
		t.Fatal(err)
	}

	setSubTitle(t, "first")
	base := time.Unix(1_700_000_000, 0)
	if got, err := subserver.CachedDisplaySettings(ss, base); err != nil || got.Title != "first" {
		t.Fatalf("cold read title = %q, want %q", got.Title, "first")
	}

	// A change within the TTL window must not be observed: the snapshot is reused.
	setSubTitle(t, "second")
	if got, err := subserver.CachedDisplaySettings(ss, base.Add(subserver.DisplaySettingsTTL-time.Second)); err != nil || got.Title != "first" {
		t.Fatalf("within-TTL read title = %q, want cached %q", got.Title, "first")
	}

	// Once the TTL elapses the database is read again and the new value appears.
	if got, err := subserver.CachedDisplaySettings(ss, base.Add(subserver.DisplaySettingsTTL+time.Second)); err != nil || got.Title != "second" {
		t.Fatalf("post-TTL read title = %q, want refreshed %q", got.Title, "second")
	}
}

// TestResetSubDisplaySettingsCacheForcesReread guards the test-isolation hook: a
// reset must discard the snapshot so a later read re-queries the database even
// within the TTL window (this is what initSubTestDB relies on between tests).
func TestResetDisplaySettingsCacheForcesReread(t *testing.T) {
	initSubTestDB(t)
	ss := &service.SettingService{}
	if _, err := ss.GetAllSetting(); err != nil {
		t.Fatal(err)
	}

	setSubTitle(t, "alpha")
	base := time.Unix(1_700_000_000, 0)
	if got, err := subserver.CachedDisplaySettings(ss, base); err != nil || got.Title != "alpha" {
		t.Fatalf("cold read title = %q, want %q", got.Title, "alpha")
	}

	setSubTitle(t, "beta")
	subserver.ResetDisplaySettingsCache()
	if got, err := subserver.CachedDisplaySettings(ss, base); err != nil || got.Title != "beta" {
		t.Fatalf("after reset title = %q, want re-read %q", got.Title, "beta")
	}
}

type failingDisplaySettingsReader struct{ err error }

func (r failingDisplaySettingsReader) GetSubShowInfo() (bool, error)     { return false, r.err }
func (r failingDisplaySettingsReader) GetSubNameInRemark() (bool, error) { return false, r.err }
func (r failingDisplaySettingsReader) GetSubUpdates() (int, error)       { return 0, r.err }
func (r failingDisplaySettingsReader) GetSubTitle() (string, error)      { return "", r.err }
func (r failingDisplaySettingsReader) GetSubSupportUrl() (string, error) { return "", r.err }
func (r failingDisplaySettingsReader) GetSubProfileUrl() (string, error) { return "", r.err }
func (r failingDisplaySettingsReader) GetSubAnnounce() (string, error)   { return "", r.err }
func (r failingDisplaySettingsReader) GetSubEncode() (bool, error)       { return false, r.err }

func TestCachedDisplaySettingsPropagatesRefreshFailure(t *testing.T) {
	subserver.ResetDisplaySettingsCache()
	want := errors.New("settings unavailable")
	if _, err := subserver.CachedDisplaySettings(failingDisplaySettingsReader{err: want}, time.Now()); !errors.Is(err, want) {
		t.Fatalf("refresh error = %v, want %v", err, want)
	}
}
