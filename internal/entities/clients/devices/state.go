// Package devices owns WireGuard device grants and credentials inside the
// existing User config. Endpoint and runtime owners consume projections; neither
// stores a second desired device inventory or introduces a provider-specific user.
package devices

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"gorm.io/gorm"
)

const Field = "wireguard"

var (
	ErrInvalid     = errors.New("CLIENT_DEVICE_INVALID")
	ErrUnavailable = errors.New("CLIENT_DEVICE_UNAVAILABLE")
	ErrLimit       = errors.New("CLIENT_DEVICE_LIMIT_REACHED")
	ErrConflict    = errors.New("CLIENT_DEVICE_REQUEST_CONFLICT")
	ErrPool        = errors.New("CLIENT_DEVICE_ADDRESS_POOL_EXHAUSTED")
)

type Grant struct {
	EndpointID  uint `json:"endpointId"`
	DeviceLimit int  `json:"deviceLimit,omitempty"`
}

type Record struct {
	ID            string      `json:"id"`
	EndpointID    uint        `json:"endpointId"`
	Name          string      `json:"name"`
	Address       string      `json:"address"`
	PublicKey     string      `json:"publicKey"`
	PrivateKey    string      `json:"privateKey"`
	PresharedKey  string      `json:"presharedKey"`
	CreateRequest string      `json:"createRequest"`
	RotateRequest string      `json:"rotateRequest,omitempty"`
	CreatedAt     int64       `json:"createdAt"`
	ExpiresAt     int64       `json:"expiresAt,omitempty"`
	RevokedAt     int64       `json:"revokedAt,omitempty"`
	DeletedAt     int64       `json:"deletedAt,omitempty"`
	Revision      uint64      `json:"revision"`
	ReleasedAt    int64       `json:"releasedAt,omitempty"`
	Up            int64       `json:"up,omitempty"`
	Down          int64       `json:"down,omitempty"`
	Observation   Observation `json:"observation"`
}

type Observation struct {
	Generation    string `json:"generation"`
	PublicKey     string `json:"publicKey"`
	Received      int64  `json:"received"`
	Sent          int64  `json:"sent"`
	LastHandshake int64  `json:"lastHandshake"`
	ObservedAt    int64  `json:"observedAt"`
	Present       bool   `json:"present"`
}

type State struct {
	Version int      `json:"version"`
	Grants  []Grant  `json:"grants"`
	Devices []Record `json:"devices"`
}

type Info struct {
	ID           string      `json:"id"`
	EndpointID   uint        `json:"endpointId"`
	Name         string      `json:"name"`
	Address      string      `json:"address"`
	PublicKey    string      `json:"publicKey"`
	CreatedAt    int64       `json:"createdAt"`
	ExpiresAt    int64       `json:"expiresAt"`
	RevokedAt    int64       `json:"revokedAt"`
	DeletedAt    int64       `json:"deletedAt"`
	Revision     uint64      `json:"revision"`
	Status       string      `json:"status"`
	CanExport    bool        `json:"canExport"`
	RuntimeState string      `json:"runtimeState"`
	Up           int64       `json:"up"`
	Down         int64       `json:"down"`
	Observation  Observation `json:"observation"`
}

type View struct {
	Available bool    `json:"available"`
	Reason    string  `json:"reason,omitempty"`
	Grants    []Grant `json:"grants"`
	Devices   []Info  `json:"devices"`
}

type Policy struct {
	EndpointID     uint
	Available      bool
	ServerPrefix   netip.Prefix
	StaticPrefixes []netip.Prefix
	DeviceLimit    int
}
type ResolvePolicy func(*gorm.DB, uint) (Policy, error)

type Request struct {
	ClientID    uint   `json:"clientId"`
	EndpointID  uint   `json:"endpointId"`
	DeviceID    string `json:"deviceId"`
	Operation   string `json:"operation"`
	RequestKey  string `json:"requestKey"`
	Name        string `json:"name"`
	ExpiresAt   int64  `json:"expiresAt"`
	DeviceLimit int    `json:"deviceLimit"`
}

func strict(raw []byte, target any) error {
	if len(raw) > 1<<20 || jsonvalue.StrictObject(raw) != nil {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return ErrInvalid
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}

func load(config []byte) (map[string]json.RawMessage, State, error) {
	root := map[string]json.RawMessage{}
	if len(config) > 0 && (jsonvalue.StrictObject(config) != nil || json.Unmarshal(config, &root) != nil || root == nil) {
		return nil, State{}, ErrInvalid
	}
	state := State{Version: 1, Grants: []Grant{}, Devices: []Record{}}
	if len(root[Field]) == 0 {
		return root, state, nil
	}
	if strict(root[Field], &state) != nil || state.Version != 1 || len(state.Grants) > 128 || len(state.Devices) > 512 {
		return root, State{}, ErrUnavailable
	}
	seen, grants := map[string]bool{}, map[uint]bool{}
	for _, grant := range state.Grants {
		if grant.EndpointID == 0 || grants[grant.EndpointID] || grant.DeviceLimit < 0 || grant.DeviceLimit > 100 {
			return root, State{}, ErrUnavailable
		}
		grants[grant.EndpointID] = true
	}
	for _, device := range state.Devices {
		if !validID(device.ID) || seen[device.ID] || device.EndpointID == 0 || device.Revision == 0 {
			return root, State{}, ErrUnavailable
		}
		seen[device.ID] = true
		address, err := netip.ParsePrefix(device.Address)
		key, keyErr := base64.StdEncoding.DecodeString(device.PublicKey)
		if err != nil || !address.Addr().Is4() || address.Bits() != 32 || keyErr != nil || len(key) != 32 {
			return root, State{}, ErrUnavailable
		}
	}
	return root, state, nil
}

func validID(value string) bool {
	decoded, err := hex.DecodeString(value)
	return len(value) == 32 && err == nil && len(decoded) == 16 && strings.ToLower(value) == value
}

func Active(client model.Client, now int64) bool {
	return client.Enable && client.AccessDisableReason == "" && (client.Expiry <= 0 || client.Expiry >= now) && client.Up >= 0 && client.Down >= 0 &&
		(client.Volume <= 0 || client.Up <= client.Volume && client.Down <= client.Volume && client.Up <= client.Volume-client.Down)
}

func granted(state State, endpointID uint) (Grant, bool) {
	for _, grant := range state.Grants {
		if grant.EndpointID == endpointID {
			return grant, true
		}
	}
	return Grant{}, false
}

func AvailableRecord(state State, record Record, client model.Client, now int64) bool {
	_, authorized := granted(state, record.EndpointID)
	return authorized && Active(client, now) && record.RevokedAt == 0 && record.DeletedAt == 0 && (record.ExpiresAt == 0 || record.ExpiresAt > now)
}

// Ordinary User edits carry an opaque owner view. Only the device mutation
// boundary may change this field; omission preserves its exact stored bytes.
func Preserve(proposed, stored map[string]json.RawMessage, clientID uint) error {
	captured := stored[Field]
	if incoming := proposed[Field]; len(incoming) > 0 {
		view, _ := json.Marshal(presence(clientID, captured))
		if !bytes.Equal(incoming, captured) && !equalJSON(incoming, view) {
			return errors.New("CLIENT_DEVICE_OWNER_REQUIRED")
		}
	}
	if len(captured) > 0 {
		proposed[Field] = captured
	} else {
		delete(proposed, Field)
	}
	return nil
}

func presence(clientID uint, raw []byte) map[string]any {
	// Counter/readback freshness cannot make an ordinary User editor stale.
	// Device commands and credentials still participate in the revision.
	var state State
	if strict(raw, &state) == nil && state.Version == 1 {
		for i := range state.Devices {
			state.Devices[i].Observation = Observation{}
			state.Devices[i].Up = 0
			state.Devices[i].Down = 0
			state.Devices[i].ReleasedAt = 0
		}
		raw, _ = json.Marshal(state)
	}
	digest := sha256.Sum256(raw)
	return map[string]any{"owner": "client-devices", "clientId": clientID, "present": len(raw) > 0, "revision": hex.EncodeToString(digest[:])}
}

// DesiredConfig excludes runtime observations from maintenance checkpoints.
// Unknown versions remain byte-preserved and cannot become editable authority.
func DesiredConfig(config []byte) []byte {
	root, state, err := load(config)
	if err != nil || len(root[Field]) == 0 {
		return config
	}
	for i := range state.Devices {
		state.Devices[i].Observation = Observation{}
		state.Devices[i].Up = 0
		state.Devices[i].Down = 0
		state.Devices[i].ReleasedAt = 0
	}
	root[Field], _ = json.Marshal(state)
	result, err := json.Marshal(root)
	if err != nil {
		return config
	}
	return result
}

func equalJSON(left, right []byte) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// Project hides the complete credential bundle, including unknown future
// device formats, without changing the canonical User or its backup payload.
func Project(clientID uint, config []byte) ([]byte, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(config, &root) != nil {
		return nil, ErrInvalid
	}
	if len(root[Field]) == 0 {
		return config, nil
	}
	root[Field], _ = json.Marshal(presence(clientID, root[Field]))
	return json.Marshal(root)
}

func Inspect(client model.Client, timeNow int64) View {
	_, state, err := load(client.Config)
	view := View{Available: err == nil, Grants: []Grant{}, Devices: []Info{}}
	if err != nil {
		view.Reason = ErrUnavailable.Error()
		return view
	}
	view.Grants = state.Grants
	for _, device := range state.Devices {
		status := "eligible"
		switch {
		case device.DeletedAt != 0:
			status = "deleted"
		case device.RevokedAt != 0:
			status = "revoked"
		case device.ExpiresAt > 0 && device.ExpiresAt <= timeNow:
			status = "expired"
		case !AvailableRecord(state, device, client, timeNow):
			status = "inactive"
		}
		view.Devices = append(view.Devices, Info{ID: device.ID, EndpointID: device.EndpointID, Name: device.Name, Address: device.Address, PublicKey: device.PublicKey,
			CreatedAt: device.CreatedAt, ExpiresAt: device.ExpiresAt, RevokedAt: device.RevokedAt, DeletedAt: device.DeletedAt, Revision: device.Revision, Status: status, CanExport: AvailableRecord(state, device, client, timeNow), Up: device.Up, Down: device.Down, Observation: device.Observation})
	}
	return view
}

func requestHash(value string) (string, error) {
	if len(value) < 16 || len(value) > 128 {
		return "", ErrInvalid
	}
	for _, c := range value {
		if c < 33 || c > 126 {
			return "", ErrInvalid
		}
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:]), nil
}

func validateName(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 64 {
		return false
	}
	for _, c := range value {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

func reference(clientID uint, deviceID, field string) (secretref.Reference, error) {
	if !validID(deviceID) || field != "private_key" && field != "preshared_key" {
		return secretref.Reference{}, ErrInvalid
	}
	// The child identity is resolved from this User's canonical record. It is
	// an AAD namespace, never an interpolated SQL/JSON lookup path.
	return ownerdb.Reference("users", clientID, "wireguard-device:"+deviceID+":"+field)
}

func generateKeys(tx *gorm.DB, clientID uint, record *Record) error {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return ErrUnavailable
	}
	key := private.Bytes()
	defer clear(key)
	psk := make([]byte, 32)
	defer clear(psk)
	if _, err := rand.Read(psk); err != nil {
		return ErrUnavailable
	}
	for _, item := range []struct {
		field  string
		plain  []byte
		target *string
	}{{"private_key", key, &record.PrivateKey}, {"preshared_key", psk, &record.PresharedKey}} {
		ref, err := reference(clientID, record.ID, item.field)
		if err != nil {
			return err
		}
		mutation, _ := json.Marshal(map[string]string{"intent": "replace", "value": base64.StdEncoding.EncodeToString(item.plain)})
		sealed, err := ownerdb.Apply(tx, ref, *item.target, mutation)
		clear(mutation)
		if err != nil {
			return ErrUnavailable
		}
		*item.target = sealed
	}
	record.PublicKey = base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
	return nil
}

func borrow(ctx context.Context, tx *gorm.DB, clientID uint, record Record, field, intent string, use func([]byte) error) error {
	ref, err := reference(clientID, record.ID, field)
	if err != nil {
		return err
	}
	captured := record.PrivateKey
	if field == "preshared_key" {
		captured = record.PresharedKey
	}
	_, err = ownerdb.Access(ctx, tx, secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "users"}, Operation: secretref.UseForOwner, IntentID: intent}, captured, use)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func save(tx *gorm.DB, client model.Client, root map[string]json.RawMessage, state State) error {
	var err error
	root[Field], err = json.Marshal(state)
	if err != nil || len(root[Field]) > 1<<20 {
		return ErrLimit
	}
	config, err := json.Marshal(root)
	if err != nil {
		return ErrInvalid
	}
	updated := tx.Model(&model.Client{}).Where("id = ? AND config = ?", client.Id, client.Config).Update("config", json.RawMessage(config))
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func Load(tx *gorm.DB, clientID uint) (model.Client, State, error) {
	var client model.Client
	if tx == nil || clientID == 0 || tx.First(&client, clientID).Error != nil {
		return client, State{}, ErrUnavailable
	}
	_, state, err := load(client.Config)
	return client, state, err
}

func DecodeRequest(raw []byte) (Request, error) {
	var request Request
	if strict(raw, &request) != nil || request.ClientID == 0 {
		return request, ErrInvalid
	}
	return request, nil
}

func Mutate(tx *gorm.DB, request Request, resolve ResolvePolicy, now int64) ([]uint, error) {
	client, state, err := Load(tx, request.ClientID)
	if err != nil {
		return nil, err
	}
	root, _, _ := load(client.Config)
	if request.EndpointID == 0 {
		return nil, ErrInvalid
	}
	switch request.Operation {
	case "grant":
		policy, err := resolve(tx, request.EndpointID)
		if err != nil || !policy.Available {
			return nil, ErrUnavailable
		}
		if request.DeviceLimit < 0 || request.DeviceLimit > 100 {
			return nil, ErrInvalid
		}
		found := false
		for i := range state.Grants {
			if state.Grants[i].EndpointID == request.EndpointID {
				state.Grants[i].DeviceLimit = request.DeviceLimit
				found = true
			}
		}
		if !found {
			if len(state.Grants) >= 128 {
				return nil, ErrLimit
			}
			state.Grants = append(state.Grants, Grant{EndpointID: request.EndpointID, DeviceLimit: request.DeviceLimit})
		}
	case "remove-grant":
		for i, grant := range state.Grants {
			if grant.EndpointID == request.EndpointID {
				state.Grants = append(state.Grants[:i], state.Grants[i+1:]...)
				break
			}
		}
	case "create":
		policy, err := resolve(tx, request.EndpointID)
		if err != nil || !policy.Available || !Active(client, now) {
			return nil, ErrUnavailable
		}
		grant, allowed := granted(state, request.EndpointID)
		if !allowed {
			return nil, ErrUnavailable
		}
		hash, err := requestHash(request.RequestKey)
		if err != nil || !validateName(request.Name) || request.ExpiresAt != 0 && (request.ExpiresAt <= now || request.ExpiresAt > now+10*365*24*3600) {
			return nil, ErrInvalid
		}
		count := 0
		for _, device := range state.Devices {
			if device.EndpointID != request.EndpointID {
				continue
			}
			if device.CreateRequest == hash {
				if device.Name != request.Name || device.ExpiresAt != request.ExpiresAt || device.DeletedAt != 0 {
					return nil, ErrConflict
				}
				return nil, nil
			}
			if device.DeletedAt == 0 {
				count++
			}
		}
		limit := grant.DeviceLimit
		if limit == 0 {
			limit = policy.DeviceLimit
		}
		if limit < 1 || limit > 100 || count >= limit || len(state.Devices) >= 512 {
			return nil, ErrLimit
		}
		address, err := allocate(tx, policy)
		if err != nil {
			return nil, err
		}
		identity := make([]byte, 16)
		if _, err := rand.Read(identity); err != nil {
			return nil, ErrUnavailable
		}
		record := Record{ID: hex.EncodeToString(identity), EndpointID: request.EndpointID, Name: request.Name, Address: address.String() + "/32", CreateRequest: hash, CreatedAt: now, ExpiresAt: request.ExpiresAt, Revision: 1}
		if err := generateKeys(tx, client.Id, &record); err != nil {
			return nil, err
		}
		state.Devices = append(state.Devices, record)
	case "revoke", "delete", "rotate":
		index := -1
		for i, device := range state.Devices {
			if device.ID == request.DeviceID && device.EndpointID == request.EndpointID {
				index = i
				break
			}
		}
		if index < 0 {
			return nil, ErrUnavailable
		}
		device := &state.Devices[index]
		if request.Operation == "rotate" {
			hash, err := requestHash(request.RequestKey)
			if err != nil {
				return nil, err
			}
			if hash == device.RotateRequest {
				return nil, nil
			}
			policy, err := resolve(tx, request.EndpointID)
			if err != nil || !policy.Available || !AvailableRecord(state, *device, client, now) {
				return nil, ErrUnavailable
			}
			if err := generateKeys(tx, client.Id, device); err != nil {
				return nil, err
			}
			device.RotateRequest = hash
		} else {
			if device.RevokedAt != 0 && (request.Operation != "delete" || device.DeletedAt != 0) {
				return nil, nil
			}
			if device.RevokedAt == 0 {
				device.RevokedAt = now
			}
			if request.Operation == "delete" && device.DeletedAt == 0 {
				device.DeletedAt = now
			}
		}
		device.Revision++
	default:
		return nil, ErrInvalid
	}
	if err := save(tx, client, root, state); err != nil {
		return nil, err
	}
	return []uint{request.EndpointID}, nil
}
