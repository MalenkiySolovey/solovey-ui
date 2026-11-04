package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	clientdevices "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients/devices"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	entitytypes "github.com/MalenkiySolovey/solovey-ui/internal/entities/types"
	"gorm.io/gorm"
)

type ClientDeviceEndpoint struct {
	ID        uint   `json:"id"`
	Tag       string `json:"tag"`
	Available bool   `json:"available"`
}
type ClientDeviceView struct {
	clientdevices.View
	CanCreate bool                   `json:"canCreate"`
	Endpoints []ClientDeviceEndpoint `json:"endpoints"`
}

// Reads and private delivery share the active DB-generation admission used by
// other User operations. No device endpoint owns another database or runtime.
func (s *ClientService) WireGuardDevices(ctx context.Context, id uint) (ClientDeviceView, error) {
	view := ClientDeviceView{Endpoints: []ClientDeviceEndpoint{}}
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return view, clientdevices.ErrUnavailable
	}
	defer release()
	activeDB := dbsqlite.DB()
	if activeDB == nil || id == 0 {
		return view, clientdevices.ErrUnavailable
	}
	db := activeDB.WithContext(ctx)
	var client model.Client
	if db.First(&client, id).Error != nil {
		return view, clientdevices.ErrUnavailable
	}
	now := time.Now().Unix()
	view.View = clientdevices.Inspect(client, now)
	view.CanCreate = view.Available && clientdevices.Active(client, now)
	var endpoints []model.Endpoint
	if db.Where("type = ?", "wireguard").Order("id").Find(&endpoints).Error != nil {
		return view, clientdevices.ErrUnavailable
	}
	available := map[uint]bool{}
	snapshots := map[uint]coreruntime.WireGuardPeerSnapshot{}
	for _, endpoint := range endpoints {
		policy, err := entityendpoints.ManagedDevicePolicy(db, endpoint.Id)
		available[endpoint.Id] = err == nil && policy.Available
		view.Endpoints = append(view.Endpoints, ClientDeviceEndpoint{ID: endpoint.Id, Tag: endpoint.Tag, Available: available[endpoint.Id]})
		if available[endpoint.Id] {
			if snapshot, err := runtimeOrDefault(s.Runtime).Core().WireGuardPeers(ctx, endpoint.Tag); err == nil {
				snapshots[endpoint.Id] = snapshot
			}
		}
	}
	for i := range view.Devices {
		device := &view.Devices[i]
		device.RuntimeState = "unobserved"
		if !available[device.EndpointID] {
			if device.Status == "eligible" {
				device.Status = "unavailable"
			}
			device.RuntimeState = "unavailable"
			continue
		}
		if snapshot, ok := snapshots[device.EndpointID]; ok {
			device.RuntimeState = "absent"
			for _, peer := range snapshot.Peers {
				if peer.PublicKey != device.PublicKey {
					continue
				}
				device.Observation = deviceObservation(snapshot, peer)
				device.RuntimeState = "pending"
				if device.Status == "eligible" && len(peer.Addresses) == 1 && peer.Addresses[0] == device.Address {
					device.RuntimeState = "applied"
				}
				break
			}
		}
	}
	if view.CanCreate {
		view.CanCreate = false
		for _, grant := range view.Grants {
			if available[grant.EndpointID] {
				view.CanCreate = true
				break
			}
		}
	}
	return view, nil
}

func deviceObservation(snapshot coreruntime.WireGuardPeerSnapshot, peer coreruntime.WireGuardPeerObservation) clientdevices.Observation {
	return clientdevices.Observation{Generation: snapshot.Generation, PublicKey: peer.PublicKey, Received: peer.Received, Sent: peer.Sent, LastHandshake: peer.LastHandshake, ObservedAt: snapshot.ObservedAt, Present: true}
}

// The existing traffic job converges User grants, quotas and expiry through
// the Core lifecycle owner. No raw IPC or private device key crosses this seam.
func (s *ConfigService) ReconcileWireGuardDevices(ctx context.Context) error {
	if !registry.WireGuardDeviceManagementAvailable() {
		return nil
	}
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return err
	}
	defer release()
	return s.runtime().restart().RunBlockingContext(ctx, func() error {
		if held, err := s.maintenanceHeldLockedContext(ctx); err != nil || held {
			return err
		}
		core := s.coreInstance()
		if core == nil || !core.IsRunning() {
			return nil
		}
		db := configDatabase()
		if db == nil {
			return clientdevices.ErrUnavailable
		}
		var endpoints []model.Endpoint
		if err := db.WithContext(ctx).Where("type = ? AND json_valid(ext) AND json_extract(ext, '$.managed') = 1", "wireguard").Limit(129).Find(&endpoints).Error; err != nil {
			return err
		}
		if len(endpoints) > 128 {
			return clientdevices.ErrLimit
		}
		for _, endpoint := range endpoints {
			if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := dbsqlite.ReserveWrite(tx); err != nil {
					return err
				}
				if err := tx.First(&endpoint, endpoint.Id).Error; err != nil {
					if err == gorm.ErrRecordNotFound {
						return nil
					}
					return err
				}
				var metadata struct {
					Managed bool `json:"managed"`
				}
				if endpoint.Type != "wireguard" || json.Unmarshal(endpoint.Ext, &metadata) != nil || !metadata.Managed {
					return nil
				}
				policy, err := entityendpoints.ManagedDevicePolicy(tx, endpoint.Id)
				if err != nil {
					return err
				}
				if !policy.Available {
					return nil
				}
				snapshot, err := core.WireGuardPeers(ctx, endpoint.Tag)
				if err != nil {
					return err
				}
				if err := accountDeviceSnapshot(tx, endpoint.Id, snapshot); err != nil {
					return err
				}
				peers, err := clientdevices.RuntimePeers(ctx, tx, policy, time.Now().Unix())
				if err != nil {
					return err
				}
				managed := make([]coreruntime.WireGuardPeer, 0, len(peers))
				for _, peer := range peers {
					addresses, _ := peer["allowed_ips"].([]string)
					public, _ := peer["public_key"].(string)
					psk, _ := peer["pre_shared_key"].(string)
					if len(addresses) != 1 || public == "" || psk == "" {
						return clientdevices.ErrInvalid
					}
					managed = append(managed, coreruntime.WireGuardPeer{PublicKey: public, PresharedKey: psk, Address: addresses[0]})
				}
				var options struct {
					Peers []struct {
						PublicKey string `json:"public_key"`
					} `json:"peers"`
				}
				if json.Unmarshal(endpoint.Options, &options) != nil {
					return clientdevices.ErrInvalid
				}
				manual := make([]string, 0, len(options.Peers))
				for _, peer := range options.Peers {
					manual = append(manual, peer.PublicKey)
				}
				snapshot, err = core.ReconcileWireGuardPeers(ctx, snapshot.Generation, endpoint.Tag, managed, manual)
				if err != nil {
					return err
				}
				return accountDeviceSnapshot(tx, endpoint.Id, snapshot)
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func accountDeviceSnapshot(tx *gorm.DB, endpointID uint, snapshot coreruntime.WireGuardPeerSnapshot) error {
	samples := map[string]clientdevices.Observation{}
	for _, peer := range snapshot.Peers {
		samples[peer.PublicKey] = deviceObservation(snapshot, peer)
	}
	deltas, err := clientdevices.Account(tx, endpointID, snapshot.Generation, snapshot.ObservedAt, samples)
	if err != nil {
		return err
	}
	users := map[entitytypes.VPNUserID]clientTrafficDelta{}
	for _, delta := range deltas {
		users[delta.UserID] = clientTrafficDelta{up: delta.Up, down: delta.Down}
	}
	return updateClientTrafficDeltas(tx, users)
}

func (s *ClientService) WireGuardDeviceConfig(ctx context.Context, clientID uint, deviceID, host string) (string, error) {
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return "", clientdevices.ErrUnavailable
	}
	defer release()
	db := dbsqlite.DB()
	if db == nil {
		return "", clientdevices.ErrUnavailable
	}
	return entityendpoints.DevicePeerConfig(db.WithContext(ctx), clientID, deviceID, host)
}
