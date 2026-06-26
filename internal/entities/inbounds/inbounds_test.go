package entityinbounds

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeInboundCore struct {
	running bool
	removed []string
	added   []string
	closed  []string
	err     error
}

func (c *fakeInboundCore) IsRunning() bool {
	return c.running
}

func (c *fakeInboundCore) RemoveInbound(tag string) error {
	c.removed = append(c.removed, tag)
	return c.err
}

func (c *fakeInboundCore) AddInbound(config []byte) error {
	var raw map[string]any
	_ = json.Unmarshal(config, &raw)
	tag, _ := raw["tag"].(string)
	c.added = append(c.added, tag)
	return c.err
}

func (c *fakeInboundCore) CloseInboundConnections(tag string) {
	c.closed = append(c.closed, tag)
}

func newInboundDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Tls{}, &model.Inbound{}, &model.Service{}); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	return db
}

func TestSupportedActions(t *testing.T) {
	want := []string{"new", "edit", "del"}
	if got := SupportedActionStrings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported inbound save actions = %#v, want %#v", got, want)
	}

	got := make([]string, 0, len(supportedSaveActions))
	for _, action := range supportedSaveActions {
		got = append(got, string(action))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inbound save actions = %#v, want %#v", got, want)
	}
}

func TestParseAction(t *testing.T) {
	action, ok := ParseAction("edit")
	if !ok {
		t.Fatal("expected edit action to be supported")
	}
	if action != ActionEdit {
		t.Fatalf("parsed action = %q, want %q", action, ActionEdit)
	}
	if _, ok := ParseAction("mystery"); ok {
		t.Fatal("unexpected support for unknown inbound save action")
	}
}

func TestSaveRejectsUnknownAction(t *testing.T) {
	_, err := Save(SaveRequest{Action: "mystery"})
	if err == nil {
		t.Fatal("expected unknown action to be rejected")
	}
	if err.Error() != "unknown action: mystery" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFillAndSaveKeepsExistingSortOrder(t *testing.T) {
	db := newInboundDB(t)
	inbound := model.Inbound{
		Type:      "socks",
		Tag:       "existing",
		SortOrder: 6,
		Options:   json.RawMessage(`{"listen":"127.0.0.1","listen_port":1080}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	inbound.Tag = "renamed"
	if err := FillAndSave(db, &inbound, "example.com"); err != nil {
		t.Fatal(err)
	}
	if inbound.SortOrder != 6 {
		t.Fatalf("sort order = %d, want 6", inbound.SortOrder)
	}
}

func TestRemoveFromCoreSkipsStoppedCore(t *testing.T) {
	core := &fakeInboundCore{}
	if err := RemoveFromCore([]string{"a"}, core); err != nil {
		t.Fatal(err)
	}
	if len(core.removed) != 0 || len(core.closed) != 0 {
		t.Fatalf("stopped core should not be touched: removed=%#v closed=%#v", core.removed, core.closed)
	}
}

func TestRemoveFromCoreIgnoresInvalidRemovalAndClosesConnections(t *testing.T) {
	core := &fakeInboundCore{running: true, err: os.ErrInvalid}
	if err := RemoveFromCore([]string{"a", "b"}, core); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(core.removed, []string{"a", "b"}) {
		t.Fatalf("removed = %#v", core.removed)
	}
	if !reflect.DeepEqual(core.closed, []string{"a", "b"}) {
		t.Fatalf("closed = %#v", core.closed)
	}
}

func TestRemoveFromCoreReturnsRealError(t *testing.T) {
	want := errors.New("boom")
	core := &fakeInboundCore{running: true, err: want}
	if err := RemoveFromCore([]string{"a"}, core); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
