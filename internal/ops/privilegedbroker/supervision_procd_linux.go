//go:build linux

package privilegedbroker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	processevidence "github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"golang.org/x/sys/unix"
)

const procdCgroupRoot = "/sys/fs/cgroup/services"

type procdSupervisionAttestor struct {
	inspector     *ProcdInspector
	observeCgroup func(int, string, string) (procdCgroupEvidence, error)
}

type procdCgroupEvidence struct {
	path         string
	availability processevidence.CgroupAvailability
	revision     string
}

func (a procdSupervisionAttestor) Bind(ctx context.Context, identity PeerIdentity, client ClientManifest, manifestRevision string) (PeerIdentity, error) {
	if a.inspector == nil {
		return identity, attestationFailure(PeerAttestationSupervisionMismatch, errors.New("procd service evidence inspector is unavailable"))
	}
	var evidence procdInstanceEvidence
	var err error
	if client.ProcdRelation == ProcdRelationAncestor {
		evidence, err = a.inspector.inspectAncestor(ctx, client, identity.PID)
	} else {
		evidence, err = a.inspector.inspect(ctx, client)
	}
	if err != nil {
		return identity, attestationFailure(PeerAttestationSupervisionMismatch, err)
	}
	identity.Supervisor = "procd"
	identity.ProcdService = client.ProcdService
	identity.ProcdInstance = evidence.Name
	identity.SupervisorRelation = client.ProcdRelation
	identity.SupervisorPID = evidence.PID
	process, err := processevidence.Observe(evidence.PID)
	if err != nil {
		return identity, attestationFailure(PeerAttestationSupervisionMismatch, errors.New("procd instance process identity is unavailable"))
	}
	startTime := process.StartTime
	identity.SupervisorStart = startTime
	descends := false
	if client.ProcdRelation == ProcdRelationAncestor {
		descends = processDescendsFrom(identity.PID, evidence.PID, 32)
	}
	if err := validateProcdProcessBinding(identity, evidence.PID, startTime, client.ProcdRelation, descends); err != nil {
		return identity, attestationFailure(PeerAttestationSupervisionMismatch, err)
	}
	if client.ProcdRelation == ProcdRelationAncestor {
		supervisor, inspectErr := inspectCommonPeer(evidence.PID, 0, 0, manifestRevision)
		if inspectErr != nil || supervisor.Executable != client.ProcdExecutable || supervisor.ExecutableDigest != client.ProcdExecutableDigest ||
			supervisor.Device != client.ProcdDevice || supervisor.Inode != client.ProcdInode || supervisor.StartTime != startTime {
			return identity, attestationFailure(PeerAttestationSupervisionMismatch, errors.New("procd ancestor executable identity differs from the release manifest"))
		}
	}
	if !client.CgroupPolicy.Valid() || client.CgroupAuthorityRevision != CgroupAuthorityRevisionV1 {
		return identity, attestationFailure(PeerAttestationCgroupPolicyMismatch, errors.New("procd cgroup policy authority is invalid"))
	}
	identity.CgroupPolicy = client.CgroupPolicy
	identity.CgroupAuthorityRevision = client.CgroupAuthorityRevision
	cgroup := procdCgroupEvidence{availability: processevidence.CgroupUnavailable, revision: Digest([]byte("procd-cgroup-unavailable"))}
	if a.observeCgroup != nil {
		cgroup, err = a.observeCgroup(identity.PID, client.ProcdService, evidence.Name)
		if err != nil {
			return identity, attestationFailure(PeerAttestationCgroupPolicyMismatch, err)
		}
	}
	if cgroup.availability == processevidence.CgroupAvailable {
		identity.SupervisorCgroup = cgroup.path
	} else if client.CgroupPolicy == CgroupRequired {
		identity.CgroupAvailability = string(cgroup.availability)
		identity.CgroupRevision = cgroup.revision
		return identity, attestationFailure(PeerAttestationCgroupPolicyMismatch, errors.New("required procd cgroup evidence is unavailable"))
	}
	identity.CgroupAvailability = string(cgroup.availability)
	identity.CgroupRevision = cgroup.revision
	return identity, nil
}

func processDescendsFrom(pid, ancestor, limit int) bool {
	seen := make(map[int]bool, limit)
	for steps := 0; steps < limit && pid > 1; steps++ {
		if seen[pid] {
			return false
		}
		seen[pid] = true
		process, err := processevidence.Observe(pid)
		if err != nil {
			return false
		}
		parent := process.ParentPID
		if parent == ancestor {
			return true
		}
		pid = parent
	}
	return false
}

func processProcdCgroup(pid int, service, instance string) (procdCgroupEvidence, error) {
	if pid <= 1 || !safeIdentifier(service) || !safeIdentifier(instance) {
		return procdCgroupEvidence{}, errors.New("procd cgroup proposition is invalid")
	}
	_, err := os.Lstat(procdCgroupRoot)
	if errors.Is(err, os.ErrNotExist) {
		return procdCgroupEvidence{availability: processevidence.CgroupUnavailable, revision: Digest([]byte("procd-cgroup-root-absent"))}, nil
	}
	if err != nil || !safeRootCgroupDirectory(procdCgroupRoot) || !cgroup2Filesystem(procdCgroupRoot) {
		return procdCgroupEvidence{}, errors.New("procd cgroup authority root is unsafe")
	}
	serviceRoot := filepath.Join(procdCgroupRoot, service)
	expectedRoot := filepath.Join(procdCgroupRoot, service, instance)
	if !safeRootCgroupDirectory(serviceRoot) || !safeRootCgroupDirectory(expectedRoot) {
		return procdCgroupEvidence{}, errors.New("procd cgroup instance authority is unavailable")
	}
	process, err := processevidence.Observe(pid)
	if err != nil {
		return procdCgroupEvidence{}, err
	}
	if process.CgroupAvailability == processevidence.CgroupUnavailable {
		return procdCgroupEvidence{availability: processevidence.CgroupUnavailable, revision: process.CgroupRevision}, nil
	}
	if process.CgroupAvailability != processevidence.CgroupAvailable {
		return procdCgroupEvidence{}, errors.New("broker peer procd cgroup evidence is unsafe")
	}
	unifiedPath, unified := processevidence.UnifiedCgroup(process)
	if !unified {
		return procdCgroupEvidence{}, errors.New("broker peer procd cgroup service identity is absent")
	}
	path, available, err := parseProcdCgroup([]byte("0::"+unifiedPath+"\n"), service, instance)
	if err != nil {
		return procdCgroupEvidence{}, err
	}
	if !available {
		return procdCgroupEvidence{}, errors.New("broker peer procd cgroup service identity is absent")
	}
	return procdCgroupEvidence{path: path, availability: processevidence.CgroupAvailable, revision: process.CgroupRevision}, nil
}

func safeRootCgroupDirectory(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for current := path; ; current = filepath.Dir(current) {
		stat := unix.Stat_t{}
		if unix.Lstat(current, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0o022 != 0 {
			return false
		}
		if current == string(filepath.Separator) {
			return true
		}
	}
}

func cgroup2Filesystem(path string) bool {
	stat := unix.Statfs_t{}
	return unix.Statfs(path, &stat) == nil && stat.Type == unix.CGROUP2_SUPER_MAGIC
}

func parseProcdCgroup(data []byte, service, instance string) (string, bool, error) {
	if len(data) == 0 || len(data) > 16<<10 || !safeIdentifier(service) || !safeIdentifier(instance) {
		return "", false, errors.New("procd cgroup evidence is malformed")
	}
	expected := "/services/" + service + "/" + instance
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || parts[0] != "0" || parts[1] != "" {
			continue
		}
		observed := filepath.Clean(parts[2])
		if observed == expected {
			return observed, true, nil
		}
		if strings.HasPrefix(observed, "/services/") {
			return "", true, errors.New("broker peer procd cgroup service identity differs")
		}
		return "", false, nil
	}
	return "", false, nil
}
