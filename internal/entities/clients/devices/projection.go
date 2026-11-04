package devices

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/netip"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

func clients(tx *gorm.DB) ([]model.Client, error) {
	rows, err := tx.Model(&model.Client{}).Select("id", "config", "enable", "access_disable_reason", "expiry", "volume", "up", "down").
		Where("json_valid(config) AND json_type(config, '$.wireguard') IS NOT NULL").Order("id").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.Client{}
	total := 0
	for rows.Next() {
		var client model.Client
		if err := tx.ScanRows(rows, &client); err != nil {
			return nil, err
		}
		total += len(client.Config)
		if len(result) >= 4096 || total > 16<<20 {
			return nil, ErrLimit
		}
		client.Config = bytes.Clone(client.Config)
		result = append(result, client)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func allocate(tx *gorm.DB, policy Policy) (netip.Addr, error) {
	if !policy.ServerPrefix.IsValid() || !policy.ServerPrefix.Addr().Is4() || policy.ServerPrefix.Bits() > 30 {
		return netip.Addr{}, ErrPool
	}
	owners, err := clients(tx)
	if err != nil {
		return netip.Addr{}, err
	}
	used := map[netip.Addr]bool{policy.ServerPrefix.Addr(): true}
	count := 0
	for _, client := range owners {
		_, state, err := load(client.Config)
		if err != nil {
			return netip.Addr{}, ErrUnavailable
		}
		for _, device := range state.Devices {
			if device.EndpointID != policy.EndpointID {
				continue
			}
			if device.ReleasedAt > 0 {
				continue
			}
			// Revoked/deleted addresses stay reserved until runtime removal is
			// acknowledged. A delayed old peer cannot inherit a new identity.
			prefix, err := netip.ParsePrefix(device.Address)
			if err != nil {
				return netip.Addr{}, ErrUnavailable
			}
			used[prefix.Addr()] = true
			count++
			if count >= 4096 {
				return netip.Addr{}, ErrLimit
			}
		}
	}
	start, end := ipv4Range(policy.ServerPrefix)
	for candidate := start + 1; candidate < end; candidate++ {
		address := ipv4Address(candidate)
		if used[address] {
			continue
		}
		conflict := false
		for _, prefix := range policy.StaticPrefixes {
			if !prefix.Contains(address) {
				continue
			}
			_, last := ipv4Range(prefix)
			if last > candidate {
				candidate = last
			}
			conflict = true
			break
		}
		if !conflict {
			return address, nil
		}
	}
	return netip.Addr{}, ErrPool
}

func ipv4Range(prefix netip.Prefix) (uint64, uint64) {
	value := prefix.Masked().Addr().As4()
	first := uint64(binary.BigEndian.Uint32(value[:]))
	return first, first + (uint64(1) << uint(32-prefix.Bits())) - 1
}

func ipv4Address(value uint64) netip.Addr {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], uint32(value))
	return netip.AddrFrom4(raw)
}

// RuntimePeers is a projection of active User-owned device records. Private
// client keys are never borrowed for server runtime construction.
func RuntimePeers(ctx context.Context, tx *gorm.DB, policy Policy, now int64) ([]map[string]any, error) {
	if !policy.Available {
		return nil, nil
	}
	if !tx.Migrator().HasTable(&model.Client{}) {
		return nil, nil
	}
	owners, err := clients(tx)
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for _, client := range owners {
		_, state, err := load(client.Config)
		if err != nil {
			continue
		} // Unknown historical state stays inactive.
		for _, device := range state.Devices {
			if device.EndpointID != policy.EndpointID || !AvailableRecord(state, device, client, now) {
				continue
			}
			address, _ := netip.ParsePrefix(device.Address)
			first, last := ipv4Range(policy.ServerPrefix)
			point, _ := ipv4Range(address)
			if point <= first || point >= last || address.Addr() == policy.ServerPrefix.Addr() {
				return nil, ErrInvalid
			}
			for _, reserved := range policy.StaticPrefixes {
				if reserved.Contains(address.Addr()) {
					return nil, ErrInvalid
				}
			}
			var psk string
			if err := borrow(ctx, tx, client.Id, device, "preshared_key", "wireguard-device-runtime", func(value []byte) error {
				key, err := base64.StdEncoding.DecodeString(string(value))
				defer clear(key)
				if err != nil || len(key) != 32 {
					return ErrUnavailable
				}
				psk = string(value)
				return nil
			}); err != nil {
				return nil, err
			}
			result = append(result, map[string]any{"public_key": device.PublicKey, "pre_shared_key": psk, "allowed_ips": []string{device.Address}})
			if len(result) > 4096 {
				return nil, ErrLimit
			}
		}
	}
	return result, nil
}

// WithPrivateKeys admits one authorized delivery. Keys never enter an Info,
// ordinary User view, endpoint desired peer list or a second credential store.
func WithPrivateKeys(ctx context.Context, tx *gorm.DB, clientID uint, deviceID string, now int64, use func(Info, []byte, []byte) error) error {
	client, state, err := Load(tx, clientID)
	if err != nil {
		return err
	}
	for _, device := range state.Devices {
		if device.ID != deviceID {
			continue
		}
		if !AvailableRecord(state, device, client, now) {
			return ErrUnavailable
		}
		view := Info{ID: device.ID, EndpointID: device.EndpointID, Name: device.Name, Address: device.Address, PublicKey: device.PublicKey, CreatedAt: device.CreatedAt, ExpiresAt: device.ExpiresAt, Revision: device.Revision}
		return borrow(ctx, tx, clientID, device, "private_key", "wireguard-device-delivery", func(private []byte) error {
			return borrow(ctx, tx, clientID, device, "preshared_key", "wireguard-device-delivery", func(psk []byte) error { return use(view, private, psk) })
		})
	}
	return ErrUnavailable
}

func EndpointIDs(config []byte) []uint {
	_, state, err := load(config)
	if err != nil {
		return nil
	}
	seen := map[uint]bool{}
	result := []uint{}
	for _, device := range state.Devices {
		if !seen[device.EndpointID] {
			seen[device.EndpointID] = true
			result = append(result, device.EndpointID)
		}
	}
	return result
}
