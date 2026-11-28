//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
)

func TestZapretObservationDeadlineIsNotRetirement(t *testing.T) {
	const peerRevision = "panel-peer-revision"
	batchID := strings.Repeat("a", 64)
	payload, err := json.Marshal(zapretactivation.BatchIdentity{BatchID: batchID})
	if err != nil {
		t.Fatal(err)
	}
	path := &installedZapretPath{
		ctx: t.Context(), peer: peerRevision, uid: 988,
		grant:  zapretactivation.BatchGrant{BatchID: batchID},
		verify: func(context.Context) error { return context.DeadlineExceeded },
	}
	z := installedZapret{active: path}
	peer := PeerIdentity{Revision: peerRevision, UID: 988}
	request := Request{Payload: payload}
	_, err = z.observe(t.Context(), request, peer)
	var classified *PublicError
	if !errors.As(err, &classified) || classified.Code != CodeDeadline || z.active != path || path.ctx.Err() != nil {
		t.Fatalf("slow readiness probe was treated as generation retirement: active=%t, error=%v", z.active == path, err)
	}
	path.verify = func(context.Context) error { return nil }
	if _, err := z.observe(t.Context(), request, peer); err != nil {
		t.Fatalf("the same generation could not be observed again: %v", err)
	}
	peer.Revision = "another-generation"
	_, err = z.observe(t.Context(), request, peer)
	if !errors.As(err, &classified) || classified.Code != CodeFence {
		t.Fatalf("mismatched peer lost its fence: %v", err)
	}
}

func TestZapretReplacementMemoryHeadroom(t *testing.T) {
	const mib = 1 << 20
	const root = "/sys/fs/cgroup"
	const leaf = "/system.slice/broker.service"
	files := map[string]string{
		root + leaf + "/memory.max":           strconv.Itoa(64 * mib),
		root + leaf + "/memory.high":          "max",
		root + leaf + "/memory.current":       strconv.Itoa(16 * mib),
		root + "/system.slice/memory.max":     strconv.Itoa(100 * mib),
		root + "/system.slice/memory.high":    strconv.Itoa(50 * mib),
		root + "/system.slice/memory.current": strconv.Itoa(47 * mib),
	}
	read := func(name string) (string, error) {
		if raw, ok := files[name]; ok {
			return raw, nil
		}
		return "", os.ErrNotExist
	}
	host := zapretMemoryHeadroom{source: "Host MemAvailable", bytes: 512 * mib}
	check := func(source string, bytes uint64, wantAdmission bool) {
		t.Helper()
		got, err := zapretCgroupHeadroom(host, root, leaf, read)
		headroom := got.hard
		if strings.HasSuffix(source, "memory.high") {
			headroom = got.throttle
		}
		if expected := (zapretMemoryHeadroom{source, bytes}); err != nil || headroom != expected {
			t.Fatalf("headroom = %+v (%v), want %+v", got, err, expected)
		}
		err = admitZapretMemory(got, 3*mib, ZapretEngineCommand{Args: []string{"--filter-l3=ipv4", "--filter-tcp=443"}})
		if wantAdmission && err != nil || !wantAdmission && !errors.Is(err, errZapretMemoryBudget) {
			t.Fatalf("admission = %v, want success %t", err, wantAdmission)
		}
		if err != nil && (!strings.Contains(err.Error(), source) || !zapretCapacityFailure(err)) {
			t.Fatalf("preflight did not expose the limiting resource: %v", err)
		}
		if err != nil && strings.Contains(err.Error(), "before throttling") != strings.HasSuffix(source, "memory.high") {
			t.Fatalf("preflight did not distinguish throttling from a hard limit: %v", err)
		}
		if err != nil {
			code, detail := publicFailure(Failure(CodeValidation, err.Error()))
			if response := failureResponse(Request{}, code, detail); response.Code != CodeValidation || !strings.Contains(response.Message, source) {
				t.Fatalf("broker hid the limiting resource: %+v", response)
			}
		}
	}
	check("broker cgroup v2 memory.high", 3*mib, false)
	files[root+"/system.slice/memory.high"] = "max"
	check("broker cgroup v2 memory.max", 48*mib, true)
	host.bytes = 4 * mib
	check("Host MemAvailable", 4*mib, false)
	files[root+leaf+"/memory.current"] = strconv.Itoa(65 * mib)
	files[root+leaf+"/memory.high"] = strconv.Itoa(50 * mib)
	check("broker cgroup v2 memory.max", 0, false)
	delete(files, root+leaf+"/memory.current")
	if _, err := zapretCgroupHeadroom(host, root, leaf, read); err == nil {
		t.Fatal("a finite limit with unavailable usage was accepted")
	}
	if _, err := zapretCgroupHeadroom(host, root, "/system.slice/../other", read); err == nil {
		t.Fatal("noncanonical membership was accepted")
	}
}

func TestZapretReplacementMemoryChargesSelectedLua(t *testing.T) {
	const workingSet = 3 << 20
	command := ZapretEngineCommand{Args: []string{"--lua-init=@/proc/self/fd/3"}}
	limits := zapretMemoryLimits{hard: zapretMemoryHeadroom{source: "broker cgroup v2 memory.max", bytes: workingSet + zapretQueueBufferReserve + 4096}}
	if err := admitZapretMemory(limits, workingSet, command); err != nil {
		t.Fatalf("command without a bundle does not fit: %v", err)
	}
	command.Lua = strings.Repeat("x", 4096)
	if err := admitZapretMemory(limits, workingSet, command); !errors.Is(err, errZapretMemoryBudget) {
		t.Fatalf("the selected bundle was not charged: %v", err)
	}
	limits.hard.bytes += 2 * uint64(len(command.Lua))
	if err := admitZapretMemory(limits, workingSet, command); err != nil {
		t.Fatalf("the same command does not fit with additional headroom: %v", err)
	}
}
