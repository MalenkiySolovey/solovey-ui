package resourcepressure

import (
	"os"
	"testing"
)

func TestMemoryWorkspaceUsesObservedHeadroomAndCapsUnknown(t *testing.T) {
	const fallback, ceiling = 6 << 20, 128 << 20
	for _, tc := range []struct {
		name      string
		available uint64
		known     bool
		want      int
	}{
		{"normal", 512 << 20, true, 32 << 20},
		{"large", 8 << 30, true, ceiling},
		{"tight", 16 << 20, true, 1 << 20},
		{"no headroom", 0, true, 0},
		{"no observation", ^uint64(0), false, fallback},
		{"unknown cgroup, observed low host memory", 16 << 20, false, 1 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := workspaceSize(tc.available, tc.known, fallback, ceiling); got != tc.want {
				t.Fatalf("workspace=%d want %d", got, tc.want)
			}
		})
	}
}

func TestCgroupWorkspaceHeadroomUsesTightestAncestor(t *testing.T) {
	const root = "/sys/fs/cgroup"
	files := map[string]string{
		"/proc/self/cgroup":    "0::/system.slice/solovey-ui.service\n",
		"/proc/self/mountinfo": "50 36 0:50 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
		root + "/system.slice/solovey-ui.service/memory.max":     "10000\n",
		root + "/system.slice/solovey-ui.service/memory.current": "2000\n",
		root + "/system.slice/memory.max":                        "max\n",
		root + "/system.slice/memory.high":                       "5000\n",
		root + "/system.slice/memory.current":                    "3000\n",
	}
	read := func(name string) ([]byte, error) {
		if value, ok := files[name]; ok {
			return []byte(value), nil
		}
		return nil, os.ErrNotExist
	}
	if got, err := cgroupWorkspaceHeadroom(read); err != nil || got != 2000 {
		t.Fatalf("headroom=%d err=%v", got, err)
	}
	files[root+"/system.slice/memory.current"] = "6000\n"
	if got, err := cgroupWorkspaceHeadroom(read); err != nil || got != 0 {
		t.Fatalf("over soft limit: headroom=%d err=%v", got, err)
	}
	files["/proc/self/mountinfo"] = "50 36 0:50 /different /sys/fs/cgroup rw - cgroup2 cgroup rw\n"
	if _, err := cgroupWorkspaceHeadroom(read); err == nil {
		t.Fatal("cannot infer headroom when the mount hides ancestors")
	}
}
