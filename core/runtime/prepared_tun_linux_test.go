package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"golang.org/x/sys/unix"
)

const preparedTUNConfig = `{
  "log":{"level":"info"},
  "inbounds":[{"type":"tun","tag":"scoped-tun","interface_name":"svtun-test",
               "address":["198.19.255.1/30"],"dns_mode":"disabled","mtu":1500,"stack":"system"}],
  "outbounds":[{"type":"direct","tag":"direct-a"},{"type":"block","tag":"deny"}],
  "route":{"rules":[{"inbound":"scoped-tun","action":"route","outbound":"direct-a"}],"final":"deny"}
}`

func TestPreparedTUNRejectsConflictingInboundAndNonTUNFile(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(preparedTUNConfig, `"svtun-test"`, `"other"`, 1),
		strings.Replace(preparedTUNConfig, `"scoped-tun"`, `"other"`, 1),
		strings.Replace(preparedTUNConfig, `"stack":"system"`, `"auto_route":true,"stack":"system"`, 1),
		strings.Replace(preparedTUNConfig, `"stack":"system"`, `"auto_route":true,"auto_redirect":true,"stack":"system"`, 1),
		strings.Replace(preparedTUNConfig, `"dns_mode":"disabled"`, `"dns_mode":"native"`, 1),
	} {
		core := NewCore()
		input := corebox.NewPreparedTUN("scoped-tun", "svtun-test", func(context.Context) (*os.File, error) {
			t.Fatal("conflicting inbound tried to receive a descriptor")
			return nil, nil
		})
		if err := core.StartPrepared([]byte(raw), nil, input); err == nil || core.IsRunning() {
			t.Fatal("conflicting inbound started")
		}
	}
	core := NewCore()
	var calls int
	var file *os.File
	input := corebox.NewPreparedTUN("scoped-tun", "svtun-test", func(context.Context) (*os.File, error) {
		calls++
		var err error
		file, err = os.Open(os.DevNull)
		return file, err
	})
	for range 2 {
		if err := core.StartPrepared([]byte(preparedTUNConfig), nil, input); err == nil || core.IsRunning() {
			t.Fatal("non-TUN or consumed descriptor started Core")
		}
	}
	if calls != 1 {
		t.Fatalf("descriptor receiver called %d times", calls)
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("rejected descriptor not closed: %v", err)
	}
}

// The disposable guest configures one kernel TUN before passing the FD to
// this process. A TCP and a UDP client run outside Core and signal completion
// only after receiving the expected payload over the TUN ingress/return path.
func TestPreparedTUNKernel(t *testing.T) {
	raw := os.Getenv("SUI_TEST_PREPARED_TUN_FD")
	if raw == "" {
		t.Skip("requires a prepared TUN in a disposable Linux guest")
	}
	fd, err := strconv.ParseUint(raw, 10, 31)
	if err != nil || os.Geteuid() == 0 {
		t.Fatal("expected an inherited TUN in a capless process")
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var caps [2]unix.CapUserData
	if err := unix.Capget(&header, &caps[0]); err != nil || caps != [2]unix.CapUserData{} {
		t.Fatalf("expected no process capabilities, got %v: %v", caps, err)
	}
	if result, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0); err != nil || result != 1 {
		t.Fatal("expected no_new_privs")
	}
	assertPreparedTUNFDCount(t, 1)
	core := NewCore()
	input := corebox.NewPreparedTUN("scoped-tun", "svtun-test", func(context.Context) (*os.File, error) {
		return os.NewFile(uintptr(fd), "prepared-tun"), nil
	})
	if err := core.StartPrepared([]byte(preparedTUNConfig), nil, input); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Stop() })
	assertPreparedTUNFDCount(t, 1)
	fmt.Println("R8C_TUN_PROVIDER_READY=1")
	done := os.Getenv("SUI_TEST_PREPARED_TUN_DONE")
	for range 200 {
		if _, err := os.Stat(done); err == nil {
			if err := core.Stop(); err != nil {
				t.Fatal(err)
			}
			assertPreparedTUNFDCount(t, 0)
			fmt.Println("R8C_TUN_DESCRIPTOR_CLOSED_AT_STOP=1")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("TCP/UDP TUN wire clients did not acknowledge the return path")
}

func assertPreparedTUNFDCount(t *testing.T, want int) {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		var request unix.Ifreq
		if unix.IoctlIfreq(fd, unix.TUNGETIFF, &request) == nil && request.Name() == "svtun-test" {
			count++
		}
	}
	if count != want {
		t.Fatalf("found %d descriptors attached to svtun-test, want %d", count, want)
	}
}
