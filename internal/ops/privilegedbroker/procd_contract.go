package privilegedbroker

import (
	"encoding/json"
	"errors"
	"slices"
)

const maxProcdEvidenceBytes = 256 << 10

// ProcdInstanceEvidence is the strict projection returned by the fixed,
// manifest-bound procd inspection. It contains no lifecycle or arbitrary ubus
// capability.
type ProcdInstanceEvidence struct {
	Name    string
	PID     int
	Command []string
	User    string
	Group   string
}

type procdInstanceEvidence = ProcdInstanceEvidence

func parseProcdInstanceEvidence(raw []byte, expected ClientManifest) (procdInstanceEvidence, error) {
	if len(raw) == 0 || len(raw) > maxProcdEvidenceBytes || validateJSON(raw) != nil {
		return procdInstanceEvidence{}, errors.New("procd service evidence is malformed")
	}
	var services map[string]json.RawMessage
	if err := json.Unmarshal(raw, &services); err != nil || len(services) != 1 {
		return procdInstanceEvidence{}, errors.New("procd service evidence is ambiguous")
	}
	serviceRaw, ok := services[expected.ProcdService]
	if !ok {
		return procdInstanceEvidence{}, errors.New("procd service identity differs")
	}
	var service map[string]json.RawMessage
	if err := json.Unmarshal(serviceRaw, &service); err != nil {
		return procdInstanceEvidence{}, errors.New("procd service projection is malformed")
	}
	instancesRaw, ok := service["instances"]
	if !ok {
		return procdInstanceEvidence{}, errors.New("procd service instances are absent")
	}
	var instances map[string]json.RawMessage
	if err := json.Unmarshal(instancesRaw, &instances); err != nil || len(instances) == 0 || len(instances) > 16 {
		return procdInstanceEvidence{}, errors.New("procd service instances are ambiguous")
	}
	instanceRaw, ok := instances[expected.ProcdInstance]
	if !ok {
		return procdInstanceEvidence{}, errors.New("procd instance identity differs")
	}
	var instance map[string]json.RawMessage
	if err := json.Unmarshal(instanceRaw, &instance); err != nil {
		return procdInstanceEvidence{}, errors.New("procd instance projection is malformed")
	}
	var running bool
	var pid int
	var command []string
	var user, group string
	if json.Unmarshal(instance["running"], &running) != nil || !running ||
		json.Unmarshal(instance["pid"], &pid) != nil || pid <= 1 ||
		json.Unmarshal(instance["command"], &command) != nil || len(command) == 0 || len(command) > 16 ||
		json.Unmarshal(instance["user"], &user) != nil || json.Unmarshal(instance["group"], &group) != nil {
		return procdInstanceEvidence{}, errors.New("procd instance identity is incomplete")
	}
	if !slices.Equal(command, expected.ProcdCommand) || user != expected.ProcdUser || group != expected.ProcdGroup {
		return procdInstanceEvidence{}, errors.New("procd instance command or account differs")
	}
	return procdInstanceEvidence{Name: expected.ProcdInstance, PID: pid, Command: command, User: user, Group: group}, nil
}

func parseProcdAncestorEvidence(raw []byte, expected ClientManifest, descends func(int) bool) (procdInstanceEvidence, error) {
	if len(raw) == 0 || len(raw) > maxProcdEvidenceBytes || validateJSON(raw) != nil || descends == nil {
		return procdInstanceEvidence{}, errors.New("procd ancestor evidence is malformed")
	}
	var services map[string]json.RawMessage
	if json.Unmarshal(raw, &services) != nil || len(services) != 1 {
		return procdInstanceEvidence{}, errors.New("procd ancestor service evidence is ambiguous")
	}
	var service struct {
		Instances map[string]json.RawMessage `json:"instances"`
	}
	if json.Unmarshal(services[expected.ProcdService], &service) != nil || len(service.Instances) == 0 || len(service.Instances) > 32 {
		return procdInstanceEvidence{}, errors.New("procd ancestor instances are ambiguous")
	}
	matches := make([]procdInstanceEvidence, 0, 1)
	for name, rawInstance := range service.Instances {
		if !safeIdentifier(name) {
			continue
		}
		var instance struct {
			Running bool     `json:"running"`
			PID     int      `json:"pid"`
			Command []string `json:"command"`
			User    string   `json:"user"`
			Group   string   `json:"group"`
		}
		if json.Unmarshal(rawInstance, &instance) != nil || !instance.Running || instance.PID <= 1 || len(instance.Command) < len(expected.ProcdCommand) || len(instance.Command) > 64 ||
			(instance.User != "" && instance.User != expected.ProcdUser) || (instance.Group != "" && instance.Group != expected.ProcdGroup) || !descends(instance.PID) {
			continue
		}
		prefix := true
		for index := range expected.ProcdCommand {
			prefix = prefix && instance.Command[index] == expected.ProcdCommand[index]
		}
		if prefix {
			matches = append(matches, procdInstanceEvidence{Name: name, PID: instance.PID, Command: instance.Command, User: expected.ProcdUser, Group: expected.ProcdGroup})
		}
	}
	if len(matches) != 1 {
		return procdInstanceEvidence{}, errors.New("SSH proof does not descend from exactly one authenticated procd proposition")
	}
	return matches[0], nil
}

func validateProcdProcessBinding(identity PeerIdentity, supervisorPID int, supervisorStart, relation string, descends bool) error {
	switch relation {
	case ProcdRelationMain:
		if supervisorPID != identity.PID || supervisorStart != identity.StartTime {
			return errors.New("broker peer differs from the procd instance main process")
		}
	case ProcdRelationAncestor:
		if supervisorPID == identity.PID || !descends {
			return errors.New("broker peer is not descended from the procd instance")
		}
	default:
		return errors.New("procd process relation is invalid")
	}
	return nil
}
