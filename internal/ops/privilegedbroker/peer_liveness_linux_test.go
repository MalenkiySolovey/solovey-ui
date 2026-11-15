//go:build linux

package privilegedbroker

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPeerLivenessRetriesOnlyInterruptedObservations(t *testing.T) {
	type observation struct {
		count  int
		events int16
		err    error
	}
	for _, test := range []struct {
		name         string
		observations []observation
		calls        int
		class        PeerAttestationClass
	}{
		{name: "live", observations: []observation{{}}, calls: 1},
		{name: "interrupted then live", observations: []observation{{err: unix.EINTR}, {}}, calls: 2},
		{name: "two interruptions then live", observations: []observation{{err: unix.EINTR}, {err: unix.EINTR}, {}}, calls: 3},
		{name: "wrapped interruption", observations: []observation{{err: errors.Join(unix.EINTR, errors.New("signal"))}, {}}, calls: 2},
		{name: "interrupted then exited", observations: []observation{{err: unix.EINTR}, {count: 1, events: unix.POLLIN}}, calls: 2, class: PeerAttestationConnectorDeath},
		{name: "interrupted then reaped", observations: []observation{{err: unix.EINTR}, {count: 1, events: unix.POLLHUP}}, calls: 2, class: PeerAttestationConnectorDeath},
		{name: "signal storm", observations: []observation{{err: unix.EINTR}, {err: unix.EINTR}, {err: unix.EINTR}}, calls: 3, class: PeerAttestationLivenessUnavailable},
		{name: "bad descriptor", observations: []observation{{err: unix.EBADF}}, calls: 1, class: PeerAttestationLivenessUnavailable},
		{name: "invalid descriptor event", observations: []observation{{count: 1, events: unix.POLLNVAL}}, calls: 1, class: PeerAttestationLivenessUnavailable},
		{name: "poll error event", observations: []observation{{count: 1, events: unix.POLLERR}}, calls: 1, class: PeerAttestationLivenessUnavailable},
		{name: "empty readiness", observations: []observation{{count: 1}}, calls: 1, class: PeerAttestationLivenessUnavailable},
		{name: "extra readiness count", observations: []observation{{count: 2, events: unix.POLLIN}}, calls: 1, class: PeerAttestationLivenessUnavailable},
		{name: "negative readiness count", observations: []observation{{count: -1}}, calls: 1, class: PeerAttestationLivenessUnavailable},
		{name: "interrupted then observation error", observations: []observation{{err: unix.EINTR}, {err: unix.EBADF}}, calls: 2, class: PeerAttestationLivenessUnavailable},
		{name: "unknown event", observations: []observation{{count: 1, events: unix.POLLPRI}}, calls: 1, class: PeerAttestationLivenessUnavailable},
		{name: "inconsistent observation", observations: []observation{{events: unix.POLLIN}}, calls: 1, class: PeerAttestationLivenessUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			poll := func(fds []unix.PollFd, timeout int) (int, error) {
				if timeout != 0 || len(fds) != 1 || fds[0].Fd != 123 || fds[0].Events != unix.POLLIN || fds[0].Revents != 0 {
					t.Fatal("liveness observation changed the held authority or wait budget")
				}
				if calls >= len(test.observations) {
					t.Fatal("unexpected syscall retry")
				}
				result := test.observations[calls]
				calls++
				fds[0].Revents = result.events
				if errors.Is(result.err, unix.EINTR) {
					fds[0].Revents = unix.POLLNVAL
				}
				return result.count, result.err
			}
			err := peerAliveWithPoll(PeerIdentity{PID: 456, livenessFD: 123, hasLiveness: true}, poll)
			if calls != test.calls {
				t.Fatalf("calls=%d want=%d", calls, test.calls)
			}
			if test.class == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if peerAttestationClass(err) != test.class {
				t.Fatalf("class=%s want=%s: %v", peerAttestationClass(err), test.class, err)
			}
		})
	}
	for _, identity := range []PeerIdentity{{}, {hasLiveness: true, livenessFD: -1}} {
		err := peerAliveWithPoll(identity, func([]unix.PollFd, int) (int, error) { t.Fatal("missing authority reached syscall"); return 0, nil })
		if peerAttestationClass(err) != PeerAttestationLivenessUnavailable {
			t.Fatalf("missing pidfd was admitted: %v", err)
		}
	}
}
