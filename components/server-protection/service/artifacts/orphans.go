package artifacts

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type OrphanSweepResult struct {
	Enumerated   int
	Deleted      int
	DeletedBytes int64
	Preserved    int
}

// SweepOwnedOrphans removes only old entries whose names and authenticated
// contents prove that they belong to this storage owner. Unknown names,
// symlinks, malformed revision directories, and arbitrary foreign files are
// preserved. The caller supplies current metadata and the full live operation
// closure so an in-flight or legally consumable generation cannot be swept.
func (s *Storage) SweepOwnedOrphans(knownPaths, knownOperations map[string]struct{}, protected map[string]string, cutoff int64) (OrphanSweepResult, error) {
	if s == nil || cutoff <= 0 {
		return OrphanSweepResult{}, errors.New("artifact orphan sweep is not initialized")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := OrphanSweepResult{}
	if err := s.sweepPublicationDebt(knownOperations, protected, cutoff, &result); err != nil {
		return result, err
	}
	if err := s.sweepUntrackedRevisions(knownPaths, protected, cutoff, &result); err != nil {
		return result, err
	}
	for _, root := range []string{"operations", "recovery"} {
		if err := s.sweepUnreferencedOperationDirectories(root, knownOperations, protected, cutoff, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *Storage) sweepPublicationDebt(knownOperations map[string]struct{}, protected map[string]string, cutoff int64, result *OrphanSweepResult) error {
	root := filepath.Join(s.root, "publication")
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !ownedPublication.MatchString(entry.Name()) {
			result.Preserved++
			continue
		}
		operationID := strings.SplitN(entry.Name(), "--", 2)[0]
		if _, known := knownOperations[operationID]; known {
			result.Preserved++
			continue
		}
		if _, live := protected[operationID]; live {
			result.Preserved++
			continue
		}
		// The publication directory name itself is the owned namespace: it binds
		// the product-generated operation id to the revision digest. This also
		// covers a crash in the tiny window between directory creation and the
		// owner marker write. Final revisions require the marker because their
		// stable names are intentionally consumer-facing.
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		result.Enumerated++
		if info.ModTime().Unix() >= cutoff {
			result.Preserved++
			continue
		}
		relative := filepathSlash("publication", entry.Name())
		bytes, sizeErr := ownedTreeBytes(filepath.Join(root, entry.Name()))
		if sizeErr != nil {
			return sizeErr
		}
		if err := s.removeLocked(relative); err != nil {
			return err
		}
		result.Deleted++
		result.DeletedBytes = saturatingAdd(result.DeletedBytes, bytes)
	}
	return nil
}

func (s *Storage) sweepUntrackedRevisions(knownPaths map[string]struct{}, protected map[string]string, cutoff int64, result *OrphanSweepResult) error {
	root := filepath.Join(s.root, "revisions")
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		relative := filepathSlash("revisions", entry.Name())
		if _, known := knownPaths[relative]; known {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || validateSegment(entry.Name()) != nil {
			result.Preserved++
			continue
		}
		manifest, verifyErr := s.verifyRevision(entry.Name(), "")
		if verifyErr != nil {
			// Without the exact manifest identity this directory is not proven to
			// be ours. Preserve it rather than treating a name as ownership.
			result.Preserved++
			continue
		}
		if _, live := protected[manifest.OperationID]; live || manifest.CreatedAt >= cutoff {
			result.Preserved++
			continue
		}
		if !s.validPublicationOwner(filepath.Join(root, entry.Name()), manifest.OperationID, manifest.Revision) {
			result.Preserved++
			continue
		}
		result.Enumerated++
		bytes, sizeErr := ownedTreeBytes(filepath.Join(root, entry.Name()))
		if sizeErr != nil {
			return sizeErr
		}
		if err := s.removeLocked(relative); err != nil {
			return err
		}
		result.Deleted++
		result.DeletedBytes = saturatingAdd(result.DeletedBytes, bytes)
	}
	return nil
}

func (s *Storage) validPublicationOwner(directory, operationID, revision string) bool {
	data, err := os.ReadFile(filepath.Join(directory, publicationOwnerName))
	if err != nil {
		return false
	}
	var owner struct {
		Version     int    `json:"version"`
		OperationID string `json:"operationId"`
		Revision    string `json:"revision"`
	}
	if json.Unmarshal(data, &owner) != nil || owner.Version != 1 || owner.OperationID != operationID || validateOperationID(owner.OperationID) != nil || owner.Revision == "" || validateSegment(owner.Revision) != nil {
		return false
	}
	return revision == "" || owner.Revision == revision
}

func (s *Storage) sweepUnreferencedOperationDirectories(rootName string, knownOperations map[string]struct{}, protected map[string]string, cutoff int64, result *OrphanSweepResult) error {
	root := filepath.Join(s.root, rootName)
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || validateOperationID(entry.Name()) != nil {
			result.Preserved++
			continue
		}
		if _, known := knownOperations[entry.Name()]; known {
			continue
		}
		if _, live := protected[entry.Name()]; live {
			result.Preserved++
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.ModTime().Unix() >= cutoff {
			result.Preserved++
			continue
		}
		result.Enumerated++
		path := filepath.Join(root, entry.Name())
		bytes, sizeErr := ownedTreeBytes(path)
		if sizeErr != nil {
			return sizeErr
		}
		if err := s.removeLocked(filepathSlash(rootName, entry.Name())); err != nil {
			return err
		}
		result.Deleted++
		result.DeletedBytes = saturatingAdd(result.DeletedBytes, bytes)
	}
	return nil
}

func ownedTreeBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total = saturatingAdd(total, info.Size())
		return nil
	})
	return total, err
}

func saturatingAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	if right < 0 && left < math.MinInt64-right {
		return math.MinInt64
	}
	return left + right
}
