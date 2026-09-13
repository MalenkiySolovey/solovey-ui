// Package artifacts owns rollback and recovery files below the deployment-
// supplied Server Protection runtime root. It never invokes system tools.
package artifacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ManifestVersion      = 1
	directoryMode        = 0o700
	fileMode             = 0o600
	publicationOwnerName = "publication-owner.json"
)

var (
	ErrPathForbidden    = errors.New("artifact path is forbidden")
	ErrChecksumMismatch = errors.New("artifact checksum mismatch")
	ErrRevisionExists   = errors.New("artifact revision already exists")
	safeSegment         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	internalOperationID = regexp.MustCompile(`^operation-[a-f0-9]{32}$`)
	ownedPublication    = regexp.MustCompile(`^operation-[a-f0-9]{32}--[a-f0-9]{64}$`)
)

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Manifest struct {
	Version     int    `json:"version"`
	OperationID string `json:"operationId"`
	Revision    string `json:"revision"`
	CreatedAt   int64  `json:"createdAt"`
	Files       []File `json:"files"`
}

type WrittenSet struct {
	RelativePath   string
	ManifestSHA256 string
	Bytes          int64
	Manifest       Manifest
}

type Storage struct {
	root               string
	now                func() time.Time
	recoveryProjection RecoveryProjection
	publicationFault   func(string) error
	mu                 sync.Mutex
}

func New(root string) (*Storage, error) {
	return newStorage(root, time.Now, RecoveryProjection{})
}

func NewWithClock(root string, now func() time.Time) (*Storage, error) {
	return newStorage(root, now, RecoveryProjection{})
}

func NewWithRecoveryProjection(root string, projection RecoveryProjection) (*Storage, error) {
	if err := projection.Validate(); err != nil {
		return nil, err
	}
	return newStorage(root, time.Now, projection)
}

// NewInstalledWithRecoveryProjection consumes a deployment-created component
// root. The component owns only its semantic child directories and must never
// recreate or chmod the deployment boundary itself.
func NewInstalledWithRecoveryProjection(root string, projection RecoveryProjection) (*Storage, error) {
	if err := projection.validateInstalled(); err != nil {
		return nil, err
	}
	return newStorageWithRootPolicy(root, time.Now, projection, false)
}

func newStorage(root string, now func() time.Time, projection RecoveryProjection) (*Storage, error) {
	return newStorageWithRootPolicy(root, now, projection, true)
}

func newStorageWithRootPolicy(root string, now func() time.Time, projection RecoveryProjection, createRoot bool) (*Storage, error) {
	if now == nil {
		now = time.Now
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	absolute = filepath.Clean(absolute)
	if filepath.Base(absolute) != "server-protection" {
		return nil, fmt.Errorf("%w: root must be the Server Protection component directory", ErrPathForbidden)
	}
	if info, statErr := os.Lstat(absolute); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: root must be a non-symlink directory", ErrPathForbidden)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	} else if !createRoot {
		return nil, fmt.Errorf("%w: deployment-created root is absent", ErrPathForbidden)
	} else if err := os.MkdirAll(absolute, directoryMode); err != nil {
		return nil, err
	}
	if createRoot {
		if err := os.Chmod(absolute, directoryMode); err != nil {
			return nil, err
		}
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	resolved = filepath.Clean(resolved)
	if filepath.Base(resolved) != "server-protection" {
		return nil, fmt.Errorf("%w: resolved root is not the component runtime directory", ErrPathForbidden)
	}
	if err := validateStorageRootOwnership(resolved, directoryMode); err != nil {
		return nil, err
	}
	storage := &Storage{root: resolved, now: now, recoveryProjection: projection}
	for _, name := range []string{"revisions", "operations", "recovery", "publication"} {
		if _, err := storage.ensureDir(name); err != nil {
			return nil, err
		}
	}
	return storage, nil
}

func (s *Storage) Root() string { return s.root }

// WriteRevision constructs an owned set below publication/, writes its manifest
// last, and atomically renames the complete directory into revisions/. A failed
// pre-rename publication is immediately removed or remains enumerable by its
// operation-bound publication name. A post-rename failure leaves a complete,
// self-authenticating revision that the orphan sweeper can reclaim.
func (s *Storage) WriteRevision(operationID, revision string, files map[string][]byte) (WrittenSet, error) {
	if err := validateOperationID(operationID); err != nil {
		return WrittenSet{}, err
	}
	if err := validateSegment(revision); err != nil {
		return WrittenSet{}, err
	}
	if len(files) == 0 {
		return WrittenSet{}, errors.New("at least one rollback artifact is required")
	}
	if _, reserved := files[publicationOwnerName]; reserved {
		return WrittenSet{}, fmt.Errorf("%w: publication owner name is reserved", ErrPathForbidden)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeRevisionLocked(operationID, revision, files)
}

func (s *Storage) writeRevisionLocked(operationID, revision string, files map[string][]byte) (WrittenSet, error) {
	finalRelative := filepath.ToSlash(filepath.Join("revisions", revision))
	finalPath, err := s.resolve(finalRelative, false)
	if err != nil {
		return WrittenSet{}, err
	}
	if _, statErr := os.Lstat(finalPath); statErr == nil {
		return WrittenSet{}, ErrRevisionExists
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return WrittenSet{}, statErr
	}
	stageName := publicationStageName(operationID, revision)
	stageRelative := filepath.ToSlash(filepath.Join("publication", stageName))
	stagePath, err := s.resolve(stageRelative, false)
	if err != nil {
		return WrittenSet{}, err
	}
	if _, statErr := os.Lstat(stagePath); statErr == nil {
		return WrittenSet{}, ErrRevisionExists
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return WrittenSet{}, statErr
	}
	if _, err := s.ensureDir(stageRelative); err != nil {
		return WrittenSet{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = s.removeLocked(stageRelative)
		}
	}()
	ownerData, err := marshalPublicationOwner(operationID, revision)
	if err != nil {
		return WrittenSet{}, err
	}
	if err := s.atomicWritePublication(filepath.ToSlash(filepath.Join(stageRelative, publicationOwnerName)), ownerData, "publication_owner"); err != nil {
		return WrittenSet{}, err
	}
	if err := s.publicationStep("directory_create"); err != nil {
		return WrittenSet{}, err
	}
	manifest := Manifest{Version: ManifestVersion, OperationID: operationID, Revision: revision, CreatedAt: s.now().Unix()}
	names := make([]string, 0, len(files))
	for name := range files {
		if name == "manifest.json" {
			return WrittenSet{}, fmt.Errorf("%w: manifest name is reserved", ErrPathForbidden)
		}
		if _, err := cleanRelative(name); err != nil {
			return WrittenSet{}, err
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var total int64
	for index, name := range names {
		data := files[name]
		path := filepath.ToSlash(filepath.Join(stageRelative, name))
		phase := "file"
		if index > 0 {
			phase = "subsequent_file"
		}
		if err := s.atomicWritePublication(path, data, phase); err != nil {
			return WrittenSet{}, err
		}
		sum := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, File{Path: filepath.ToSlash(name), SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))})
		total += int64(len(data))
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return WrittenSet{}, err
	}
	manifestData = append(manifestData, '\n')
	if err := s.atomicWritePublication(filepath.ToSlash(filepath.Join(stageRelative, "manifest.json")), manifestData, "manifest"); err != nil {
		return WrittenSet{}, err
	}
	manifestSum := sha256.Sum256(manifestData)
	if err := syncArtifactDirectory(stagePath); err != nil {
		return WrittenSet{}, err
	}
	if err := s.publicationStep("publication_directory_sync"); err != nil {
		return WrittenSet{}, err
	}
	if err := os.Rename(stagePath, finalPath); err != nil {
		return WrittenSet{}, err
	}
	published = true
	if err := s.publicationStep("revision_rename"); err != nil {
		return WrittenSet{}, err
	}
	if err := syncArtifactDirectory(filepath.Dir(finalPath)); err != nil {
		return WrittenSet{}, err
	}
	if err := s.publicationStep("revisions_directory_sync"); err != nil {
		return WrittenSet{}, err
	}
	manifestSHA := hex.EncodeToString(manifestSum[:])
	if _, err := s.verifyRevision(revision, manifestSHA); err != nil {
		return WrittenSet{}, err
	}
	if err := s.publicationStep("revision_reopen"); err != nil {
		return WrittenSet{}, err
	}
	if err := s.writeOperationRevision(operationID, revision, manifestSHA); err != nil {
		return WrittenSet{}, err
	}
	return WrittenSet{RelativePath: finalRelative, ManifestSHA256: manifestSHA, Bytes: total + int64(len(manifestData)), Manifest: manifest}, nil
}

func (s *Storage) writeOperationRevision(operationID, revision, manifestSHA string) error {
	relativeDir := filepath.ToSlash(filepath.Join("operations", operationID))
	if _, err := s.ensureDir(relativeDir); err != nil {
		return err
	}
	pointer := struct {
		OperationID    string `json:"operationId"`
		Revision       string `json:"revision"`
		ManifestSHA256 string `json:"manifestSha256"`
	}{operationID, revision, manifestSHA}
	data, err := json.MarshalIndent(pointer, "", "  ")
	if err != nil {
		return err
	}
	return s.atomicWritePublication(filepath.ToSlash(filepath.Join(relativeDir, "revision.json")), append(data, '\n'), "pointer")
}

func marshalPublicationOwner(operationID, revision string) ([]byte, error) {
	owner := struct {
		Version     int    `json:"version"`
		OperationID string `json:"operationId"`
		Revision    string `json:"revision"`
	}{Version: 1, OperationID: operationID, Revision: revision}
	data, err := json.Marshal(owner)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (s *Storage) removePublicationOwner(relativeDir string) error {
	path, err := s.resolve(filepath.ToSlash(filepath.Join(relativeDir, publicationOwnerName)), true)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncArtifactDirectory(filepath.Dir(path))
}

// MarkMutation writes the durable marker used by crash recovery. The caller
// must do this immediately before a future helper mutation, never afterward.
func (s *Storage) MarkMutation(operationID, revision string) error {
	if err := validateOperationID(operationID); err != nil {
		return err
	}
	if err := validateSegment(revision); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(struct {
		OperationID string `json:"operationId"`
		Revision    string `json:"revision"`
		MarkedAt    int64  `json:"markedAt"`
	}{operationID, revision, s.now().Unix()})
	return s.atomicWrite(filepath.ToSlash(filepath.Join("operations", operationID, "mutation-marker.json")), append(data, '\n'))
}

func (s *Storage) HasMutationMarker(operationID string) bool {
	if validateOperationID(operationID) != nil {
		return false
	}
	path, err := s.resolve(filepath.ToSlash(filepath.Join("operations", operationID, "mutation-marker.json")), true)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func (s *Storage) VerifyRevision(revision, expectedManifestSHA string) (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verifyRevision(revision, expectedManifestSHA)
}

func (s *Storage) verifyRevision(revision, expectedManifestSHA string) (Manifest, error) {
	if err := validateSegment(revision); err != nil {
		return Manifest{}, err
	}
	manifestPath, err := s.resolve(filepath.ToSlash(filepath.Join("revisions", revision, "manifest.json")), true)
	if err != nil {
		return Manifest{}, err
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Manifest{}, err
	}
	manifestSum := sha256.Sum256(data)
	if expectedManifestSHA != "" && !strings.EqualFold(expectedManifestSHA, hex.EncodeToString(manifestSum[:])) {
		return Manifest{}, fmt.Errorf("%w: manifest", ErrChecksumMismatch)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.Version != ManifestVersion || manifest.Revision != revision || validateOperationID(manifest.OperationID) != nil || manifest.CreatedAt <= 0 || len(manifest.Files) == 0 {
		return Manifest{}, errors.New("artifact manifest identity is invalid")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		clean, cleanErr := cleanRelative(file.Path)
		if cleanErr != nil || clean == "manifest.json" || file.Bytes < 0 || safeSHA256(file.SHA256) != file.SHA256 {
			return Manifest{}, errors.New("artifact manifest member is invalid")
		}
		if _, duplicate := seen[clean]; duplicate {
			return Manifest{}, errors.New("artifact manifest member is duplicated")
		}
		seen[clean] = struct{}{}
		path, err := s.resolve(filepath.ToSlash(filepath.Join("revisions", revision, file.Path)), true)
		if err != nil {
			return Manifest{}, err
		}
		opened, err := os.Open(path)
		if err != nil {
			return Manifest{}, err
		}
		hash := sha256.New()
		written, copyErr := io.Copy(hash, opened)
		closeErr := opened.Close()
		if copyErr != nil || closeErr != nil {
			return Manifest{}, errors.Join(copyErr, closeErr)
		}
		if written != file.Bytes || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), file.SHA256) {
			return Manifest{}, fmt.Errorf("%w: %s", ErrChecksumMismatch, file.Path)
		}
	}
	return manifest, nil
}

func (s *Storage) Remove(relative string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removeLocked(relative)
}

func (s *Storage) removeLocked(relative string) error {
	clean, err := cleanRelative(relative)
	if err != nil {
		return err
	}
	if clean == "revisions" || clean == "operations" || clean == "recovery" || clean == "publication" {
		return fmt.Errorf("%w: top-level artifact directories cannot be removed", ErrPathForbidden)
	}
	path, err := s.resolve(clean, false)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return syncArtifactDirectory(filepath.Dir(path))
}

func (s *Storage) DropAll() error {
	if s == nil || s.root == "" {
		return nil
	}
	return os.RemoveAll(s.root)
}

func (s *Storage) atomicWrite(relative string, data []byte) error {
	return s.atomicWritePublication(relative, data, "")
}

func (s *Storage) atomicWritePublication(relative string, data []byte, phase string) error {
	clean, err := cleanRelative(relative)
	if err != nil {
		return err
	}
	path, err := s.resolve(clean, false)
	if err != nil {
		return err
	}
	if _, err := s.ensureDir(filepath.ToSlash(filepath.Dir(clean))); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".artifact-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(fileMode); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return err
	}
	if phase != "" {
		if err := s.publicationStep(phase + "_write"); err != nil {
			cleanup()
			return err
		}
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if phase != "" {
		if err := s.publicationStep(phase + "_sync"); err != nil {
			cleanup()
			return err
		}
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		cleanup()
		return err
	}
	if phase != "" {
		if err := s.publicationStep(phase + "_rename"); err != nil {
			return err
		}
	}
	if err := syncArtifactDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if phase != "" {
		if err := s.publicationStep(phase + "_directory_sync"); err != nil {
			return err
		}
	}
	reopened, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(reopened, data) {
		return errors.Join(errors.New("artifact publication reopen mismatch"), err)
	}
	if phase != "" {
		if err := s.publicationStep(phase + "_reopen"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Storage) ensureDir(relative string) (string, error) {
	clean, err := cleanRelative(relative)
	if err != nil {
		return "", err
	}
	path, err := s.resolve(clean, false)
	if err != nil {
		return "", err
	}
	_, statErr := os.Lstat(path)
	missing := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !missing {
		return "", statErr
	}
	if err := os.MkdirAll(path, directoryMode); err != nil {
		return "", err
	}
	if err := os.Chmod(path, directoryMode); err != nil {
		return "", err
	}
	if missing {
		if err := syncArtifactDirectory(filepath.Dir(path)); err != nil {
			return "", err
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !within(s.root, resolved) {
		return "", fmt.Errorf("%w: directory escapes artifact root", ErrPathForbidden)
	}
	return resolved, nil
}

func (s *Storage) resolve(relative string, mustExist bool) (string, error) {
	clean, err := cleanRelative(relative)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(s.root, filepath.FromSlash(clean))
	parent := candidate
	if !mustExist {
		parent = filepath.Dir(candidate)
	}
	for {
		resolved, evalErr := filepath.EvalSymlinks(parent)
		if evalErr == nil {
			if !within(s.root, resolved) {
				return "", fmt.Errorf("%w: symlink escape", ErrPathForbidden)
			}
			break
		}
		if !errors.Is(evalErr, os.ErrNotExist) {
			return "", fmt.Errorf("%w: path cannot be resolved", ErrPathForbidden)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", fmt.Errorf("%w: no existing parent", ErrPathForbidden)
		}
		parent = next
	}
	if mustExist {
		resolved, evalErr := filepath.EvalSymlinks(candidate)
		if evalErr != nil || !within(s.root, resolved) {
			return "", fmt.Errorf("%w: path cannot be resolved", ErrPathForbidden)
		}
		return resolved, nil
	}
	return candidate, nil
}

func cleanRelative(value string) (string, error) {
	if value == "" || len(value) > 1024 || strings.ContainsRune(value, 0) || filepath.IsAbs(value) || filepath.VolumeName(value) != "" || strings.HasPrefix(value, `/`) || strings.HasPrefix(value, `\`) {
		return "", fmt.Errorf("%w: path must be root-relative", ErrPathForbidden)
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." || !safeSegment.MatchString(part) {
			return "", fmt.Errorf("%w: invalid path segment", ErrPathForbidden)
		}
	}
	return filepath.ToSlash(filepath.Clean(normalized)), nil
}

func validateSegment(value string) error {
	if !safeSegment.MatchString(value) {
		return fmt.Errorf("%w: invalid identifier", ErrPathForbidden)
	}
	return nil
}

func validateOperationID(value string) error {
	if !internalOperationID.MatchString(value) {
		return fmt.Errorf("%w: operation identifier is not an internal generated id", ErrPathForbidden)
	}
	return nil
}

func publicationStageName(operationID, revision string) string {
	digest := sha256.Sum256([]byte(revision))
	return operationID + "--" + hex.EncodeToString(digest[:])
}

func (s *Storage) publicationStep(name string) error {
	if s.publicationFault == nil {
		return nil
	}
	return s.publicationFault(name)
}

func within(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
