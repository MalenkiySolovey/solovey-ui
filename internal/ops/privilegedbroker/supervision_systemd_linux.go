//go:build linux

package privilegedbroker

import (
	"context"
	"errors"
	"strings"

	processevidence "github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
)

type systemdSupervisionAttestor struct {
	readCgroup func(int) (systemdCgroupEvidence, error)
}

type systemdCgroupEvidence struct {
	unit         string
	availability processevidence.CgroupAvailability
	revision     string
}

func (a systemdSupervisionAttestor) Bind(_ context.Context, identity PeerIdentity, client ClientManifest, _ string) (PeerIdentity, error) {
	if a.readCgroup == nil {
		return identity, attestationFailure(PeerAttestationSupervisionMismatch, errors.New("systemd service evidence is unavailable"))
	}
	evidence, err := a.readCgroup(identity.PID)
	identity.CgroupUnit = evidence.unit
	identity.CgroupAvailability = string(evidence.availability)
	identity.CgroupPolicy = client.CgroupPolicy
	identity.CgroupRevision = evidence.revision
	identity.CgroupAuthorityRevision = client.CgroupAuthorityRevision
	identity.Supervisor = "systemd"
	if err != nil || client.CgroupPolicy != CgroupRequired || client.CgroupAuthorityRevision != CgroupAuthorityRevisionV1 ||
		evidence.availability != processevidence.CgroupAvailable {
		return identity, attestationFailure(PeerAttestationCgroupPolicyMismatch, errors.New("broker peer required cgroup evidence is unavailable"))
	}
	if !systemdCgroupProofMatches(client.CgroupUnit, evidence.unit) {
		return identity, attestationFailure(PeerAttestationSupervisionMismatch, errors.New("broker peer systemd service identity differs"))
	}
	return identity, nil
}

func processSystemdCgroupUnit(pid int) (systemdCgroupEvidence, error) {
	if pid <= 1 {
		return systemdCgroupEvidence{}, errors.New("broker peer systemd process identity is invalid")
	}
	fact, err := processevidence.Observe(pid)
	if err != nil {
		return systemdCgroupEvidence{}, err
	}
	evidence := systemdCgroupEvidence{availability: fact.CgroupAvailability, revision: fact.CgroupRevision}
	if fact.CgroupAvailability != processevidence.CgroupAvailable {
		return evidence, nil
	}
	for _, entry := range fact.Cgroups {
		line := entry.HierarchyID + ":" + strings.Join(entry.Controllers, ",") + ":" + entry.Path
		if unit, parseErr := parseSystemdCgroupUnit([]byte(line)); parseErr == nil {
			evidence.unit = unit
			return evidence, nil
		}
	}
	return evidence, errors.New("broker peer systemd service identity is absent")
}

func parseSystemdCgroupUnit(data []byte) (string, error) {
	if len(data) == 0 || len(data) > 16<<10 {
		return "", errors.New("broker peer systemd cgroup evidence is malformed")
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		for _, element := range strings.Split(parts[2], "/") {
			if (strings.HasSuffix(element, ".service") || strings.HasSuffix(element, ".scope")) && safeIdentifier(element) {
				return element, nil
			}
		}
	}
	return "", errors.New("broker peer systemd service identity is absent")
}
