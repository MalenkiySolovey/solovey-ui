//go:build linux && !minimal && zapretproof

package privilegedbroker

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInheritedZapretDenyConsumesOnlyLocalOwnerCopy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		protocol int
		bind     bool
		mutate   func(systemdStoredFD) []systemdStoredFD
		want     bool
	}{
		{name: "exact stored filter port", protocol: unix.NETLINK_NETFILTER, bind: true, want: true},
		{name: "unbound filter port", protocol: unix.NETLINK_NETFILTER},
		{name: "routing protocol", protocol: unix.NETLINK_ROUTE, bind: true},
		{name: "not retained", protocol: unix.NETLINK_NETFILTER, bind: true, mutate: func(_ systemdStoredFD) []systemdStoredFD { return nil }},
		{name: "duplicate", protocol: unix.NETLINK_NETFILTER, bind: true, mutate: func(s systemdStoredFD) []systemdStoredFD { return []systemdStoredFD{s, s} }},
		{name: "wrong name", protocol: unix.NETLINK_NETFILTER, bind: true, mutate: func(s systemdStoredFD) []systemdStoredFD { s.Name = "other"; return []systemdStoredFD{s} }},
		{name: "wrong inode", protocol: unix.NETLINK_NETFILTER, bind: true, mutate: func(s systemdStoredFD) []systemdStoredFD { s.Inode++; return []systemdStoredFD{s} }},
		{name: "wrong device", protocol: unix.NETLINK_NETFILTER, bind: true, mutate: func(s systemdStoredFD) []systemdStoredFD { s.DevMinor++; return []systemdStoredFD{s} }},
		{name: "wrong mode", protocol: unix.NETLINK_NETFILTER, bind: true, mutate: func(s systemdStoredFD) []systemdStoredFD { s.Mode = unix.S_IFREG; return []systemdStoredFD{s} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, tc.protocol)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(owner)
			if tc.bind {
				if err := unix.Bind(owner, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
					t.Fatal(err)
				}
			}
			inherited, err := unix.FcntlInt(uintptr(owner), unix.F_DUPFD_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			var stat unix.Stat_t
			if err := unix.Fstat(owner, &stat); err != nil {
				t.Fatal(err)
			}
			entry := systemdStoredFD{Name: zapretDenyFDName, Mode: stat.Mode,
				DevMajor: unix.Major(stat.Dev), DevMinor: unix.Minor(stat.Dev), Inode: stat.Ino}
			dump := func(ctx context.Context) ([]systemdStoredFD, error) {
				if _, bounded := ctx.Deadline(); !bounded {
					t.Error("systemd read is unbounded")
				}
				if tc.mutate != nil {
					return tc.mutate(entry), nil
				}
				return []systemdStoredFD{entry}, nil
			}
			if _, err := inspectAndCloseZapretDeny(inherited, dump); (err == nil) != tc.want {
				t.Errorf("unexpected inherited owner validation: %v", err)
			}
			if _, err := unix.FcntlInt(uintptr(inherited), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
				t.Errorf("inherited descriptor not closed: %v", err)
			}
			if _, err := unix.Getsockname(owner); err != nil {
				t.Errorf("independent owner copy affected: %v", err)
			}
		})
	}
}
