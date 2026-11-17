//go:build !minimal

package backendoutbounds_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/backendoutbounds"
	remote "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/internal/bindingfixture"
	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func fixture(t *testing.T) (*gorm.DB, backendoutbounds.Source, json.RawMessage) {
	t.Helper()
	identity := bindingfixture.Provider(t)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "binding.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	db = db.WithContext(backendoutbounds.WithProviderObservation(context.Background(), func() coreprovider.Identity { return identity }))
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&remote.RemoteOutboundSubscription{}, &remote.RemoteOutboundConnection{}, &remote.RemoteOutboundGroup{}, &remote.RemoteOutboundGroupConnection{}, &model.Outbound{}, &model.Endpoint{}, &model.Service{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	source := remote.RemoteOutboundSubscription{Name: "source"}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	connection := remote.RemoteOutboundConnection{SubscriptionId: source.Id, SourceKey: "endpoint", OutboundTag: "managed-node", Type: "vless", Enabled: true, Options: json.RawMessage(`{"server":"example.org","server_port":443}`)}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	view := backendoutbounds.Source{Id: connection.Id, SubscriptionId: source.Id, SourceKey: connection.SourceKey, OutboundTag: connection.OutboundTag, Type: connection.Type, Enabled: true, Options: connection.Options}
	definition := json.RawMessage(`{"type":"vless","tag":"managed-node","server":"example.org","server_port":443}`)
	return db, view, definition
}
func TestActualBindingCommitAndRollback(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			db, source, definition := fixture(t)
			sentinel := errors.New("rollback source transaction")
			err := db.Transaction(func(tx *gorm.DB) error {
				owner := backendoutbounds.New(tx, source)
				c, err := owner.Prepare(tx.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{})
				if err != nil {
					return err
				}
				r, err := hostoutbounds.ApplyBinding(tx.Statement.Context, c, owner)
				if err != nil {
					return err
				}
				if r.State != hostoutbounds.Pending || r.Resource.ID == 0 {
					t.Fatalf("uncommitted write claimed success: %+v", r)
				}
				if rollback {
					return sentinel
				}
				return tx.Table("remote_outbound_connections").Where("id = ?", source.Id).Update("outbound_id", r.Resource.ID).Error
			})
			if rollback && !errors.Is(err, sentinel) || !rollback && err != nil {
				t.Fatal(err)
			}
			var rows []model.Outbound
			if err := db.Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if rollback {
				if len(rows) != 0 {
					t.Fatal("rollback retained an Outbound")
				}
				return
			}
			if len(rows) != 1 {
				t.Fatal("canonical Outbound missing")
			}
			source.OutboundId = &rows[0].Id
			owner := backendoutbounds.New(db, source)
			query := hostoutbounds.Query{Binding: owner.BindingRef(), Resource: hostoutbounds.ResourceRef{Host: owner.BindingRef().Host, ID: rows[0].Id, Tag: source.OutboundTag}}
			observed, err := hostoutbounds.ObserveBinding(db.Statement.Context, query, owner)
			if err != nil || !observed.Present || observed.ResourceRevision == "ABSENT" {
				t.Fatalf("committed binding not observed: %+v %v", observed, err)
			}
		})
	}
}
func TestActualBindingRejectsStaleSourceAndUnknownProvider(t *testing.T) {
	db, source, definition := fixture(t)
	owner := backendoutbounds.New(db, source)
	c, err := owner.Prepare(db.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("remote_outbound_connections").Where("id = ?", source.Id).Update("options", json.RawMessage(`{"server":"replacement.example.org"}`)).Error; err != nil {
		t.Fatal(err)
	}
	r, err := hostoutbounds.ApplyBinding(db.Statement.Context, c, owner)
	if !errors.Is(err, hostoutbounds.ErrRevision) || r.ApplyAttempted {
		t.Fatalf("stale source admitted: %+v %v", r, err)
	}
	db, source, definition = fixture(t)
	db = db.WithContext(backendoutbounds.WithProviderObservation(context.Background(), func() coreprovider.Identity { return coreprovider.Identity{State: coreprovider.Unknown} }))
	if _, err := backendoutbounds.New(db, source).Prepare(db.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{}); !errors.Is(err, hostoutbounds.ErrProvider) {
		t.Fatal("unknown provider fell back to a build flavor")
	}
}
func TestActualBindingPreservesCompetingLocalOutbound(t *testing.T) {
	db, source, definition := fixture(t)
	owner := backendoutbounds.New(db, source)
	c, err := owner.Prepare(db.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{})
	if err != nil {
		t.Fatal(err)
	}
	local := model.Outbound{Type: "direct", Tag: source.OutboundTag, SortOrder: 17, Options: json.RawMessage(`{"domain_strategy":"prefer_ipv6"}`)}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	r, err := hostoutbounds.ApplyBinding(db.Statement.Context, c, owner)
	if err == nil || r.ApplyAttempted {
		t.Fatalf("competing writer admitted: %+v %v", r, err)
	}
	var after model.Outbound
	if err := db.First(&after, local.Id).Error; err != nil {
		t.Fatal(err)
	}
	if after.Type != local.Type || string(after.Options) != string(local.Options) || after.SortOrder != 17 {
		t.Fatal("local outbound overwritten")
	}
}
func TestActualBindingConcurrentCandidatesHaveOneWriter(t *testing.T) {
	db, source, definition := fixture(t)
	owner := backendoutbounds.New(db, source)
	c, err := owner.Prepare(db.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan bool, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			<-start
			r, err := hostoutbounds.ApplyBinding(db.Statement.Context, c, backendoutbounds.New(db, source))
			results <- err == nil && r.State == hostoutbounds.Applied
		})
	}
	close(start)
	workers.Wait()
	close(results)
	accepted := 0
	for ok := range results {
		if ok {
			accepted++
		}
	}
	var n int64
	if err := db.Model(&model.Outbound{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || n != 1 {
		t.Fatalf("accepted=%d resources=%d", accepted, n)
	}
}

func TestBindingObservationDoesNotDetachAnExactResourceForAnotherInvalidLink(t *testing.T) {
	db, source, definition := fixture(t)
	row := model.Outbound{Tag: source.OutboundTag, Type: source.Type, Options: source.Options}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	source.OutboundId = &row.Id
	if err := db.Table("remote_outbound_connections").Where("id = ?", source.Id).Update("outbound_id", row.Id).Error; err != nil {
		t.Fatal(err)
	}
	other := remote.RemoteOutboundConnection{SubscriptionId: source.SubscriptionId, SourceKey: "disabled-other", Type: "direct", OutboundTag: "other-disabled", OutboundId: &row.Id, Enabled: false}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	owner := backendoutbounds.New(db, source)
	q := hostoutbounds.Query{Binding: owner.BindingRef(), Resource: hostoutbounds.ResourceRef{Host: owner.BindingRef().Host, ID: row.Id, Tag: row.Tag}}
	observed, err := hostoutbounds.ObserveBinding(db.Statement.Context, q, owner)
	if err != nil || !observed.Present {
		t.Fatalf("exact resource detached during observation: %+v %v", observed, err)
	}
	c, err := owner.Prepare(db.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := hostoutbounds.ApplyBinding(db.Statement.Context, c, owner)
	if !errors.Is(err, hostoutbounds.ErrConflict) || r.ApplyAttempted {
		t.Fatalf("competing write admitted: %+v %v", r, err)
	}
}

func TestActualBindingFencesLocalOptionsChangedAfterPreview(t *testing.T) {
	db, source, definition := fixture(t)
	row := model.Outbound{Tag: source.OutboundTag, Type: source.Type, Options: source.Options}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	source.OutboundId = &row.Id
	if err := db.Table("remote_outbound_connections").Where("id = ?", source.Id).Update("outbound_id", row.Id).Error; err != nil {
		t.Fatal(err)
	}
	owner := backendoutbounds.New(db, source)
	c, err := owner.Prepare(db.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{})
	if err != nil {
		t.Fatal(err)
	}
	local := json.RawMessage(`{"server":"local-override.example.org","server_port":8443}`)
	if err := db.Model(&row).Update("options", local).Error; err != nil {
		t.Fatal(err)
	}
	r, err := hostoutbounds.ApplyBinding(db.Statement.Context, c, owner)
	if !errors.Is(err, hostoutbounds.ErrRevision) || r.ApplyAttempted {
		t.Fatalf("local options revision not fenced: %+v %v", r, err)
	}
	var after model.Outbound
	if err := db.First(&after, row.Id).Error; err != nil || string(after.Options) != string(local) {
		t.Fatal("local options replaced", err)
	}
}

func TestRetagSourceBindingPreservesResourceAndRollback(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			db, source, definition := fixture(t)
			owner := backendoutbounds.New(db, source)
			candidate, err := owner.Prepare(db.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := hostoutbounds.ApplyBinding(db.Statement.Context, candidate, owner)
			if err != nil {
				t.Fatal(err)
			}
			source.OutboundId = &receipt.Resource.ID
			if err := db.Table("remote_outbound_connections").Where("id = ?", source.Id).Updates(map[string]any{"outbound_id": receipt.Resource.ID, "synced": true}).Error; err != nil {
				t.Fatal(err)
			}
			var before model.Outbound
			if err := db.First(&before, receipt.Resource.ID).Error; err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New("rollback retag")
			err = db.Transaction(func(tx *gorm.DB) error {
				changed, err := backendoutbounds.New(tx, source).Retag(tx.Statement.Context, source.OutboundTag, "renamed-node")
				if err != nil {
					return err
				}
				if !changed {
					t.Fatal("owned retag did not change the resource")
				}
				if rollback {
					return sentinel
				}
				return tx.Table("remote_outbound_connections").Where("id = ?", source.Id).Update("outbound_tag", "renamed-node").Error
			})
			if rollback && !errors.Is(err, sentinel) || !rollback && err != nil {
				t.Fatal(err)
			}
			var after model.Outbound
			if err := db.First(&after, before.Id).Error; err != nil {
				t.Fatal(err)
			}
			want := source.OutboundTag
			if !rollback {
				want = "renamed-node"
			}
			if after.Tag != want || after.Type != before.Type || after.SortOrder != before.SortOrder || string(after.Options) != string(before.Options) {
				t.Fatalf("retag changed unrelated resource or escaped rollback: %+v", after)
			}
		})
	}
}
func TestRetagRejectsChangedSourceAndCompetingBinding(t *testing.T) {
	for _, competing := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed source", true: "competing binding"}[competing], func(t *testing.T) {
			db, source, definition := fixture(t)
			owner := backendoutbounds.New(db, source)
			candidate, err := owner.Prepare(db.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := hostoutbounds.ApplyBinding(db.Statement.Context, candidate, owner)
			if err != nil {
				t.Fatal(err)
			}
			source.OutboundId = &receipt.Resource.ID
			if err := db.Table("remote_outbound_connections").Where("id = ?", source.Id).Update("outbound_id", receipt.Resource.ID).Error; err != nil {
				t.Fatal(err)
			}
			expected := hostoutbounds.ErrRevision
			if competing {
				expected = hostoutbounds.ErrConflict
				other := remote.RemoteOutboundConnection{SubscriptionId: source.SubscriptionId, SourceKey: "other", OutboundTag: "other-tag", OutboundId: source.OutboundId}
				if err := db.Create(&other).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Table("remote_outbound_connections").Where("id = ?", source.Id).Update("name", "changed source").Error; err != nil {
					t.Fatal(err)
				}
			}
			changed, err := backendoutbounds.New(db, source).Retag(db.Statement.Context, source.OutboundTag, "renamed-node")
			if changed || !errors.Is(err, expected) {
				t.Fatalf("unowned retag accepted: %v %v", changed, err)
			}
			var after model.Outbound
			if err := db.First(&after, receipt.Resource.ID).Error; err != nil {
				t.Fatal(err)
			}
			if after.Tag != source.OutboundTag {
				t.Fatal("rejected retag changed resource")
			}
		})
	}
}
