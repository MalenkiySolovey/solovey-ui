//go:build linux

package firewall

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/listenerevidence"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	sshmanagement "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

func TestNativeLinuxRealListenerAuthorityReachesTypedFirewallCandidate(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := uint16(listener.Addr().(*net.TCPAddr).Port)

	observed, err := listenerevidence.ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true})
	if err != nil || len(observed.Sockets) != 1 {
		t.Fatalf("production listener observer did not create exact authority input: observation=%#v err=%v", observed, err)
	}
	processEvidence, err := processevidence.Observe(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	posture := stockDropbearPostureFixture(now)
	endpointID := fmt.Sprintf("management:ssh:configured:ipv4:%d:section-A:%s", port, sshmanagement.Revision(observed.Sockets[0])[:16])
	posture.Endpoints[0].ID = endpointID
	posture.Endpoints[0].Family = hostresources.AddressFamilyIPv4
	posture.Endpoints[0].Bind = "127.0.0.1"
	posture.Endpoints[0].Port = port
	posture.Endpoints[0].Exposure = hostresources.EndpointIntentForBind("127.0.0.1")
	posture.Endpoints[0].Wildcard = false
	posture.Endpoints[0].DualStack = false
	posture.Endpoints[0].ObservedAt = now.Unix()
	posture.Endpoints[0].ExpiresAt = now.Add(sshmanagement.MaxPostureLifetime).Unix()

	pid, parent, session, uid, gid := processEvidence.PID, processEvidence.ParentPID, processEvidence.SessionID, processEvidence.UID, processEvidence.GID
	process := hostfacts.ProcessFact{
		ProviderRevision: processEvidence.ProviderRevision, EvidenceRevision: processEvidence.Revision,
		PID: &pid, ParentPID: &parent, SessionID: &session, StartTime: processEvidence.StartTime,
		ExeDigest: processEvidence.ExeDigest, Executable: processEvidence.Executable,
		ExeDevice: processEvidence.ExeDevice, ExeInode: processEvidence.ExeInode, UID: &uid, GID: &gid,
	}
	serviceRevision := posture.ServiceRevision
	service := hostfacts.ServiceFact{
		SupervisorRevision: sshmanagement.Revision(struct{ Kind, Instance, Process string }{"procd-contract-shape", "opaque-instance-A", processEvidence.Revision}),
		CgroupAvailability: "unavailable", CgroupPolicy: "optional", CgroupRevision: sshmanagement.Revision("native-linux-contract-no-supervisor-cgroup"),
		MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "dropbear", ProcdInstance: "opaque-instance-A",
		ProcdCommand: []string{processEvidence.Executable, "-F", "-p", fmt.Sprint(port)},
	}
	authority := sshmanagement.SSHListenerAuthorityV1{
		Schema: sshmanagement.ListenerAuthoritySchemaV1, EndpointIDs: []string{endpointID}, InstanceID: "opaque-instance-A",
		Socket: observed.Sockets[0], Process: process, Service: service, BinaryRevision: processEvidence.ExeDigest,
		ServiceRevision: serviceRevision, ConfigurationRevision: posture.ConfigurationRevision,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(sshmanagement.MaxListenerAuthorityLifetime).Unix(),
	}
	authority.Seal()
	posture.Binary.Digest = processEvidence.ExeDigest
	posture.BinaryRevision = processEvidence.ExeDigest
	posture.ListenerAuthorities = []sshmanagement.SSHListenerAuthorityV1{authority}
	posture.ObservedAt = now.Unix()
	posture.ExpiresAt = now.Add(sshmanagement.MaxPostureLifetime).Unix()
	posture.SemanticRevision = sshmanagement.PostureSemanticRevision(posture)

	assertSSHPostureReachesPreparedTypedFirewallCandidate(t, now, posture)
}
