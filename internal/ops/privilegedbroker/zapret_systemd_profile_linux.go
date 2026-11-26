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
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	packetfilter "github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/google/nftables"
)

const (
	zapretProfilePath = "/etc/solovey-ui/zapret.json"
	zapretEngineRoot  = "/usr/local/lib/solovey-ui/zapret-engine"
	zapretEnginePath  = zapretEngineRoot + "/nfqws2"
	zapretEngineRev   = "038c97e5819df10f8e8c22a2f1fffe57f2f7379b"
	zapretAutoLuaSHA  = "23a9b99a776d868d92f4cc0ee5110f5c1f12fe746620d9c0337022dbc2f52df0"
	zapretInventory   = "/usr/local/solovey-ui/components/installed.json"
)

// The component resolves and compiles the ordered profiles. The broker still
// owns the trusted inputs, Planner allocation and engine executable.
type InstalledZapretStrategies struct {
	Intents         []intent.ZapretPacketPathIntent
	Families        []planner.ZapretFamilies
	UsesCustomBlobs bool
	UsesManagedLua  bool
	HandlesQUIC     func(binding int, ipv6 bool, port uint16) bool
	Compile         func(engineRoot string, queue uint16, loopMark uint32, queueMarks []uint32, trace, replies bool) (ZapretEngineCommand, error)
}

type ZapretEngineCommand struct {
	Args  []string
	Trace ZapretNativeTrace
	Lua   string
}

func (s InstalledZapretStrategies) needsReturn() bool {
	return slices.ContainsFunc(s.Intents, func(p intent.ZapretPacketPathIntent) bool { return p.NeedsReturn })
}

type ZapretNativeTrace interface {
	io.Writer
	MatchedProfiles(binding int) []string
}

type ResolveInstalledZapretStrategies func([]json.RawMessage, []zapretactivation.Request, map[string]json.RawMessage) (InstalledZapretStrategies, error)

type installedZapretProfile struct {
	Schema       string            `json:"schema"`
	OutboundRef  string            `json:"outboundRef,omitempty"`
	OutboundRefs []string          `json:"outboundRefs,omitempty"`
	Interface    string            `json:"interface"`
	IPv6         bool              `json:"ipv6,omitempty"`
	EngineDigest string            `json:"engineSHA256"`
	Strategy     json.RawMessage   `json:"strategy,omitempty"`
	Strategies   []json.RawMessage `json:"strategies,omitempty"`
	// Managed Lua requires a separate operator decision from editor-managed profiles.
	AllowManagedStrategies bool                `json:"allowManagedStrategies,omitempty"`
	AllowCustomBlobs       bool                `json:"allowCustomBlobs,omitempty"`
	AllowManagedLua        bool                `json:"allowManagedLua,omitempty"`
	ObserveNativeProfiles  bool                `json:"observeNativeProfiles,omitempty"`
	TUN                    *installedZapretTUN `json:"tun,omitempty"`
}

type installedZapretTUN struct {
	Inbound zapretactivation.TUNRequest `json:"inbound"`
	Scope   planner.ZapretTUNScope      `json:"scope"`
}

var errZapretDialBinding = errors.New("selected Zapret Dial binding is incompatible with the installed egress")

func (p installedZapretProfile) verifyDialBindings(bindings []zapretactivation.Request, ifindex int32) error {
	var sources map[netip.Addr]bool
	for _, request := range bindings {
		dial := request.Dial
		if dial.Interface != "" && dial.Interface != p.Interface {
			return fmt.Errorf("%w: bind_interface must match the operator-approved interface", errZapretDialBinding)
		}
		if dial.IPv6.IsValid() && !p.IPv6 {
			return fmt.Errorf("%w: the installed profile has no admitted IPv6 path", errZapretDialBinding)
		}
		if dial.IPv4.IsValid() || dial.IPv6.IsValid() || dial.Interface != "" {
			if sources == nil {
				iface, err := net.InterfaceByName(p.Interface)
				if err != nil || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || ifindex != 0 && iface.Index != int(ifindex) {
					return fmt.Errorf("%w: the operator-approved interface is unavailable or changed", errZapretDialBinding)
				}
				addrs, err := iface.Addrs()
				if err != nil {
					return fmt.Errorf("%w: could not verify local source addresses", errZapretDialBinding)
				}
				sources = make(map[netip.Addr]bool, len(addrs))
				for _, addr := range addrs {
					if prefix, err := netip.ParsePrefix(addr.String()); err == nil {
						sources[prefix.Addr().Unmap()] = true
					}
				}
			}
			if dial.IPv4.IsValid() && !sources[dial.IPv4] || dial.IPv6.IsValid() && !sources[dial.IPv6] {
				return fmt.Errorf("%w: assign the selected source address to the operator-approved interface", errZapretDialBinding)
			}
		}
	}
	return nil
}

func (p installedZapretProfile) verifyTUN(ctx context.Context, req *zapretactivation.TUNRequest) error {
	if req == nil {
		return nil
	}
	if p.TUN == nil || !req.Valid() || !safeIdentifier(req.Tag) || *req != p.TUN.Inbound {
		return errors.New("installed Zapret TUN inbound does not match the operator profile")
	}
	if err := checkZapretTUNDevice(); err != nil {
		return fmt.Errorf("broker TUN capability: %w", err)
	}
	iface, err := net.InterfaceByName(p.Interface)
	if err != nil {
		return err
	}
	ingress, err := planner.CheckZapretTUNScope(ctx, p.TUN.Scope, iface.Index)
	if err != nil {
		return err
	}
	return planner.CheckZapretEgressAdmission(ctx, ingress)
}

func readInstalledZapretProfile() (installedZapretProfile, error) {
	mode, err := readZapretAsset("/etc/solovey-ui/deployment-profile", 256)
	if err != nil || strings.TrimSpace(string(mode)) != "native-hardened" {
		return installedZapretProfile{}, errors.New("the native-hardened systemd installation is required for Zapret")
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
		return installedZapretProfile{}, errors.New("the Zapret component is not installed")
	}
	var profile installedZapretProfile
	data, err = readZapretAsset(zapretProfilePath, 32<<10)
	if err != nil || decodeStrict(data, &profile) != nil || profile.Schema != planner.ZapretSystemdBackend ||
		!digestPattern.MatchString(profile.EngineDigest) {
		return installedZapretProfile{}, errors.New("the systemd Zapret profile is invalid or unavailable")
	}
	return profile, nil
}

func (p installedZapretProfile) selectStrategies(input zapretactivation.Validation) ([]json.RawMessage, error) {
	refs, definitions := p.OutboundRefs, p.Strategies
	if p.OutboundRef != "" || len(p.Strategy) != 0 {
		if len(refs) != 0 || len(definitions) != 0 || len(p.Strategy) == 0 {
			return nil, errors.New("ambiguous installed Zapret profile")
		}
		refs, definitions = []string{p.OutboundRef}, []json.RawMessage{p.Strategy}
	}
	allowed := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if !safeIdentifier(ref) || allowed[ref] {
			return nil, errors.New("invalid installed Zapret outbound allowlist")
		}
		allowed[ref] = true
	}
	catalog := make(map[string]json.RawMessage, len(definitions))
	for _, raw := range definitions {
		var item struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &item) != nil || !safeIdentifier(item.ID) ||
			zapretactivation.ManagedStrategyRef(item.ID) || catalog[item.ID] != nil {
			return nil, errors.New("invalid installed Zapret strategy catalog")
		}
		catalog[item.ID] = raw
	}
	if len(input.Definitions) != 0 && !p.AllowManagedStrategies {
		return nil, errors.New("installed Zapret profile does not allow editor-managed strategies")
	}
	used := make(map[string]bool, len(input.Definitions))
	result := make([]json.RawMessage, len(input.Bindings))
	for i, request := range input.Bindings {
		definition := catalog[request.StrategyRef]
		if zapretactivation.ManagedStrategyRef(request.StrategyRef) {
			definition = input.Definitions[request.StrategyRef]
			used[request.StrategyRef] = true
		}
		if !allowed[request.OutboundRef] || len(definition) == 0 || len(definition) > zapretactivation.MaxDefinitionBytes {
			return nil, fmt.Errorf("installed Zapret profile does not authorize outbound %q and Strategy %q", request.OutboundRef, request.StrategyRef)
		}
		result[i] = definition
	}
	if len(used) != len(input.Definitions) {
		return nil, errors.New("installed Zapret received unselected strategy definitions")
	}
	return result, nil
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

var errZapretReplyAsset = errors.New("the Circular strategy requires the pinned zapret-auto.lua; install the matching Zapret2 engine package")

func verifyZapretReplyAsset() error {
	data, err := readZapretAsset(zapretEngineRoot+"/lua/zapret-auto.lua", 512<<10)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != zapretAutoLuaSHA {
		return errZapretReplyAsset
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

func observeInstalledZapretNetwork(ctx context.Context, interfaceName string, ipv6, tun, replies, hasDeny bool, verifyActive func(context.Context) error) (planner.ZapretEgress, []uint16, error) {
	if err := checkZapretForeignHooks(ctx, tun, replies, hasDeny, verifyActive); err != nil {
		return planner.ZapretEgress{}, nil, err
	}
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
		return planner.ZapretEgress{}, nil, errors.New("the approved Zapret egress interface is unavailable")
	}
	if err := planner.CheckZapretEgressAdmission(ctx, iface.Index); err != nil {
		return planner.ZapretEgress{}, nil, err
	}
	egress, err := planner.ObserveZapretLinuxEgress(ctx, int32(iface.Index), true, ipv6)
	if err != nil {
		return planner.ZapretEgress{}, nil, err
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
				return planner.ZapretEgress{}, nil, errors.New("NFQUEUE inventory is invalid")
			}
			queues = append(queues, uint16(queue))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return planner.ZapretEgress{}, nil, errors.New("NFQUEUE inventory is unavailable")
	}
	return egress, slices.Clip(queues), nil
}

func installedZapretFacts(request zapretactivation.Request, egress *planner.ZapretEgress, queues []uint16, serial uint32) planner.Facts {
	return planner.Facts{HostRef: hostref.Local(), Revision: Digest([]byte(planner.ZapretSystemdBackend)),
		CapabilityRevision: Digest([]byte(planner.ZapretSystemdBackend)), InventoryComplete: true,
		Capability: platform.PlatformFact{Availability: platform.Available, Backend: planner.ZapretSystemdBackend},
		Zapret: &planner.ZapretFacts{OutboundRef: request.OutboundRef, OutboundRevision: request.OutboundRevision,
			Provider: "sing-box", InstalledEgress: egress, SystemdSerial: serial, OccupiedQueues: queues,
			SystemdBindInterface: request.Dial.Interface != "", SystemdSource4: request.Dial.IPv4, SystemdSource6: request.Dial.IPv6,
			DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true, EgressGuard: true}}
}

var errZapretReplyHook = errors.New("a foreign INPUT hook occupies the Circular reply guard; move that chain before the Zapret approval hooks")

func checkZapretForeignHooks(ctx context.Context, tun, replies, hasDeny bool, verifyActive func(context.Context) error) error {
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
	return checkZapretHookInventory(ctx, chains, tun, replies, hasDeny, verifyActive)
}

func checkZapretHookInventory(ctx context.Context, chains []*nftables.Chain, tun, replies, hasDeny bool, verifyActive func(context.Context) error) error {
	var sawActive bool
	for _, chain := range chains {
		if chain.Hooknum == nil {
			continue
		}
		// Netdev egress and inet input both have hook number 1.
		switch chain.Table.Family {
		case nftables.TableFamilyINet, nftables.TableFamilyIPv4, nftables.TableFamilyIPv6:
			if *chain.Hooknum == *nftables.ChainHookInput {
				if !replies || chain.Priority != nil && int32(*chain.Priority) < packetfilter.ZapretReplyPrecheckPriority {
					continue
				}
				if chain.Table.Family == nftables.TableFamilyINet {
					if hasDeny && chain.Table.Name == planner.ZapretDenyTable && chain.Priority != nil && packetfilter.ZapretReplyDenyHook(chain.Name, int32(*chain.Priority)) {
						continue // openDeny must verify the retained owner and shape.
					}
					if chain.Table.Name == planner.ZapretInstalledTable && verifyActive != nil {
						sawActive = true
						continue
					}
				}
				return errZapretReplyHook
			}
			if !tun && *chain.Hooknum == *nftables.ChainHookForward {
				continue
			}
		}
		if chain.Table.Family == nftables.TableFamilyINet {
			switch chain.Table.Name {
			case planner.ZapretDenyTable:
				if chain.Name == "deny_unadmitted" && *chain.Hooknum == *nftables.ChainHookPostrouting ||
					chain.Name == "deny_retired_tun" && *chain.Hooknum == *nftables.ChainHookForward {
					continue
				}
			case planner.ZapretInstalledTable:
				if verifyActive != nil {
					sawActive = true
					continue
				}
			}
		}
		return errors.New("foreign packet hooks conflict with the Zapret profile")
	}
	// Only preflight on the current Panel can see its live batch. The
	// immutable owner port must still match; a reserved table name alone
	// does not establish ownership.
	if sawActive {
		return verifyActive(ctx)
	}
	return ctx.Err()
}
