//go:build linux && zapretproof

package nftables

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// RenderZapretProbe is a compiler for the isolated namespace adapter only.
// The production broker does not admit this table, hook, or queue action.
func RenderZapretProbe(plan planner.ZapretPlan) (string, error) {
	if plan.Plan().Revision() == "" || plan.Queue() == 0 || plan.SocketMark() == 0 || plan.QueueMark() == plan.SocketMark() {
		return "", errors.New("no validated Zapret probe plan")
	}
	scope := plan.PacketScope()
	var text strings.Builder
	table := "inet " + planner.ZapretProbeTable
	fmt.Fprintf(&text, "create table %s { comment %q; }\n", table, probeComment(plan))
	for _, network := range scope.Protocols {
		fmt.Fprintf(&text, "add counter %s %s_seen\nadd counter %s %s_captured\n", table, network, table, network)
	}
	fmt.Fprintf(&text, "add chain %s outbound { type filter hook output priority 150; policy accept; }\n", table)
	for _, network := range scope.Protocols {
		ports := scope.TCPPorts
		if network == "udp" {
			ports = scope.UDPPorts
		}
		values := make([]string, 0, len(ports))
		for _, port := range ports {
			values = append(values, strconv.Itoa(int(port)))
		}
		match := fmt.Sprintf("oifname \"lo\" ct direction original %s dport { %s }", network, strings.Join(values, ", "))
		fmt.Fprintf(&text, "add rule %s outbound %s counter name %s_seen\n", table, match, network)
		// Issue the pass bit in the same rule as queue, without bypass. A
		// removed rule cannot release packets through a later accept rule.
		fmt.Fprintf(&text, "add rule %s outbound %s meta mark 0x%x counter name %s_captured meta mark set 0x%x queue num %d\n",
			table, match, plan.SocketMark(), network, plan.QueueMark(), plan.Queue())
	}
	fmt.Fprintf(&text, "add rule %s outbound meta mark 0x%x drop\n", table, plan.SocketMark())
	return text.String(), nil
}

func probeComment(plan planner.ZapretPlan) string { return "solovey-r8a:" + plan.Plan().Revision() }

// ZapretProbe is the qualification-only platform adapter. It cannot start a
// process, alter a prior table, or operate outside its original namespace.
type ZapretProbe struct {
	executor  systemNFTExecutor
	guard     zapretGuard
	namespace string
	parent    string
	plan      planner.ZapretPlan
	candidate []byte
}

type ZapretProbeValidation struct {
	accepted planner.Accepted
	baseline string
}

// ZapretProbeSeal binds the absent before-image, the atomic create, and the
// observed generation. Only a trusted broker completion may supply this seal
// to Verify or Rollback; it is not a request payload or proof of traffic.
type ZapretProbeSeal struct {
	PlanRevision    string `json:"planRevision"`
	CandidateDigest string `json:"candidateDigest"`
	BeforeDigest    string `json:"beforeDigest"`
	TableDigest     string `json:"tableDigest"`
	GuardDigest     string `json:"guardDigest"`
	QueuePeer       uint32 `json:"queuePeer"`
}

var (
	ErrZapretProbeQueue    = errors.New("Zapret probe queue consumer unavailable or changed")
	ErrZapretProbeRecovery = errors.New("Zapret probe mutation requires reconciliation")
)

func NewZapretProbe(plan planner.ZapretPlan, parentNamespace string) (*ZapretProbe, error) {
	current, err := probeNamespace(parentNamespace)
	if err != nil {
		return nil, err
	}
	candidate, err := RenderZapretProbe(plan)
	if err != nil {
		return nil, err
	}
	executor, ok := newSystemNFTExecutor().(systemNFTExecutor)
	if !ok || executor.binary == nil {
		return nil, errors.New("trusted nft executable not available")
	}
	tc := openNFTExecutable([]string{"/usr/sbin/tc", "/usr/bin/tc", "/sbin/tc", "/bin/tc"})
	if tc == nil {
		_ = executor.binary.Close()
		return nil, errors.New("trusted tc executable not available")
	}
	return &ZapretProbe{executor: executor, guard: zapretGuard{binary: tc, mark: plan.SocketMark(), mask: plan.QueueMark()}, plan: plan,
		parent: parentNamespace, namespace: current, candidate: []byte(candidate)}, nil
}

func (p *ZapretProbe) Close() error {
	return errors.Join(p.executor.binary.Close(), p.guard.binary.Close())
}

func probeNamespace(parent string) (string, error) {
	current, err := os.Readlink("/proc/self/ns/net")
	initNS, initErr := os.Readlink("/proc/1/ns/net")
	links, linkErr := net.Interfaces()
	if !strings.HasPrefix(parent, "net:[") || err != nil || initErr != nil || current == parent || current == initNS || os.Geteuid() != 0 ||
		linkErr != nil || len(links) != 1 || links[0].Name != "lo" || links[0].Flags&net.FlagLoopback == 0 {
		return "", errors.New("Zapret adapter requires an isolated loopback-only network namespace")
	}
	return current, nil
}

func (p *ZapretProbe) currentNamespace() error {
	if current, err := probeNamespace(p.parent); err != nil || current != p.namespace {
		return errors.New("Zapret probe namespace changed")
	}
	return nil
}

func (p *ZapretProbe) QueuePeer() (uint32, error) {
	if err := p.currentNamespace(); err != nil {
		return 0, err
	}
	file, err := os.Open("/proc/net/netfilter/nfnetlink_queue")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return 0, fmt.Errorf("%w: invalid inventory", ErrZapretProbeQueue)
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 9 || fields[0] != strconv.Itoa(int(p.plan.Queue())) {
		return 0, ErrZapretProbeQueue
	}
	peer, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil || peer == 0 {
		return 0, ErrZapretProbeQueue
	}
	return uint32(peer), nil
}

type zapretProbeSnapshot struct {
	baseline, table, marker string
	handle                  uint64
}

// Separate the exact table (including kernel handles, excluding counters) from
// the rest of the ruleset. A matching name by itself never proves ownership.
func (p *ZapretProbe) snapshot(ctx context.Context) (zapretProbeSnapshot, error) {
	if err := p.currentNamespace(); err != nil {
		return zapretProbeSnapshot{}, err
	}
	raw, _, err := p.executor.run(ctx, "--stateless", "--handle", "--json", "list", "ruleset")
	if err != nil {
		return zapretProbeSnapshot{}, err
	}
	var ruleset struct {
		NFTables []json.RawMessage
	}
	if err := json.Unmarshal(raw, &ruleset); err != nil || ruleset.NFTables == nil {
		return zapretProbeSnapshot{}, errors.New("cannot inspect Zapret probe ruleset")
	}
	var other, owned []json.RawMessage
	var result zapretProbeSnapshot
	tables := 0
	for _, item := range ruleset.NFTables {
		var wrapper map[string]json.RawMessage
		if json.Unmarshal(item, &wrapper) != nil || len(wrapper) != 1 {
			return zapretProbeSnapshot{}, errors.New("unrecognized nft inventory")
		}
		for kind, raw := range wrapper {
			var data struct {
				Family, Name, Table, Comment string
				Handle                       uint64
			}
			if json.Unmarshal(raw, &data) != nil {
				return zapretProbeSnapshot{}, errors.New("unrecognized nft object")
			}
			// Some nft versions emit named counter values in JSON even with
			// --stateless. They are observations, not part of the table identity.
			if kind == "counter" {
				var counter map[string]json.RawMessage
				if json.Unmarshal(raw, &counter) != nil {
					return zapretProbeSnapshot{}, errors.New("unrecognized nft counter")
				}
				delete(counter, "packets")
				delete(counter, "bytes")
				wrapper[kind], _ = json.Marshal(counter)
				item, _ = json.Marshal(wrapper)
			}
			if data.Family != "inet" || data.Table != planner.ZapretProbeTable && !(kind == "table" && data.Name == planner.ZapretProbeTable) {
				other = append(other, item)
				continue
			}
			if kind == "table" {
				tables++
				result.marker = data.Comment
				result.handle = data.Handle
			}
			owned = append(owned, item)
		}
	}
	if tables > 1 || tables == 0 && len(owned) > 0 || tables == 1 && result.handle == 0 {
		return zapretProbeSnapshot{}, errors.New("ambiguous Zapret table observation")
	}
	before, _ := json.Marshal(other)
	result.baseline = sha256Hex(before)
	if tables == 1 {
		table, _ := json.Marshal(owned)
		result.table = sha256Hex(table)
	}
	return result, nil
}

func (p *ZapretProbe) Validate(ctx context.Context, facts planner.Facts) (ZapretProbeValidation, error) {
	before, err := p.snapshot(ctx)
	if err != nil {
		return ZapretProbeValidation{}, err
	}
	if err := p.requireAbsent(ctx, before); err != nil {
		return ZapretProbeValidation{}, err
	}
	if _, _, err := p.executor.runWithInput(ctx, p.candidate, "--check", "-f", "-"); err != nil {
		return ZapretProbeValidation{}, err
	}
	accepted, err := planner.Accept(p.plan.Plan(), facts, sha256Hex(p.candidate))
	return ZapretProbeValidation{accepted: accepted, baseline: before.baseline}, err
}

func (p *ZapretProbe) requireAbsent(ctx context.Context, before zapretProbeSnapshot) error {
	guard, _, err := p.guard.snapshot(ctx)
	if err != nil || guard != "" || before.table != "" {
		return errors.Join(errors.New("Zapret probe cannot adopt pre-existing table or qdisc"), err)
	}
	return nil
}

func (p *ZapretProbe) Apply(ctx context.Context, validated ZapretProbeValidation, current planner.Facts) (ZapretProbeSeal, error) {
	candidateDigest := sha256Hex(p.candidate)
	if err := planner.CheckAccepted(validated.accepted, current, candidateDigest); err != nil {
		return ZapretProbeSeal{}, err
	}
	before, err := p.snapshot(ctx)
	if err != nil {
		return ZapretProbeSeal{}, err
	}
	if before.baseline != validated.baseline {
		return ZapretProbeSeal{}, errors.New("nft before-image changed after Zapret validation")
	}
	if err := p.requireAbsent(ctx, before); err != nil {
		return ZapretProbeSeal{}, err
	}
	peer, err := p.QueuePeer()
	if err != nil {
		return ZapretProbeSeal{}, err
	}
	guard, err := p.guard.install(ctx)
	if err != nil {
		return ZapretProbeSeal{}, errors.Join(ErrZapretProbeRecovery, err)
	}
	if _, _, err := p.executor.runWithInput(ctx, p.candidate, "-f", "-"); err != nil {
		return ZapretProbeSeal{}, errors.Join(ErrZapretProbeRecovery, err)
	}
	after, err := p.snapshot(ctx)
	if err != nil || after.table == "" || after.marker != probeComment(p.plan) || after.baseline != before.baseline {
		return ZapretProbeSeal{}, errors.Join(ErrZapretProbeRecovery, err)
	}
	return ZapretProbeSeal{PlanRevision: p.plan.Plan().Revision(), CandidateDigest: candidateDigest,
		BeforeDigest: before.baseline, TableDigest: after.table, GuardDigest: guard, QueuePeer: peer}, nil
}

func (p *ZapretProbe) verifySeal(seal ZapretProbeSeal) error {
	if seal.PlanRevision != p.plan.Plan().Revision() || seal.CandidateDigest != sha256Hex(p.candidate) ||
		!validSHA256(seal.BeforeDigest) || !validSHA256(seal.TableDigest) || !validSHA256(seal.GuardDigest) || seal.QueuePeer == 0 {
		return errors.New("Zapret probe completion does not match its plan")
	}
	return nil
}

func (p *ZapretProbe) Verify(ctx context.Context, seal ZapretProbeSeal) error {
	return p.verify(ctx, seal, true)
}

// VerifyCurrent observes the committed owned generation without treating later
// independent ruleset changes as damage to this table. It grants no repair authority.
func (p *ZapretProbe) VerifyCurrent(ctx context.Context, seal ZapretProbeSeal) error {
	return p.verify(ctx, seal, false)
}

func (p *ZapretProbe) verify(ctx context.Context, seal ZapretProbeSeal, checkBeforeImage bool) error {
	if err := p.verifySeal(seal); err != nil {
		return err
	}
	current, err := p.snapshot(ctx)
	if err != nil {
		return err
	}
	if current.table != seal.TableDigest || current.marker != probeComment(p.plan) {
		return errors.New("Zapret probe table changed before verification")
	}
	if checkBeforeImage && current.baseline != seal.BeforeDigest {
		return errors.New("unrelated rules changed before Zapret probe verification")
	}
	if guard, _, err := p.guard.snapshot(ctx); err != nil || guard != seal.GuardDigest {
		return errors.Join(errors.New("Zapret probe egress guard changed"), err)
	}
	if peer, err := p.QueuePeer(); err != nil || peer != seal.QueuePeer {
		return ErrZapretProbeQueue
	}
	return nil
}

func (p *ZapretProbe) Rollback(ctx context.Context, seal ZapretProbeSeal) error {
	if err := p.verifySeal(seal); err != nil {
		return err
	}
	before, err := p.snapshot(ctx)
	if err != nil {
		return err
	}
	guard, _, err := p.guard.snapshot(ctx)
	if err != nil || guard != "" && guard != seal.GuardDigest || guard == "" && before.table != "" {
		return errors.Join(errors.New("refusing rollback without the exact Zapret egress guard"), err)
	}
	if before.table == "" && guard == "" {
		return nil // the before-image (absence) already holds
	}
	if before.table != "" && (before.table != seal.TableDigest || before.marker != probeComment(p.plan)) {
		return errors.New("refusing to remove a changed Zapret probe table")
	}
	if before.table != "" {
		if _, _, err := p.executor.run(ctx, "delete", "table", "inet", "handle", strconv.FormatUint(before.handle, 10)); err != nil {
			return err
		}
	}
	after, err := p.snapshot(ctx)
	if err != nil || after.table != "" || after.baseline != before.baseline {
		return errors.Join(errors.New("Zapret probe rollback was not verified"), err)
	}
	// The caller must quiesce/stop the exact Core generation before Rollback.
	// Removing the guard first would reopen the marked sockets on rule loss.
	if current, _, err := p.guard.snapshot(ctx); err != nil || current != guard {
		return errors.Join(errors.New("Zapret egress guard changed before release"), err)
	}
	if _, err := p.guard.run(ctx, "qdisc", "del", "dev", "lo", "clsact"); err != nil {
		return err
	}
	return p.requireAbsent(ctx, after)
}
