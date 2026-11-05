package endpoints

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	clientdevices "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients/devices"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWireGuardManagedUserDevicesKeepOneCredentialOwner(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", "")
	t.Setenv("SUI_SECRET", "")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.Setting{}, &model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	server, _ := ecdh.X25519().GenerateKey(rand.Reader)
	manual, _ := ecdh.X25519().GenerateKey(rand.Reader)
	encode := base64.StdEncoding.EncodeToString
	options, _ := json.Marshal(map[string]any{"private_key": encode(server.Bytes()), "listen_port": 51820, "address": []string{"10.80.0.1/29"}, "peers": []map[string]any{{"public_key": encode(manual.PublicKey().Bytes()), "allowed_ips": []string{"10.80.0.2/32"}}}})
	endpoint := model.Endpoint{Type: "wireguard", Tag: "user-devices", Options: options, Ext: json.RawMessage(`{"managed":true,"defaultDeviceLimit":2,"publicEndpoint":"vpn.example:51820","dns":[]}`)}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	users := []model.Client{{Name: "alice", Enable: true, Config: json.RawMessage(`{}`)}, {Name: "bob", Enable: true, Config: json.RawMessage(`{}`)}}
	for i := range users {
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Exercise the User owner independently of optional host build tags. The
	// production resolver still admits only the available endpoint capability.
	resolve := func(tx *gorm.DB, id uint) (clientdevices.Policy, error) {
		policy, err := ManagedDevicePolicy(tx, id)
		policy.Available = true
		return policy, err
	}
	mutate := func(request clientdevices.Request) error {
		return db.Transaction(func(tx *gorm.DB) error { _, err := clientdevices.Mutate(tx, request, resolve, 100); return err })
	}
	for _, user := range users {
		if err := mutate(clientdevices.Request{ClientID: user.Id, EndpointID: endpoint.Id, Operation: "grant"}); err != nil {
			t.Fatal(err)
		}
	}
	request := clientdevices.Request{ClientID: users[0].Id, EndpointID: endpoint.Id, Operation: "create", Name: "phone", RequestKey: "request-create-alice-1"}
	if err := mutate(request); err != nil {
		t.Fatal(err)
	}
	if err := mutate(request); err != nil {
		t.Fatal("idempotent create failed", err)
	}
	load := func(id uint) (model.Client, clientdevices.State) {
		t.Helper()
		client, state, err := clientdevices.Load(db, id)
		if err != nil {
			t.Fatal(err)
		}
		return client, state
	}
	alice, first := load(users[0].Id)
	if len(first.Devices) != 1 || first.Devices[0].Address != "10.80.0.3/32" || !secretbox.IsEncrypted(first.Devices[0].PrivateKey) || !secretbox.IsEncrypted(first.Devices[0].PresharedKey) {
		t.Fatal("device allocation/credential ownership failed")
	}
	device := first.Devices[0]
	view, err := credentials.ProjectClient(alice.Id, alice.Config)
	if err != nil || bytes.Contains(view, []byte(device.PrivateKey)) || bytes.Contains(view, []byte("privateKey")) || bytes.Contains(view, []byte(device.CreateRequest)) {
		t.Fatal("ordinary read exposed device credentials")
	}
	request.ClientID = users[1].Id
	request.RequestKey = "request-create-bob-01"
	if err := mutate(request); err != nil {
		t.Fatal(err)
	}
	_, second := load(users[1].Id)
	if second.Devices[0].Address != "10.80.0.4/32" {
		t.Fatal("cross-user allocation collided")
	}
	policy, err := resolve(db, endpoint.Id)
	if err != nil {
		t.Fatal(err)
	}
	peers, err := clientdevices.RuntimePeers(db.Statement.Context, db, policy, 100)
	if err != nil || len(peers) != 2 {
		t.Fatal("active runtime projection failed", err)
	}
	if _, present := peers[0]["private_key"]; present {
		t.Fatal("server projection borrowed client private key")
	}
	config, err := DevicePeerConfig(db, alice.Id, device.ID, "panel.example")
	if err != nil || !strings.Contains(config, "Address = 10.80.0.3/32") || !strings.Contains(config, "Endpoint = vpn.example:51820") || strings.Contains(config, "DNS =") {
		t.Fatal("canonical private config delivery failed", err)
	}
	account := func(generation string, samples map[string]clientdevices.Observation) []clientdevices.TrafficDelta {
		t.Helper()
		var deltas []clientdevices.TrafficDelta
		if err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			deltas, err = clientdevices.Account(tx, endpoint.Id, generation, 200, samples)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return deltas
	}
	samples := map[string]clientdevices.Observation{device.PublicKey: {Received: 100, Sent: 200, LastHandshake: 123}}
	if deltas := account("runtime:1", samples); len(deltas) != 1 || deltas[0].UserID != alice.VPNUserID() || deltas[0].Up != 100 || deltas[0].Down != 200 {
		t.Fatal("traffic did not retain stable User identity")
	}
	if deltas := account("runtime:1", samples); len(deltas) != 0 {
		t.Fatal("snapshot traffic was double counted")
	}
	afterCounters, _ := load(alice.Id)
	afterView, _ := credentials.ProjectClient(alice.Id, afterCounters.Config)
	if !bytes.Equal(view, afterView) {
		t.Fatal("runtime counters invalidated ordinary User ownership revision")
	}
	if deltas := account("runtime:2", samples); len(deltas) != 1 || deltas[0].Up != 100 {
		t.Fatal("new endpoint incarnation inherited old counters")
	}
	rotation := clientdevices.Request{ClientID: alice.Id, EndpointID: endpoint.Id, DeviceID: device.ID, Operation: "rotate", RequestKey: "request-rotate-alice1"}
	if err := mutate(rotation); err != nil {
		t.Fatal(err)
	}
	if err := mutate(rotation); err != nil {
		t.Fatal(err)
	}
	_, rotated := load(alice.Id)
	if rotated.Devices[0].ID != device.ID || rotated.Devices[0].Address != device.Address || rotated.Devices[0].Revision != 2 || rotated.Devices[0].PublicKey == device.PublicKey {
		t.Fatal("rotation changed identity or replayed")
	}
	account("runtime:2", samples) // The old immutable key is no longer authority.
	if deltas := account("runtime:2", map[string]clientdevices.Observation{rotated.Devices[0].PublicKey: {Received: 300, Sent: 400}}); len(deltas) != 1 || deltas[0].Up != 300 {
		t.Fatal("rotated key inherited an obsolete counter baseline")
	}
	rotation.Operation = "revoke"
	if err := mutate(rotation); err != nil {
		t.Fatal(err)
	}
	if _, err := DevicePeerConfig(db, alice.Id, device.ID, "panel.example"); err == nil {
		t.Fatal("revoked key was delivered")
	}
	peers, err = clientdevices.RuntimePeers(db.Statement.Context, db, policy, 100)
	if err != nil || len(peers) != 1 {
		t.Fatal("revoked peer remained effective", err)
	}
	if err := db.Model(&model.Client{}).Where("id = ?", users[1].Id).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	peers, err = clientdevices.RuntimePeers(db.Statement.Context, db, policy, 100)
	if err != nil || len(peers) != 0 {
		t.Fatal("disabled User remained effective", err)
	}
	rotation.Operation = "delete"
	if err := mutate(rotation); err != nil {
		t.Fatal(err)
	}
	request.ClientID = alice.Id
	request.RequestKey = "request-next-alice-01"
	if err := mutate(request); err != nil {
		t.Fatal(err)
	}
	_, next := load(alice.Id)
	if next.Devices[1].Address != "10.80.0.5/32" {
		t.Fatal("unacknowledged old address was reused")
	}
	account("runtime:2", map[string]clientdevices.Observation{})
	request.RequestKey = "request-after-readback"
	if err := mutate(request); err != nil {
		t.Fatal(err)
	}
	_, reused := load(alice.Id)
	if reused.Devices[2].Address != device.Address {
		t.Fatal("acknowledged deletion did not release the address")
	}
}

func TestWireGuardPrivatePeerDeliveryRetainsAmnezia(t *testing.T) {
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
	server, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.StdEncoding.EncodeToString
	headerKey := encode(bytes.Repeat([]byte{7}, 32))
	options := map[string]any{
		"private_key": encode(server.Bytes()), "listen_port": 51820, "mtu": 1420,
		"peers":   []any{map[string]any{"public_key": encode(peer.PublicKey().Bytes()), "allowed_ips": []string{"10.0.0.2/32"}, "pre_shared_key": encode(bytes.Repeat([]byte{8}, 32)), "persistent_keepalive_interval": 25}},
		"amnezia": map[string]any{"s1": 16, "h1": 123, "h2": "200-300", "i1": "<b 0x1234>", "header_protection_key": headerKey, "content_padding_addition": "1-8", "rekey_timeout": 0, "random_trailers": false, "disable_cookies": true},
	}
	ext := map[string]any{"public_key": "stale-derived-display-value", "dns": "10.0.0.1", "keys": []any{map[string]any{"public_key": encode(peer.PublicKey().Bytes()), "private_key": encode(peer.Bytes())}}}
	raw, _ := json.Marshal(options)
	extRaw, _ := json.Marshal(ext)
	row := model.Endpoint{Type: "wireguard", Tag: "private-peer-delivery", Options: raw, Ext: extRaw}
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
	configs, err := PeerConfigs(db, row.Id, "2001:db8::1")
	if err != nil || len(configs) != 1 || configs[0].Peer != 1 {
		t.Fatal("owned peer delivery failed")
	}
	for _, fragment := range []string{"HeaderProtectionKey = " + headerKey, "H2 = 200-300\n", "I1 = <b 0x1234>\n", "ContentPaddingAddition = 1-8\n", "RekeyTimeout = 0\n", "RandomTrailers = false\n", "DisableCookies = true\n", "Endpoint = [2001:db8::1]:51820\n", "PublicKey = " + encode(server.PublicKey().Bytes())} {
		if !strings.Contains(configs[0].Config, fragment) {
			t.Fatal("private delivery lost a native parameter")
		}
	}
	// Client policy must not inherit the server's public listen port, peer
	// route authority or keepalive when an explicit delivery override exists.
	ext["dns"] = []string{"10.0.0.1", "2001:db8::53"}
	ext["publicEndpoint"] = "[2001:db8::2]:63443"
	ext["clientAllowedIPs"] = []string{"192.0.2.0/24", "2001:db8:1::/64"}
	ext["clientKeepalive"] = 0
	row.Ext, _ = json.Marshal(ext)
	if err := db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
	configs, err = PeerConfigs(db, row.Id, "panel.example")
	if err != nil || len(configs) != 1 {
		t.Fatal("native client policy failed", err)
	}
	for _, fragment := range []string{"DNS = 10.0.0.1, 2001:db8::53\n", "Endpoint = [2001:db8::2]:63443\n", "AllowedIPs = 192.0.2.0/24, 2001:db8:1::/64\n", "Address = 10.0.0.2/32\n"} {
		if !strings.Contains(configs[0].Config, fragment) {
			t.Fatal("client policy changed or omitted")
		}
	}
	if strings.Contains(configs[0].Config, "PersistentKeepalive") {
		t.Fatal("explicit zero inherited server peer keepalive")
	}
	ext["dns"] = []string{}
	row.Ext, _ = json.Marshal(ext)
	if err := db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
	configs, err = PeerConfigs(db, row.Id, "panel.example")
	if err != nil || len(configs) != 1 || strings.Contains(configs[0].Config, "DNS =") {
		t.Fatal("explicit empty DNS was replaced", err)
	}
	masked, err := GetAll(db)
	if err != nil {
		t.Fatal(err)
	}
	view, _ := json.Marshal(masked)
	if bytes.Contains(view, []byte(headerKey)) || bytes.Contains(row.Options, []byte(headerKey)) {
		t.Fatal("ordinary read or storage exposed the managed key")
	}
	if _, err := PeerConfigs(db, row.Id, "example.test\nPrivateKey = injected"); !errors.Is(err, ErrPeerConfigUnavailable) {
		t.Fatal("host injection accepted")
	}
	var captured map[string]json.RawMessage
	_ = json.Unmarshal(row.Options, &captured)
	var amnezia map[string]json.RawMessage
	_ = json.Unmarshal(captured["amnezia"], &amnezia)
	amnezia["future_wire_option"] = json.RawMessage(`true`)
	captured["amnezia"], _ = json.Marshal(amnezia)
	row.Options, _ = json.Marshal(captured)
	if err := db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), row.Options...)
	if _, err := PeerConfigs(db, row.Id, "example.test"); !errors.Is(err, ErrPeerConfigUnavailable) {
		t.Fatal("unknown wire option was silently stripped")
	}
	if err := db.First(&row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, row.Options) {
		t.Fatal("unavailable delivery mutated preserved desired state")
	}
	delete(captured, "amnezia")
	row.Options, _ = json.Marshal(captured)
	ext["keys"] = []any{map[string]any{"public_key": encode(peer.PublicKey().Bytes()), "private_key": encode(server.Bytes())}}
	row.Ext, _ = json.Marshal(ext)
	if err := db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := PeerConfigs(db, row.Id, "example.test"); !errors.Is(err, ErrPeerConfigUnavailable) {
		t.Fatal("unrelated private key produced a peer config")
	}
}

func TestWireGuardClientPolicyRejectsInvalidMetadataBeforeSave(t *testing.T) {
	for _, metadata := range []string{
		`{"publicEndpoint":"vpn.example:0"}`, `{"publicEndpoint":"vpn.example:65536"}`,
		`{"publicEndpoint":"vpn.example:443\nPrivateKey = injected"}`,
		`{"dns":["1.1.1.1\nPrivateKey = injected"]}`, `{"dns":42}`, `{"dns":[42]}`, `{"dns":[""]}`,
		`{"clientAllowedIPs":[]}`, `{"clientAllowedIPs":["192.0.2.0/24\nEndpoint = injected"]}`,
		`{"clientKeepalive":3601}`, `{"clientKeepalive":-1}`, `{"clientKeepalive":0.5}`,
	} {
		var candidate map[string]any
		if err := json.Unmarshal([]byte(metadata), &candidate); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"type": "wireguard", "tag": "invalid-policy", "ext": candidate})
		// A nil transaction proves no persistence or credential operation was
		// reached. Valid metadata proceeds to the existing endpoint owner.
		if _, err := Save(nil, "new", raw, nil); err == nil || err.Error() != "invalid WireGuard client delivery policy" {
			t.Fatal("invalid delivery metadata reached persistence", err)
		}
	}
}

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

func TestWireGuardAmneziaHeaderKeyIsOwnedAndExplicitlyRemovable(t *testing.T) {
	for _, kind := range []string{"wireguard", "warp"} {
		t.Run(kind, func(t *testing.T) {
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
			plain := map[string]interface{}{}
			if err := projectEndpointCredentials(model.Endpoint{Id: 1, Type: kind, Options: json.RawMessage(`{}`)}, plain); err != nil {
				t.Fatal(err)
			}
			if _, exists := plain["amnezia"]; exists {
				t.Fatal("masked read enabled Amnezia for ordinary WireGuard")
			}
			row := model.Endpoint{Type: kind, Tag: "owned-awg", Options: json.RawMessage(`{"amnezia":{"h1":123,"h2":"200-300","header_protection_key":"private-header-canary","future":{"retained":true}},"future":{"counter":9007199254740993}}`)}
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
				t.Fatal("import persisted plaintext header key")
			}
			view := map[string]interface{}{}
			if err := projectEndpointCredentials(row, view); err != nil {
				t.Fatal(err)
			}
			masked, _ := json.Marshal(view)
			if bytes.Contains(masked, []byte("canary")) || bytes.Contains(masked, []byte("sbox:v1:")) || !bytes.Contains(masked, []byte("amnezia.header_protection_key")) {
				t.Fatal("header key view lost its fixed masked owner")
			}
			runtime, err := marshalEndpointRuntime(db, row)
			if err != nil || !bytes.Contains(runtime, []byte("private-header-canary")) || !bytes.Contains(runtime, []byte(`"h2":"200-300"`)) || !bytes.Contains(runtime, []byte("9007199254740993")) {
				t.Fatal("runtime lost native key, range or unknown fields", err)
			}
			foreign := row
			foreign.Id++
			if _, err := marshalEndpointRuntime(db, foreign); err == nil {
				t.Fatal("header key crossed endpoint identity")
			}
			row.Options = json.RawMessage(`{"amnezia":{"header_protection_key":{"intent":"clear"}},"future":{"counter":9007199254740993}}`)
			if err := db.Transaction(func(tx *gorm.DB) error {
				if err := prepareEndpointCredentials(tx, &row); err != nil {
					return err
				}
				return tx.Save(&row).Error
			}); err != nil {
				t.Fatal(err)
			}
			runtime, err = marshalEndpointRuntime(db, row)
			if err != nil || bytes.Contains(runtime, []byte("amnezia")) || !bytes.Contains(runtime, []byte("9007199254740993")) {
				t.Fatal("explicit disable retained Amnezia or lost unrelated state", err)
			}
		})
	}
}

func TestOpenVPNClientOwnsNestedAndStaticCredentials(t *testing.T) {
	for _, kind := range []string{"openvpn-client", "openvpn-server"} {
		t.Run(kind, func(t *testing.T) {
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
			if kind == "openvpn-server" {
				row.Type = kind
				row.Options = json.RawMessage(`{"mode":"tls","users":[{"username":"fixture","password":"private-password-canary"}],"tls":{"key":["private-pem-canary","second-line"],"control_wrap":{"type":"tls_auth","key":["private-wrap-canary","second-line"]}},"future_option":{"retained":true}}`)
				for _, invalid := range []string{`[{"username":"fixture","password":null}]`, `[{"username":"fixture","password":"one"},{"username":"fixture","password":"two"}]`, `[{"username":"fixture","Password":"hidden"}]`} {
					if _, err := endpointCredentialPlain("users", invalid); !errors.Is(err, errEndpointCredential) {
						t.Fatal("invalid native authentication bundle was accepted", err)
					}
				}
			}
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
			if kind == "openvpn-server" {
				row.Options = json.RawMessage(`{"mode":"static_key","users":{"intent":"clear"},"tls":{"key":{"intent":"clear"},"control_wrap":{"key":{"intent":"clear"}}},"static_key":{"intent":"replace","value":"private-static-canary"},"future_option":{"retained":true}}`)
			}
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
		})
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
