//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/google/nftables"
	"golang.org/x/sys/unix"
)

func RegisterHostBaselineObserver(registry *Registry, transport TransportMode) error {
	if transport != SystemdActivated {
		return nil
	}
	return registry.Register(VerbHostBaseline, Definition{Role: RolePanel, Handler: observeHostBaseline})
}

func observeHostBaseline(ctx context.Context, call Request, peer PeerIdentity) (any, error) {
	var input struct{}
	if DecodePayload(call.Payload, &input) != nil || peer.UID == 0 || peer.Supervisor != "systemd" || peer.CgroupPolicy != CgroupRequired || !peer.hasLiveness {
		return nil, Failure(CodeValidation, "an attested unprivileged systemd Panel is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, path := range []string{"/proc/net/ip_tables_names", "/proc/net/ip6_tables_names"} {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) || strings.TrimSpace(string(data)) != "" {
			return nil, Failure(CodeCapability, "legacy local packet hooks cannot be excluded")
		}
	}
	chains, err := (&nftables.Conn{}).ListChains()
	if err != nil || !baselineHooksObservable(chains) {
		return nil, Failure(CodeCapability, "the host's packet hooks cannot exclude local traffic steering")
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, Failure(CodeCapability, "the host socket namespace cannot be observed")
	}
	defer unix.Close(fd)
	value, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	if err != nil || value == 0 {
		return nil, Failure(CodeCapability, "the host socket namespace cannot be observed")
	}
	return HostBaselineObservation{Namespace: value}, ctx.Err()
}

// The first admission scope does not interpret arbitrary firewall programs.
// Ordinary input/forward filter chains do not establish a local output path.
func baselineHooksObservable(chains []*nftables.Chain) bool {
	for _, chain := range chains {
		if chain == nil || chain.Table == nil {
			return false
		}
		if chain.Hooknum == nil {
			continue
		}
		switch chain.Table.Family {
		case nftables.TableFamilyINet, nftables.TableFamilyIPv4, nftables.TableFamilyIPv6:
			if chain.Type == nftables.ChainTypeFilter && (*chain.Hooknum == *nftables.ChainHookInput || *chain.Hooknum == *nftables.ChainHookForward) {
				continue
			}
		}
		return false
	}
	return true
}
