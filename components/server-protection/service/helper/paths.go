package helper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrManagedPathForbidden = errors.New("managed path is forbidden")
)

type ManagedRoot struct {
	path string
}

func NewManagedRoot(path string) (ManagedRoot, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ManagedRoot{}, err
	}
	absolute = filepath.Clean(absolute)
	if filepath.Base(absolute) != "server-protection" || filepath.Base(filepath.Dir(absolute)) != ".runtime" {
		return ManagedRoot{}, errors.New("managed root must end in .runtime/server-protection")
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return ManagedRoot{}, fmt.Errorf("inspect managed root: %w", err)
	}
	if !info.IsDir() {
		return ManagedRoot{}, errors.New("managed root is not a directory")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return ManagedRoot{}, fmt.Errorf("resolve managed root: %w", err)
	}
	resolved = filepath.Clean(resolved)
	if err := validateResolvedManagedRoot(resolved); err != nil {
		return ManagedRoot{}, err
	}
	return ManagedRoot{path: resolved}, nil
}

func validateResolvedManagedRoot(path string) error {
	path = filepath.Clean(path)
	if filepath.Base(path) != "server-protection" || filepath.Base(filepath.Dir(path)) != ".runtime" {
		return fmt.Errorf("%w: resolved root escapes .runtime/server-protection", ErrManagedPathForbidden)
	}
	return nil
}

func (r ManagedRoot) Path() string { return r.path }

func (r ManagedRoot) Resolve(relative string, mustExist bool) (string, error) {
	return r.resolve(relative, mustExist, filepath.EvalSymlinks)
}

// ResolveNoSymlink is used for nginx candidate and revision identities. Every
// existing path segment, including the final file, must be non-symlinked.
func (r ManagedRoot) ResolveNoSymlink(relative string, mustExist bool) (string, error) {
	resolved, err := r.Resolve(relative, mustExist)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(r.path, filepath.Clean(relative))
	current := r.path
	rel, err := filepath.Rel(r.path, candidate)
	if err != nil {
		return "", fmt.Errorf("%w: managed path cannot be related", ErrManagedPathForbidden)
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) && !mustExist && index == len(parts)-1 {
			break
		}
		if statErr != nil {
			return "", fmt.Errorf("%w: managed path cannot be inspected", ErrManagedPathForbidden)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: symlinked managed path is forbidden", ErrManagedPathForbidden)
		}
	}
	return resolved, nil
}

func (r ManagedRoot) resolve(relative string, mustExist bool, eval func(string) (string, error)) (string, error) {
	if r.path == "" {
		return "", fmt.Errorf("%w: managed root is not initialized", ErrManagedPathForbidden)
	}
	if relative == "" || len(relative) > 1024 || strings.ContainsRune(relative, 0) ||
		strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, `\`) ||
		filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", fmt.Errorf("%w: path must be a non-empty managed-root-relative path", ErrManagedPathForbidden)
	}
	for _, part := range strings.Split(strings.ReplaceAll(relative, `\`, "/"), "/") {
		if part == ".." {
			return "", fmt.Errorf("%w: path traversal is forbidden", ErrManagedPathForbidden)
		}
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path traversal is forbidden", ErrManagedPathForbidden)
	}
	candidate := filepath.Join(r.path, clean)
	resolved, err := resolveWithExistingParent(candidate, mustExist, eval)
	if err != nil {
		return "", fmt.Errorf("%w: managed path cannot be resolved", ErrManagedPathForbidden)
	}
	if !pathWithin(r.path, resolved) {
		return "", fmt.Errorf("%w: path escapes the Solovey-managed runtime root", ErrManagedPathForbidden)
	}
	return resolved, nil
}

func resolveWithExistingParent(candidate string, mustExist bool, eval func(string) (string, error)) (string, error) {
	resolved, err := eval(candidate)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if mustExist || !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("resolve managed path: %w", err)
	}
	missing := make([]string, 0, 4)
	parent := candidate
	for {
		if _, statErr := os.Lstat(parent); statErr == nil {
			break
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("inspect managed path: %w", statErr)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", errors.New("managed path has no existing parent")
		}
		missing = append(missing, filepath.Base(parent))
		parent = next
	}
	resolvedParent, err := eval(parent)
	if err != nil {
		return "", fmt.Errorf("resolve managed parent: %w", err)
	}
	for index := len(missing) - 1; index >= 0; index-- {
		resolvedParent = filepath.Join(resolvedParent, missing[index])
	}
	return filepath.Clean(resolvedParent), nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
