//go:build !minimal

package backendoutbounds_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/backendoutbounds"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

var errCommitAcknowledgement = errors.New("commit acknowledgement unavailable")

type uncertainCommitPool struct {
	*sql.DB
	commits *atomic.Int32
}

func (p uncertainCommitPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &uncertainCommitTx{Tx: tx, commits: p.commits}, nil
}

type uncertainCommitTx struct {
	*sql.Tx
	commits *atomic.Int32
}

func (tx uncertainCommitTx) Commit() error {
	tx.commits.Add(1)
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	return errCommitAcknowledgement
}

func TestActualBindingLostCommitAcknowledgementReportsUnknownWithoutReplay(t *testing.T) {
	db, source, definition := fixture(t)
	row := model.Outbound{Type: source.Type, Tag: source.OutboundTag, SortOrder: 17, Options: json.RawMessage(`{"server":"old.example.org","server_port":1080,"bind_interface":"local-user-choice"}`)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	source.OutboundId = &row.Id
	if err := db.Table("remote_outbound_connections").Where("id = ?", source.Id).Update("outbound_id", row.Id).Error; err != nil {
		t.Fatal(err)
	}
	owner := backendoutbounds.New(db, source)
	candidate, err := owner.Prepare(db.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	var commits atomic.Int32
	// SQLite commits the real transaction; only its acknowledgement is lost.
	// This exercises the actual owner's finalization path, including its rollback
	// attempt after the commit error, without a mock Outbounds writer.
	fault := db.Session(&gorm.Session{Context: db.Statement.Context})
	fault.Statement.ConnPool = uncertainCommitPool{DB: sqlDB, commits: &commits}
	receipt, err := hostoutbounds.ApplyBinding(fault.Statement.Context, candidate, backendoutbounds.New(fault, source))
	if !errors.Is(err, errCommitAcknowledgement) || !receipt.ApplyAttempted || receipt.State != hostoutbounds.Unknown {
		t.Fatalf("uncertain commit reported %+v %v", receipt, err)
	}
	if commits.Load() != 1 {
		t.Fatalf("ambiguous apply replayed: commits=%d", commits.Load())
	}
	var after model.Outbound
	if err := db.First(&after, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	var options map[string]any
	if err := json.Unmarshal(after.Options, &options); err != nil {
		t.Fatal(err)
	}
	if after.Id != row.Id || after.Tag != row.Tag || options["server"] != "example.org" {
		t.Fatal("fault did not leave the actual committed resource available for reconciliation")
	}
	observed, err := hostoutbounds.ObserveBinding(db.Statement.Context, hostoutbounds.Query{Binding: owner.BindingRef(), Resource: candidate.Resource}, owner)
	if err != nil || !observed.Present || observed.Resource.ID != row.Id {
		t.Fatalf("uncertain outcome cannot be observed: %+v %v", observed, err)
	}
	if commits.Load() != 1 {
		t.Fatal("read-only reconciliation replayed the write")
	}
}
