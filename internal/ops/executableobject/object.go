// Package executableobject contains one low-level executable-object security
// mechanism. It assigns no command, daemon, deployment, or feature meaning to
// a candidate; semantic owners choose their bounded candidate sets and
// operation vocabulary.
package executableobject

import (
	"errors"
	"os"
)

var ErrUnavailable = errors.New("coherent executable-object security is unavailable")

// Policy describes only filesystem/object propositions. It deliberately has no
// command, argv, environment, or semantic-owner fields.
type Policy struct {
	MaxBytes               int64
	AllowSymlink           bool
	RequireRegular         bool
	RequireExecutable      bool
	RequireRootOwner       bool
	ForbiddenMode          os.FileMode
	RequireTrustedAncestry bool
	AncestryOwner          uint32
	AncestryForbiddenMode  os.FileMode

	// AllowRootOwnedStickyAncestry permits an otherwise forbidden group/world
	// write boundary only when both the leaf and every non-sticky ancestor are
	// required to be root-owned and non-writable. This models a root-owned
	// sticky runtime boundary such as OpenWrt's /tmp without changing the
	// stronger default used by executable callers.
	AllowRootOwnedStickyAncestry bool

	// RequireStablePath binds the owner-selected logical label and resolved
	// canonical path to the opened descriptor at Open and Revalidate. Owners
	// that intentionally keep using an already-open object across pathname
	// replacement leave this false.
	RequireStablePath bool
}

type Identity struct {
	Label        string      `json:"label"`
	ResolvedPath string      `json:"resolvedPath"`
	Device       uint64      `json:"device"`
	Inode        uint64      `json:"inode"`
	Size         int64       `json:"size"`
	Mode         os.FileMode `json:"mode"`
	UID          uint32      `json:"uid"`
	GID          uint32      `json:"gid"`
	// Digest is the lowercase SHA-256 of the executable's raw content bytes.
	// It is not a revision of this Identity's JSON representation.
	Digest string `json:"digest"`
}
