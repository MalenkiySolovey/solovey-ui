//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	clientdevices "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients/devices"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/sagernet/sing-tun"
	M "github.com/sagernet/sing/common/metadata"
)

func TestNativeManagedWireGuardDevicesApplyRuntimeTrafficAndQuota(t *testing.T) {
	if !tun.WithGVisor || !registry.WireGuardDeviceManagementAvailable() {
		t.Skip("managed native WireGuard is absent from this profile")
	}
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}],"route":{"rules":[{"ip_cidr":["198.18.0.1/32"],"action":"route-options","override_address":"127.0.0.1"}]}}`))
	db := dbsqlite.DB()
	core := coreruntime.NewCore()
	t.Cleanup(func() { _ = core.Stop() })
	svc := NewConfigServiceWithRuntime(NewRuntime(core))
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.LocalAddr().(*net.UDPAddr).Port
	_ = reservation.Close()
	endpoint := map[string]any{"type": "wireguard", "tag": "managed-wg", "private_key": base64.StdEncoding.EncodeToString(serverKey.Bytes()), "listen_port": port, "address": []string{"10.79.0.1/29"}, "ext": map[string]any{"managed": true, "defaultDeviceLimit": 2, "publicEndpoint": net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), "dns": []string{}}}
	user := map[string]any{"name": "managed-user", "enable": true, "inbounds": []uint{}, "config": map[string]any{}}
	for _, item := range []struct {
		object string
		data   any
	}{{"endpoints", endpoint}, {"clients", user}} {
		raw, _ := json.Marshal(item.data)
		if _, err := svc.Save(item.object, "new", raw, "", "admin", "fixture.invalid"); err != nil {
			t.Fatal("managed desired state rejected", err)
		}
	}
	var owner model.Client
	var ep model.Endpoint
	if db.Where("name = ?", "managed-user").First(&owner).Error != nil || db.Where("tag = ?", "managed-wg").First(&ep).Error != nil {
		t.Fatal("canonical identities missing")
	}
	for _, command := range []clientdevices.Request{{ClientID: owner.Id, EndpointID: ep.Id, Operation: "grant"}, {ClientID: owner.Id, EndpointID: ep.Id, Operation: "create", Name: "phone", RequestKey: "native-managed-device-create"}} {
		raw, _ := json.Marshal(command)
		if _, err := svc.Save("clients", "wireguard", raw, "", "admin", "fixture.invalid"); err != nil {
			t.Fatal("User device command rejected", err)
		}
	}
	_, state, err := clientdevices.Load(db, owner.Id)
	if err != nil || len(state.Devices) != 1 {
		t.Fatal("device owner missing", err)
	}
	device := state.Devices[0]
	private, err := svc.ClientService.WireGuardDeviceConfig(t.Context(), owner.Id, device.ID, "fixture.invalid")
	if err != nil {
		t.Fatal("private client delivery unavailable", err)
	}
	fields := map[string]string{}
	for line := range strings.SplitSeq(private, "\n") {
		if key, value, ok := strings.Cut(line, " = "); ok {
			fields[key] = value
		}
	}
	if fields["Address"] != device.Address || fields["Endpoint"] != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
		t.Fatal("delivered client identity differs from canonical device")
	}
	if err := svc.RestartCore(); err != nil {
		t.Fatal("managed runtime start failed", err)
	}
	peer := coreruntime.NewCore()
	t.Cleanup(func() { _ = peer.Stop() })
	peerConfig, _ := json.Marshal(map[string]any{"log": map[string]any{"disabled": true}, "endpoints": []map[string]any{{"type": "wireguard", "tag": "managed-client", "address": []string{fields["Address"]}, "private_key": fields["PrivateKey"], "peers": []map[string]any{{"address": "127.0.0.1", "port": port, "public_key": fields["PublicKey"], "pre_shared_key": fields["PresharedKey"], "allowed_ips": []string{"0.0.0.0/0"}}}}}})
	if err := peer.Start(peerConfig); err != nil {
		t.Fatal("delivered peer start failed", err)
	}
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		conn, err := echo.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		_, _ = io.Copy(conn, conn)
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	stream, err := peer.DialOutbound(ctx, "managed-client", "tcp", M.Socksaddr{Addr: netip.MustParseAddr("198.18.0.1"), Port: uint16(echo.Addr().(*net.TCPAddr).Port)})
	if err != nil {
		t.Fatal("managed device could not carry runtime traffic", err)
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(5 * time.Second))
	payload := []byte("managed-device-runtime-traffic")
	if _, err := stream.Write(payload); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(payload))
	if _, err := io.ReadFull(stream, response); err != nil || !bytes.Equal(response, payload) {
		t.Fatal("managed device runtime payload changed", err)
	}
	before, err := providerDesiredRevision(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileWireGuardDevices(t.Context()); err != nil {
		t.Fatal("managed readback/accounting failed", err)
	}
	if db.First(&owner, owner.Id).Error != nil || owner.Up <= 0 || owner.Down <= 0 {
		t.Fatal("device traffic did not reach canonical User counters")
	}
	_, state, err = clientdevices.Load(db, owner.Id)
	if err != nil || state.Devices[0].Up <= 0 || state.Devices[0].Down <= 0 || !state.Devices[0].Observation.Present || state.Devices[0].Observation.LastHandshake == 0 {
		t.Fatal("device accounting/handshake readback missing", err)
	}
	after, err := providerDesiredRevision(db)
	if err != nil || before != after {
		t.Fatal("runtime observation changed provider desired-state checkpoint", err)
	}
	if err := db.Model(&owner).Update("volume", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileWireGuardDevices(t.Context()); err != nil {
		t.Fatal("User quota reconciliation failed", err)
	}
	snapshot, err := core.WireGuardPeers(t.Context(), ep.Tag)
	if err != nil || len(snapshot.Peers) != 0 {
		t.Fatal("over-quota User retained a live peer", err)
	}
}

func TestNativeWireGuardAmneziaCandidateAndLifecycle(t *testing.T) {
	if !tun.WithGVisor {
		t.Skip("userspace WireGuard requires the gVisor build profile")
	}
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
	svc, applier, lifecycle := newHotReloadConfigService(t)
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	draft := map[string]any{"type": "wireguard", "tag": "native-awg", "private_key": ":private-invalid-canary", "address": []string{"10.23.0.1/32"}, "preallocated_buffers_per_pool": 512, "disable_pauses": false,
		"amnezia": map[string]any{"h1": 123, "h2": "200-300", "h3": 400, "h4": "500-600", "s1": 16, "s2": 16, "s3": 16, "s4": 16, "header_protection_key": map[string]string{"intent": "replace", "value": key}}}
	raw, _ := json.Marshal(draft)
	if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_ENDPOINT_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid Amnezia candidate was not privately rejected", err)
	}
	var count int64
	if err := db.Model(&model.Endpoint{}).Count(&count).Error; err != nil || count != 0 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 {
		t.Fatal("invalid candidate committed state or effects", err)
	}
	draft["private_key"] = key
	draft["preallocated_buffers_per_pool"] = 1
	raw, _ = json.Marshal(draft)
	if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_ENDPOINT_CANDIDATE_REJECTED") {
		t.Fatal("unsafe native receive budget was admitted", err)
	}
	if err := db.Model(&model.Endpoint{}).Count(&count).Error; err != nil || count != 0 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 {
		t.Fatal("unsafe receive budget committed state or effects", err)
	}
	draft["preallocated_buffers_per_pool"] = 512
	raw, _ = json.Marshal(draft)
	if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err != nil {
		t.Fatal("valid Amnezia candidate rejected", err)
	}
	view, err := entityendpoints.GetAll(db)
	masked, _ := json.Marshal(view)
	var fields []struct{ Amnezia map[string]json.RawMessage }
	if err != nil || json.Unmarshal(masked, &fields) != nil || len(fields) != 1 || bytes.Contains(fields[0].Amnezia["header_protection_key"], []byte(key)) {
		t.Fatal("header key disclosed in ordinary read", err)
	}
	config, err := svc.GetConfig("")
	if err != nil || singboxvalidation.ValidateConfig(*config) != nil {
		t.Fatal("native Amnezia projection invalid", err)
	}
	core := coreruntime.NewCore()
	t.Cleanup(func() { _ = core.Stop() })
	for i := 0; i < 2; i++ {
		if err := core.Start(*config); err != nil {
			t.Fatal("native Amnezia start failed", err)
		}
		if registry.WireGuardDeviceManagementAvailable() {
			snapshot, err := core.WireGuardPeers(t.Context(), "native-awg")
			if err != nil || snapshot.Generation == "" || len(snapshot.Peers) != 0 {
				t.Fatal("native peer readback unavailable", err)
			}
			peer := coreruntime.WireGuardPeer{PublicKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)), PresharedKey: key, Address: "10.23.0.2/32"}
			applied, err := core.ReconcileWireGuardPeers(t.Context(), snapshot.Generation, "native-awg", []coreruntime.WireGuardPeer{peer}, nil)
			if err != nil || len(applied.Peers) != 1 || applied.Peers[0].PublicKey != peer.PublicKey {
				t.Fatal("native peer mutation/readback failed", err)
			}
			public, _ := json.Marshal(applied)
			if bytes.Contains(public, []byte(key)) {
				t.Fatal("public peer readback exposed a private key")
			}
			removed, err := core.ReconcileWireGuardPeers(t.Context(), snapshot.Generation, "native-awg", nil, nil)
			if err != nil || len(removed.Peers) != 0 {
				t.Fatal("native peer revocation was not confirmed", err)
			}
			var candidate struct {
				Endpoints []json.RawMessage `json:"endpoints"`
			}
			if json.Unmarshal(*config, &candidate) != nil || len(candidate.Endpoints) != 1 {
				t.Fatal("missing native candidate")
			}
			if err := core.RemoveEndpoint("native-awg"); err != nil {
				t.Fatal(err)
			}
			if err := core.AddEndpoint(candidate.Endpoints[0]); err != nil {
				t.Fatal(err)
			}
			if _, err := core.ReconcileWireGuardPeers(t.Context(), snapshot.Generation, "native-awg", nil, nil); !errors.Is(err, coreruntime.ErrStaleGeneration) {
				t.Fatal("obsolete endpoint incarnation remained authoritative", err)
			}
		}
		if err := core.Stop(); err != nil {
			t.Fatal("native Amnezia stop failed", err)
		}
	}
}

func TestNativeOpenVPNClientCandidateUsesCanonicalOwnerAndDNSReferences(t *testing.T) {
	if !registry.Resolve("endpoints", "openvpn-client").Available() {
		t.Skip("OpenVPN client is absent from this profile")
	}
	if registry.Resolve("endpoints", "openvpn-server").Available() {
		t.Fatal("client integration advertised an unowned serving workflow")
	}
	for _, mode := range []string{"tls", "static_key"} {
		t.Run(mode, func(t *testing.T) {
			initSettingTestDB(t)
			seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
			db := dbsqlite.DB()
			svc, applier, lifecycle := newHotReloadConfigService(t)
			const password = "private-openvpn-candidate-canary"
			draft := map[string]any{"type": "openvpn-client", "tag": "native-openvpn", "mode": mode, "network": "udp", "servers": []map[string]any{{"server": "192.0.2.1", "server_port": 1194}}}
			if mode == "tls" {
				cert, key := makeSelfSignedCertPEM(t, time.Now().Add(time.Hour))
				draft["username"] = "fixture"
				draft["password"] = map[string]string{"intent": "replace", "value": password}
				draft["tls"] = map[string]any{"certificate": []string{string(cert)}, "client_certificate": []string{string(cert)}, "client_key": map[string]string{"intent": "replace", "value": string(key)}}
				draft["data_ciphers"] = []string{"AES-256-GCM"}
			} else {
				draft["address"] = []string{"10.8.0.2/24"}
				draft["peer_address"] = "10.8.0.1"
				draft["cipher"] = "AES-256-CBC"
				draft["static_key"] = map[string]string{"intent": "replace", "value": "-----BEGIN OpenVPN Static key V1-----\n" + strings.Repeat("01", 256) + "\n-----END OpenVPN Static key V1-----"}
			}
			draft["network"] = "private-invalid-network-canary"
			raw, _ := json.Marshal(draft)
			if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_ENDPOINT_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
				t.Fatal("invalid client candidate was not privately rejected", err)
			}
			var count int64
			if err := db.Model(&model.Endpoint{}).Count(&count).Error; err != nil || count != 0 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 {
				t.Fatal("failed client candidate committed state or runtime effects", err)
			}
			draft["network"] = "udp"
			raw, _ = json.Marshal(draft)
			if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err != nil {
				t.Fatal("valid native client rejected", err)
			}
			view, err := entityendpoints.GetAll(db)
			masked, _ := json.Marshal(view)
			if err != nil || bytes.Contains(masked, []byte(password)) || bytes.Contains(masked, []byte("sbox:v1:")) || bytes.Contains(masked, []byte("BEGIN OpenVPN Static key")) {
				t.Fatal("endpoint read disclosed credentials", err)
			}
			base := NewSingBoxBaseConfigStore(nil)
			valid := json.RawMessage(`{"log":{"disabled":true},"dns":{"servers":[{"type":"openvpn","tag":"vpn-dns","endpoint":"native-openvpn","accept_default_resolvers":true,"accept_search_domain":true}],"final":"vpn-dns"},"outbounds":[{"type":"direct","tag":"direct"}]}`)
			if err := base.Save(db, valid); err != nil {
				t.Fatal("native client DNS reference rejected", err)
			}
			config, err := svc.GetConfig("")
			if err != nil || singboxvalidation.ValidateConfig(*config) != nil {
				t.Fatal("native client/DNS runtime projection failed", err)
			}
			if mode == "tls" && !bytes.Contains(*config, []byte(password)) {
				t.Fatal("native password lost owner binding")
			}
			invalid := bytes.ReplaceAll(valid, []byte(`"endpoint":"native-openvpn"`), []byte(`"endpoint":"missing-vpn"`))
			if err := base.Save(db, invalid); err == nil {
				t.Fatal("missing client DNS endpoint committed")
			}
			if _, err := svc.Save("endpoints", "del", json.RawMessage(`"native-openvpn"`), "", "admin", "fixture.invalid"); err == nil {
				t.Fatal("referenced VPN endpoint was removed")
			}
		})
	}
}

func TestHysteria2RealmOutboundUsesCanonicalCredentialsAndPrivateAdmission(t *testing.T) {
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
	svc, applier, lifecycle := newHotReloadConfigService(t)
	const token = "private-realm-outbound-token-canary"
	realm := map[string]any{"server_url": ":private-invalid-url-canary", "realm_id": "native-realm", "token": map[string]string{"intent": "replace", "value": token}, "stun_servers": []string{"127.0.0.1:3478"}, "ip_version": 4}
	draft := map[string]any{"type": "hysteria2", "tag": "realm-client", "realm": realm, "password": "protocol-authentication", "tls": map[string]any{"enabled": true}}
	save := func(action string, value any) error {
		raw, _ := json.Marshal(value)
		_, err := svc.Save("outbounds", action, raw, "", "admin", "fixture.invalid")
		return err
	}
	if err := save("new", draft); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_OUTBOUND_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid Realm candidate was not privately rejected", err)
	}
	var count int64
	if err := db.Model(&model.Outbound{}).Where("tag = ?", "realm-client").Count(&count).Error; err != nil || count != 0 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 {
		t.Fatal("rejected Realm candidate committed effects", err)
	}
	realm["server_url"] = "https://127.0.0.1:8443"
	if err := save("new", draft); err != nil {
		t.Fatal(err)
	}
	var stored model.Outbound
	if err := db.Where("tag = ?", "realm-client").First(&stored).Error; err != nil || bytes.Contains(stored.Options, []byte(token)) {
		t.Fatal("Realm token was not sealed", err)
	}
	views, err := entityoutbounds.GetAll(db)
	masked, _ := json.Marshal(views)
	if err != nil || bytes.Contains(masked, []byte(token)) || bytes.Contains(masked, []byte("sbox:v1:")) {
		t.Fatal("Realm read disclosed a credential", err)
	}
	var rows []map[string]any
	_ = json.Unmarshal(masked, &rows)
	var editable map[string]any
	for _, row := range rows {
		if row["tag"] == "realm-client" {
			editable = row
		}
	}
	if editable == nil {
		t.Fatal("Realm resource disappeared")
	}
	nested := editable["realm"].(map[string]any)
	view := nested["token"].(map[string]any)
	if view["present"] != true {
		t.Fatal("Realm token presence missing")
	}
	nested["token"] = map[string]any{"intent": "unchanged", "reference": view["reference"], "present": view["present"], "rotationRevision": view["rotationRevision"]}
	nested["realm_id"] = "renamed-realm"
	if err := save("edit", editable); err != nil {
		t.Fatal("Realm Keep edit failed", err)
	}
	config, err := svc.GetConfig("")
	if err != nil || !bytes.Contains(*config, []byte(token)) || !bytes.Contains(*config, []byte("renamed-realm")) || singboxvalidation.ValidateConfig(*config) != nil {
		t.Fatal("Realm runtime projection lost owned semantics", err)
	}
	foreign := map[string]any{"type": "hysteria2", "tag": "realm-copy", "realm": nested, "tls": map[string]any{"enabled": true}}
	if err := save("new", foreign); err == nil {
		t.Fatal("another resource borrowed a Realm token")
	}
	if err := db.First(&stored, stored.Id).Error; err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), stored.Options...)
	nested["TOKEN"] = map[string]string{"intent": "replace", "value": "private-alias-canary"}
	if err := save("edit", editable); err == nil {
		t.Fatal("case alias bypassed Realm ownership")
	}
	if err := db.First(&stored, stored.Id).Error; err != nil || !bytes.Equal(before, stored.Options) {
		t.Fatal("rejected alias rewrote desired state", err)
	}
}

func TestNativeOpenConnectCandidateUsesCanonicalOwnerAndDNSReferences(t *testing.T) {
	if !registry.Resolve("endpoints", "openconnect").Available() {
		t.Skip("OpenConnect is absent from this profile")
	}
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
	svc, applier, lifecycle := newHotReloadConfigService(t)
	const secret = "private-openconnect-candidate-canary"
	draft := map[string]any{"type": "openconnect", "tag": "native-vpn", "server": "https://192.0.2.1", "flavor": "invalid-canary", "username": "fixture", "password": map[string]string{"intent": "replace", "value": secret}}
	raw, _ := json.Marshal(draft)
	if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_ENDPOINT_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid native endpoint was not privately rejected", err)
	}
	var count int64
	if err := db.Model(&model.Endpoint{}).Count(&count).Error; err != nil || count != 0 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 {
		t.Fatal("rejected endpoint committed identity or runtime effects", err)
	}
	draft["flavor"] = "anyconnect"
	raw, _ = json.Marshal(draft)
	if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err != nil {
		t.Fatal(err)
	}
	view, err := entityendpoints.GetAll(db)
	masked, _ := json.Marshal(view)
	if err != nil || bytes.Contains(masked, []byte(secret)) || bytes.Contains(masked, []byte("sbox:v1:")) {
		t.Fatal("endpoint API disclosed credentials", err)
	}
	base := NewSingBoxBaseConfigStore(nil)
	valid := json.RawMessage(`{"log":{"disabled":true},"dns":{"servers":[{"type":"openconnect","tag":"vpn-dns","endpoint":"native-vpn","accept_default_resolvers":true,"accept_search_domain":true}],"final":"vpn-dns"},"outbounds":[{"type":"direct","tag":"direct"}]}`)
	if err := base.Save(db, valid); err != nil {
		t.Fatal("canonical OpenConnect DNS candidate rejected", err)
	}
	config, err := svc.GetConfig("")
	if err != nil || !bytes.Contains(*config, []byte(secret)) || singboxvalidation.ValidateConfig(*config) != nil {
		t.Fatal("native endpoint/DNS runtime projection failed", err)
	}
	invalid := bytes.ReplaceAll(valid, []byte(`"endpoint":"native-vpn"`), []byte(`"endpoint":"missing-vpn"`))
	if err := base.Save(db, invalid); err == nil {
		t.Fatal("missing native DNS endpoint committed")
	}
	if _, err := svc.Save("endpoints", "del", json.RawMessage(`"native-vpn"`), "", "admin", "fixture.invalid"); err == nil {
		t.Fatal("referenced native endpoint was removed")
	}
}

func TestNativeOutboundCandidateOwnsCredentialsTransactionally(t *testing.T) {
	for _, kind := range []string{"mieru", "trusttunnel", "sudoku"} {
		t.Run(kind, func(t *testing.T) {
			initSettingTestDB(t)
			seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
			db := dbsqlite.DB()
			svc, applier, lifecycle := newHotReloadConfigService(t)
			svc.setLastUpdate(12345)
			state := func() string {
				t.Helper()
				var counts []int64
				for _, record := range []any{&model.Outbound{}, &model.AuditEvent{}, &model.Setting{}, &model.Changes{}} {
					var count int64
					if err := db.Model(record).Count(&count).Error; err != nil {
						t.Fatal(err)
					}
					counts = append(counts, count)
				}
				raw, _ := json.Marshal(counts)
				return string(raw)
			}
			const password = "private-native-outbound-password-canary"
			draft := map[string]any{"type": kind, "tag": "owned-" + kind, "server": "192.0.2.1", "server_port": 443, "username": "fixture-user", "password": map[string]string{"intent": "replace", "value": password}, "detour": "direct"}
			field := "password"
			if kind == "mieru" {
				draft["transport"] = "private-invalid-transport"
			} else if kind == "sudoku" {
				field = "key"
				delete(draft, "password")
				delete(draft, "username")
				draft[field] = map[string]string{"intent": "replace", "value": password}
				draft["multiplex"] = "private-invalid-multiplex"
			} else {
				draft["tls"] = map[string]any{"enabled": true}
				draft["username"] = ""
			}
			before := state()
			raw, _ := json.Marshal(draft)
			if _, err := svc.Save("outbounds", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_OUTBOUND_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
				t.Fatal("invalid native outbound was not privately rejected", err)
			}
			if state() != before || len(applier.calls) != 0 || len(lifecycle.calls) != 0 || svc.getLastUpdate() != 12345 {
				t.Fatal("failed native outbound committed identity, secret, audit, Change or effects")
			}
			if kind == "mieru" {
				draft["transport"] = "TCP"
				draft["multiplexing"] = "MULTIPLEXING_LOW"
				draft["handshake_mode"] = "HANDSHAKE_STANDARD"
			} else if kind == "sudoku" {
				draft["multiplex"] = "off"
				draft["http_mask"] = map[string]any{"enabled": false}
			} else {
				draft["username"] = "fixture-user"
				draft["multiplex"] = map[string]any{"enabled": false, "max_streams": 8}
			}
			raw, _ = json.Marshal(draft)
			if _, err := svc.Save("outbounds", "new", raw, "", "admin", "fixture.invalid"); err != nil {
				t.Fatal(err)
			}
			config, err := svc.GetConfig("")
			if err != nil || singboxvalidation.ValidateConfig(*config) != nil || !bytes.Contains(*config, []byte(password)) || bytes.Contains(*config, []byte("sbox:v1:")) {
				t.Fatal("trusted native outbound projection lost the password", err)
			}
			var row model.Outbound
			if err := db.Where("tag = ?", draft["tag"]).First(&row).Error; err != nil {
				t.Fatal(err)
			}
			view, err := entityoutbounds.GetAll(db)
			masked, _ := json.Marshal(view)
			if err != nil || bytes.Contains(masked, []byte(password)) || bytes.Contains(masked, []byte("sbox:v1:")) || bytes.Contains(row.Options, []byte(password)) {
				t.Fatal("native outbound storage or normal read disclosed its password")
			}
			var ref map[string]any
			var normal []map[string]any
			if json.Unmarshal(masked, &normal) != nil {
				t.Fatal("invalid normal Outbounds representation")
			}
			for _, entry := range normal {
				if entry["tag"] == draft["tag"] {
					ref, _ = entry[field].(map[string]any)
				}
			}
			if ref == nil || ref["present"] != true {
				t.Fatal("native password has no masked reference")
			}
			draft["id"], draft[field] = row.Id, map[string]any{"intent": "unchanged", "reference": ref["reference"], "present": ref["present"], "rotationRevision": ref["rotationRevision"]}
			raw, _ = json.Marshal(draft)
			if _, err := svc.Save("outbounds", "edit", raw, "", "admin", "fixture.invalid"); err != nil {
				t.Fatal("masked edit lost password ownership", err)
			}
			before = state()
			draft[field] = map[string]string{"intent": "clear"}
			raw, _ = json.Marshal(draft)
			if _, err := svc.Save("outbounds", "edit", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_OUTBOUND_CANDIDATE_REJECTED") || state() != before {
				t.Fatal("clearing the required native password committed a partial candidate", err)
			}
		})
	}
}
