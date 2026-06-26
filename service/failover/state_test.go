package failover

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeGroupReader map[string]string

func (f fakeGroupReader) GroupNow(tag string) (string, bool) {
	active, ok := f[tag]
	return active, ok
}

func newStateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Outbound{}, &model.FailoverMemberState{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestWriteMemberStatesUpserts(t *testing.T) {
	db := newStateDB(t)
	initial := []model.FailoverMemberState{{GroupTag: "g", MemberTag: "a", Healthy: false, ConsecDown: 1, LastProbeAt: 1}}
	if err := WriteMemberStates(db, initial); err != nil {
		t.Fatal(err)
	}
	updated := []model.FailoverMemberState{{GroupTag: "g", MemberTag: "a", Healthy: true, ConsecUp: 2, LastProbeAt: 2}}
	if err := WriteMemberStates(db, updated); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadMemberStates(db, "g")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].Healthy || rows[0].ConsecUp != 2 || rows[0].ConsecDown != 0 {
		t.Fatalf("state = %#v", rows)
	}
}

func TestStatusPreservesPriorityAndReadsLiveActiveMember(t *testing.T) {
	db := newStateDB(t)
	group := model.Outbound{Type: "failover", Tag: "g", Options: json.RawMessage(`{"outbounds":["a","b"]}`)}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := WriteMemberStates(db, []model.FailoverMemberState{
		{GroupTag: "g", MemberTag: "a", Healthy: false},
		{GroupTag: "g", MemberTag: "b", Healthy: true},
	}); err != nil {
		t.Fatal(err)
	}
	status, err := Status(db, fakeGroupReader{"g": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 1 || status[0].Active != "b" || status[0].AllDown {
		t.Fatalf("status = %#v", status)
	}
	if len(status[0].Members) != 2 || status[0].Members[0].Tag != "a" || status[0].Members[1].Priority != 1 {
		t.Fatalf("members = %#v", status[0].Members)
	}
}
