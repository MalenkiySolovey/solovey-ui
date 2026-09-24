package privilegedbroker

import (
	"errors"
	"net"
	"sync"
)

// ListenerSet owns the listeners returned by one trusted transport
// composition. Standalone sets also own the exact socket paths and remove
// only the inode that they created.
type ListenerSet struct {
	Listeners map[Role]*net.UnixListener
	close     func() error
}

func (s *ListenerSet) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// OpenTransport is the only production transport selector. Callers must
// provide the mode from a trusted deployment entrypoint; no fallback is
// performed when systemd activation is absent or malformed.
func OpenTransport(mode TransportMode) (*ListenerSet, error) {
	if !mode.Valid() {
		return nil, errors.New("privileged broker transport mode is invalid")
	}
	if mode == SystemdActivated {
		listeners, err := ActivatedListeners()
		if err != nil {
			return nil, err
		}
		set := &ListenerSet{Listeners: listeners}
		var once sync.Once
		set.close = func() error {
			var closeErr error
			once.Do(func() {
				for _, role := range []Role{RolePanel, RoleSSHProof} {
					if listener := listeners[role]; listener != nil {
						closeErr = errors.Join(closeErr, listener.Close())
					}
				}
			})
			return closeErr
		}
		return set, nil
	}
	return openStandaloneListeners(StandaloneSocketRoot, 0)
}

// ManifestPathForTransport selects only between the broker-owned persistent
// systemd authority and the broker-owned standalone runtime authority. The
// deployment entrypoint must still select the transport explicitly.
func ManifestPathForTransport(mode TransportMode) (string, error) {
	switch mode {
	case SystemdActivated:
		return DefaultManifest, nil
	case StandaloneOwned:
		return RuntimeManifestPath, nil
	default:
		return "", errors.New("privileged broker manifest transport mode is invalid")
	}
}

// ParseTransportArgs accepts only the two fixed deployment spellings. It is
// used by the broker command, not by API or remote callers.
func ParseTransportArgs(args []string) (TransportMode, error) {
	if len(args) != 1 {
		return "", errors.New("privileged broker requires one explicit transport mode")
	}
	switch args[0] {
	case "--transport=systemd-activated":
		return SystemdActivated, nil
	case "--transport=standalone-owned":
		return StandaloneOwned, nil
	default:
		return "", errors.New("privileged broker transport argument is unsupported")
	}
}
