package drafts

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateInboundDraftValidatesAndPersistsReviewDraft(t *testing.T) {
	db := newDraftDB(t)

	draft, err := Create(db, CreateInput{
		Source:      " fixture-provider ",
		SourceRef:   " site/1 ",
		Status:      StatusReviewRequired,
		InboundType: "vless",
		Tag:         "draft-in",
		Payload:     json.RawMessage(`{"type":"vless"}`),
		CreatedBy:   "tester",
		ExpiresAt:   123,
		Now:         10,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if draft.Id == 0 || draft.Source != "fixture-provider" || draft.SourceRef != "site/1" || draft.Status != StatusReviewRequired {
		t.Fatalf("unexpected draft: %#v", draft)
	}

	var stored model.InboundDraft
	if err := db.First(&stored, draft.Id).Error; err != nil {
		t.Fatalf("stored draft: %v", err)
	}
	if string(stored.Payload) != `{"type":"vless"}` || stored.CreatedAt != 10 || stored.UpdatedAt != 10 {
		t.Fatalf("unexpected stored draft: %#v", stored)
	}
}

func TestCreateInboundDraftRejectsInvalidJSONAndStatus(t *testing.T) {
	db := newDraftDB(t)
	if _, err := Create(db, CreateInput{Source: "x", SourceRef: "y", Status: "ready"}); err == nil {
		t.Fatalf("unsupported status should be rejected")
	}
	if _, err := Create(db, CreateInput{Source: "x", SourceRef: "y", Status: StatusBlocked, Payload: json.RawMessage(`{bad`)}); err == nil {
		t.Fatalf("invalid payload should be rejected")
	}
}

func TestCleanupExpiredOnlyRemovesOpenDrafts(t *testing.T) {
	db := newDraftDB(t)
	rows := []model.InboundDraft{
		{Source: "x", SourceRef: "blocked", Status: StatusBlocked, Payload: []byte("{}"), ExpiresAt: 10},
		{Source: "x", SourceRef: "review", Status: StatusReviewRequired, Payload: []byte("{}"), ExpiresAt: 10},
		{Source: "x", SourceRef: "applied", Status: StatusApplied, Payload: []byte("{}"), ExpiresAt: 10},
		{Source: "x", SourceRef: "future", Status: StatusBlocked, Payload: []byte("{}"), ExpiresAt: 30},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed drafts: %v", err)
	}
	if err := CleanupExpired(db, 20); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	var refs []string
	if err := db.Model(&model.InboundDraft{}).Order("source_ref").Pluck("source_ref", &refs).Error; err != nil {
		t.Fatalf("list refs: %v", err)
	}
	got := strings.Join(refs, ",")
	if got != "applied,future" {
		t.Fatalf("remaining refs = %s, want applied,future", got)
	}
}

func TestListOpenCleansExpiredAndReturnsOpenDraftsNewestFirst(t *testing.T) {
	db := newDraftDB(t)
	rows := []model.InboundDraft{
		{Source: "x", SourceRef: "old", Status: StatusBlocked, Payload: []byte("{}"), ExpiresAt: 10},
		{Source: "x", SourceRef: "ready", Status: StatusReviewRequired, Payload: []byte("{}"), ExpiresAt: 30},
		{Source: "x", SourceRef: "applied", Status: StatusApplied, Payload: []byte("{}"), ExpiresAt: 30},
		{Source: "x", SourceRef: "blocked", Status: StatusBlocked, Payload: []byte("{}"), ExpiresAt: 30},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed drafts: %v", err)
	}
	got, err := ListOpen(db, 20)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	refs := make([]string, 0, len(got))
	for _, row := range got {
		refs = append(refs, row.SourceRef)
	}
	if strings.Join(refs, ",") != "blocked,ready" {
		t.Fatalf("open refs = %v, want blocked,ready", refs)
	}
}

func TestMarkAppliedClosesOnlyOpenDraft(t *testing.T) {
	db := newDraftDB(t)
	rows := []model.InboundDraft{
		{Source: "x", SourceRef: "ready", Status: StatusReviewRequired, Payload: []byte("{}"), ExpiresAt: 30},
		{Source: "x", SourceRef: "applied", Status: StatusApplied, Payload: []byte("{}"), ExpiresAt: 30},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed drafts: %v", err)
	}
	if err := MarkApplied(db, rows[0].Id, 40); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}
	var stored model.InboundDraft
	if err := db.First(&stored, rows[0].Id).Error; err != nil {
		t.Fatalf("load applied draft: %v", err)
	}
	if stored.Status != StatusApplied || stored.UpdatedAt != 40 {
		t.Fatalf("unexpected applied draft: %#v", stored)
	}
	if err := MarkApplied(db, rows[1].Id, 50); err == nil {
		t.Fatal("already closed draft should not be applied again")
	}
	open, err := ListOpen(db, 20)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("open drafts after apply = %#v", open)
	}
}

func TestMarkAppliedRejectsRetiredLegacySelfStealDraft(t *testing.T) {
	db := newDraftDB(t)
	row := model.InboundDraft{
		Source: "fixture-provider" + retiredSelfStealSourceSuffix, SourceRef: "historical", Status: StatusReviewRequired,
		Payload: json.RawMessage(`{"inboundCandidate":{"type":"vless"}}`), CreatedAt: 1, UpdatedAt: 1,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := MarkApplied(db, row.Id, 2); err == nil || err.Error() != "legacy_self_steal_retired" {
		t.Fatalf("retired draft apply err=%v", err)
	}
	if err := db.First(&row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != StatusReviewRequired || row.UpdatedAt != 1 {
		t.Fatalf("retired draft changed: %#v", row)
	}
}

func newDraftDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.InboundDraft{}); err != nil {
		t.Fatal(err)
	}
	return db
}
