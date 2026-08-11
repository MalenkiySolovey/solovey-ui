package recoverypath

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type Helper interface {
	Execute(context.Context, protectionhelper.Request) (protectionhelper.Response, error)
}

type SSHObserver struct {
	Store       Store
	Helper      Helper
	InstanceID  string
	Endpoints   func(context.Context, time.Time) []hostresources.ManagementEndpointV1
	Now         func() time.Time
	SinceMicros int64
	OnError     func()
}

func (o *SSHObserver) Run(ctx context.Context) {
	if o == nil || o.Store == nil || o.Helper == nil || o.InstanceID == "" {
		return
	}
	now := time.Now().UTC()
	if o.Now != nil {
		now = o.Now().UTC()
	}
	oldest := now.Add(-9 * time.Minute).UnixMicro()
	if o.SinceMicros <= 0 {
		o.SinceMicros = now.UnixMicro()
	} else if o.SinceMicros < oldest {
		o.SinceMicros = oldest
	}
	failed := false
	poll := func() {
		err := o.Poll(ctx)
		if err != nil && !failed && o.OnError != nil {
			o.OnError()
		}
		failed = err != nil
	}
	poll()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}

func (o *SSHObserver) Poll(ctx context.Context) error {
	if o == nil || o.Store == nil || o.Helper == nil || o.InstanceID == "" || o.SinceMicros <= 0 {
		return errors.New("SSH recovery observer is incomplete")
	}
	now := time.Now().UTC()
	if o.Now != nil {
		now = o.Now().UTC()
	}
	if oldest := now.Add(-9 * time.Minute).UnixMicro(); o.SinceMicros < oldest {
		o.SinceMicros = oldest
	}
	request := protectionhelper.Request{
		ProtocolVersion:    protectionhelper.ProtocolVersion,
		Correlation:        protectionhelper.Correlation{OperationID: "recovery-observer", InstanceID: o.InstanceID},
		Operation:          protectionhelper.OperationSSHRecoveryObserve,
		SSHRecoveryObserve: &protectionhelper.SSHRecoveryObserveRequest{SinceUnixMicros: o.SinceMicros, MaxEvents: 64},
	}
	response, err := o.Helper.Execute(ctx, request)
	if err != nil || !response.OK || response.SSHRecovery == nil || !validRevision(response.SSHRecovery.VerifierRevision) || len(response.SSHRecovery.Observations) > 64 {
		return errors.Join(errors.New("SSH recovery observation failed"), err)
	}
	if err := o.Store.InvalidateRecoveryPathsBySourceRevision(ctx, string(hostresources.ManagementSSH), response.SSHRecovery.VerifierRevision, "ssh_verifier_revision_changed"); err != nil {
		return err
	}
	endpoints := o.Endpoints
	if endpoints == nil {
		endpoints = CurrentManagementEndpoints
	}
	management := endpoints(ctx, now)
	for _, observation := range response.SSHRecovery.Observations {
		if observation.ObservedAtMicros <= o.SinceMicros || observation.ObservedAtMicros/1_000_000 != observation.ObservedAt || observation.ObservedAt > now.Add(5*time.Minute).Unix() || observation.AuthenticationClass != "publickey" || !opaqueRevisionID(observation.ObservationID, "recovery:") || !opaqueRevisionID(observation.PrincipalID, "principal:") {
			continue
		}
		prefix, parseErr := netip.ParsePrefix(observation.SourcePrefix)
		if parseErr != nil || prefix.Masked() != prefix {
			continue
		}
		family := hostresources.AddressFamilyIPv6
		if prefix.Addr().Is4() {
			family = hostresources.AddressFamilyIPv4
		}
		matches := make([]hostresources.ManagementEndpointV1, 0)
		for _, endpoint := range management {
			if endpoint.ServiceKind == hostresources.ManagementSSH && endpoint.Network == hostresources.NetworkTCP && endpoint.Family == family && hostresources.ManagementEndpointCurrent(endpoint) {
				matches = append(matches, endpoint)
			}
		}
		if len(matches) != 1 || observation.PrincipalID == "" {
			continue
		}
		verifiedAt := time.Unix(observation.ObservedAt, 0).UTC()
		if verifiedAt.Add(RecoveryPathLifetime).Before(now) {
			continue
		}
		reasons, _ := json.Marshal([]string{})
		id := RecoveryPathID(string(hostresources.ManagementSSH), matches[0].ID, observation.PrincipalID, observation.SourcePrefix, "fresh_ssh_login")
		if err := o.Store.UpsertRecoveryPath(ctx, protectionrepository.RecoveryPathModel{
			RecoveryPathID: id, Kind: string(hostresources.ManagementSSH), EndpointID: matches[0].ID, PrincipalID: observation.PrincipalID,
			SourcePrefix: observation.SourcePrefix, VerificationMethod: "fresh_ssh_login", VerifiedAt: verifiedAt.Unix(), ExpiresAt: verifiedAt.Add(RecoveryPathLifetime).Unix(),
			IndependenceClass: "independent_reconnect", VerificationState: "verified", ReasonCodesJSON: reasons,
			SourceRevision: response.SSHRecovery.VerifierRevision, ConfigurationRevision: matches[0].ConfigurationRevision,
		}); err != nil {
			return err
		}
	}
	o.SinceMicros = now.UnixMicro()
	return nil
}

func opaqueRevisionID(value, prefix string) bool {
	return strings.HasPrefix(value, prefix) && validRevision(strings.TrimPrefix(value, prefix))
}
