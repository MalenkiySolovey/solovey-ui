package service

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	"gorm.io/gorm"
)

func resetAccessClient(name, reason string, enabled bool, now int64) model.Client {
	return model.Client{Name: name, Enable: enabled, AccessDisableReason: reason,
		SubSecret: "fixture-" + name, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[1]`), Links: json.RawMessage(`[]`),
		AutoReset: true, NextReset: now - 1, ResetDays: 1, Up: 10, Down: 20, TotalUp: 100, TotalDown: 200, Volume: 300}
}

func TestScheduledResetPreservesStrongerAccessBlockers(t *testing.T) {
	initSettingTestDB(t)
	const now = int64(1_700_000_000)
	cases := []struct {
		name, reason, wantReason string
		enabled, wantEnabled     bool
		expiry                   int64
	}{
		{"manual", model.ClientDisabledManual, model.ClientDisabledManual, false, false, 0},
		{"legacy", model.ClientDisabledLegacy, model.ClientDisabledLegacy, false, false, 0},
		{"unclassified", "", "", false, false, 0},
		{"revoked", "CREDENTIAL_REVOKED", "CREDENTIAL_REVOKED", false, false, 0},
		{"future-blocker", "FUTURE_POLICY_BLOCK", "FUTURE_POLICY_BLOCK", false, false, 0},
		{"expired", model.ClientDisabledExpired, model.ClientDisabledExpired, false, false, now - 1},
		{"quota-and-expired", model.ClientDisabledQuota, model.ClientDisabledExpired, false, false, now - 1},
		{"quota-only", model.ClientDisabledQuota, "", false, true, now + 60},
		{"quota-unlimited-expiry", model.ClientDisabledQuota, "", false, true, 0},
		{"quota-at-expiry-boundary", model.ClientDisabledQuota, "", false, true, now},
		{"enabled", "", "", true, true, 0},
	}
	for _, tc := range cases {
		row := resetAccessClient(tc.name, tc.reason, tc.enabled, now)
		row.Expiry = tc.expiry
		if err := dbsqlite.DB().Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (&ClientService{}).ResetClients(dbsqlite.DB(), now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		var row model.Client
		if err := dbsqlite.DB().Where("name = ?", tc.name).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.Enable != tc.wantEnabled || row.AccessDisableReason != tc.wantReason {
			t.Errorf("%s: enabled=%v reason=%q", tc.name, row.Enable, row.AccessDisableReason)
		}
		if row.Up != 0 || row.Down != 0 || row.TotalUp != 110 || row.TotalDown != 220 || row.NextReset != now+86400 {
			t.Errorf("%s: reset accounting/schedule changed: %#v", tc.name, row)
		}
		if row.Volume != 300 || row.Expiry != tc.expiry || row.SubSecret != "fixture-"+tc.name || string(row.Inbounds) != `[1]` {
			t.Errorf("%s: unrelated access identity changed", tc.name)
		}
	}
}

func TestDepleteClassifiesQuotaAndExpiryBeforeQuotaOnlyReset(t *testing.T) {
	initSettingTestDB(t)
	now := time.Now().Unix()
	quota := resetAccessClient("quota-cycle", "", true, now)
	quota.NextReset, quota.Up, quota.Volume = now+86400, 301, 300
	expired := resetAccessClient("expired-cycle", "", true, now)
	expired.NextReset, expired.Up, expired.Volume, expired.Expiry = now+86400, 301, 300, now-60
	for _, row := range []*model.Client{&quota, &expired} {
		if err := dbsqlite.DB().Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (&ClientService{}).DepleteClients(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		row    *model.Client
		reason string
	}{{&quota, model.ClientDisabledQuota}, {&expired, model.ClientDisabledExpired}} {
		var got model.Client
		if err := dbsqlite.DB().First(&got, tc.row.Id).Error; err != nil {
			t.Fatal(err)
		}
		if got.Enable || got.AccessDisableReason != tc.reason {
			t.Fatalf("depletion: %#v", got)
		}
		if err := dbsqlite.DB().Model(&model.Client{}).Where("id = ?", got.Id).Update("next_reset", now-1).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (&ClientService{}).ResetClients(dbsqlite.DB(), now+1); err != nil {
		t.Fatal(err)
	}
	var gotQuota, gotExpired model.Client
	if err := dbsqlite.DB().First(&gotQuota, quota.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().First(&gotExpired, expired.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !gotQuota.Enable || gotQuota.AccessDisableReason != "" || gotExpired.Enable || gotExpired.AccessDisableReason != model.ClientDisabledExpired {
		t.Fatal("quota cycle reactivated a stronger expiry blocker or failed to clear quota-only state")
	}
}

func TestManualResetTrafficNeverGrantsAccess(t *testing.T) {
	initSettingTestDB(t)
	for _, reason := range []string{model.ClientDisabledManual, model.ClientDisabledQuota, model.ClientDisabledExpired, "CREDENTIAL_REVOKED", model.ClientDisabledLegacy} {
		row := resetAccessClient("manual-reset-"+reason, reason, false, 1_700_000_000)
		if err := dbsqlite.DB().Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := (&ClientService{}).ResetTraffic(strconv.FormatUint(uint64(row.Id), 10)); err != nil {
			t.Fatal(err)
		}
		var got model.Client
		if err := dbsqlite.DB().First(&got, row.Id).Error; err != nil {
			t.Fatal(err)
		}
		if got.Enable || got.AccessDisableReason != reason || got.Up != 0 || got.Down != 0 || got.TotalUp != 110 || got.TotalDown != 220 {
			t.Fatalf("manual reset altered access or accounting: %#v", got)
		}
	}
}

func TestScheduledResetUsesCurrentPolicyAndCounters(t *testing.T) {
	const now = int64(1_700_000_000)
	cases := []struct {
		name                               string
		update                             map[string]interface{}
		manualSave, wantReset, wantEnabled bool
		wantReason                         string
	}{
		{name: "manual-save-after-selection", manualSave: true, wantReset: true, wantReason: model.ClientDisabledManual},
		{name: "revoked-after-selection", update: map[string]interface{}{"enable": false, "access_disable_reason": "CREDENTIAL_REVOKED"}, wantReset: true, wantReason: "CREDENTIAL_REVOKED"},
		{name: "expiry-after-selection", update: map[string]interface{}{"expiry": now - 1}, wantReset: true, wantReason: model.ClientDisabledExpired},
		{name: "schedule-after-selection", update: map[string]interface{}{"next_reset": now + 3600}, wantReason: model.ClientDisabledQuota},
		{name: "period-policy-after-selection", update: map[string]interface{}{"reset_days": 2}, wantReason: model.ClientDisabledQuota},
		{name: "reset-disabled-after-selection", update: map[string]interface{}{"auto_reset": false}, wantReason: model.ClientDisabledQuota},
		{name: "traffic-after-selection", update: map[string]interface{}{"up": 33, "down": 44, "total_up": 1000, "total_down": 2000}, wantReset: true, wantEnabled: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initSettingTestDB(t)
			row := resetAccessClient(tc.name, model.ClientDisabledQuota, false, now)
			if err := dbsqlite.DB().Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			const callback = "test:reset-current-policy"
			triggered := false
			if err := dbsqlite.DB().Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
				if triggered || tx.Statement.Table != "clients" {
					return
				}
				triggered = true
				current := tx.Session(&gorm.Session{NewDB: true})
				if tc.manualSave {
					payload, err := json.Marshal(row)
					if err != nil {
						t.Error(err)
						return
					}
					_, err = entityclients.Save(entityclients.SaveRequest{Tx: current, Action: "edit", Data: payload, Hostname: "fixture.invalid"})
					if err != nil {
						t.Error(err)
					}
				} else if err := current.Model(&model.Client{}).Where("id = ?", row.Id).Updates(tc.update).Error; err != nil {
					t.Error(err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = dbsqlite.DB().Callback().Update().Remove(callback) })
			if _, err := (&ClientService{}).ResetClients(dbsqlite.DB(), now); err != nil {
				t.Fatal(err)
			}
			var got model.Client
			if err := dbsqlite.DB().First(&got, row.Id).Error; err != nil {
				t.Fatal(err)
			}
			if !triggered || got.Enable != tc.wantEnabled || got.AccessDisableReason != tc.wantReason {
				t.Fatalf("stale access resurrected or blocker lost: %#v", got)
			}
			if tc.wantReset {
				wantUp, wantDown := int64(110), int64(220)
				if tc.name == "traffic-after-selection" {
					wantUp, wantDown = 1033, 2044
				}
				if got.Up != 0 || got.Down != 0 || got.TotalUp != wantUp || got.TotalDown != wantDown || got.NextReset != now+86400 {
					t.Fatalf("current counters not reset once: %#v", got)
				}
			} else if got.Up != row.Up || got.Down != row.Down || got.TotalUp != row.TotalUp || got.TotalDown != row.TotalDown {
				t.Fatalf("stale reset consumed a changed period: %#v", got)
			}
		})
	}
}
