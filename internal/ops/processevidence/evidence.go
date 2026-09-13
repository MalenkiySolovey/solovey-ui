// Package processevidence owns the bounded, read-only Linux kernel process
// observation used by semantic owners. It deliberately assigns no supervisor,
// daemon, application, deployment, or authorization meaning to those facts.
package processevidence

import (
	"bufio"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
)

var ErrDescriptorInventoryBound = errors.New("process descriptor inventory exceeds its bound")

const (
	RevisionV1       = "linux-proc-process-evidence/v1"
	RevisionV2       = "linux-proc-process-evidence/v2"
	maxStatBytes     = 8 << 10
	maxStatusBytes   = 64 << 10
	maxCgroupBytes   = 16 << 10
	maxCgroupEntries = 64
)

type CgroupAvailability string

const (
	CgroupAvailable   CgroupAvailability = "available"
	CgroupUnavailable CgroupAvailability = "unavailable"
	CgroupMalformed   CgroupAvailability = "malformed"
	CgroupUnsafe      CgroupAvailability = "unsafe"
)

type CgroupEntry struct {
	HierarchyID string   `json:"hierarchyId"`
	Controllers []string `json:"controllers"`
	Path        string   `json:"path"`
}

type Fact struct {
	ProviderRevision   string             `json:"providerRevision"`
	PID                int                `json:"pid"`
	ParentPID          int                `json:"parentPid"`
	SessionID          int                `json:"sessionId"`
	StartTime          string             `json:"startTime"`
	UID                int                `json:"uid"`
	GID                int                `json:"gid"`
	Groups             []uint32           `json:"groups"`
	Executable         string             `json:"executable"`
	ExecutableDeleted  bool               `json:"executableDeleted,omitempty"`
	ExeDevice          uint64             `json:"exeDevice"`
	ExeInode           uint64             `json:"exeInode"`
	ExeSize            int64              `json:"exeSize"`
	ExeMode            uint32             `json:"exeMode"`
	ExeUID             uint32             `json:"exeUid"`
	ExeGID             uint32             `json:"exeGid"`
	ExeDigest          string             `json:"exeDigest"`
	CgroupAvailability CgroupAvailability `json:"cgroupAvailability"`
	CgroupRevision     string             `json:"cgroupRevision"`
	Cgroups            []CgroupEntry      `json:"cgroups"`
	Revision           string             `json:"revision"`
}

// DescriptorSnapshot is a bounded kernel inventory for one process. It
// deliberately exposes only descriptor count and socket inode identity; the
// semantic owner remains responsible for interpreting those facts.
type DescriptorSnapshot struct {
	Count        int
	SocketInodes map[int]string
}

func parseStat(data []byte) (int, int, string, error) {
	closing := strings.LastIndexByte(string(data), ')')
	if closing < 0 {
		return 0, 0, "", errors.New("process stat is malformed")
	}
	fields := strings.Fields(string(data)[closing+1:])
	if len(fields) <= 19 {
		return 0, 0, "", errors.New("process stat is incomplete")
	}
	parent, parentErr := strconv.Atoi(fields[1])
	session, sessionErr := strconv.Atoi(fields[3])
	if parentErr != nil || sessionErr != nil || parent < 0 || session < 0 {
		return 0, 0, "", errors.New("process stat relationship is malformed")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return 0, 0, "", errors.New("process start identity is malformed")
	}
	return parent, session, fields[19], nil
}

func parseStatus(data []byte) (int, int, []uint32, error) {
	uid, gid := -1, -1
	groups := make([]uint32, 0, 8)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "Uid":
			if len(fields) != 5 || fields[1] != fields[2] || fields[1] != fields[3] || fields[1] != fields[4] {
				return 0, 0, nil, errors.New("process UID identity is not stable")
			}
			uid64, err := strconv.ParseUint(fields[1], 10, 32)
			if err != nil {
				return 0, 0, nil, errors.New("process UID identity is malformed")
			}
			uid = int(uid64)
		case "Gid":
			if len(fields) != 5 || fields[1] != fields[2] || fields[1] != fields[3] || fields[1] != fields[4] {
				return 0, 0, nil, errors.New("process GID identity is not stable")
			}
			gid64, err := strconv.ParseUint(fields[1], 10, 32)
			if err != nil {
				return 0, 0, nil, errors.New("process GID identity is malformed")
			}
			gid = int(gid64)
		case "Groups":
			if len(fields) > 65 {
				return 0, 0, nil, errors.New("process supplementary groups exceed their bound")
			}
			for _, value := range fields[1:] {
				parsed, err := strconv.ParseUint(value, 10, 32)
				if err != nil {
					return 0, 0, nil, errors.New("process supplementary group is malformed")
				}
				groups = append(groups, uint32(parsed))
			}
		}
	}
	if err := scanner.Err(); err != nil || uid < 0 || gid < 0 {
		return 0, 0, nil, errors.Join(errors.New("process credentials are unavailable"), err)
	}
	return uid, gid, groups, nil
}

func parseCgroups(data []byte) ([]CgroupEntry, error) {
	if len(data) == 0 {
		return nil, errors.New("process cgroup evidence is absent")
	}
	result := make([]CgroupEntry, 0, 4)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || parts[0] == "" || !strings.HasPrefix(parts[2], "/") || path.Clean(parts[2]) != parts[2] {
			return nil, fmt.Errorf("process cgroup entry is malformed")
		}
		controllers := []string{}
		if parts[1] != "" {
			controllers = strings.Split(parts[1], ",")
		}
		result = append(result, CgroupEntry{HierarchyID: parts[0], Controllers: controllers, Path: parts[2]})
		if len(result) > maxCgroupEntries {
			return nil, errors.New("process cgroup entries exceed their bound")
		}
	}
	return result, nil
}

func UnifiedCgroup(fact Fact) (string, bool) {
	if fact.CgroupAvailability != CgroupAvailable {
		return "", false
	}
	for _, entry := range fact.Cgroups {
		if entry.HierarchyID == "0" && len(entry.Controllers) == 0 {
			return entry.Path, true
		}
	}
	return "", false
}
