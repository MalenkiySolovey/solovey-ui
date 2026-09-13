//go:build linux

package mountevidence

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func Observe(target string) (Fact, error) {
	logical := filepath.ToSlash(filepath.Clean(target))
	if !canonicalAbsolute(logical) {
		return Fact{}, ErrUnavailable
	}
	resolved, err := filepath.EvalSymlinks(logical)
	if err != nil {
		return Fact{}, err
	}
	resolved = filepath.ToSlash(filepath.Clean(resolved))
	file, err := os.Open("/proc/self/mountinfo") // #nosec G304 -- fixed kernel evidence path.
	if err != nil {
		return Fact{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxMountInfo+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) == 0 || len(data) > MaxMountInfo {
		return Fact{}, errors.Join(ErrUnavailable, readErr, closeErr)
	}
	fact, err := Parse(data, resolved)
	if err != nil {
		return Fact{}, err
	}
	// Parse selects the mount by canonical filesystem path. Preserve the
	// caller's logical path as a separate sealed proposition instead of
	// rewriting the observed filesystem reality back into that namespace.
	fact.Target = logical
	var state unix.Statfs_t
	if err := unix.Statfs(resolved, &state); err != nil {
		return Fact{}, err
	}
	return BindStatfs(fact, resolved, state.Flags&unix.ST_RDONLY != 0, int64(state.Type))
}

func ObserveOverlayLabel(label string) (OverlayLabelFact, error) {
	label = filepath.ToSlash(filepath.Clean(label))
	if !canonicalAbsolute(label) {
		return OverlayLabelFact{Label: label, Visibility: OverlayLabelInvalid}, errors.New("overlay label is malformed")
	}
	if _, err := os.Lstat(label); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fact := OverlayLabelFact{Label: label, Visibility: OverlayLabelOpaque}
			return fact, fact.Seal()
		}
		return OverlayLabelFact{Label: label, Visibility: OverlayLabelInvalid}, err
	}
	mount, err := Observe(label)
	if err != nil {
		return OverlayLabelFact{Label: label, Visibility: OverlayLabelInvalid}, err
	}
	fact := OverlayLabelFact{Label: label, Visibility: OverlayLabelVisible, Mount: mount}
	return fact, fact.Seal()
}

func AvailableBytes(target string) (uint64, error) {
	var state unix.Statfs_t
	if err := unix.Statfs(target, &state); err != nil {
		return 0, err
	}
	if state.Bsize <= 0 {
		return 0, ErrUnavailable
	}
	return state.Bavail * uint64(state.Bsize), nil
}
