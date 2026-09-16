//go:build linux

package openwrt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
	"golang.org/x/sys/unix"
)

// LoadStorageSelection reads a bounded, root-owned deployment fact. Missing
// selection means the stock contract; malformed or unsafe selection never does.
func LoadStorageSelection() (StorageSelection, error) {
	return loadStorageSelection(StorageSelectionPath)
}

func loadStorageSelection(name string) (StorageSelection, error) {
	info, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return StorageSelection{}, nil
	}
	if err != nil {
		return StorageSelection{}, err
	}
	before, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0o444 || before.Uid != 0 || before.Gid != 0 || info.Size() <= 0 || info.Size() > MaxStorageSelectionBytes {
		return StorageSelection{}, ErrUnprovenDurableState
	}
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil || resolved != name {
		return StorageSelection{}, ErrUnprovenDurableState
	}
	parent, err := os.Stat(filepath.Dir(name))
	if err != nil || !parent.IsDir() {
		return StorageSelection{}, ErrUnprovenDurableState
	}
	parentOwner, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || parentOwner.Uid != 0 || parentOwner.Gid != 0 || parent.Mode().Perm()&0o022 != 0 {
		return StorageSelection{}, ErrUnprovenDurableState
	}
	f, err := os.Open(name)
	if err != nil {
		return StorageSelection{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, MaxStorageSelectionBytes+1))
	afterInfo, statErr := f.Stat()
	closeErr := f.Close()
	if readErr != nil || statErr != nil || closeErr != nil {
		return StorageSelection{}, errors.Join(readErr, statErr, closeErr)
	}
	after, ok := afterInfo.Sys().(*syscall.Stat_t)
	if !ok || before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || before.Mtim != after.Mtim {
		return StorageSelection{}, ErrUnprovenDurableState
	}
	return parseStorageSelection(data)
}

// observeBlockSource binds the visible mount's major:minor to its current block
// device node and kernel sysfs identity. It performs no I/O probe or writeback.
func observeBlockSource(f mountevidence.Fact) error {
	if !strings.HasPrefix(f.Source, "/dev/") {
		return ErrUnprovenDurableState
	}
	var st unix.Stat_t
	if err := unix.Stat(f.Source, &st); err != nil {
		return err
	}
	device := fmt.Sprintf("%d:%d", unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev)))
	if st.Mode&unix.S_IFMT != unix.S_IFBLK || device != f.Device || unix.Major(uint64(st.Rdev)) == 0 {
		return ErrUnprovenDurableState
	}
	var target unix.Stat_t
	if err := unix.Stat(f.Target, &target); err != nil {
		return err
	}
	if uint64(target.Dev) != uint64(st.Rdev) {
		return ErrUnprovenDurableState
	}
	var identity unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, f.Target, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &identity); err != nil {
		return err
	}
	if identity.Mask&unix.STATX_MNT_ID == 0 || identity.Mnt_id != f.MountID {
		return ErrUnprovenDurableState
	}
	info, err := os.Stat("/sys/dev/block/" + device)
	if err != nil || !info.IsDir() {
		return errors.Join(ErrUnprovenDurableState, err)
	}
	return nil
}

// PrepareSelectedStorage belongs to package setup. The platform owns mounting;
// no directory is created until its selected backing has passed admission.
func PrepareSelectedStorage() error {
	s, err := LoadStorageSelection()
	if err != nil || s.IsDefault() {
		return err
	}
	if os.Geteuid() != 0 {
		return ErrUnprovenDurableState
	}
	env := productionDurabilityEnvironmentFor(s)
	before, err := validateSelectedMount(s, s.MountPoint, env)
	if err != nil {
		return err
	}
	mountInfo, err := os.Lstat(s.MountPoint)
	if err != nil || !mountInfo.IsDir() {
		return errors.Join(ErrUnprovenDurableState, err)
	}
	mountOwner, ok := mountInfo.Sys().(*syscall.Stat_t)
	if !ok || mountOwner.Uid != 0 || mountOwner.Gid != 0 || mountInfo.Mode().Perm()&0o022 != 0 {
		return ErrUnprovenDurableState
	}
	// Refuse a second durable namespace on an existing installation.
	for _, old := range []string{DefaultDatabaseFolder, DefaultOpenWrtInstanceIDPath} {
		if _, err := os.Lstat(old); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(ErrUnprovenDurableState, err)
		}
	}
	rel, err := filepath.Rel(s.MountPoint, s.Root())
	if err != nil {
		return err
	}
	current := s.MountPoint
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0o711); err == nil {
			if err := syncPreservationDirectory(filepath.Dir(current)); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.Join(ErrUnprovenDurableState, err)
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || st.Gid != 0 || info.Mode().Perm()&0o022 != 0 {
			return ErrUnprovenDurableState
		}
	}
	after, err := validateSelectedMount(s, s.Root(), env)
	if err != nil || !before.SameMount(after) {
		return errors.Join(ErrUnprovenDurableState, err)
	}
	return syncPreservationDirectory(s.MountPoint)
}
