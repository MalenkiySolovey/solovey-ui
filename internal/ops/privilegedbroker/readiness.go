package privilegedbroker

import (
	"context"
	"errors"
	"slices"
	"time"
)

const (
	DefaultReadinessTimeout = 15 * time.Second
	readinessAttemptTimeout = time.Second
	readinessRetryInterval  = 100 * time.Millisecond
)

// WaitForReadiness performs a fresh typed capability exchange on MAIN. The
// standalone broker binds both role sockets atomically before it serves MAIN,
// so the unprivileged readiness executable must not impersonate the dedicated
// SSH proof client merely to probe PROOF. A broker crash, restart, or stale
// socket still forces a new exchange.
func WaitForReadiness(ctx context.Context, timeout time.Duration) error {
	return waitForReadiness(ctx, timeout, readinessAttempt)
}

func waitForReadiness(ctx context.Context, timeout time.Duration, attempt func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 || timeout > 30*time.Second || attempt == nil {
		return errors.New("broker readiness timeout is invalid")
	}
	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if attempt(waitContext) == nil {
			return nil
		}
		timer := time.NewTimer(readinessRetryInterval)
		select {
		case <-waitContext.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return errors.New("privileged broker readiness timed out")
		case <-timer.C:
		}
	}
}

func readinessAttempt(ctx context.Context) error {
	attemptContext, cancel := context.WithTimeout(ctx, readinessAttemptTimeout)
	defer cancel()
	client := NewClient(RolePanel)
	var capabilities CapabilitiesV1
	if _, err := client.Invoke(attemptContext, Call{
		Verb: VerbCapabilities, OperationID: "broker-readiness", Purpose: "procd-panel-start",
		Timeout: readinessAttemptTimeout, Payload: struct{}{},
	}, &capabilities); err != nil {
		return err
	}
	if capabilities.ProtocolVersion != ProtocolVersion || capabilities.CapabilityRevision != CapabilityRevision ||
		capabilities.Role != RolePanel || !slices.Contains(capabilities.Verbs, VerbCapabilities) || capabilities.Revision == "" {
		return errors.New("privileged broker readiness capability identity differs")
	}
	return nil
}
