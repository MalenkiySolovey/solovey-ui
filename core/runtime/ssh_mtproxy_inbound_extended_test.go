//go:build shtorm_extended && with_quic

package runtime

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"maps"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/dolonet/mtg-multi/essentials"
	"github.com/dolonet/mtg-multi/mtglib"
	"github.com/dolonet/mtg-multi/mtglib/obfuscation"
	"golang.org/x/crypto/ssh"
)

// These peers exercise SSH forwarding and authenticated MTProto transport.
// A loopback DC terminates obfuscated2; this is not a public Telegram session.
// Routing explicitly redirects authenticated MTProxy DC dials and rejects its
// unauthenticated fronting/scout traffic, so no external network is required.
type nativeServingFixture struct {
	f         *quicFixture
	sshSigner ssh.Signer
	secrets   map[uint]mtglib.Secret
	passwords map[uint]string
}

func newSSHMTProxyFixture(t *testing.T, kind string) *nativeServingFixture {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	s := &nativeServingFixture{f: &quicFixture{kind: kind, core: NewCore(), port: port}, secrets: map[uint]mtglib.Secret{}, passwords: map[uint]string{7: quicFixturePassword, 9: quicFixturePassword}}
	f := s.f
	f.inbound = map[string]any{"type": kind, "tag": "quic-fixture", "listen": "127.0.0.1", "listen_port": port}
	if kind == "ssh" {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		s.sshSigner, err = ssh.NewSignerFromKey(key)
		if err != nil {
			t.Fatal(err)
		}
		block, err := ssh.MarshalPrivateKey(key, "runtime-only-serving-fixture")
		if err != nil {
			t.Fatal(err)
		}
		f.inbound["host_key"] = []string{string(pem.EncodeToMemory(block))}
		f.inbound["server_version"], f.inbound["max_auth_tries"] = "SSH-2.0-R7Fixture", 3
		f.echo = nativeTCPWitness(t)
	} else {
		for _, user := range []uint{7, 9} {
			s.secrets[user] = mtglib.GenerateSecret("mtproxy-fixture.invalid")
		}
		f.inbound["prefer_ip"], f.inbound["auto_update"], f.inbound["concurrency"] = "only-ipv4", false, 4
		f.inbound["tolerate_time_skewness"], f.inbound["handshake_timeout"] = "1m", "3s"
		f.inbound["doppelganger_each"], f.inbound["throttle_check_interval"] = "2h", "1m"
		f.inbound["doppelganger_per_raid"] = 1
		f.inbound["doppelganger_urls"] = []string{"https://mtproxy-fixture.invalid/"}
		f.echo = nativeTCPWitnessWithHandler(t, func(conn net.Conn) {
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			dc, wire, err := (obfuscation.Obfuscator{}).ReadHandshake(essentials.WrapNetConn(conn))
			if err != nil || dc != 2 {
				return
			}
			_ = conn.SetDeadline(time.Time{})
			_, _ = io.Copy(wire, wire)
		})
	}
	s.users(7, 9)
	if err := f.core.StartWithBindings(s.raw(), quicBindings(7, 9)); err != nil {
		t.Fatal("native serving startup failed", err)
	}
	t.Cleanup(func() {
		if err := f.core.Stop(); err != nil {
			t.Error("native serving stop failed", err)
		}
	})
	return s
}

func (s *nativeServingFixture) raw() []byte {
	f := s.f
	raw := f.raw()
	if f.kind == "mtproxy" {
		var root map[string]any
		_ = json.Unmarshal(raw, &root)
		root["route"] = map[string]any{"rules": []any{
			map[string]any{"inbound": []string{"quic-fixture"}, "auth_user": []string{"verified-7", "verified-9"}, "action": "route", "outbound": "direct", "override_address": "127.0.0.1", "override_port": f.echo.Addr().(*net.TCPAddr).Port},
			map[string]any{"inbound": []string{"quic-fixture"}, "action": "reject"},
		}}
		raw, _ = json.Marshal(root)
	}
	return raw
}

func (s *nativeServingFixture) users(users ...uint) {
	entries := []any{}
	for _, user := range users {
		entry := map[string]any{"name": fmt.Sprintf("verified-%d", user)}
		if s.f.kind == "mtproxy" {
			entry["secret"] = s.secrets[user].Hex()
		} else {
			entry["password"] = s.passwords[user]
		}
		entries = append(entries, entry)
	}
	s.f.inbound["users"] = entries
}

func (s *nativeServingFixture) replace(t *testing.T, users ...uint) {
	t.Helper()
	s.users(users...)
	raw, _ := json.Marshal(s.f.inbound)
	if err := s.f.core.RemoveInbound("quic-fixture"); err != nil {
		t.Fatal("native owner removal failed", err)
	}
	s.f.core.CloseInboundConnections("quic-fixture")
	if err := s.f.core.AddInboundWithBindings(raw, quicBindings(users...)); err != nil {
		t.Fatal("native owner replacement failed", err)
	}
}

func (s *nativeServingFixture) peer(t *testing.T, user uint, wrong bool) (net.Conn, error) {
	t.Helper()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.f.port))
	if s.f.kind == "mtproxy" {
		secret := s.secrets[user]
		if wrong {
			secret.Key[0] ^= 0xff
		}
		return mtProxyPeer(t, address, secret)
	}
	raw, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		return nil, err
	}
	_ = raw.SetDeadline(time.Now().Add(3 * time.Second))
	password := s.passwords[user]
	if wrong {
		password = "synthetic-wrong-password"
	}
	conn, channels, requests, err := ssh.NewClientConn(raw, address, &ssh.ClientConfig{
		User: fmt.Sprintf("verified-%d", user), Auth: []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if !bytes.Equal(key.Marshal(), s.sshSigner.PublicKey().Marshal()) {
				return fmt.Errorf("unexpected fixture host key")
			}
			return nil
		},
	})
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	_ = raw.SetDeadline(time.Time{})
	client := ssh.NewClient(conn, channels, requests)
	t.Cleanup(func() { _ = client.Close() })
	if string(client.ServerVersion()) != "SSH-2.0-R7Fixture" {
		t.Fatal("saved SSH identification was not effective")
	}
	stream, err := client.Dial("tcp", s.f.echo.Addr().String())
	if err == nil {
		t.Cleanup(func() { _ = stream.Close() })
	}
	return stream, err
}

func (s *nativeServingFixture) echo(conn net.Conn) error {
	if s.f.kind == "ssh" {
		return nativeEcho(conn)
	}
	// A padded-intermediate MTProto plaintext req_pq_multi frame. The witness
	// echoes transport bytes; it does not pretend to implement Telegram RPC.
	packet := make([]byte, 4+20+20+4)
	binary.LittleEndian.PutUint32(packet[:4], uint32(len(packet)-4))
	binary.LittleEndian.PutUint64(packet[12:20], uint64(time.Now().Unix())<<32)
	binary.LittleEndian.PutUint32(packet[20:24], 20)
	binary.LittleEndian.PutUint32(packet[24:28], 0xbe7e8ef1)
	if _, err := rand.Read(packet[28:]); err != nil {
		return err
	}
	want := bytes.Clone(packet)
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(packet); err != nil {
		return err
	}
	response := make([]byte, len(want))
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if !bytes.Equal(response, want) {
		return fmt.Errorf("MTProto transport response mismatch")
	}
	return nil
}

func (s *nativeServingFixture) stream(t *testing.T, user uint) net.Conn {
	t.Helper()
	conn, err := s.peer(t, user, false)
	if err != nil {
		t.Fatal("native authenticated peer failed", err)
	}
	if err := s.echo(conn); err != nil {
		t.Fatal("native authenticated transport failed", err)
	}
	return conn
}

func TestSSHMTProxyAuthenticatedDataPlaneIdentityAndIsolation(t *testing.T) {
	for _, kind := range []string{"ssh", "mtproxy"} {
		t.Run(kind, func(t *testing.T) {
			s := newSSHMTProxyFixture(t, kind)
			first, other := s.stream(t, 7), s.stream(t, 9)
			if conn, err := s.peer(t, 7, true); err == nil && s.echo(conn) == nil {
				t.Fatal("incorrect native credential forwarded traffic")
			}
			snapshot := s.f.parents(t, 7)
			if len(snapshot.Connections) != 1 || snapshot.ParentTotal != 0 {
				t.Fatal("native serving flow missing or parent control overclaimed")
			}
			flow := snapshot.Connections[0]
			if flow.ClientID != 7 || flow.InboundType != kind || flow.Identity != "verified" || flow.Upload == 0 || flow.Download == 0 {
				t.Fatal("native authentication did not feed the existing identity and counters")
			}
			result, err := s.f.core.Disconnect(t.Context(), DisconnectRequest{Generation: snapshot.Generation, ClientID: 7, FlowID: flow.ID})
			if err != nil || result.Closed != 1 || result.ParentClosed {
				t.Fatal("native flow disconnect failed", err)
			}
			if s.echo(first) == nil {
				t.Fatal("disconnected native flow kept forwarding")
			}
			if err := s.echo(other); err != nil {
				t.Fatal("native disconnect affected another user", err)
			}
			s.replace(t, 9)
			if conn, err := s.peer(t, 7, false); err == nil && s.echo(conn) == nil {
				t.Fatal("revoked native credential forwarded through a fresh session")
			}
			_ = s.stream(t, 9)
			if s.f.parents(t, 7).Total != 0 {
				t.Fatal("revoked native user retained an admitted flow")
			}
		})
	}
}

func TestSSHMTProxyFinalUserRevocationAndReenable(t *testing.T) {
	for _, kind := range []string{"ssh", "mtproxy"} {
		t.Run(kind, func(t *testing.T) {
			s := newSSHMTProxyFixture(t, kind)
			streams := []net.Conn{s.stream(t, 7), s.stream(t, 9)}
			s.replace(t)
			for _, stream := range streams {
				if s.echo(stream) == nil {
					t.Fatal("zero-user replacement retained a native data-plane flow")
				}
			}
			occupied, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.f.port)))
			if err != nil {
				t.Fatal("zero-user native listener retained its port", err)
			}
			_ = occupied.Close()
			if conn, err := s.peer(t, 7, false); err == nil && s.echo(conn) == nil {
				t.Fatal("zero-user native inbound admitted traffic")
			}
			for _, user := range []uint{7, 9} {
				if s.f.parents(t, user).Total != 0 {
					t.Fatal("zero-user native replacement retained identity state")
				}
			}
			s.replace(t, 7, 9)
			_ = s.stream(t, 7)
		})
	}
}

func TestSSHMTProxyCredentialRotationAndRuntimeRestart(t *testing.T) {
	for _, kind := range []string{"ssh", "mtproxy"} {
		t.Run(kind, func(t *testing.T) {
			s := newSSHMTProxyFixture(t, kind)
			previous := *s
			previous.passwords, previous.secrets = maps.Clone(s.passwords), maps.Clone(s.secrets)
			oldFlow := s.stream(t, 7)
			if kind == "ssh" {
				s.passwords[7] = "synthetic-rotated-loopback-only"
			} else {
				s.secrets[7] = mtglib.GenerateSecret("mtproxy-fixture.invalid")
			}
			s.replace(t, 7, 9)
			if s.echo(oldFlow) == nil {
				t.Fatal("credential replacement retained an old authenticated flow")
			}
			assertPreviousRejected := func() {
				t.Helper()
				if conn, err := previous.peer(t, 7, false); err == nil && s.echo(conn) == nil {
					t.Fatal("previous credential admitted a fresh authenticated flow")
				}
			}
			assertPreviousRejected()
			rotated, other := s.stream(t, 7), s.stream(t, 9)
			before := s.f.parents(t, 7)
			if len(before.Connections) != 1 || before.Connections[0].Identity != "verified" || before.Connections[0].ClientID != 7 {
				t.Fatal("rotated credential lost canonical identity")
			}
			if err := s.f.core.Stop(); err != nil {
				t.Fatal("native runtime stop failed", err)
			}
			for _, flow := range []net.Conn{rotated, other} {
				if s.echo(flow) == nil {
					t.Fatal("runtime stop retained a native authenticated flow")
				}
			}
			listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.f.port)))
			if err != nil {
				t.Fatal("runtime stop retained its listening port", err)
			}
			_ = listener.Close()
			if err := s.f.core.StartWithBindings(s.raw(), quicBindings(7, 9)); err != nil {
				t.Fatal("native runtime restart with retained config failed", err)
			}
			assertPreviousRejected()
			_ = s.stream(t, 7)
			_ = s.stream(t, 9)
			after := s.f.parents(t, 7)
			if after.Generation == before.Generation || len(after.Connections) != 1 || after.Connections[0].ClientID != 7 || after.Connections[0].Identity != "verified" {
				t.Fatal("restarted native runtime lost generation or canonical identity")
			}
		})
	}
}

// Only the test peer needs a fake-TLS record wrapper. MTG's public obfuscator
// supplies its exact obfuscated2 consumer; no implementation is vendored here.
type mtProxyRecordConn struct {
	essentials.Conn
	pending bytes.Buffer
}

func (c *mtProxyRecordConn) Read(buffer []byte) (int, error) {
	for c.pending.Len() == 0 {
		kind, _, payload, err := mtProxyReadRecord(c.Conn)
		if err != nil {
			return 0, err
		}
		if kind != 0x17 {
			return 0, fmt.Errorf("unexpected fake-TLS record")
		}
		c.pending.Write(payload)
	}
	return c.pending.Read(buffer)
}

func (c *mtProxyRecordConn) Write(buffer []byte) (int, error) {
	if len(buffer) > 16379 {
		return 0, fmt.Errorf("fixture record too large")
	}
	packet := make([]byte, 5+len(buffer))
	copy(packet, []byte{0x17, 3, 3})
	binary.BigEndian.PutUint16(packet[3:5], uint16(len(buffer)))
	copy(packet[5:], buffer)
	_, err := io.Copy(c.Conn, bytes.NewReader(packet))
	if err != nil {
		return 0, err
	}
	return len(buffer), nil
}

func mtProxyReadRecord(conn net.Conn) (byte, []byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, nil, err
	}
	length := int(binary.BigEndian.Uint16(header[3:5]))
	if header[1] != 3 || header[2] != 3 || length > 16379 {
		return 0, nil, nil, fmt.Errorf("invalid fixture TLS record")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return 0, nil, nil, err
	}
	return header[0], header, payload, nil
}

func mtProxyPeer(t *testing.T, address string, secret mtglib.Secret) (net.Conn, error) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		return nil, err
	}
	admitted := false
	defer func() {
		if !admitted {
			_ = conn.Close()
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	hello := mtProxyClientHello(secret)
	if _, err := io.Copy(conn, bytes.NewReader(hello)); err != nil {
		return nil, err
	}
	var server []byte
	for _, expected := range []byte{0x16, 0x14, 0x17} {
		kind, header, payload, err := mtProxyReadRecord(conn)
		if err != nil {
			return nil, err
		}
		if kind != expected {
			return nil, fmt.Errorf("fake-TLS server handshake record mismatch")
		}
		server = append(server, header...)
		server = append(server, payload...)
	}
	if len(server) < 43 {
		return nil, fmt.Errorf("truncated fake-TLS server handshake")
	}
	actual := bytes.Clone(server[11:43])
	clear(server[11:43])
	mac := hmac.New(sha256.New, secret.Key[:])
	mac.Write(hello[11:43])
	mac.Write(server)
	if !hmac.Equal(actual, mac.Sum(nil)) {
		return nil, fmt.Errorf("fake-TLS server authentication failed")
	}
	stream, err := (obfuscation.Obfuscator{Secret: secret.Key[:]}).SendHandshake(&mtProxyRecordConn{Conn: essentials.WrapNetConn(conn)}, 2)
	if err != nil {
		return nil, err
	}
	admitted = true
	t.Cleanup(func() { _ = conn.Close() })
	return stream, nil
}

func mtProxyClientHello(secret mtglib.Secret) []byte {
	body := bytes.NewBuffer([]byte{3, 3})
	body.Write(make([]byte, 32))
	session := make([]byte, 32)
	_, _ = rand.Read(session)
	body.WriteByte(byte(len(session)))
	body.Write(session)
	body.Write([]byte{0, 2, 0x13, 1, 1, 0})
	sni := &bytes.Buffer{}
	_ = binary.Write(sni, binary.BigEndian, uint16(len(secret.Host)+3))
	sni.WriteByte(0)
	_ = binary.Write(sni, binary.BigEndian, uint16(len(secret.Host)))
	sni.WriteString(secret.Host)
	_ = binary.Write(body, binary.BigEndian, uint16(sni.Len()+4))
	body.Write([]byte{0, 0})
	_ = binary.Write(body, binary.BigEndian, uint16(sni.Len()))
	body.Write(sni.Bytes())
	packet := []byte{0x16, 3, 1, 0, 0, 1, 0, 0, 0}
	binary.BigEndian.PutUint16(packet[3:5], uint16(body.Len()+4))
	packet[7], packet[8] = byte(body.Len()>>8), byte(body.Len())
	packet = append(packet, body.Bytes()...)
	mac := hmac.New(sha256.New, secret.Key[:])
	mac.Write(packet)
	random := mac.Sum(nil)
	timestamp := make([]byte, 4)
	binary.LittleEndian.PutUint32(timestamp, uint32(time.Now().Unix()))
	for index := range timestamp {
		random[28+index] ^= timestamp[index]
	}
	copy(packet[11:43], random)
	return packet
}
