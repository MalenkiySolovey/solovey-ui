//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/google/nftables"
)

const (
	zapretProfilePath = "/etc/solovey-ui/zapret.json"
	zapretEngineRoot  = "/usr/local/lib/solovey-ui/zapret-engine"
	zapretEnginePath  = zapretEngineRoot + "/nfqws2"
	zapretEngineRev   = "038c97e5819df10f8e8c22a2f1fffe57f2f7379b"
	zapretInventory   = "/usr/local/solovey-ui/components/installed.json"
)

// InstalledZapretStrategy is resolved by the optional Zapret component at the
// broker composition root. The broker keeps control of trusted input and argv execution.
type InstalledZapretStrategy struct {
	Intent  intent.ZapretPacketPathIntent
	Compile func(engineRoot string, queue uint16, queueMark, loopMark uint32) ([]string, error)
}

type ResolveInstalledZapretStrategy func(json.RawMessage, zapretactivation.Request) (InstalledZapretStrategy, error)

type installedZapretProfile struct {
	Schema       string          `json:"schema"`
	OutboundRef  string          `json:"outboundRef"`
	Interface    string          `json:"interface"`
	EngineDigest string          `json:"engineSHA256"`
	Strategy     json.RawMessage `json:"strategy"`
}

func readInstalledZapretProfile(request zapretactivation.Request) (installedZapretProfile, error) {
	mode, err := readZapretAsset("/etc/solovey-ui/deployment-profile", 256)
	if err != nil || strings.TrimSpace(string(mode)) != "native-hardened" {
		return installedZapretProfile{}, errors.New("Zapret requires the native-hardened systemd installation")
	}
	data, err := readZapretAsset(zapretInventory, installstate.MaxInstalledMetadataBytes)
	if err != nil {
		return installedZapretProfile{}, fmt.Errorf("installed component inventory: %w", err)
	}
	metadata, err := installstate.Decode(data)
	if err != nil || metadata.Binary != "full" {
		return installedZapretProfile{}, errors.New("installed Full component inventory is unavailable")
	}
	installed := false
	for _, item := range metadata.EffectiveComponents() {
		installed = installed || item.ID == "zapret" && item.Installed
	}
	if !installed {
		return installedZapretProfile{}, errors.New("Zapret component is not installed")
	}
	var profile installedZapretProfile
	data, err = readZapretAsset(zapretProfilePath, 32<<10)
	if err != nil || decodeStrict(data, &profile) != nil || profile.Schema != planner.ZapretSystemdBackend ||
		profile.OutboundRef != request.OutboundRef || !digestPattern.MatchString(profile.EngineDigest) {
		return installedZapretProfile{}, errors.New("the one-owner systemd Zapret profile does not authorize this binding")
	}
	return profile, nil
}

func (p installedZapretProfile) verifyEngine() error {
	data, err := readZapretAsset(zapretEnginePath, 16<<20)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != p.EngineDigest {
		return errors.New("installed nfqws2 identity does not match the operator-pinned artifact")
	}
	data, err = readZapretAsset(zapretEngineRoot+"/source.json", 4096)
	var pin struct{ Commit, Engine string }
	if err != nil || json.Unmarshal(data, &pin) != nil || pin.Commit != zapretEngineRev || pin.Engine != "nfqws2" {
		return errors.New("installed nfqws2 does not declare the pinned upstream source")
	}
	for _, file := range []string{"zapret-lib.lua", "zapret-antidpi.lua"} {
		if _, err := readZapretAsset(zapretEngineRoot+"/lua/"+file, 512<<10); err != nil {
			return err
		}
	}
	return nil
}

func readZapretAsset(path string, maxSize int64) ([]byte, error) {
	// Directories and files are trusted deployment assets; neither Panel nor
	// the engine's post-drop UID can replace the executable, profile or Lua.
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || !rootOwnedZapretAsset(info) {
			return nil, errors.New("unsafe or missing installed Zapret asset directory")
		}
		if parent == "/" {
			break
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !rootOwnedZapretAsset(info) || info.Size() < 1 || info.Size() > maxSize {
		return nil, errors.New("unsafe or missing installed Zapret asset")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("installed Zapret asset changed during open")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil || int64(len(data)) != info.Size() {
		return nil, errors.New("installed Zapret asset changed during read")
	}
	return data, nil
}

func rootOwnedZapretAsset(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode()&(os.ModeSetuid|os.ModeSetgid|0o022) == 0
}

func planInstalledZapret(ctx context.Context, request zapretactivation.Request, interfaceName string, selected intent.ZapretPacketPathIntent, serial uint32) (planner.ZapretPlan, error) {
	if err := checkZapretForeignHooks(); err != nil {
		return planner.ZapretPlan{}, err
	}
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
		return planner.ZapretPlan{}, errors.New("the approved Zapret egress interface is unavailable")
	}
	if err := planner.CheckZapretEgressAdmission(ctx, iface.Index); err != nil {
		return planner.ZapretPlan{}, err
	}
	egress, err := planner.ObserveZapretLinuxEgress(ctx, int32(iface.Index), true, false)
	if err != nil {
		return planner.ZapretPlan{}, err
	}
	var queues []uint16
	if table, err := os.ReadFile("/proc/net/netfilter/nfnetlink_queue"); err == nil && len(table) <= 1<<20 {
		for line := range strings.SplitSeq(string(table), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			queue, err := strconv.ParseUint(fields[0], 10, 16)
			if err != nil {
				return planner.ZapretPlan{}, errors.New("NFQUEUE inventory is invalid")
			}
			queues = append(queues, uint16(queue))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return planner.ZapretPlan{}, errors.New("NFQUEUE inventory is unavailable")
	}
	facts := planner.Facts{HostRef: hostref.Local(), Revision: Digest([]byte(planner.ZapretSystemdBackend)),
		CapabilityRevision: Digest([]byte(planner.ZapretSystemdBackend)), InventoryComplete: true,
		Capability: platform.PlatformFact{Availability: platform.Available, Backend: planner.ZapretSystemdBackend},
		Zapret: &planner.ZapretFacts{OutboundRef: request.OutboundRef, OutboundRevision: request.OutboundRevision,
			Provider: "sing-box", InstalledEgress: &egress, SystemdSerial: serial, OccupiedQueues: queues,
			DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true, EgressGuard: true}}
	return planner.CompileZapret(selected, facts)
}

func checkZapretForeignHooks() error {
	for _, path := range []string{"/proc/net/ip_tables_names", "/proc/net/ip6_tables_names"} {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) || len(strings.TrimSpace(string(data))) != 0 {
			return errors.New("legacy firewall hooks are not supported by the Zapret profile")
		}
	}
	chains, err := (&nftables.Conn{}).ListChains()
	if err != nil {
		return err
	}
	for _, chain := range chains {
		if chain.Hooknum == nil {
			continue
		}
		// Netdev egress and inet input both have hook number 1.
		switch chain.Table.Family {
		case nftables.TableFamilyINet, nftables.TableFamilyIPv4, nftables.TableFamilyIPv6:
			if *chain.Hooknum == *nftables.ChainHookInput || *chain.Hooknum == *nftables.ChainHookForward {
				continue
			}
		}
		if chain.Table.Family == nftables.TableFamilyINet && chain.Table.Name == planner.ZapretDenyTable &&
			*chain.Hooknum == *nftables.ChainHookPostrouting {
			continue
		}
		return errors.New("foreign packet hooks conflict with the Zapret profile")
	}
	return nil
}
