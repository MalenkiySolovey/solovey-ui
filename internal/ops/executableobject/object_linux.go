//go:build linux

package executableobject

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const defaultMaxBytes int64 = 512 << 20

type Object struct {
	file     *os.File
	identity Identity
	policy   Policy
}

func Open(label string, policy Policy) (*Object, error) {
	if policy.RequiredOwner != nil {
		owner := *policy.RequiredOwner
		policy.RequiredOwner = &owner // Freeze caller-owned policy memory.
	}
	if err := validatePolicy(policy); err != nil {
		return nil, err
	}
	if label == "" || !filepath.IsAbs(label) || filepath.Clean(label) != label {
		return nil, errors.New("executable candidate path is invalid")
	}
	if policy.MaxBytes <= 0 {
		policy.MaxBytes = defaultMaxBytes
	}
	if policy.ForbiddenMode == 0 {
		policy.ForbiddenMode = 0o022
	}
	if policy.AncestryForbiddenMode == 0 {
		policy.AncestryForbiddenMode = 0o022
	}
	linkInfo, err := os.Lstat(label)
	if err != nil {
		return nil, err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 && !policy.AllowSymlink {
		return nil, errors.New("executable candidate symlink is not supported")
	}
	resolved, err := filepath.EvalSymlinks(label)
	if err != nil || !filepath.IsAbs(resolved) {
		return nil, errors.New("executable candidate resolution is unavailable")
	}
	resolved = filepath.Clean(resolved)
	if policy.RequireTrustedAncestry {
		if err := trustedAncestry(resolved, policy); err != nil {
			return nil, err
		}
	}
	file, err := os.Open(label)
	if err != nil {
		return nil, err
	}
	object := &Object{file: file, policy: policy}
	if err := object.capture(label, resolved); err != nil {
		_ = file.Close()
		return nil, err
	}
	if policy.RequireStablePath {
		if err := object.validatePathBinding(); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return object, nil
}

func trustedAncestry(resolved string, policy Policy) error {
	for current := filepath.Dir(resolved); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("executable candidate ancestry is unsafe")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != policy.AncestryOwner {
			return errors.New("executable candidate ancestry ownership is unsafe")
		}
		forbidden := info.Mode().Perm() & policy.AncestryForbiddenMode
		if forbidden != 0 && !rootOwnedStickyAncestryAllowed(info.Mode(), stat.Uid, forbidden, policy) {
			return errors.New("executable candidate ancestry is unsafe")
		}
		if current == string(filepath.Separator) {
			return nil
		}
	}
}

func rootOwnedStickyAncestryAllowed(mode os.FileMode, owner uint32, forbidden os.FileMode, policy Policy) bool {
	return policy.AllowRootOwnedStickyAncestry && policy.RequireRootOwner && policy.AncestryOwner == 0 && owner == 0 &&
		policy.ForbiddenMode&0o022 == 0o022 && policy.AncestryForbiddenMode&0o022 == 0o022 &&
		mode&os.ModeSticky != 0 && forbidden&^os.FileMode(0o022) == 0
}

func (o *Object) capture(label, resolved string) error {
	if o == nil || o.file == nil {
		return ErrUnavailable
	}
	before, err := fstat(o.file)
	if err != nil {
		return err
	}
	if err := validateStat(before, o.policy); err != nil {
		return err
	}
	if err := seekStart(o.file); err != nil {
		return err
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(o.file, o.policy.MaxBytes+1))
	if err != nil || written != before.Size {
		return errors.New("executable candidate changed while hashing")
	}
	if err := seekStart(o.file); err != nil {
		return err
	}
	after, err := fstat(o.file)
	if err != nil || !sameObject(before, after) {
		return errors.New("executable candidate changed after hashing")
	}
	o.identity = Identity{Label: label, ResolvedPath: resolved, Device: before.Dev, Inode: before.Ino,
		Size: before.Size, Mode: os.FileMode(before.Mode), UID: before.Uid, GID: before.Gid,
		Digest: hex.EncodeToString(hash.Sum(nil))}
	return nil
}

func validateStat(stat unix.Stat_t, policy Policy) error {
	if err := validatePolicy(policy); err != nil {
		return err
	}
	if policy.RequireRegular && stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("executable candidate is not a regular file")
	}
	if policy.RequireExecutable && stat.Mode&0o111 == 0 {
		return errors.New("executable candidate lacks execute permission")
	}
	if policy.RequireRootOwner && (stat.Uid != 0 || stat.Gid != 0) {
		return errors.New("executable candidate is not root-owned")
	}
	if owner := policy.RequiredOwner; owner != nil && (stat.Uid != owner.UID || stat.Gid != owner.GID) {
		return errors.New("executable candidate ownership differs from its exact policy")
	}
	if policy.RequiredMode != 0 {
		mode := os.FileMode(stat.Mode & 0o777)
		if stat.Mode&unix.S_ISUID != 0 {
			mode |= os.ModeSetuid
		}
		if stat.Mode&unix.S_ISGID != 0 {
			mode |= os.ModeSetgid
		}
		if stat.Mode&unix.S_ISVTX != 0 {
			mode |= os.ModeSticky
		}
		if mode != policy.RequiredMode {
			return errors.New("executable candidate mode differs from its exact policy")
		}
	}
	if os.FileMode(stat.Mode).Perm()&policy.ForbiddenMode != 0 {
		return errors.New("executable candidate is writable by an untrusted class")
	}
	if stat.Size <= 0 || stat.Size > policy.MaxBytes {
		return errors.New("executable candidate size is outside its bound")
	}
	if stat.Dev == 0 || stat.Ino == 0 {
		return errors.New("executable candidate object identity is unavailable")
	}
	return nil
}

func validatePolicy(policy Policy) error {
	if policy.RequireRootOwner && policy.RequiredOwner != nil ||
		policy.RequiredMode & ^(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return errors.New("executable object policy is ambiguous or invalid")
	}
	return nil
}

// ValidateMetadata applies the same leaf constraints to descriptor-bound process
// evidence. It does not establish ancestry, path binding or content identity.
func ValidateMetadata(identity Identity, policy Policy) error {
	if policy.MaxBytes <= 0 {
		policy.MaxBytes = defaultMaxBytes
	}
	if policy.ForbiddenMode == 0 {
		policy.ForbiddenMode = 0o022
	}
	return validateStat(unix.Stat_t{Mode: uint32(identity.Mode), Uid: identity.UID, Gid: identity.GID,
		Size: identity.Size, Dev: identity.Device, Ino: identity.Inode}, policy)
}

func fstat(file *os.File) (unix.Stat_t, error) {
	if file == nil {
		return unix.Stat_t{}, ErrUnavailable
	}
	stat := unix.Stat_t{}
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return unix.Stat_t{}, err
	}
	return stat, nil
}

func seekStart(file *os.File) error {
	_, err := file.Seek(0, io.SeekStart)
	return err
}

func sameObject(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Size == right.Size &&
		left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid
}

func (o *Object) Identity() Identity {
	if o == nil {
		return Identity{}
	}
	return o.identity
}

func (o *Object) Label() string {
	if o == nil {
		return ""
	}
	return o.identity.Label
}

func (o *Object) ResolvedPath() string {
	if o == nil {
		return ""
	}
	return o.identity.ResolvedPath
}

// File returns the already verified descriptor. Owners pass it as the only
// ExtraFiles entry and execute ExecPath(0), binding the child to this object.
func (o *Object) File() *os.File {
	if o == nil {
		return nil
	}
	return o.file
}

// ExecPath returns the proc-fd execution label for an ExtraFiles index. It is
// intentionally a mechanism only; the owner still supplies its fixed argv and
// environment and must not expose a generic command API.
func (o *Object) ExecPath(extraIndex int) string {
	if o == nil || extraIndex < 0 {
		return ""
	}
	return fmt.Sprintf("/proc/self/fd/%d", 3+extraIndex)
}

func (o *Object) Revalidate() error {
	if o == nil || o.file == nil {
		return ErrUnavailable
	}
	stat, err := fstat(o.file)
	if err != nil {
		return err
	}
	if err := validateStat(stat, o.policy); err != nil {
		return err
	}
	identity := o.identity
	if stat.Dev != identity.Device || stat.Ino != identity.Inode || stat.Size != identity.Size ||
		os.FileMode(stat.Mode) != identity.Mode || stat.Uid != identity.UID || stat.Gid != identity.GID {
		return errors.New("executable candidate object identity changed")
	}
	if o.policy.RequireStablePath {
		return o.validatePathBinding()
	}
	return nil
}

func (o *Object) validatePathBinding() error {
	if o == nil || o.file == nil || o.identity.Label == "" || o.identity.ResolvedPath == "" {
		return ErrUnavailable
	}
	resolved, err := filepath.EvalSymlinks(o.identity.Label)
	if err != nil || filepath.Clean(resolved) != o.identity.ResolvedPath {
		return errors.New("executable candidate path resolution changed")
	}
	pathInfo, err := os.Stat(o.identity.Label)
	fileInfo, fileErr := o.file.Stat()
	if err != nil || fileErr != nil || !os.SameFile(pathInfo, fileInfo) {
		return errors.New("executable candidate path object changed")
	}
	return nil
}

func (o *Object) Close() error {
	if o == nil || o.file == nil {
		return nil
	}
	return o.file.Close()
}
