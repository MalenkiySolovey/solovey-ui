package backup

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func ownedPathIsDirect(original, resolved string) bool {
	// EvalSymlinks also expands Windows short names. Compare the OS's long
	// lexical form before accepting an alias; actual reparse-point traversal
	// still changes that form and remains forbidden. Prove object identity as
	// well, so case-sensitive directories cannot turn case folding into trust.
	input, err := windows.UTF16PtrFromString(original)
	if err != nil {
		return false
	}
	buffer := make([]uint16, 32768)
	length, err := windows.GetLongPathName(input, &buffer[0], uint32(len(buffer)))
	if err != nil || length == 0 || length >= uint32(len(buffer)) ||
		!strings.EqualFold(filepath.Clean(windows.UTF16ToString(buffer[:length])), filepath.Clean(resolved)) {
		return false
	}
	before, err := os.Lstat(original)
	if err != nil || before.Mode()&os.ModeSymlink != 0 {
		return false
	}
	after, err := os.Lstat(resolved)
	return err == nil && os.SameFile(before, after)
}
