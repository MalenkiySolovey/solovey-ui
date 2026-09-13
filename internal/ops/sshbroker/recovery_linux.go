//go:build linux

package sshbroker

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

const (
	maxRecoveryVerifierFiles = 512
	maxRecoveryVerifierBytes = 2 << 20
)

type recoveryObserver struct {
	host *Host
}

// NewRecoveryObserver consumes the same closed resolved composition as the
// SSH broker host. Construction and every semantic decision remain inside the
// SSH owner; callers receive only the narrow recovery capability.
func NewRecoveryObserver(composition ResolvedSSHComposition) RecoveryObserver {
	if !composition.Valid() {
		return unavailableRecoveryObserver{platformKnown: true, linux: true, reason: "ssh_recovery_composition_unresolved"}
	}
	host, err := newReadOnlyHostFromResolvedComposition(composition)
	if err != nil {
		return unavailableRecoveryObserver{platformKnown: true, linux: true, reason: "ssh_recovery_owner_unavailable"}
	}
	return &recoveryObserver{host: host}
}

func (o *recoveryObserver) Detect(ctx context.Context) RecoverySupport {
	base := RecoverySupport{PlatformKnown: true, Linux: true, ObserverRevision: RecoveryObserverRevision()}
	if o == nil || o.host == nil || o.host.logs == nil {
		base.Reason = "ssh_recovery_owner_unavailable"
		return base
	}
	base.EvidenceKind = o.host.logs.Kind()
	posture, err := o.host.observe(ctx)
	if err != nil {
		base.Reason = "ssh_recovery_posture_unproven"
		return base
	}
	privateRevision, err := o.privateVerifierRevision(ctx)
	if err != nil {
		base.Reason = "ssh_verifier_revision_unproven"
		return base
	}
	base.VerifierRevision = domain.Revision(struct {
		Contract, Posture, Private string
	}{"sshbroker-recovery-verifier/v2", posture.Posture.SemanticRevision, privateRevision})
	base.Available = true
	return base
}

func (o *recoveryObserver) Observe(ctx context.Context, request RecoveryObserveRequest) (*RecoveryResult, error) {
	if err := validateRecoveryObserveRequest(request, o.host.time()); err != nil {
		return nil, err
	}
	support := o.Detect(ctx)
	if !support.Available {
		return nil, errors.New(support.Reason)
	}
	data, err := o.host.logs.Recent(ctx)
	if err != nil {
		return nil, errors.New("bounded SSH recovery log observation failed")
	}
	now := o.host.time()
	var observations []RecoveryObservation
	switch o.host.implementation {
	case ImplementationOpenSSH:
		journal, ok := o.host.logs.(journaldSSHLogEvidence)
		if !ok || o.host.sshd == nil {
			return nil, errors.New("OpenSSH recovery projection is unavailable")
		}
		observations, err = parseOpenSSHRecoveryJournal(data, request, now, o.host.sshd.Label(), journal.units)
	case ImplementationDropbear:
		if o.host.dropbear == nil {
			return nil, errors.New("Dropbear recovery projection is unavailable")
		}
		observations, err = parseDropbearRecoveryLog(data, request, now, func(pid int) bool {
			fact, observeErr := processevidence.Observe(pid)
			return observeErr == nil && fact.Executable == o.host.dropbear.dropbear.Label()
		})
	default:
		err = errors.New("SSH recovery implementation is unsupported")
	}
	if err != nil {
		return nil, err
	}
	return &RecoveryResult{VerifierRevision: support.VerifierRevision, ObserverRevision: support.ObserverRevision, Observations: observations}, nil
}

func (o *recoveryObserver) privateVerifierRevision(ctx context.Context) (string, error) {
	switch o.host.implementation {
	case ImplementationOpenSSH:
		return o.openSSHVerifierRevision(ctx)
	case ImplementationDropbear:
		return o.dropbearVerifierRevision()
	default:
		return "", errors.New("SSH recovery implementation is unsupported")
	}
}

func (o *recoveryObserver) openSSHVerifierRevision(ctx context.Context) (string, error) {
	effective, err := o.host.run(ctx, o.host.sshd, "-T")
	if err != nil || !recoveryPublicKeyVerifierConfigurationProven(string(effective)) {
		return "", errors.New("effective SSH public-key verification is not locally provable")
	}
	paths := []string{MainConfig, "/etc/passwd", o.host.logs.executable(), o.host.sshd.Label()}
	for _, pattern := range []string{"/etc/ssh/sshd_config.d/*.conf", "/etc/ssh/ssh_host_*_key.pub"} {
		matches, globErr := filepath.Glob(pattern)
		if globErr != nil {
			return "", globErr
		}
		paths = append(paths, matches...)
	}
	passwd, _, err := readRecoveryVerifierFile("/etc/passwd", maxRecoveryVerifierBytes)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(passwd), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 7 || fields[5] == "" || !filepath.IsAbs(fields[5]) || filepath.Clean(fields[5]) != fields[5] {
			continue
		}
		for _, name := range []string{"authorized_keys", "authorized_keys2"} {
			candidate := filepath.Join(fields[5], ".ssh", name)
			if _, statErr := os.Lstat(candidate); statErr == nil {
				paths = append(paths, candidate)
			}
		}
	}
	files, err := recoveryVerifierInventory(paths)
	if err != nil {
		return "", err
	}
	return domain.Revision(struct {
		Contract  string
		Effective string
		Files     []recoveryVerifierFile
	}{"openssh-recovery-verifier/v2", domain.Revision(effective), files}), nil
}

func (o *recoveryObserver) dropbearVerifierRevision() (string, error) {
	if o.host.dropbear == nil {
		return "", errors.New("Dropbear recovery verifier is unavailable")
	}
	files, err := recoveryVerifierInventory([]string{"/etc/config/dropbear", "/etc/dropbear/authorized_keys", "/etc/passwd", o.host.logs.executable(), o.host.dropbear.dropbear.Label()})
	if err != nil {
		return "", err
	}
	return domain.Revision(struct {
		Contract string
		Files    []recoveryVerifierFile
	}{"dropbear-recovery-verifier/v2", files}), nil
}

type recoveryVerifierFile struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

func recoveryVerifierInventory(values []string) ([]recoveryVerifierFile, error) {
	paths := uniqueRecoveryPaths(values)
	if len(paths) == 0 || len(paths) > maxRecoveryVerifierFiles {
		return nil, errors.New("SSH recovery verifier inventory is empty or exceeds its bound")
	}
	files := make([]recoveryVerifierFile, 0, len(paths))
	total := 0
	for _, path := range paths {
		remaining := maxRecoveryVerifierBytes - total
		data, info, err := readRecoveryVerifierFile(path, remaining)
		if err != nil {
			return nil, err
		}
		total += len(data)
		files = append(files, recoveryVerifierFile{Path: path, Mode: uint32(info.Mode().Perm()), Size: info.Size(), Digest: domain.Revision(data)})
	}
	return files, nil
}

func uniqueRecoveryPaths(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.Clean(value)
		if !filepath.IsAbs(value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func readRecoveryVerifierFile(path string, limit int) ([]byte, os.FileInfo, error) {
	if limit < 1 {
		return nil, nil, io.ErrShortBuffer
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, nil, errors.New("SSH recovery verifier contains a missing, non-regular, or linked file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, nil, errors.New("SSH recovery verifier file identity changed before read")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > limit {
		return nil, nil, io.ErrShortBuffer
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, after) || opened.Size() != int64(len(data)) {
		return nil, nil, errors.New("SSH recovery verifier file identity changed during read")
	}
	return data, opened, nil
}
