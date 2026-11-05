package endpoints

import (
	"encoding/binary"
	"encoding/json"
	"net/netip"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	clientdevices "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients/devices"
	"gorm.io/gorm"
)

func ManagedDevicePolicy(tx *gorm.DB, id uint) (clientdevices.Policy, error) {
	var endpoint model.Endpoint
	if tx == nil || id == 0 || tx.First(&endpoint, id).Error != nil {
		return clientdevices.Policy{}, clientdevices.ErrUnavailable
	}
	return managedDevicePolicy(endpoint)
}

func managedDevicePolicy(endpoint model.Endpoint) (clientdevices.Policy, error) {
	policy := clientdevices.Policy{EndpointID: endpoint.Id}
	if endpoint.Type != "wireguard" {
		return policy, clientdevices.ErrUnavailable
	}
	var metadata struct {
		Managed            bool `json:"managed"`
		DefaultDeviceLimit int  `json:"defaultDeviceLimit"`
	}
	var options struct {
		Address    []string `json:"address"`
		ListenPort uint16   `json:"listen_port"`
		Peers      []struct {
			AllowedIPs []string `json:"allowed_ips"`
		} `json:"peers"`
	}
	if json.Unmarshal(endpoint.Ext, &metadata) != nil || !metadata.Managed || metadata.DefaultDeviceLimit < 1 || metadata.DefaultDeviceLimit > 100 ||
		json.Unmarshal(endpoint.Options, &options) != nil || options.ListenPort == 0 || len(options.Address) > 16 || len(options.Peers) > 128 {
		return policy, clientdevices.ErrUnavailable
	}
	if _, _, err := peerDeliveryPolicy(endpoint.Ext); err != nil {
		return policy, err
	}
	for _, value := range options.Address {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return policy, clientdevices.ErrInvalid
		}
		if !prefix.Addr().Is4() {
			continue
		}
		if policy.ServerPrefix.IsValid() || prefix.Bits() > 30 || prefix.Addr() == prefix.Masked().Addr() {
			return policy, clientdevices.ErrInvalid
		}
		network, host := prefix.Masked().Addr().As4(), prefix.Addr().As4()
		last := uint64(binary.BigEndian.Uint32(network[:])) + (uint64(1) << uint(32-prefix.Bits())) - 1
		if uint64(binary.BigEndian.Uint32(host[:])) == last {
			return policy, clientdevices.ErrInvalid
		}
		policy.ServerPrefix = prefix
	}
	if !policy.ServerPrefix.IsValid() {
		return policy, clientdevices.ErrInvalid
	}
	for _, peer := range options.Peers {
		for _, value := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return policy, clientdevices.ErrInvalid
			}
			if prefix.Addr().Is4() {
				policy.StaticPrefixes = append(policy.StaticPrefixes, prefix)
			}
		}
	}
	policy.DeviceLimit = metadata.DefaultDeviceLimit
	policy.Available = registry.WireGuardDeviceManagementAvailable() && entitycapabilities.ResolveResource("endpoints", endpoint.Type, endpoint.Options).Available
	return policy, nil
}

func managedDevicesEnabled(endpoint model.Endpoint) bool {
	var metadata struct {
		Managed bool `json:"managed"`
	}
	return endpoint.Type == "wireguard" && json.Unmarshal(endpoint.Ext, &metadata) == nil && metadata.Managed
}

func appendManagedDevicePeers(tx *gorm.DB, endpoint model.Endpoint, raw []byte) ([]byte, error) {
	if !managedDevicesEnabled(endpoint) {
		return raw, nil
	}
	defer clear(raw)
	policy, err := managedDevicePolicy(endpoint)
	if err != nil {
		return nil, err
	}
	peers, err := clientdevices.RuntimePeers(tx.Statement.Context, tx, policy, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	var options map[string]json.RawMessage
	if json.Unmarshal(raw, &options) != nil {
		return nil, clientdevices.ErrInvalid
	}
	var merged []map[string]any
	if len(options["peers"]) > 0 && json.Unmarshal(options["peers"], &merged) != nil {
		return nil, clientdevices.ErrInvalid
	}
	merged = append(merged, peers...)
	keys, addresses := map[string]bool{}, map[string]bool{}
	for _, peer := range merged {
		key, ok := peer["public_key"].(string)
		if !ok || key == "" || keys[key] {
			return nil, clientdevices.ErrInvalid
		}
		keys[key] = true
	}
	for _, peer := range peers {
		for _, address := range peer["allowed_ips"].([]string) {
			if addresses[address] {
				return nil, clientdevices.ErrInvalid
			}
			addresses[address] = true
		}
	}
	options["peers"], err = json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return json.Marshal(options)
}

// DevicePeerConfig composes the same endpoint delivery policy and native
// serializer used by manual peers. The User owner grants one scoped key borrow.
func DevicePeerConfig(tx *gorm.DB, clientID uint, deviceID, host string) (string, error) {
	if tx == nil || !peerConfigHost(host) {
		return "", ErrPeerConfigUnavailable
	}
	var result string
	err := tx.Transaction(func(db *gorm.DB) error {
		return clientdevices.WithPrivateKeys(db.Statement.Context, db, clientID, deviceID, time.Now().Unix(), func(device clientdevices.Info, private, psk []byte) error {
			var endpoint model.Endpoint
			if db.First(&endpoint, device.EndpointID).Error != nil || endpoint.Type != "wireguard" {
				return ErrPeerConfigUnavailable
			}
			raw, err := marshalEndpointCredentials(db, endpoint, "wireguard-user-device-delivery")
			if err != nil {
				return err
			}
			defer clear(raw)
			var options, metadata map[string]json.RawMessage
			if json.Unmarshal(raw, &options) != nil || json.Unmarshal(endpoint.Ext, &metadata) != nil {
				return ErrPeerConfigUnavailable
			}
			options["peers"], _ = json.Marshal([]map[string]any{{"public_key": device.PublicKey, "pre_shared_key": string(psk), "allowed_ips": []string{device.Address}}})
			metadata["keys"], _ = json.Marshal([]map[string]any{{"public_key": device.PublicKey, "private_key": string(private)}})
			candidate, _ := json.Marshal(options)
			extension, _ := json.Marshal(metadata)
			defer clear(candidate)
			defer clear(extension)
			configs, err := renderPeerConfigs(candidate, extension, host)
			if err != nil || len(configs) != 1 {
				return ErrPeerConfigUnavailable
			}
			result = configs[0].Config
			return nil
		})
	})
	if err != nil {
		return "", ErrPeerConfigUnavailable
	}
	return result, nil
}
