//go:build linux

package sshbroker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
)

const maxSSHLogEvidenceRecords = 256

// sshLogEvidenceAdapter owns bounded retrieval from a logging backend. SSH
// implementation adapters retain only their daemon-specific record parser.
type sshLogEvidenceAdapter interface {
	Kind() LogEvidence
	Recent(context.Context) ([]byte, error)
	executable() string
}

type journaldSSHLogEvidence struct {
	journalctl *executableobject.Object
	units      []string
}
type logreadSSHLogEvidence struct{ logread *executableobject.Object }

const (
	openWrtPublicLogreadPath = "/sbin/logread"
	openWrtUboxLogreadPath   = "/usr/libexec/logread-ubox"
)

// logreadCandidateFacts is deliberately narrower than a general filesystem
// abstraction. It captures only the fixed-path facts needed to decide whether
// a bounded OpenWrt logread capability may be executed by the root broker.
type logreadCandidateFacts struct {
	path         string
	resolvedPath string
	exists       bool
	regular      bool
	symlink      bool
	pathSafe     bool // raw inspector claim; never trusted without proof
	uid          uint32
	gid          uint32
	mode         os.FileMode
	trust        *logreadTrustEvidence
}

// logreadTrustAuthority is an owner-local provenance marker.  Only the
// filesystem inspector can mint a candidate carrying this marker; callers
// receive the resolved executable projection, never candidate facts.
type logreadTrustAuthority struct{}

var trustedLogreadAuthority = &logreadTrustAuthority{}

// logreadTrustEvidence keeps the facts that were observed together.  The
// chooser verifies that the candidate still agrees with this cohesive proof,
// which rejects contradictory synthetic facts without repeating the ancestry
// walk performed by inspectLogreadCandidate.
type logreadTrustEvidence struct {
	authority    *logreadTrustAuthority
	path         string
	resolvedPath string
	regular      bool
	uid          uint32
	gid          uint32
	mode         os.FileMode
	ancestrySafe bool
}

func newJournaldSSHLogEvidence(units ...string) (sshLogEvidenceAdapter, error) {
	if err := (logEvidenceTarget{journaldUnits: units}).validateFor(LogEvidenceJournald); err != nil {
		return nil, err
	}
	journalctl, err := firstFixedBinary("/usr/bin/journalctl", "/bin/journalctl")
	if err != nil {
		return nil, err
	}
	return journaldSSHLogEvidence{journalctl: journalctl, units: append([]string(nil), units...)}, nil
}

func newLogreadSSHLogEvidence() (sshLogEvidenceAdapter, error) {
	path, err := resolveTrustedOpenWrtLogread()
	if err != nil {
		return nil, err
	}
	logread, err := firstFixedBinary(path)
	if err != nil {
		return nil, err
	}
	return logreadSSHLogEvidence{logread: logread}, nil
}

func resolveTrustedOpenWrtLogread() (string, error) {
	return resolveTrustedOpenWrtLogreadAt(openWrtPublicLogreadPath, openWrtUboxLogreadPath, trustedLogreadPath)
}

// resolveTrustedOpenWrtLogreadAt is the owner-local filesystem test seam. The
// production call binds the fixed OpenWrt paths and root-owned ancestry below;
// tests may provide a fixture-root ancestry predicate without introducing a
// general filesystem abstraction.
func resolveTrustedOpenWrtLogreadAt(publicPath, uboxPath string, ancestry func(string) bool) (string, error) {
	return resolveTrustedOpenWrtLogreadOwnedBy(publicPath, uboxPath, ancestry, 0)
}

func resolveTrustedOpenWrtLogreadOwnedBy(publicPath, uboxPath string, ancestry func(string) bool, owner uint32) (string, error) {
	facts := make([]logreadCandidateFacts, 0, 2)
	for _, path := range []string{publicPath, uboxPath} {
		fact, err := inspectLogreadCandidateWithAncestry(path, ancestry)
		if err == nil {
			facts = append(facts, fact)
		}
	}
	return chooseTrustedOpenWrtLogreadOwnedBy(facts, publicPath, uboxPath, owner)
}

func inspectLogreadCandidateWithAncestry(path string, ancestry func(string) bool) (logreadCandidateFacts, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return logreadCandidateFacts{}, err
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return logreadCandidateFacts{}, err
	}
	resolvedPath = filepath.Clean(resolvedPath)
	info, err := os.Stat(path)
	if err != nil {
		return logreadCandidateFacts{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return logreadCandidateFacts{}, errors.New("OpenWrt logread metadata is unavailable")
	}
	ancestrySafe := ancestry(resolvedPath)
	if !ancestrySafe {
		return logreadCandidateFacts{}, errors.New("OpenWrt logread target ancestry is unsafe")
	}
	facts := logreadCandidateFacts{
		path: path, resolvedPath: resolvedPath, exists: true,
		regular: info.Mode().IsRegular(), symlink: linkInfo.Mode()&os.ModeSymlink != 0,
		pathSafe: ancestrySafe,
		uid:      stat.Uid, gid: stat.Gid, mode: info.Mode(),
	}
	facts.trust = &logreadTrustEvidence{
		authority: trustedLogreadAuthority, path: facts.path,
		resolvedPath: facts.resolvedPath, regular: facts.regular,
		uid: facts.uid, gid: facts.gid, mode: facts.mode,
		ancestrySafe: ancestrySafe,
	}
	return facts, nil
}

func chooseTrustedOpenWrtLogread(candidates []logreadCandidateFacts) (string, error) {
	return chooseTrustedOpenWrtLogreadAt(candidates, openWrtPublicLogreadPath, openWrtUboxLogreadPath)
}

func chooseTrustedOpenWrtLogreadAt(candidates []logreadCandidateFacts, publicPath, uboxPath string) (string, error) {
	return chooseTrustedOpenWrtLogreadOwnedBy(candidates, publicPath, uboxPath, 0)
}

func chooseTrustedOpenWrtLogreadOwnedBy(candidates []logreadCandidateFacts, publicPath, uboxPath string, owner uint32) (string, error) {
	for _, candidate := range candidates {
		if !candidate.coherentTrusted() || candidate.mode.Perm()&0o022 != 0 || candidate.mode.Perm()&0o111 == 0 || candidate.uid != owner || candidate.gid != owner {
			continue
		}
		switch {
		case candidate.path == publicPath && candidate.symlink:
			// The public alternatives entrypoint may select any safe provider;
			// the target facts above, rather than a vendor implementation name,
			// establish trust.
		case candidate.path == publicPath && !candidate.symlink:
			if candidate.resolvedPath != publicPath {
				continue
			}
		case candidate.path == uboxPath && !candidate.symlink:
			if candidate.resolvedPath != uboxPath {
				continue
			}
		default:
			// The bounded candidate set is the authority; arbitrary PATH or
			// filesystem locations never become logread authority.
			continue
		}
		return candidate.resolvedPath, nil
	}
	return "", errRequiredFixedHostBinaryUnavailable
}

func (candidate logreadCandidateFacts) coherentTrusted() bool {
	proof := candidate.trust
	return candidate.exists && candidate.regular && proof != nil &&
		proof.authority == trustedLogreadAuthority && proof.ancestrySafe &&
		proof.path == candidate.path && proof.resolvedPath == candidate.resolvedPath &&
		candidate.pathSafe == proof.ancestrySafe &&
		proof.regular == candidate.regular && proof.uid == candidate.uid &&
		proof.gid == candidate.gid && proof.mode == candidate.mode
}

func trustedLogreadPath(path string) bool {
	return trustedLogreadPathOwnedBy(path, 0)
}

func trustedLogreadPathOwnedBy(path string, owner uint32) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
			return false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != owner {
			return false
		}
		if current == string(filepath.Separator) {
			return true
		}
	}
}

func (journaldSSHLogEvidence) Kind() LogEvidence { return LogEvidenceJournald }
func (e journaldSSHLogEvidence) executable() string {
	if e.journalctl == nil {
		return ""
	}
	return e.journalctl.Label()
}
func (e journaldSSHLogEvidence) Recent(ctx context.Context) ([]byte, error) {
	if e.journalctl == nil || (logEvidenceTarget{journaldUnits: e.units}).validateFor(LogEvidenceJournald) != nil {
		return nil, errors.New("journald SSH evidence is unavailable")
	}
	arguments := []string{"--no-pager", "--output=json"}
	for _, unit := range e.units {
		arguments = append(arguments, "--unit="+unit)
	}
	arguments = append(arguments, "--since=-11min")
	return runSSHCapabilityCommand(ctx, e.journalctl, arguments...)
}

func (logreadSSHLogEvidence) Kind() LogEvidence { return LogEvidenceLogread }
func (e logreadSSHLogEvidence) executable() string {
	if e.logread == nil {
		return ""
	}
	return e.logread.Label()
}
func (e logreadSSHLogEvidence) Recent(ctx context.Context) ([]byte, error) {
	if e.logread == nil {
		return nil, errors.New("logread SSH evidence is unavailable")
	}
	return runSSHCapabilityCommand(ctx, e.logread, "-l", strconv.Itoa(maxSSHLogEvidenceRecords), "-t")
}
