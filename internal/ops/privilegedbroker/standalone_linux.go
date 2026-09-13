//go:build linux

package privilegedbroker

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	standaloneDirectoryMode = os.FileMode(0o750)
	standaloneSocketMode    = os.FileMode(0o660)
	standaloneProbeTimeout  = 100 * time.Millisecond
)

type standaloneSocketIdentity struct {
	device uint64
	inode  uint64
}

type standaloneSocket struct {
	role     Role
	path     string
	listener *net.UnixListener
	identity standaloneSocketIdentity
}

func openStandaloneListeners(socketRoot string, socketGID uint32) (*ListenerSet, error) {
	if !canonicalStandaloneRoot(socketRoot) {
		return nil, errors.New("standalone broker socket root is not canonical")
	}
	if socketGID == 0 {
		var err error
		socketGID, err = lookupBrokerGroup()
		if err != nil {
			return nil, err
		}
	}
	if err := verifyStandaloneDirectory(socketRoot, socketGID); err != nil {
		return nil, err
	}
	mainSocket, err := bindStandaloneSocket(RolePanel, filepath.Join(socketRoot, filepath.Base(DefaultSocketPath)), socketGID)
	if err != nil {
		return nil, err
	}
	if err := verifyStandaloneDirectory(socketRoot, socketGID); err != nil {
		_ = mainSocket.close()
		return nil, err
	}
	proofSocket, err := bindStandaloneSocket(RoleSSHProof, filepath.Join(socketRoot, filepath.Base(ProofSocketPath)), socketGID)
	if err != nil {
		_ = mainSocket.close()
		return nil, err
	}
	if err := verifyStandaloneDirectory(socketRoot, socketGID); err != nil {
		_ = mainSocket.close()
		_ = proofSocket.close()
		return nil, err
	}
	set := &ListenerSet{Listeners: map[Role]*net.UnixListener{
		RolePanel: mainSocket.listener, RoleSSHProof: proofSocket.listener,
	}}
	var once sync.Once
	set.close = func() error {
		var closeErr error
		once.Do(func() {
			closeErr = errors.Join(mainSocket.close(), proofSocket.close())
		})
		return closeErr
	}
	return set, nil
}

func canonicalStandaloneRoot(value string) bool {
	return value != "" && value != "/" && filepath.IsAbs(value) && filepath.Clean(value) == value && !strings.ContainsAny(value, "\x00\r\n\t")
}

func lookupBrokerGroup() (uint32, error) {
	group, err := user.LookupGroup("solovey-ui")
	if err != nil {
		return 0, errors.New("standalone broker socket group is unavailable")
	}
	gid, err := strconv.ParseUint(group.Gid, 10, 32)
	if err != nil || gid == 0 {
		return 0, errors.New("standalone broker socket group identity is malformed")
	}
	return uint32(gid), nil
}

func verifyStandaloneDirectory(socketRoot string, socketGID uint32) error {
	info, err := os.Lstat(socketRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != standaloneDirectoryMode.Perm() {
		return errors.New("standalone broker socket root is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != socketGID {
		return errors.New("standalone broker socket root ownership is unsafe")
	}
	return nil
}

func bindStandaloneSocket(role Role, socketPath string, socketGID uint32) (*standaloneSocket, error) {
	if role != RolePanel && role != RoleSSHProof {
		return nil, errors.New("standalone broker socket role is invalid")
	}
	if err := prepareStandalonePath(socketPath, socketGID); err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("bind standalone broker socket: %w", err)
	}
	boundIdentity, identityErr := inspectBoundStandaloneSocket(socketPath)
	if identityErr != nil {
		_ = listener.Close()
		return nil, identityErr
	}
	if err := os.Chmod(socketPath, standaloneSocketMode.Perm()); err != nil || os.Chown(socketPath, 0, int(socketGID)) != nil {
		_ = listener.Close()
		_ = removeOwnedStandalonePath(socketPath, boundIdentity)
		return nil, errors.New("standalone broker socket ownership could not be established")
	}
	identity, err := inspectStandaloneSocket(socketPath, socketGID)
	if err != nil || identity != boundIdentity {
		_ = listener.Close()
		_ = removeOwnedStandalonePath(socketPath, boundIdentity)
		if err == nil {
			err = errors.New("standalone broker socket changed while establishing ownership")
		}
		return nil, err
	}
	return &standaloneSocket{role: role, path: socketPath, listener: listener, identity: identity}, nil
}

func inspectBoundStandaloneSocket(socketPath string) (standaloneSocketIdentity, error) {
	info, err := os.Lstat(socketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return standaloneSocketIdentity{}, errors.New("bound standalone broker socket is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Nlink != 1 {
		return standaloneSocketIdentity{}, errors.New("bound standalone broker socket identity is unsafe")
	}
	return standaloneSocketIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}

func prepareStandalonePath(socketPath string, socketGID uint32) error {
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != standaloneSocketMode.Perm() {
		return errors.New("standalone broker stale path is not an owned socket")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != socketGID || stat.Nlink != 1 {
		return errors.New("standalone broker stale socket ownership is unsafe")
	}
	identity := standaloneSocketIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}
	connection, dialErr := net.DialTimeout("unix", socketPath, standaloneProbeTimeout)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("standalone broker socket is already active")
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, syscall.ENOENT) {
		return errors.New("standalone broker stale socket state is ambiguous")
	}
	if err := removeOwnedStandalonePath(socketPath, identity); err != nil {
		return fmt.Errorf("remove stale standalone broker socket: %w", err)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		return errors.New("standalone broker stale socket cleanup was not complete")
	}
	return nil
}

func inspectStandaloneSocket(socketPath string, socketGID uint32) (standaloneSocketIdentity, error) {
	info, err := os.Lstat(socketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != standaloneSocketMode.Perm() {
		return standaloneSocketIdentity{}, errors.New("standalone broker socket identity is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != socketGID || stat.Nlink != 1 {
		return standaloneSocketIdentity{}, errors.New("standalone broker socket ownership is unsafe")
	}
	return standaloneSocketIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}

func (s *standaloneSocket) close() error {
	if s == nil {
		return nil
	}
	closeErr := s.listener.Close()
	removeErr := removeOwnedStandalonePath(s.path, s.identity)
	return errors.Join(closeErr, removeErr)
}

func removeOwnedStandalonePath(socketPath string, expected standaloneSocketIdentity) error {
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || expected.inode == 0 || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || stat.Uid != 0 || stat.Nlink != 1 ||
		uint64(stat.Dev) != expected.device || uint64(stat.Ino) != expected.inode {
		return errors.New("standalone broker socket changed before cleanup")
	}
	return os.Remove(socketPath)
}
