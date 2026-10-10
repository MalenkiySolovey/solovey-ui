//go:build !windows

package backup

func ownedPathIsDirect(original, resolved string) bool {
	return original == resolved
}
