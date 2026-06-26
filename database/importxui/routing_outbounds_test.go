package importxui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/importxui/mapping"
	"github.com/MalenkiySolovey/solovey-ui/database/importxui/source"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPlanRoutingDisabledNoticeWarnsAboutOutbounds(t *testing.T) {
	dir := makeImportXUITempDir(t)
	srcPath := filepath.Join(dir, "x-ui.db")
	buildCompatSource(t, forkVariant, srcPath)
	db, err := gorm.Open(sqlite.Open(srcPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	xray := `{"outbounds":[{"tag":"chain","protocol":"vless","settings":{"vnext":[{"address":"a.example.com","port":443,"users":[{"id":"u"}]}]}}]}`
	if err := db.Exec("INSERT INTO settings(key, value) VALUES(?, ?)", "xrayConfig", xray).Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	src, err := source.Open(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	plan := &MigrationPlan{}
	if err := planRoutingDisabledNotice(context.Background(), src, plan); err != nil {
		t.Fatal(err)
	}
	for _, item := range plan.Items {
		for _, warning := range item.Warnings {
			if item.Kind == KindRouting && item.Action == ActionSkip && strings.Contains(warning, "routing import is disabled") && strings.Contains(warning, "proxy outbound") {
				return
			}
		}
	}
	t.Fatalf("expected routing-disabled notice; items=%#v", plan.Items)
}

func TestCreateNewOutboundsIdempotentNoClobber(t *testing.T) {
	initCompatDest(t)
	db := dbsqlite.DB()
	cfg := `{"outbounds":[{"tag":"chain-out","protocol":"trojan","settings":{"servers":[{"address":"t.example.com","port":443,"password":"tpw"}]},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"t.example.com"}}}]}`
	_, outbounds, _, _ := mapping.MapXrayOutbounds(cfg)
	report := &Report{}
	if err := createNewOutbounds(db, outbounds, report); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Outbound{}).Where("tag = ?", "chain-out").Update("options", json.RawMessage(`{"server":"edited"}`)).Error; err != nil {
		t.Fatal(err)
	}
	_, next, _, _ := mapping.MapXrayOutbounds(cfg)
	report = &Report{}
	if err := createNewOutbounds(db, next, report); err != nil {
		t.Fatal(err)
	}
	var outbound model.Outbound
	if err := db.Where("tag = ?", "chain-out").First(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(outbound.Options), "edited") || report.Summary.Outbounds.Skipped != 1 {
		t.Fatalf("operator edit was not preserved: %#v options=%s", report.Summary.Outbounds, outbound.Options)
	}
}
