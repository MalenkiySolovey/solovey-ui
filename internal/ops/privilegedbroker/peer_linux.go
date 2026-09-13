//go:build linux

package privilegedbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	processevidence "github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"golang.org/x/sys/unix"
)

const maxPeerExecutableBytes int64 = 512 << 20

type ManifestAttestor struct {
	Manifest    Manifest
	supervision peerSupervisionAttestor
}

func NewManifestAttestor(manifest Manifest) (ManifestAttestor, error) {
	proof, err := manifest.SupervisorProof()
	if err != nil {
		return ManifestAttestor{}, err
	}
	supervision, err := newPeerSupervisionAttestor(proof)
	if err != nil {
		return ManifestAttestor{}, err
	}
	return ManifestAttestor{Manifest: manifest, supervision: supervision}, nil
}

func (a ManifestAttestor) Attest(ctx context.Context, connection *net.UnixConn, role Role) (PeerIdentity, error) {
	if connection == nil {
		return PeerIdentity{}, attestationFailure(PeerAttestationCredentialsUnavailable, errors.New("broker peer connection is absent"))
	}
	var credential *unix.Ucred
	raw, err := connection.SyscallConn()
	if err != nil {
		return PeerIdentity{}, attestationFailure(PeerAttestationCredentialsUnavailable, err)
	}
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credential, socketErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return PeerIdentity{}, attestationFailure(PeerAttestationCredentialsUnavailable, err)
	}
	if socketErr != nil || credential == nil || credential.Pid <= 1 {
		return PeerIdentity{}, attestationFailure(PeerAttestationCredentialsUnavailable, errors.New("broker peer credentials are unavailable"))
	}
	partial := PeerIdentity{PID: int(credential.Pid), UID: uint32(credential.Uid), GID: uint32(credential.Gid)}
	pidfd, err := unix.PidfdOpen(int(credential.Pid), 0)
	if err != nil {
		return partial, attestationFailure(PeerAttestationLivenessUnavailable, errors.New("broker peer pidfd authority is unavailable"))
	}
	identity, err := a.inspect(ctx, int(credential.Pid), uint32(credential.Uid), uint32(credential.Gid), role)
	if err != nil {
		_ = unix.Close(pidfd)
		return identity, err
	}
	identity.livenessFD = pidfd
	identity.hasLiveness = true
	if err := peerAlive(identity); err != nil {
		_ = unix.Close(pidfd)
		return identity, err
	}
	return identity, nil
}

func (a ManifestAttestor) Recheck(ctx context.Context, expected PeerIdentity, role Role) error {
	if err := peerAlive(expected); err != nil {
		return err
	}
	actual, err := a.inspect(ctx, expected.PID, expected.UID, expected.GID, role)
	if err != nil {
		return err
	}
	if actual.BootID != expected.BootID {
		return attestationFailure(PeerAttestationBootMismatch, errors.New("broker peer boot identity changed after initial attestation"))
	}
	if actual.StartTime != expected.StartTime {
		return attestationFailure(PeerAttestationStartIdentityMismatch, errors.New("broker peer start identity changed after initial attestation"))
	}
	if actual.ManifestRevision != expected.ManifestRevision {
		return attestationFailure(PeerAttestationGenerationMismatch, errors.New("broker peer manifest generation changed after initial attestation"))
	}
	if actual.ExecutableDigest != expected.ExecutableDigest || actual.Device != expected.Device || actual.Inode != expected.Inode ||
		actual.ExecutableSize != expected.ExecutableSize || actual.ExecutableMode != expected.ExecutableMode ||
		actual.ExecutableUID != expected.ExecutableUID || actual.ExecutableGID != expected.ExecutableGID {
		return attestationFailure(PeerAttestationExecutableMismatch, errors.New("broker peer mapped executable object changed after initial attestation"))
	}
	if actual.CgroupAvailability != expected.CgroupAvailability || actual.CgroupPolicy != expected.CgroupPolicy ||
		actual.CgroupRevision != expected.CgroupRevision || actual.CgroupAuthorityRevision != expected.CgroupAuthorityRevision ||
		actual.CgroupUnit != expected.CgroupUnit || actual.SupervisorCgroup != expected.SupervisorCgroup {
		return attestationFailure(PeerAttestationCgroupPolicyMismatch, errors.New("broker peer cgroup authority changed after initial attestation"))
	}
	if actual.Supervisor != expected.Supervisor || actual.ProcdService != expected.ProcdService ||
		actual.ProcdInstance != expected.ProcdInstance || actual.SupervisorRelation != expected.SupervisorRelation ||
		actual.SupervisorPID != expected.SupervisorPID || actual.SupervisorStart != expected.SupervisorStart {
		return attestationFailure(PeerAttestationSupervisionMismatch, errors.New("broker peer supervision identity changed after initial attestation"))
	}
	if actual.Revision != expected.Revision {
		return attestationFailure(PeerAttestationProcessMismatch, errors.New("broker peer changed after initial attestation"))
	}
	return peerAlive(expected)
}

func (a ManifestAttestor) VerifyWriter(_ context.Context, expected PeerIdentity, writer WriterCredentials) error {
	if writer.PID != expected.PID || writer.UID != expected.UID || writer.GID != expected.GID {
		return attestationFailure(PeerAttestationWriterMismatch, errors.New("broker request writer differs from connector identity"))
	}
	return peerAlive(expected)
}

func (ManifestAttestor) ClosePeer(identity PeerIdentity) {
	if identity.hasLiveness && identity.livenessFD >= 0 {
		_ = unix.Close(identity.livenessFD)
	}
}

func peerAlive(identity PeerIdentity) error {
	if !identity.hasLiveness || identity.livenessFD < 0 {
		return attestationFailure(PeerAttestationLivenessUnavailable, errors.New("broker peer pidfd is unavailable"))
	}
	if err := unix.PidfdSendSignal(identity.livenessFD, 0, nil, 0); err != nil {
		return attestationFailure(PeerAttestationConnectorDeath, errors.New("broker peer connector is no longer alive"))
	}
	return nil
}

func (a ManifestAttestor) inspect(ctx context.Context, pid int, uid, gid uint32, role Role) (PeerIdentity, error) {
	identity, err := inspectCommonPeer(pid, uid, gid, a.Manifest.Revision)
	if err != nil {
		return identity, err
	}
	candidates := a.Manifest.commonMatching(role, identity)
	if len(candidates) != 1 {
		return identity, attestationFailure(a.Manifest.commonMismatchClass(role, identity), errors.New("broker peer manifest identity is absent or ambiguous"))
	}
	client := candidates[0]
	if a.supervision == nil {
		return identity, attestationFailure(PeerAttestationSupervisionMismatch, errors.New("broker supervisor proof adapter is unavailable"))
	}
	identity, err = a.supervision.Bind(ctx, identity, client, a.Manifest.Revision)
	if err != nil {
		return identity, err
	}
	identity.ManifestClient = client.Name
	identity.CapabilitiesOnly = client.CapabilitiesOnly
	identity.Revision = peerRevision(identity)
	if _, ok := a.Manifest.matching(role, identity); !ok {
		return identity, attestationFailure(PeerAttestationSupervisionMismatch, errors.New("broker peer does not match the release manifest"))
	}
	return identity, nil
}

func inspectCommonPeer(pid int, uid, gid uint32, manifestRevision string) (PeerIdentity, error) {
	identity := PeerIdentity{PID: pid, UID: uid, GID: gid, ManifestRevision: manifestRevision}
	root := filepath.Join("/proc", strconv.Itoa(pid))
	evidence, err := processevidence.Observe(pid)
	if err != nil {
		return identity, attestationFailure(PeerAttestationProcessMismatch, err)
	}
	identity.Groups = evidence.Groups
	identity.Executable = evidence.Executable
	identity.ExecutableDigest = evidence.ExeDigest
	identity.Device, identity.Inode = evidence.ExeDevice, evidence.ExeInode
	identity.ExecutableSize, identity.ExecutableMode = evidence.ExeSize, evidence.ExeMode
	identity.ExecutableUID, identity.ExecutableGID = evidence.ExeUID, evidence.ExeGID
	identity.StartTime = evidence.StartTime
	if uint32(evidence.UID) != uid || uint32(evidence.GID) != gid {
		return identity, attestationFailure(PeerAttestationUIDGIDMismatch, errors.New("broker peer UID or GID identity changed"))
	}
	if err := validateInitialUserNamespace(root); err != nil {
		return identity, attestationFailure(PeerAttestationNamespaceMismatch, err)
	}
	executable := evidence.Executable
	if evidence.ExeSize <= 0 || evidence.ExeSize > maxPeerExecutableBytes || evidence.ExeMode&unix.S_IFMT != unix.S_IFREG ||
		evidence.ExeMode&0o111 == 0 || evidence.ExeMode&0o022 != 0 || evidence.ExeUID != 0 || evidence.ExeGID != 0 ||
		!digestPattern.MatchString(evidence.ExeDigest) || evidence.ExeDevice == 0 || evidence.ExeInode == 0 {
		return identity, attestationFailure(PeerAttestationExecutableMismatch, errors.New("broker peer executable is invalid"))
	}
	bootIDBytes, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return identity, attestationFailure(PeerAttestationBootMismatch, err)
	}
	bootID := strings.TrimSpace(string(bootIDBytes))
	if !safeIdentifier("boot-" + bootID) {
		return identity, attestationFailure(PeerAttestationBootMismatch, errors.New("broker peer boot identity is invalid"))
	}
	identity.Executable = executable
	identity.BootID = bootID
	return identity, nil
}

func validateInitialUserNamespace(root string) error {
	for _, name := range []string{"uid_map", "gid_map"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		fields := strings.Fields(string(data))
		if len(fields) != 3 || fields[0] != "0" || fields[1] != "0" {
			return errors.New("broker peer is in a remapped user namespace")
		}
		length, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil || length < 4294967295 {
			return errors.New("broker peer user namespace mapping is incomplete")
		}
	}
	return nil
}

func peerRevision(identity PeerIdentity) string {
	identity.Revision = ""
	data, _ := jsonMarshal(identity)
	return Digest(data)
}

func validateOwnedByRoot(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return fmt.Errorf("%s is not owned by root", path)
	}
	return nil
}

func jsonMarshal(value any) ([]byte, error) {
	// Kept here so the Linux identity code does not expose a mutable encoder.
	return json.Marshal(value)
}
