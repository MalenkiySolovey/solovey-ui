package hostsurface

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	processevidence "github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
)

// productionProcessFixture mirrors the complete projection emitted by
// platform_linux.readProcess. Fixture-only identity fields do not belong in
// RawProcess or its production normalizer.
func productionProcessFixture(pid int, executable, revisionSeed string) RawProcess {
	sum := sha256.Sum256([]byte(revisionSeed))
	return RawProcess{
		PID: pid, ParentPID: 1, SessionID: pid, StartTime: strconv.Itoa(10_000 + pid),
		Executable: executable, ExeDevice: 10, ExeInode: uint64(20 + pid), UID: 1000, GID: 1000,
		ControlGroup: "/system.slice/fixture.service", ProviderRevision: processevidence.RevisionV2,
		EvidenceRevision: hex.EncodeToString(sum[:]),
	}
}
