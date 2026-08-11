package ipmonitor

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestIntegrationAllowEnforcePathLimitModes(t *testing.T) {
	t.Run("monitor mode allows new IP over limit", func(t *testing.T) {
		initIPMonitorTestDB(t)
		seedIntegrationIPClient(t, "integration-monitor", 1, ModeMonitor, true)
		Record("integration-monitor", "198.51.100.10")
		if err := Flush(); err != nil {
			t.Fatal(err)
		}
		warmUpIPMonitorForTest(t)
		if !Allow("integration-monitor", "198.51.100.11") {
			t.Fatal("monitor mode should not enforce limit")
		}
	})

	t.Run("enforce mode allows within limit and rejects over limit", func(t *testing.T) {
		initIPMonitorTestDB(t)
		seedIntegrationIPClient(t, "integration-enforce", 2, ModeEnforce, true)
		Record("integration-enforce", "198.51.100.10")
		if err := Flush(); err != nil {
			t.Fatal(err)
		}
		warmUpIPMonitorForTest(t)
		if !Allow("integration-enforce", "198.51.100.11") {
			t.Fatal("second IP should be allowed when limit=2")
		}
		Record("integration-enforce", "198.51.100.11")
		if Allow("integration-enforce", "198.51.100.12") {
			t.Fatal("third IP should be rejected when limit=2")
		}
	})

	t.Run("disabled client fails open", func(t *testing.T) {
		initIPMonitorTestDB(t)
		seedIntegrationIPClient(t, "integration-disabled", 1, ModeEnforce, false)
		Record("integration-disabled", "198.51.100.10")
		if err := Flush(); err != nil {
			t.Fatal(err)
		}
		warmUpIPMonitorForTest(t)
		if !Allow("integration-disabled", "198.51.100.11") {
			t.Fatal("disabled client should not be enforced")
		}
	})
}

func seedIntegrationIPClient(t *testing.T, name string, limit int, mode string, enabled bool) {
	t.Helper()
	if err := dbsqlite.DB().Create(&model.Client{
		Enable:      enabled,
		Name:        name,
		LimitIP:     limit,
		IPLimitMode: mode,
		Inbounds:    []byte("[]"),
		Links:       []byte("[]"),
	}).Error; err != nil {
		t.Fatal(err)
	}
}
