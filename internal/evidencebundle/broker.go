package evidencebundle

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BrokerSnapshot is the closed product-to-evidence seam for a denied broker
// event. It intentionally excludes payload, environment, command line and
// arbitrary command output.
type BrokerSnapshot struct {
	Timestamp               time.Time
	FailureClass            string
	Role                    string
	PID                     int
	UID                     uint32
	GID                     uint32
	ExecutableLabel         string
	ExecutableSHA256        string
	ExecutableDevice        uint64
	ExecutableInode         uint64
	ExecutableMode          uint32
	ExecutableUID           uint32
	ExecutableGID           uint32
	Service                 string
	Instance                string
	CgroupAvailability      string
	CgroupPolicy            string
	CgroupUnit              string
	CgroupAuthorityRevision string
	SupervisorCgroup        string
	Supervisor              string
	SupervisorPID           int
	SupervisorStart         string
	BootID                  string
	StartIdentity           string
	ManifestClient          string
	ManifestRevision        string
	HandlerOwner            string
	HandlerStage            string
	HandlerReason           string
	HandlerErrno            string
	ProofMethod             string
}

func BrokerCollectors(snapshot BrokerSnapshot) map[Group]Collector {
	captured := func(facts Facts) Collector {
		return func(context.Context, Trigger) Capture { return Capture{State: StateCaptured, Facts: facts} }
	}
	failed := func(reason string) Collector {
		return func(context.Context, Trigger) Capture { return Capture{State: StateCaptureError, Reason: reason} }
	}
	collectors := map[Group]Collector{}
	if snapshot.Service != "" {
		collectors[GroupService] = captured(Facts{Service: &ServiceFact{Name: snapshot.Service}})
	} else {
		collectors[GroupService] = failed("service_identity_unavailable")
	}
	if snapshot.Instance != "" {
		collectors[GroupInstance] = captured(Facts{Instance: &InstanceFact{Name: snapshot.Instance}})
	} else {
		collectors[GroupInstance] = failed("instance_identity_unavailable")
	}
	if snapshot.PID > 1 {
		collectors[GroupPeerPID] = captured(Facts{PeerPID: &PeerPIDFact{PID: snapshot.PID}})
		collectors[GroupPeerCredentials] = captured(Facts{PeerCredentials: &PeerCredentialsFact{PID: snapshot.PID, UID: snapshot.UID, GID: snapshot.GID}})
	} else {
		collectors[GroupPeerPID] = failed("peer_pid_unavailable")
		collectors[GroupPeerCredentials] = failed("peer_credentials_unavailable")
	}
	if snapshot.ExecutableLabel != "" {
		collectors[GroupExecutableIdentity] = captured(Facts{ExecutableIdentity: &ExecutableIdentityFact{
			Label: snapshot.ExecutableLabel, SHA256: snapshot.ExecutableSHA256, Device: snapshot.ExecutableDevice,
			Inode: snapshot.ExecutableInode, Mode: snapshot.ExecutableMode, UID: snapshot.ExecutableUID,
			GID: snapshot.ExecutableGID, Role: snapshot.Role,
		}})
	} else {
		collectors[GroupExecutableIdentity] = failed("executable_identity_unavailable")
	}
	collectors[GroupProcessCgroup] = func(ctx context.Context, _ Trigger) Capture {
		lines, err := readBoundedProcCgroup(ctx, snapshot.PID)
		if err != nil {
			return Capture{State: StateCaptureError, Reason: "process_cgroup_unavailable"}
		}
		return Capture{State: StateCaptured, Facts: Facts{ProcessCgroup: &ProcessCgroupFact{
			Availability: snapshot.CgroupAvailability, Policy: snapshot.CgroupPolicy, Unit: snapshot.CgroupUnit,
			SupervisorCgroup: snapshot.SupervisorCgroup, BoundedProcLines: lines,
			AuthorityRevision: snapshot.CgroupAuthorityRevision,
		}}}
	}
	if snapshot.Supervisor != "" && snapshot.SupervisorPID > 1 {
		collectors[GroupSupervisorProcess] = captured(Facts{SupervisorProcess: &SupervisorProcessFact{
			Supervisor: snapshot.Supervisor, Service: snapshot.Service, Instance: snapshot.Instance,
			PID: snapshot.SupervisorPID, StartIdentity: snapshot.SupervisorStart,
			ManifestClient: snapshot.ManifestClient, ManifestRevision: snapshot.ManifestRevision,
		}})
	} else {
		collectors[GroupSupervisorProcess] = failed("supervisor_process_unavailable")
	}
	if snapshot.BootID != "" {
		collectors[GroupBootIdentity] = captured(Facts{BootIdentity: &BootIdentityFact{BootID: snapshot.BootID}})
	} else {
		collectors[GroupBootIdentity] = failed("boot_identity_unavailable")
	}
	if snapshot.StartIdentity != "" {
		collectors[GroupProcessStartIdentity] = captured(Facts{ProcessStartIdentity: &ProcessStartIdentityFact{StartIdentity: snapshot.StartIdentity}})
	} else {
		collectors[GroupProcessStartIdentity] = failed("process_start_identity_unavailable")
	}
	collectors[GroupBoundedLogs] = func(context.Context, Trigger) Capture {
		at := snapshot.Timestamp.UTC()
		return Capture{State: StateCaptured, Facts: Facts{BoundedLogs: &BoundedLogsFact{
			Source: "broker", WindowStart: at.Add(-LogWindowBefore), WindowEnd: at.Add(LogWindowAfter),
			Lines: []LogLine{{Timestamp: at, Class: "broker_denial", Message: boundedBrokerDiagnosticLine(snapshot)}},
		}}}
	}
	return collectors
}

func boundedBrokerDiagnosticLine(snapshot BrokerSnapshot) string {
	values := []string{"role=" + snapshot.Role, "failure=" + snapshot.FailureClass}
	for _, field := range []struct{ name, value string }{
		{"owner", snapshot.HandlerOwner}, {"reason", snapshot.HandlerReason}, {"stage", snapshot.HandlerStage},
		{"errno", snapshot.HandlerErrno}, {"proof", snapshot.ProofMethod},
	} {
		if field.value != "" {
			values = append(values, field.name+"="+field.value)
		}
	}
	return strings.Join(values, " ")
}

func readBoundedProcCgroup(ctx context.Context, pid int) ([]string, error) {
	if pid <= 1 {
		return nil, fmt.Errorf("peer PID is unavailable")
	}
	file, err := os.Open(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 512), 4096)
	lines := make([]string, 0, 8)
	bytes := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.ContainsAny(line, "\x00\r\n") || len(line) > 512 || len(lines) >= 32 {
			return nil, fmt.Errorf("process cgroup evidence is malformed or unbounded")
		}
		bytes += len(line)
		if bytes > 4096 {
			return nil, fmt.Errorf("process cgroup evidence exceeds byte bound")
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil || len(lines) == 0 {
		return nil, fmt.Errorf("process cgroup evidence is unavailable")
	}
	return lines, nil
}
