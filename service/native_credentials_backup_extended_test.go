//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	settingsstore "github.com/MalenkiySolovey/solovey-ui/internal/settings/store"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

func TestNativeCredentialsPrivateArchiveRehearsalRestoreAndRollback(t *testing.T) {
	ctx := context.Background()
	t.Setenv("SUI_SECRETBOX_KEY", "")
	t.Setenv("SUI_SECRET", "")
	t.Setenv(backup.LogicalFileBackupEnv, backup.LogicalFileBackupRequired)
	registerFixtureSettingContributionsForTest(t)
	// TempDir cleanup runs in reverse registration order. Reserve both paths
	// before the database cleanup so Windows closes SQLite before removal.
	sourceDir, targetDir := t.TempDir(), t.TempDir()
	t.Cleanup(resumeTokenUseFlush)
	t.Cleanup(func() {
		if err := StopAuditWriter(ctx); err != nil {
			t.Error("drain native archive fixture audit", err)
		}
		if err := dbsqlite.Close(); err != nil {
			t.Error(err)
		}
	})
	openFresh := func(dir string) *gorm.DB {
		t.Helper()
		t.Setenv("SUI_DB_FOLDER", dir)
		if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
			t.Fatal(err)
		}
		if err := migration.EnsureCurrentSchemaJournal(dbsqlite.DB(), true); err != nil {
			t.Fatal(err)
		}
		// These represent independently bootstrapped installations. Reserve a
		// DB-owned master before the process-scoped settings defaults run.
		if _, err := settingsstore.EnsureMasterSecret(dbsqlite.DB()); err != nil {
			t.Fatal(err)
		}
		if _, err := (&SettingService{}).GetAllSetting(); err != nil {
			t.Fatal(err)
		}
		seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
		return dbsqlite.DB()
	}
	// Compare owner bytes without printing private rows on a failing assertion.
	ownerState := func(db *gorm.DB) []byte {
		t.Helper()
		var rows struct {
			Clients   []model.Client
			Inbounds  []model.Inbound
			Outbounds []model.Outbound
			TLS       []model.Tls
			Settings  []model.Setting
		}
		for _, table := range []any{&rows.Clients, &rows.Inbounds, &rows.Outbounds, &rows.TLS, &rows.Settings} {
			if err := db.Order("id").Find(table).Error; err != nil {
				t.Fatal(err)
			}
		}
		raw, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	master := func(db *gorm.DB) string {
		t.Helper()
		var row model.Setting
		if err := db.Where("key = ?", "secret").First(&row).Error; err != nil || row.Value == "" {
			t.Fatal("database master is unavailable")
		}
		return row.Value
	}
	db := openFresh(sourceDir)
	cert, key := makeSelfSignedCertPEM(t, time.Now().Add(24*time.Hour))
	server, _ := json.Marshal(map[string]any{"enabled": true, "certificate": []string{string(cert)}, "key": []string{string(key)}})
	profile := model.Tls{Name: "native-archive-tls", Server: server, Client: json.RawMessage(`{}`)}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	inbounds := []model.Inbound{
		{Type: "mieru", Tag: "archive-mieru", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23456,"listen_ports":["23458-23459"],"transport":"TCP","user_hint_is_mandatory":true,"mtu":1400}`)},
		{Type: "trusttunnel", Tag: "archive-trusttunnel", TlsId: profile.Id, Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23457,"network":["tcp","udp"],"congestion_controller":"cubic","cwnd":48,"disable_path_mtu_discovery":true}`)},
		{Type: "ssh", Tag: "archive-ssh", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23460,"server_version":"SSH-2.0-ArchiveFixture","max_auth_tries":4}`)},
		{Type: "mtproxy", Tag: "archive-mtproxy", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23461,"prefer_ip":"only-ipv4","concurrency":4,"auto_update":false,"doppelganger_each":"2h","throttle_check_interval":"45s"}`)},
		{Type: "sudoku", Tag: "archive-sudoku", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23462,"aead_method":"none","padding_min":0,"padding_max":0,"enable_pure_downlink":false,"http_mask_mode":"poll","path_root":"archive"}`)},
	}
	var sshOptions map[string]any
	_ = json.Unmarshal(inbounds[2].Options, &sshOptions)
	sshOptions["host_key"] = map[string]string{"intent": "replace", "value": string(key)}
	inbounds[2].Options, _ = json.Marshal(sshOptions)
	sudokuMaster, err := credentials.GenerateSudokuMaster()
	if err != nil {
		t.Fatal(err)
	}
	sudokuPublic, _ := credentials.SudokuPublicKey(sudokuMaster)
	outboundSudokuKey, err := credentials.SplitSudokuMaster(sudokuMaster)
	if err != nil {
		t.Fatal(err)
	}
	var sudokuOptions map[string]any
	_ = json.Unmarshal(inbounds[4].Options, &sudokuOptions)
	sudokuOptions["master_key"] = map[string]string{"intent": "replace", "value": sudokuMaster}
	inbounds[4].Options, _ = json.Marshal(sudokuOptions)
	const callCookie = "synthetic-archive-call-cookie"
	const callPassword = "synthetic-archive-call-password"
	const callJoinLink = "https://dion.vc/event/synthetic-archive-private-room"
	callOptions, _ := json.Marshal(map[string]any{"platform": "dion", "email": "operator@fixture.invalid",
		"cookies":   map[string]string{"intent": "replace", "value": `[{"name":"session","value":"` + callCookie + `"}]`},
		"password":  map[string]string{"intent": "replace", "value": callPassword},
		"join_link": map[string]string{"intent": "replace", "value": callJoinLink}})
	callInbound := model.Inbound{Type: "call", Tag: "archive-call", Options: callOptions}
	const outboundRoom = "https://dion.vc/event/synthetic-archive-outbound-room"
	callOutbound, _ := json.Marshal(map[string]any{"type": "call", "tag": "archive-call-outbound", "platform": "dion",
		"cookies":   map[string]string{"intent": "replace", "value": `[{"name":"session","value":"` + callCookie + `"}]`},
		"join_link": map[string]string{"intent": "replace", "value": outboundRoom}})
	nativeOutboundPasswords := map[string]string{"mieru": "synthetic-private-mieru-outbound-password", "trusttunnel": "synthetic-private-trusttunnel-outbound-password"}
	masqueSecrets := map[string]string{"auth_token": "synthetic-private-masque-token", "license_key": "synthetic-private-masque-license", "private_key": "synthetic-private-masque-key"}
	masqueProfile := map[string]any{"id": "archive-masque-device", "recreate": false}
	for field, value := range masqueSecrets {
		masqueProfile[field] = map[string]string{"intent": "replace", "value": value}
	}
	masqueOutbound, _ := json.Marshal(map[string]any{"type": "masque", "tag": "archive-masque-outbound", "use_l4_proxy": true, "profile": masqueProfile})
	if err := db.Transaction(func(tx *gorm.DB) error {
		for index := range inbounds {
			if err := entityinbounds.FillAndSave(tx, &inbounds[index], "fixture.invalid"); err != nil {
				return err
			}
		}
		for _, row := range []model.Inbound{
			{Type: "socks", Tag: "archive-bond-first", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23466}`)},
			{Type: "socks", Tag: "archive-bond-second", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23467}`)},
			{Type: "bond", Tag: "archive-bond", Options: json.RawMessage(`{"inbounds":["archive-bond-second","archive-bond-first"]}`)},
			{Type: "socks", Tag: "archive-failover-first", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23468}`)},
			{Type: "socks", Tag: "archive-failover-second", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23469}`)},
			{Type: "core-failover", Tag: "archive-failover", Options: json.RawMessage(`{"inbounds":["archive-failover-second","archive-failover-first"]}`)},
		} {
			if err := entityinbounds.FillAndSave(tx, &row, "fixture.invalid"); err != nil {
				return err
			}
		}
		if err := entityinbounds.FillAndSave(tx, &callInbound, "fixture.invalid"); err != nil {
			return err
		}
		if _, err := entityoutbounds.Save(tx, "new", callOutbound); err != nil {
			return err
		}
		if _, err := entityoutbounds.Save(tx, "new", masqueOutbound); err != nil {
			return err
		}
		for kind, password := range nativeOutboundPasswords {
			draft := map[string]any{"type": kind, "tag": "archive-" + kind + "-outbound", "server": "192.0.2.1", "server_port": 443, "username": "archive-user", "password": map[string]string{"intent": "replace", "value": password}}
			if kind == "mieru" {
				draft["transport"] = "TCP"
				draft["handshake_mode"] = "HANDSHAKE_STANDARD"
			} else {
				draft["tls"] = map[string]any{"enabled": true}
				draft["multiplex"] = map[string]any{"enabled": false, "max_streams": 8}
			}
			raw, _ := json.Marshal(draft)
			if _, err := entityoutbounds.Save(tx, "new", raw); err != nil {
				return err
			}
		}
		sudokuOutbound, _ := json.Marshal(map[string]any{"type": "sudoku", "tag": "archive-sudoku-outbound", "server": "192.0.2.1", "server_port": 443,
			"key": map[string]string{"intent": "replace", "value": outboundSudokuKey}, "multiplex": "off", "http_mask": map[string]any{"enabled": false}})
		if _, err := entityoutbounds.Save(tx, "new", sudokuOutbound); err != nil {
			return err
		}
		xhttpOutbound, _ := json.Marshal(map[string]any{"type": "vless", "tag": "archive-xhttp-outbound", "server": "192.0.2.1", "server_port": 443,
			"uuid": "f0aa8764-88a4-4d8e-8ca8-24df4d750382", "transport": map[string]any{"type": "xhttp", "mode": "packet-up", "x_padding_bytes": "100-1000",
				"download": map[string]any{"server": "192.0.2.2", "server_port": 443, "x_padding_bytes": "100-1000",
					"tls": map[string]any{"enabled": true, "client_certificate": []string{string(cert)}, "client_key": map[string]string{"intent": "replace", "value": string(key)}}}}})
		if _, err := entityoutbounds.Save(tx, "new", xhttpOutbound); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal("native Inbounds owner fixture failed", err)
	}
	mtSecret, err := credentials.Generate("mtproxy")
	if err != nil {
		t.Fatal(err)
	}
	passwords := map[string]string{"mieru": "synthetic-native-archive-mieru-password", "trusttunnel": "synthetic-native-archive-trust-password", "ssh": "synthetic-native-archive-ssh-password", "mtproxy": mtSecret}
	config := map[string]any{}
	for kind, password := range passwords {
		fact, _ := clientfacts.Resolve(kind)
		config[kind] = map[string]any{fact.SecretField: map[string]string{"intent": "replace", "value": password}}
	}
	clientConfig, _ := json.Marshal(config)
	var inboundIDs []uint
	for _, inbound := range inbounds {
		inboundIDs = append(inboundIDs, inbound.Id)
	}
	membership, _ := json.Marshal(inboundIDs)
	client := model.Client{Name: "archive-alice", Enable: true, Config: clientConfig, Inbounds: membership, Links: json.RawMessage(`[]`)}
	data, _ := json.Marshal(client)
	_, err = entityclients.Save(entityclients.SaveRequest{Tx: db, Action: "new", Data: data, Hostname: "fixture.invalid"})
	if err != nil {
		t.Fatal("native Users owner fixture failed", err)
	}
	if err := db.Where("name = ?", client.Name).First(&client).Error; err != nil {
		t.Fatal(err)
	}
	var stored map[string]json.RawMessage
	if json.Unmarshal(client.Config, &stored) != nil {
		t.Fatal("stored owner fields are invalid")
	}
	for kind := range passwords {
		fact, _ := clientfacts.Resolve(kind)
		var user map[string]string
		if json.Unmarshal(stored[kind], &user) != nil || !secretbox.IsEncrypted(user[fact.SecretField]) {
			t.Fatal("native credential was not sealed by the Users owner")
		}
	}
	var sudokuSplit string
	if err := credentials.UseClient(ctx, db, client.Id, "sudoku", stored["sudoku"], "archive-sudoku-canary", func(raw []byte) error {
		var delivery struct{ Keys map[string]string }
		if json.Unmarshal(raw, &delivery) != nil || !credentials.SudokuSplitMatches(delivery.Keys[strconv.Itoa(int(inbounds[4].Id))], sudokuMaster) {
			t.Fatal("archive fixture did not receive a matching split key")
		}
		sudokuSplit = delivery.Keys[strconv.Itoa(int(inbounds[4].Id))]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	checkProjection := func(candidate *gorm.DB) {
		t.Helper()
		projection, err := NewSingBoxConfigBuilder(nil).BuildCandidateProjectionFromDB(candidate, "", false)
		if err != nil || singboxvalidation.ValidateConfig(projection.Config) != nil {
			t.Fatal("native archived candidate failed the exact provider check", err)
		}
		var root struct {
			Outbounds []struct {
				Type     string
				Password string
				Key      string
				JoinLink string `json:"join_link"`
				Profile  struct {
					ID         string `json:"id"`
					AuthToken  string `json:"auth_token"`
					LicenseKey string `json:"license_key"`
					PrivateKey string `json:"private_key"`
				}
				Cookies   []struct{ Name, Value string }
				Transport json.RawMessage
			}
			Inbounds []struct {
				Type     string
				Users    []map[string]string
				HostKey  []string `json:"host_key"`
				Key      string
				JoinLink string `json:"join_link"`
				Password string
				Cookies  []struct{ Name, Value string }
				Inbounds []struct {
					Tag  string
					Port uint16 `json:"listen_port"`
				}
			}
		}
		if json.Unmarshal(projection.Config, &root) != nil || len(root.Inbounds) != len(inbounds)+3 {
			t.Fatal("native runtime projection lost an inbound")
		}
		if bytes.Contains(projection.Config, []byte(sudokuMaster)) || bytes.Contains(projection.Config, []byte(sudokuSplit)) || bytes.Contains(projection.Config, []byte("master_key")) {
			t.Fatal("Sudoku runtime projection disclosed delivery material")
		}
		callOutboundFound := false
		masqueOutboundFound, sudokuOutboundFound := false, false
		xhttpOutboundFound := false
		nativeOutboundsFound := 0
		for _, outbound := range root.Outbounds {
			if outbound.Type == "vless" {
				var transport struct {
					Download struct {
						TLS struct {
							ClientKey string `json:"client_key"`
						}
					}
				}
				xhttpOutboundFound = json.Unmarshal(outbound.Transport, &transport) == nil && transport.Download.TLS.ClientKey == string(key)

			}
			if outbound.Type == "masque" {
				masqueOutboundFound = true
				if outbound.Profile.ID != "archive-masque-device" || outbound.Profile.AuthToken != masqueSecrets["auth_token"] || outbound.Profile.LicenseKey != masqueSecrets["license_key"] || outbound.Profile.PrivateKey != masqueSecrets["private_key"] {
					t.Fatal("archive changed nested MASQUE profile ownership")
				}
			}
			if outbound.Type == "sudoku" {
				sudokuOutboundFound = true
				if outbound.Key != outboundSudokuKey {
					t.Fatal("archive changed private Sudoku outbound key ownership")
				}
			}
			if password, native := nativeOutboundPasswords[outbound.Type]; native {
				nativeOutboundsFound++
				if outbound.Password != password {
					t.Fatal("archive changed native outbound password ownership")
				}
			}
			if outbound.Type == "call" {
				callOutboundFound = true
				if outbound.JoinLink != outboundRoom || len(outbound.Cookies) != 1 || outbound.Cookies[0].Value != callCookie {
					t.Fatal("archive changed private Call outbound room/cookie ownership")
				}
			}
		}
		if !callOutboundFound || !masqueOutboundFound || !sudokuOutboundFound || !xhttpOutboundFound || nativeOutboundsFound != len(nativeOutboundPasswords) {
			t.Fatal("archive lost a native outbound")
		}
		for _, inbound := range root.Inbounds {
			if inbound.Type == "call" {
				if len(inbound.Users) != 0 || inbound.JoinLink != callJoinLink || inbound.Password != callPassword || len(inbound.Cookies) != 1 || inbound.Cookies[0].Value != callCookie {
					t.Fatal("archive changed private Call room/account ownership")
				}
				continue
			}
			if inbound.Type == "bond" || inbound.Type == "failover" {
				port := uint16(23466)
				if inbound.Type == "failover" {
					port = 23468
				}
				prefix := "archive-" + inbound.Type
				if len(inbound.Users) != 0 || len(inbound.Inbounds) != 2 || inbound.Inbounds[0].Tag != prefix+"-second" || inbound.Inbounds[0].Port != port+1 || inbound.Inbounds[1].Tag != prefix+"-first" || inbound.Inbounds[1].Port != port {
					t.Fatal("private archive changed canonical group membership, ordering or member options")
				}
				continue
			}
			if inbound.Type == "sudoku" {
				if len(inbound.Users) != 0 || inbound.Key != sudokuPublic {
					t.Fatal("archive invented Sudoku Users or changed the serving identity")
				}
				continue
			}
			fact, _ := clientfacts.Resolve(inbound.Type)
			if len(inbound.Users) != 1 || inbound.Users[0]["name"] != client.Name || inbound.Users[0][fact.SecretField] != passwords[inbound.Type] {
				t.Fatal("native runtime projection lost its owner-bound credential")
			}
			if inbound.Type == "ssh" && (len(inbound.HostKey) != 1 || inbound.HostKey[0] != string(key)) {
				t.Fatal("archived SSH host key lost its owner binding")
			}
		}
		var candidateClient model.Client
		if err := candidate.First(&candidateClient, client.Id).Error; err != nil {
			t.Fatal(err)
		}
		var candidateFields map[string]json.RawMessage
		_ = json.Unmarshal(candidateClient.Config, &candidateFields)
		if err := credentials.UseClient(candidate.Statement.Context, candidate, client.Id, "sudoku", candidateFields["sudoku"], "archive-sudoku-proof", func(raw []byte) error {
			var delivery struct{ Keys map[string]string }
			if json.Unmarshal(raw, &delivery) != nil || delivery.Keys[strconv.Itoa(int(inbounds[4].Id))] != sudokuSplit {
				t.Fatal("restored delivery key changed")
			}
			return nil
		}); err != nil {
			t.Fatal("restored Sudoku delivery credential lost its owner binding", err)
		}
		normal, err := entityinbounds.GetByIDs(candidate, strconv.Itoa(int(inbounds[2].Id))+","+strconv.Itoa(int(inbounds[4].Id))+","+strconv.Itoa(int(callInbound.Id)))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(normal)
		if bytes.Contains(encoded, []byte("PRIVATE KEY")) || bytes.Contains(encoded, []byte("sbox:v1:")) || bytes.Contains(encoded, []byte(sudokuMaster)) || bytes.Contains(encoded, []byte(callCookie)) || bytes.Contains(encoded, []byte(callPassword)) || bytes.Contains(encoded, []byte(callJoinLink)) {
			t.Fatal("restored normal SSH read disclosed host-key material")
		}
		outboundView, err := entityoutbounds.GetAll(candidate)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ = json.Marshal(outboundView)
		if bytes.Contains(encoded, []byte("PRIVATE KEY")) {
			t.Fatal("restored normal Outbounds read disclosed a download TLS private key")
		}
		for _, secret := range masqueSecrets {
			if bytes.Contains(encoded, []byte(secret)) {
				t.Fatal("restored normal read disclosed a MASQUE profile credential")
			}
		}
		if bytes.Contains(encoded, []byte(outboundSudokuKey)) {
			t.Fatal("restored normal Outbounds read disclosed a private Sudoku key")
		}
		if bytes.Contains(encoded, []byte("sbox:v1:")) || bytes.Contains(encoded, []byte(callCookie)) || bytes.Contains(encoded, []byte(outboundRoom)) {
			t.Fatal("restored normal Outbounds read disclosed private Call material")
		}
		for _, password := range nativeOutboundPasswords {
			if bytes.Contains(encoded, []byte(password)) {
				t.Fatal("restored normal Outbounds read disclosed a native password")
			}
		}
	}
	checkProjection(db)
	masked, err := entityclients.GetByID(db, strconv.Itoa(int(client.Id)))
	if err != nil {
		t.Fatal(err)
	}
	maskedConfig := append([]byte(nil), (*masked)[0].Config...)
	sourceState, sourceMaster := ownerState(db), master(db)
	var usesBefore, usesAfter int64
	if err := db.Model(&model.AuditEvent{}).Where("event = ?", "owner_secret_use").Count(&usesBefore).Error; err != nil {
		t.Fatal(err)
	}
	archive, err := backup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	for _, password := range passwords {
		if bytes.Contains(archive, []byte(password)) {
			t.Fatal("private archive persisted a transient native password")
		}
	}
	if bytes.Contains(archive, key) || bytes.Contains(archive, []byte(sudokuMaster)) || bytes.Contains(archive, []byte(sudokuSplit)) || bytes.Contains(archive, []byte(outboundSudokuKey)) {
		t.Fatal("private archive persisted a transient inbound or delivery key")
	}
	for _, value := range []string{callCookie, callPassword, callJoinLink, outboundRoom} {
		if bytes.Contains(archive, []byte(value)) {
			t.Fatal("private archive persisted transient Call credentials")
		}
	}
	for _, password := range nativeOutboundPasswords {
		if bytes.Contains(archive, []byte(password)) {
			t.Fatal("private archive persisted a transient outbound password")
		}
	}
	var encrypted, decrypted bytes.Buffer
	passphrase := []byte(rand.Text())
	if _, _, err := backupenvelope.SealStream(&encrypted, bytes.NewReader(archive), passphrase); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backupenvelope.OpenStream(&decrypted, bytes.NewReader(encrypted.Bytes()), passphrase, backup.MaxRestoreBytes); err != nil || !bytes.Equal(archive, decrypted.Bytes()) {
		t.Fatal("private envelope changed the native archive", err)
	}
	rehearsal, err := backup.Rehearse(ctx, bytes.NewReader(decrypted.Bytes()))
	if err != nil || !rehearsal.Possible {
		t.Fatalf("native archive rehearsal rejected: error=%v reasons=%v", err, rehearsal.ReasonCodes)
	}
	if err := db.Model(&model.AuditEvent{}).Where("event = ?", "owner_secret_use").Count(&usesAfter).Error; err != nil || usesAfter != usesBefore || !bytes.Equal(sourceState, ownerState(db)) {
		t.Fatal("rehearsal changed live owners or wrote a source secret-use audit")
	}
	sourcePath := configstorage.GetDBPath()
	if err := StopAuditWriter(ctx); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	sourceDigest := sha256.Sum256(sourceBytes)
	target := openFresh(targetDir)
	if master(target) == sourceMaster {
		t.Fatal("fresh restore target did not receive an independent database master")
	}
	for _, kind := range []string{"mieru", "trusttunnel", "ssh", "mtproxy", "sudoku"} {
		used := false
		err := credentials.UseClient(ctx, target, client.Id, kind, stored[kind], "wrong-database", func([]byte) error { used = true; return nil })
		if err == nil || used {
			t.Fatal("native credential crossed into a database with a different master")
		}
	}
	targetState := ownerState(target)
	backup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { backup.SetSendSighupHook(nil) })
	result, err := backup.RestoreContextDetailed(ctx, bytes.NewReader(decrypted.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	pending := true
	t.Cleanup(func() {
		if pending {
			if err := backup.AbortPendingRestore(); err != nil {
				t.Error(err)
			}
		}
	})
	candidate := dbsqlite.DB().WithContext(result.DatabaseContext(ctx))
	if master(candidate) != sourceMaster {
		t.Fatal("restore did not carry the credential owner's database master")
	}
	var restored model.Client
	if err := candidate.First(&restored, client.Id).Error; err != nil || !bytes.Equal(restored.Config, client.Config) || !bytes.Equal(restored.Inbounds, membership) {
		t.Fatal("restore changed native ciphertext, identity or assignment")
	}
	for _, inbound := range inbounds {
		var restored model.Inbound
		if err := candidate.First(&restored, inbound.Id).Error; err != nil || !bytes.Equal(restored.Options, inbound.Options) || restored.TlsId != inbound.TlsId {
			t.Fatal("restore lost native semantic fields or TLS ownership")
		}
	}
	checkProjection(candidate)
	masked, err = entityclients.GetByID(candidate, strconv.Itoa(int(client.Id)))
	if err != nil || len(*masked) != 1 || !bytes.Equal((*masked)[0].Config, maskedConfig) {
		t.Fatal("restore changed the masked credential reference or revision")
	}
	if err := backup.AbortPendingRestore(); err != nil {
		t.Fatal(err)
	}
	pending = false
	if !bytes.Equal(targetState, ownerState(dbsqlite.DB())) {
		t.Fatal("rejected native restore did not recover the exact target owners")
	}
	afterSource, err := os.ReadFile(sourcePath)
	if err != nil || sha256.Sum256(afterSource) != sourceDigest {
		t.Fatal("restore changed the closed source database")
	}
}
