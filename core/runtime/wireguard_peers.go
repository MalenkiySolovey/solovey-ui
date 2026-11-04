package runtime

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

var ErrWireGuardUnavailable = errors.New("wireguard_peer_control_unavailable")

type WireGuardPeer struct{ PublicKey, PresharedKey, Address string }
type WireGuardPeerObservation struct {
	PublicKey                     string
	Addresses                     []string
	Received, Sent, LastHandshake int64
}
type WireGuardPeerSnapshot struct {
	Generation string
	ObservedAt int64
	Peers      []WireGuardPeerObservation
}
type wireGuardIPC interface {
	IpcGet() (string, error)
	IpcSet(string) error
}
type wireGuardIPCPeer struct {
	WireGuardPeerObservation
	preshared string
}

// The native IPC response includes secrets. Only this bounded parser sees it;
// public snapshots contain keys identifying peers and counters, never key data.
func readWireGuardPeers(raw string) ([]wireGuardIPCPeer, error) {
	if len(raw) > 16<<20 {
		return nil, ErrWireGuardUnavailable
	}
	var peers []wireGuardIPCPeer
	seen := map[string]bool{}
	for line := range strings.SplitSeq(raw, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if key == "public_key" {
			decoded, err := hex.DecodeString(value)
			if err != nil || len(decoded) != 32 || len(peers) >= 4096 {
				return nil, ErrWireGuardUnavailable
			}
			public := base64.StdEncoding.EncodeToString(decoded)
			if seen[public] {
				return nil, ErrWireGuardUnavailable
			}
			seen[public] = true
			peers = append(peers, wireGuardIPCPeer{WireGuardPeerObservation: WireGuardPeerObservation{PublicKey: public}})
			continue
		}
		if len(peers) == 0 {
			continue
		}
		peer := &peers[len(peers)-1]
		switch key {
		case "preshared_key":
			peer.preshared = value
		case "allowed_ip":
			prefix, err := netip.ParsePrefix(value)
			if err != nil || len(peer.Addresses) >= 128 {
				return nil, ErrWireGuardUnavailable
			}
			peer.Addresses = append(peer.Addresses, prefix.String())
		case "rx_bytes", "tx_bytes", "last_handshake_time_sec":
			number, err := strconv.ParseInt(value, 10, 64)
			if err != nil || number < 0 {
				return nil, ErrWireGuardUnavailable
			}
			switch key {
			case "rx_bytes":
				peer.Received = number
			case "tx_bytes":
				peer.Sent = number
			default:
				peer.LastHandshake = number
			}
		}
	}
	return peers, nil
}

func wireGuardSnapshot(generation string, peers []wireGuardIPCPeer) WireGuardPeerSnapshot {
	result := WireGuardPeerSnapshot{Generation: generation, ObservedAt: time.Now().Unix(), Peers: []WireGuardPeerObservation{}}
	for _, peer := range peers {
		result.Peers = append(result.Peers, peer.WireGuardPeerObservation)
	}
	return result
}
func wireGuardHex(encoded string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	defer clear(key)
	if err != nil || len(key) != 32 {
		return "", ErrWireGuardUnavailable
	}
	return hex.EncodeToString(key), nil
}

func (c *Core) WireGuardPeers(ctx context.Context, tag string) (WireGuardPeerSnapshot, error) {
	var result WireGuardPeerSnapshot
	if c == nil {
		return result, ErrWireGuardUnavailable
	}
	err := c.withMutation(func(rt coreRuntime) error {
		if ctx == nil || ctx.Err() != nil || rt.generation == "" || rt.endpointManager == nil {
			return ErrWireGuardUnavailable
		}
		endpoint, found := rt.endpointManager.Get(tag)
		if !found || endpoint == nil || endpoint.Type() != "wireguard" {
			return ErrWireGuardUnavailable
		}
		ipc, ok := endpoint.(wireGuardIPC)
		if !ok {
			return ErrWireGuardUnavailable
		}
		raw, err := ipc.IpcGet()
		if err != nil {
			return ErrWireGuardUnavailable
		}
		peers, err := readWireGuardPeers(raw)
		raw = ""
		if err != nil {
			return err
		}
		result = wireGuardSnapshot(rt.generation+":"+strconv.FormatUint(c.endpointRevisions[tag], 10), peers)
		return nil
	})
	return result, err
}

// A fixed, trusted owner command; callers cannot submit raw IPC. The mutation
// and its readback hold the Core lifecycle/mutation fence through completion.
// Manual desired peers stay untouched; obsolete managed peers are removed first.
func (c *Core) ReconcileWireGuardPeers(ctx context.Context, generation, tag string, managed []WireGuardPeer, manual []string) (WireGuardPeerSnapshot, error) {
	var result WireGuardPeerSnapshot
	if c == nil || generation == "" || len(managed) > 4096 || len(manual) > 128 {
		return result, ErrWireGuardUnavailable
	}
	err := c.withMutation(func(rt coreRuntime) error {
		if ctx == nil || ctx.Err() != nil || rt.endpointManager == nil {
			return ErrWireGuardUnavailable
		}
		if rt.generation+":"+strconv.FormatUint(c.endpointRevisions[tag], 10) != generation {
			return ErrStaleGeneration
		}
		endpoint, found := rt.endpointManager.Get(tag)
		if !found || endpoint == nil || endpoint.Type() != "wireguard" {
			return ErrWireGuardUnavailable
		}
		ipc, ok := endpoint.(wireGuardIPC)
		if !ok {
			return ErrWireGuardUnavailable
		}
		raw, err := ipc.IpcGet()
		if err != nil {
			return ErrWireGuardUnavailable
		}
		actual, err := readWireGuardPeers(raw)
		raw = ""
		if err != nil {
			return err
		}
		keep, desired := map[string]bool{}, map[string]WireGuardPeer{}
		for _, key := range manual {
			if _, err := wireGuardHex(key); err != nil || keep[key] {
				return ErrWireGuardUnavailable
			}
			keep[key] = true
		}
		for _, peer := range managed {
			prefix, err := netip.ParsePrefix(peer.Address)
			if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 || keep[peer.PublicKey] {
				return ErrWireGuardUnavailable
			}
			if _, err := wireGuardHex(peer.PublicKey); err != nil {
				return err
			}
			if _, err := wireGuardHex(peer.PresharedKey); err != nil {
				return err
			}
			keep[peer.PublicKey] = true
			desired[peer.PublicKey] = peer
		}
		current := map[string]wireGuardIPCPeer{}
		var update strings.Builder
		for _, peer := range actual {
			current[peer.PublicKey] = peer
			if !keep[peer.PublicKey] {
				key, _ := wireGuardHex(peer.PublicKey)
				update.WriteString("public_key=" + key + "\nremove=true\n")
			}
		}
		for _, peer := range managed {
			key, _ := wireGuardHex(peer.PublicKey)
			psk, _ := wireGuardHex(peer.PresharedKey)
			old, exists := current[peer.PublicKey]
			if exists && old.preshared == psk && len(old.Addresses) == 1 && old.Addresses[0] == peer.Address {
				continue
			}
			update.WriteString("public_key=" + key + "\npreshared_key=" + psk + "\nreplace_allowed_ips=true\nallowed_ip=" + peer.Address + "\n")
		}
		if update.Len() > 0 {
			if err := ipc.IpcSet(update.String()); err != nil {
				return ErrWireGuardUnavailable
			}
		}
		raw, err = ipc.IpcGet()
		if err != nil {
			return ErrWireGuardUnavailable
		}
		actual, err = readWireGuardPeers(raw)
		raw = ""
		if err != nil {
			return err
		}
		for _, peer := range actual {
			if !keep[peer.PublicKey] {
				return ErrWireGuardUnavailable
			}
			if expected, ok := desired[peer.PublicKey]; ok {
				psk, _ := wireGuardHex(expected.PresharedKey)
				if peer.preshared != psk || len(peer.Addresses) != 1 || peer.Addresses[0] != expected.Address {
					return ErrWireGuardUnavailable
				}
				delete(desired, peer.PublicKey)
			}
		}
		if len(desired) != 0 || ctx.Err() != nil {
			return ErrWireGuardUnavailable
		}
		result = wireGuardSnapshot(generation, actual)
		return nil
	})
	return result, err
}
