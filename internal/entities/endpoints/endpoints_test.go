package endpoints

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

type fakeEndpointCore struct {
	running bool
	calls   []string
	err     error
}

func (c *fakeEndpointCore) IsRunning() bool { return c.running }

func (c *fakeEndpointCore) RemoveEndpoint(tag string) error {
	c.calls = append(c.calls, tag)
	return c.err
}

func (c *fakeEndpointCore) AddEndpoint([]byte) error { return nil }

func TestRemoveFromCore(t *testing.T) {
	core := &fakeEndpointCore{running: true}
	if err := RemoveFromCore([]string{"a", "b"}, core); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(core.calls, []string{"a", "b"}) {
		t.Fatalf("calls = %#v", core.calls)
	}
}

func TestRemoveFromCoreIgnoresMissingEndpoint(t *testing.T) {
	core := &fakeEndpointCore{running: true, err: os.ErrInvalid}
	if err := RemoveFromCore([]string{"gone"}, core); err != nil {
		t.Fatalf("missing endpoint should be ignored: %v", err)
	}
}

func TestRemoveFromCoreReturnsRealError(t *testing.T) {
	want := errors.New("boom")
	core := &fakeEndpointCore{running: true, err: want}
	if err := RemoveFromCore([]string{"bad"}, core); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestRemoveFromCoreSkipsStoppedCore(t *testing.T) {
	core := &fakeEndpointCore{}
	if err := RemoveFromCore([]string{"a"}, core); err != nil {
		t.Fatal(err)
	}
	if len(core.calls) != 0 {
		t.Fatalf("stopped core should not be called: %#v", core.calls)
	}
}

func TestOpenVPNClientOwnsNestedAndStaticCredentials(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", "")
	t.Setenv("SUI_SECRET", "")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Setting{}, &model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	row := model.Endpoint{Type: "openvpn-client", Tag: "owned-openvpn", Options: json.RawMessage(`{"mode":"tls","server":"192.0.2.1","password":"private-password-canary","tls":{"client_key":["private-pem-canary","second-line"],"control_wrap":{"type":"tls_auth","key":["private-wrap-canary","second-line"]}},"future_option":{"retained":true}}`)}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := importEndpointCredentials(tx, &row, false); err != nil {
			return err
		}
		if err := prepareEndpointCredentials(tx, &row); err != nil {
			return err
		}
		return tx.Save(&row).Error
	}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(row.Options, []byte("canary")) {
		t.Fatal("native import persisted endpoint secrets")
	}
	view := map[string]interface{}{}
	if err := projectEndpointCredentials(row, view); err != nil {
		t.Fatal(err)
	}
	masked, _ := json.Marshal(view)
	if bytes.Contains(masked, []byte("canary")) || bytes.Contains(masked, []byte("sbox:v1:")) {
		t.Fatal("masked endpoint read disclosed a secret")
	}
	runtime, err := marshalEndpointRuntime(db, row)
	if err != nil || !bytes.Contains(runtime, []byte(`private-wrap-canary\nsecond-line`)) {
		t.Fatal("native Listable key did not retain its runtime value", err)
	}
	foreign := row
	foreign.Id++
	if _, err := marshalEndpointRuntime(db, foreign); err == nil {
		t.Fatal("nested key crossed endpoint identity")
	}
	// The explicit editor conversion clears every TLS credential; omission
	// would retain the old value and leave static mode with invalid TLS state.
	row.Options = json.RawMessage(`{"mode":"static_key","server":"192.0.2.1","password":{"intent":"clear"},"tls":{"client_key":{"intent":"clear"},"control_wrap":{"key":{"intent":"clear"}}},"static_key":{"intent":"replace","value":"private-static-canary"},"future_option":{"retained":true}}`)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := prepareEndpointCredentials(tx, &row); err != nil {
			return err
		}
		return tx.Save(&row).Error
	}); err != nil {
		t.Fatal(err)
	}
	runtime, err = marshalEndpointRuntime(db, row)
	if err != nil || bytes.Contains(runtime, []byte(`"tls"`)) || bytes.Contains(runtime, []byte(`"password"`)) || !bytes.Contains(runtime, []byte("private-static-canary")) || !bytes.Contains(runtime, []byte(`"retained":true`)) {
		t.Fatal("mode conversion lost unrelated state or retained TLS credentials", err)
	}
}

func TestOpenConnectCredentialsStayOwnedAcrossEditImportAndRuntime(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", "")
	t.Setenv("SUI_SECRET", "")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Setting{}, &model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	const password = "private-endpoint-password-canary"
	const forms = `[{"form_id":"auth","submission_key":"login","name":"custom","value":"private-form-canary","promote":true}]`
	options, _ := json.Marshal(map[string]any{"server": "https://192.0.2.1", "password": map[string]string{"intent": "replace", "value": password}, "form_entries": map[string]string{"intent": "replace", "value": forms}, "token": map[string]any{"mode": "totp", "secret": map[string]string{"intent": "replace", "value": "private-token-canary"}}})
	row := model.Endpoint{Type: "openconnect", Tag: "owned-vpn", Options: options}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := prepareEndpointCredentials(tx, &row); err != nil {
			return err
		}
		return tx.Save(&row).Error
	}); err != nil {
		t.Fatal(err)
	}
	if row.Id == 0 || bytes.Contains(row.Options, []byte("canary")) {
		t.Fatal("endpoint credential identity or sealing failed")
	}
	view := map[string]interface{}{}
	if err := projectEndpointCredentials(row, view); err != nil {
		t.Fatal(err)
	}
	masked, _ := json.Marshal(view)
	if bytes.Contains(masked, []byte("canary")) || bytes.Contains(masked, []byte("sbox:v1:")) {
		t.Fatal("normal endpoint read disclosed credentials")
	}
	before := append([]byte(nil), row.Options...)
	row.Options = masked
	fields, err := endpointCredentialObject(row.Options)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range endpointCredentialPaths(row.Type) {
		raw, err := endpointCredentialField(fields, path, nil)
		if err != nil {
			t.Fatal(err)
		}
		var view map[string]any
		_ = json.Unmarshal(raw, &view)
		view["intent"] = "unchanged"
		delete(view, "correlation") // editor submits the fixed mutation contract
		if _, ok := view["rotationRevision"]; !ok {
			view["rotationRevision"] = ""
		}
		encoded, _ := json.Marshal(view)
		mutation := json.RawMessage(encoded)
		if _, err := endpointCredentialField(fields, path, &mutation); err != nil {
			t.Fatal(err)
		}
	}
	row.Options, _ = json.Marshal(fields)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := prepareEndpointCredentials(tx, &row); err != nil {
			return err
		}
		return tx.Save(&row).Error
	}); err != nil {
		t.Fatal("masked editor could not retain credentials", err)
	}
	var old, kept map[string]json.RawMessage
	_ = json.Unmarshal(before, &old)
	_ = json.Unmarshal(row.Options, &kept)
	if !bytes.Equal(old["password"], kept["password"]) || !bytes.Equal(old["form_entries"], kept["form_entries"]) {
		t.Fatal("unchanged editor rotated captured credentials")
	}
	runtime, err := marshalEndpointRuntime(db, row)
	if err != nil || !bytes.Contains(runtime, []byte(password)) || !bytes.Contains(runtime, []byte(`"form_entries":[`)) || !bytes.Contains(runtime, []byte("private-form-canary")) {
		t.Fatal("native runtime credential shape was not restored", err)
	}
	foreign := row
	foreign.Id++
	if _, err := marshalEndpointRuntime(db, foreign); err == nil {
		t.Fatal("foreign row borrowed captured credentials")
	}
	imported := row
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := importEndpointCredentials(tx, &imported, true); err != nil {
			return err
		}
		imported.Id = 0
		imported.Tag = "rebound-vpn"
		if err := prepareEndpointCredentials(tx, &imported); err != nil {
			return err
		}
		return tx.Save(&imported).Error
	}); err != nil {
		t.Fatal("captured import could not rebind native credentials", err)
	}
	if imported.Id == row.Id || bytes.Equal(imported.Options, row.Options) {
		t.Fatal("recreated endpoint retained old secret binding")
	}
	if raw, err := marshalEndpointRuntime(db, imported); err != nil || !bytes.Contains(raw, []byte(password)) {
		t.Fatal("recreated endpoint lost credentials", err)
	}
	// Native imports use the same owner; copied envelopes have no authority.
	native := model.Endpoint{Type: "openconnect", Tag: "native-import", Options: json.RawMessage(`{"server":"https://192.0.2.2","password":"native-import-canary","form_entries":` + forms + `}`)}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := importEndpointCredentials(tx, &native, false); err != nil {
			return err
		}
		return prepareEndpointCredentials(tx, &native)
	}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(native.Options, []byte("canary")) {
		t.Fatal("native import persisted private values")
	}
	copy := model.Endpoint{Type: row.Type, Options: row.Options}
	if err := importEndpointCredentials(db, &copy, false); err == nil {
		t.Fatal("new import accepted an opaque foreign envelope")
	}
	bad := row
	bad.Options = json.RawMessage(`{"password":"raw-password-canary"}`)
	if err := prepareEndpointCredentials(db, &bad); err == nil {
		t.Fatal("editor accepted a raw credential without an intent")
	}
}
