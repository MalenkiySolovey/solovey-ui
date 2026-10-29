package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeServiceCore struct {
	running bool
	calls   []string
	err     error
}

func (c *fakeServiceCore) IsRunning() bool { return c.running }

func (c *fakeServiceCore) RemoveService(tag string) error {
	c.calls = append(c.calls, tag)
	return c.err
}

func (c *fakeServiceCore) AddService([]byte) error { return nil }

func TestRemoveFromCore(t *testing.T) {
	core := &fakeServiceCore{running: true}
	if err := RemoveFromCore([]string{"a", "b"}, core); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(core.calls, []string{"a", "b"}) {
		t.Fatalf("calls = %#v", core.calls)
	}
}

func TestRemoveFromCoreIgnoresMissingService(t *testing.T) {
	core := &fakeServiceCore{running: true, err: os.ErrInvalid}
	if err := RemoveFromCore([]string{"gone"}, core); err != nil {
		t.Fatalf("missing service should be ignored: %v", err)
	}
}

func TestRemoveFromCoreReturnsRealError(t *testing.T) {
	want := errors.New("boom")
	core := &fakeServiceCore{running: true, err: want}
	if err := RemoveFromCore([]string{"bad"}, core); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestRemoveFromCoreSkipsStoppedCore(t *testing.T) {
	core := &fakeServiceCore{}
	if err := RemoveFromCore([]string{"a"}, core); err != nil {
		t.Fatal(err)
	}
	if len(core.calls) != 0 {
		t.Fatalf("stopped core should not be called: %#v", core.calls)
	}
}

func TestRealmTokensStayBoundToServiceAndIdentity(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", "")
	t.Setenv("SUI_SECRET", "")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.AutoMigrate(&model.Service{}, &model.Tls{}, &model.Setting{}, &model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	const token = "private-realm-token-canary"
	row := model.Service{Type: "hysteria-realm", Tag: "realm", Options: json.RawMessage(`{"listen":"127.0.0.1","users":[{"name":"operator","token":{"intent":"replace","value":"` + token + `"},"max_realms":2}],"idle_timeout":"15s"}`)}
	persist := func(candidate *model.Service) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := prepareRealmCredentials(tx, candidate); err != nil {
				return err
			}
			return tx.Save(candidate).Error
		})
	}
	if err := persist(&row); err != nil || row.Id == 0 || bytes.Contains(row.Options, []byte(token)) {
		t.Fatal("realm token was not sealed", err)
	}
	before := append([]byte(nil), row.Options...)
	views, err := GetAll(db)
	masked, _ := json.Marshal(views)
	if err != nil || len(*views) != 1 || bytes.Contains(masked, []byte(token)) || bytes.Contains(masked, []byte("sbox:v1:")) {
		t.Fatal("service read disclosed a realm credential", err)
	}
	editor, _ := json.Marshal((*views)[0])
	// The shared editor submits explicit Keep intents from masked views.
	var fields map[string]any
	_ = json.Unmarshal(editor, &fields)
	fields["users"].([]any)[0].(map[string]any)["token"].(map[string]any)["intent"] = "unchanged"
	delete(fields["users"].([]any)[0].(map[string]any)["token"].(map[string]any), "correlation")
	editor, _ = json.Marshal(fields)
	var edited model.Service
	if err := edited.UnmarshalJSON(editor); err != nil {
		t.Fatal(err)
	}
	if err := persist(&edited); err != nil || !bytes.Equal(before, edited.Options) {
		t.Fatal("masked realm edit did not retain its token", err)
	}
	runtime, err := marshalServiceRuntime(db, edited)
	if err != nil || !bytes.Contains(runtime, []byte(token)) || !bytes.Contains(runtime, []byte(`"idle_timeout":"15s"`)) {
		t.Fatal("runtime projection lost native semantics", err)
	}
	foreign := edited
	foreign.Id++
	if _, err := marshalServiceRuntime(db, foreign); err == nil {
		t.Fatal("foreign service borrowed a realm token")
	}
	viewBefore := append([]byte(nil), editor...)
	for _, mutation := range []func(map[string]any){
		func(m map[string]any) { m["users"].([]any)[0].(map[string]any)["name"] = "renamed" },
		func(m map[string]any) {
			m["users"].([]any)[0].(map[string]any)["token"] = map[string]string{"intent": "clear"}
		},
		func(m map[string]any) { m["users"] = []any{} },
		func(m map[string]any) { m["users"] = append(m["users"].([]any), m["users"].([]any)[0]) },
	} {
		var fields map[string]any
		_ = json.Unmarshal(viewBefore, &fields)
		mutation(fields)
		raw, _ := json.Marshal(fields)
		var invalid model.Service
		_ = invalid.UnmarshalJSON(raw)
		if err := persist(&invalid); err == nil {
			t.Fatal("invalid realm identity mutation was admitted")
		}
	}
	var after model.Service
	if err := db.First(&after, row.Id).Error; err != nil || !bytes.Equal(after.Options, before) {
		t.Fatal("rejected realm mutation changed desired state", err)
	}
}
