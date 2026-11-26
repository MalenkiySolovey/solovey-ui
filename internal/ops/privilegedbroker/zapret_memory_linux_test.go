//go:build linux && !minimal

package privilegedbroker

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

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
		if expected := (zapretMemoryHeadroom{source, bytes}); err != nil || got != expected {
			t.Fatalf("headroom = %+v (%v), want %+v", got, err, expected)
		}
		err = admitZapretMemory(got, 3*mib, []string{"--filter-l3=ipv4", "--filter-tcp=443"})
		if wantAdmission && err != nil || !wantAdmission && !errors.Is(err, errZapretMemoryBudget) {
			t.Fatalf("admission = %v, want success %t", err, wantAdmission)
		}
		if err != nil && (!strings.Contains(err.Error(), source) || !zapretCapacityFailure(err)) {
			t.Fatalf("preflight did not expose the limiting resource: %v", err)
		}
	}
	check("broker cgroup v2 memory.high", 3*mib, false)
	files[root+"/system.slice/memory.high"] = "max"
	check("broker cgroup v2 memory.max", 48*mib, true)
	host.bytes = 4 * mib
	check("Host MemAvailable", 4*mib, false)
	files[root+leaf+"/memory.current"] = strconv.Itoa(65 * mib)
	check("broker cgroup v2 memory.max", 0, false)
	delete(files, root+leaf+"/memory.current")
	if _, err := zapretCgroupHeadroom(host, root, leaf, read); err == nil {
		t.Fatal("a finite limit with unavailable usage was accepted")
	}
	if _, err := zapretCgroupHeadroom(host, root, "/system.slice/../other", read); err == nil {
		t.Fatal("noncanonical membership was accepted")
	}
}
