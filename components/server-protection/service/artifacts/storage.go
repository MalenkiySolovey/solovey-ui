// Package artifacts owns rollback and recovery files below the
// .runtime/server-protection component root. It never invokes system tools.
package artifacts

import (
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
	ManifestVersion = 1
	directoryMode   = 0o700
	fileMode        = 0o600
)

var (
	ErrPathForbidden    = errors.New("artifact path is forbidden")
	ErrChecksumMismatch = errors.New("artifact checksum mismatch")
	ErrRevisionExists   = errors.New("artifact revision already exists")
	safeSegment         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	internalOperationID = regexp.MustCompile(`^operation-[a-f0-9]{32}$`)
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
	root string
	now  func() time.Time
	mu   sync.Mutex
}

func New(root string) (*Storage, error) {
	return NewWithClock(root, time.Now)
}

func NewWithClock(root string, now func() time.Time) (*Storage, error) {
	if now == nil {
		now = time.Now
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	absolute = filepath.Clean(absolute)
	if filepath.Base(absolute) != "server-protection" || filepath.Base(filepath.Dir(absolute)) != ".runtime" {
		return nil, fmt.Errorf("%w: root must end in .runtime/server-protection", ErrPathForbidden)
	}
	if err := os.MkdirAll(absolute, directoryMode); err != nil {
		return nil, err
	}
	if err := os.Chmod(absolute, directoryMode); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	resolved = filepath.Clean(resolved)
	if filepath.Base(resolved) != "server-protection" || filepath.Base(filepath.Dir(resolved)) != ".runtime" {
		return nil, fmt.Errorf("%w: resolved root escapes the component runtime directory", ErrPathForbidden)
	}
	storage := &Storage{root: resolved, now: now}
	for _, name := range []string{"revisions", "operations", "recovery"} {
		if _, err := storage.ensureDir(name); err != nil {
			return nil, err
		}
	}
	return storage, nil
}

func (s *Storage) Root() string { return s.root }

// WriteRevision atomically writes every file and then publishes manifest.json.
// The manifest is the commit marker: readers ignore incomplete sets without it.
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
	s.mu.Lock()
	defer s.mu.Unlock()

	relativeDir := filepath.ToSlash(filepath.Join("revisions", revision))
	if manifestPath, resolveErr := s.resolve(filepath.ToSlash(filepath.Join(relativeDir, "manifest.json")), false); resolveErr == nil {
		if _, statErr := os.Stat(manifestPath); statErr == nil {
			return WrittenSet{}, ErrRevisionExists
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return WrittenSet{}, statErr
		}
	}
	if _, err := s.ensureDir(relativeDir); err != nil {
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
	for _, name := range names {
		data := files[name]
		path := filepath.ToSlash(filepath.Join(relativeDir, name))
		if err := s.atomicWrite(path, data); err != nil {
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
	if err := s.atomicWrite(filepath.ToSlash(filepath.Join(relativeDir, "manifest.json")), manifestData); err != nil {
		return WrittenSet{}, err
	}
	manifestSum := sha256.Sum256(manifestData)
	if err := s.writeOperationRevision(operationID, revision, hex.EncodeToString(manifestSum[:])); err != nil {
		_ = os.RemoveAll(filepath.Join(s.root, filepath.FromSlash(relativeDir)))
		return WrittenSet{}, err
	}
	return WrittenSet{RelativePath: relativeDir, ManifestSHA256: hex.EncodeToString(manifestSum[:]), Bytes: total + int64(len(manifestData)), Manifest: manifest}, nil
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
	return s.atomicWrite(filepath.ToSlash(filepath.Join(relativeDir, "revision.json")), append(data, '\n'))
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
	if manifest.Version != ManifestVersion || manifest.Revision != revision {
		return Manifest{}, errors.New("artifact manifest identity is invalid")
	}
	for _, file := range manifest.Files {
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
	clean, err := cleanRelative(relative)
	if err != nil {
		return err
	}
	if clean == "revisions" || clean == "operations" || clean == "recovery" {
		return fmt.Errorf("%w: top-level artifact directories cannot be removed", ErrPathForbidden)
	}
	path, err := s.resolve(clean, false)
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func (s *Storage) DropAll() error {
	if s == nil || s.root == "" {
		return nil
	}
	return os.RemoveAll(s.root)
}

func (s *Storage) atomicWrite(relative string, data []byte) error {
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
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		cleanup()
		return err
	}
	return os.Chmod(path, fileMode)
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
	if err := os.MkdirAll(path, directoryMode); err != nil {
		return "", err
	}
	if err := os.Chmod(path, directoryMode); err != nil {
		return "", err
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

func within(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
