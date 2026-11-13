//go:build !minimal

package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
)

// backendContent retains the accepted owned-path checks and filesystem
// compensation. Feature code receives only site/revision/template capabilities.
type backendContent struct{}

var templateIDPattern = regexp.MustCompile("^[a-z0-9][a-z0-9-]{0,63}$")

func validateTemplateID(id string) error {
	if !templateIDPattern.MatchString(id) {
		return errors.New("invalid fallback-html template id")
	}
	return nil
}
func validateRelativeContentPath(name string) error {
	if name == "." || !fs.ValidPath(name) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.ContainsAny(name, "\\:\x00") {
		return errors.New("invalid relative fallback-html content path")
	}
	return ensurePathInside("content", filepath.Join("content", filepath.FromSlash(name)))
}
func (backendContent) ReadAsset(a fallbackdomain.Asset) ([]byte, error) {
	return readOwnedRegularFile(assetRoot(a.SiteID), a.FilePath)
}
func (backendContent) CreateAsset(siteID uint, name string, data []byte) (string, error) {
	if siteID == 0 || name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\:") {
		return "", errors.New("invalid fallback-html asset name")
	}
	target := filepath.Join(assetRoot(siteID), name)
	if err := writeOwnedNewFile(assetRoot(siteID), target, data, 0o640); err != nil {
		return "", err
	}
	return target, nil
}
func (backendContent) RemoveCreatedAsset(siteID uint, target string) error {
	if err := ensurePathInside(assetRoot(siteID), target); err != nil {
		return err
	}
	return os.Remove(target)
}

type backendContentChange struct{ commit, rollback func() error }

func (c backendContentChange) Commit() error   { return c.commit() }
func (c backendContentChange) Rollback() error { return c.rollback() }
func (backendContent) StageAssetRemoval(a fallbackdomain.Asset) (ContentChange, error) {
	staged, changed, err := stageOwnedFileRemoval(assetRoot(a.SiteID), a.FilePath)
	if err != nil {
		return nil, err
	}
	return backendContentChange{commit: func() error {
		if changed {
			return os.Remove(staged)
		}
		return nil
	}, rollback: func() error {
		if changed {
			return os.Rename(staged, a.FilePath)
		}
		return nil
	}}, nil
}

type backendPublicationDraft struct {
	siteID uint
	root   string
}

func (backendContent) BeginPublication(siteID uint, version string) (PublicationDraft, error) {
	if siteID == 0 || version == "" || strings.IndexFunc(version, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return nil, errors.New("invalid fallback-html publication identity")
	}
	root := publishRoot(siteID, version)
	if err := ensureOwnedDir(root, root); err != nil {
		return nil, err
	}
	return &backendPublicationDraft{siteID: siteID, root: root}, nil
}
func (d *backendPublicationDraft) RootDir() string { return d.root }
func (d *backendPublicationDraft) WritePublicFile(publicPath string, data []byte) (string, error) {
	canonical, err := fallbackdomain.NormalizePublicPath(publicPath)
	if err != nil || canonical != publicPath {
		return "", errors.New("invalid fallback-html public file path")
	}
	relative := fallbackdomain.PublicPathToFilePath(publicPath)
	target := filepath.Join(d.root, relative)
	if err := writeOwnedNewFile(d.root, target, data, 0o640); err != nil {
		return "", err
	}
	return target, nil
}
func (d *backendPublicationDraft) WriteArtifact(part ArtifactPart, data []byte) error {
	if !part.Valid() {
		return errors.New("invalid fallback-html artifact part")
	}
	return writeOwnedNewFile(d.root, filepath.Join(d.root, string(part)), data, 0o640)
}
func (d *backendPublicationDraft) Discard() error { return removeOwnedPublishRoot(d.siteID, d.root) }
func validatePublishRoot(p fallbackdomain.Publish) error {
	base := filepath.Join(storageRoot(), "publishes", "site-"+uintString(p.SiteID))
	if p.SiteID == 0 || strings.TrimSpace(p.RootDir) == "" {
		return errors.New("publication storage is unavailable")
	}
	if err := ensurePathInside(base, p.RootDir); err != nil {
		return err
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(p.RootDir)
	if err != nil {
		return err
	}
	if rootAbs == baseAbs {
		return errors.New("publication root is not a revision")
	}
	return nil
}
func (backendContent) ReadPublishFile(p fallbackdomain.Publish, f fallbackdomain.PublishFile) ([]byte, error) {
	if p.ID == 0 || f.PublishID != p.ID {
		return nil, errors.New("publication file does not belong to this revision")
	}
	if err := validatePublishRoot(p); err != nil {
		return nil, err
	}
	return readOwnedRegularFile(p.RootDir, f.FilePath)
}
func (backendContent) ReadPublishMetadata(p fallbackdomain.Publish, part ArtifactPart) ([]byte, error) {
	if !part.Valid() {
		return nil, errors.New("invalid fallback-html artifact part")
	}
	if err := validatePublishRoot(p); err != nil {
		return nil, err
	}
	return readOwnedRegularFile(p.RootDir, filepath.Join(p.RootDir, string(part)))
}
func (backendContent) RemovePublication(siteID uint, root string) error {
	return removeOwnedPublishRoot(siteID, root)
}
func (backendContent) RemoveSite(siteID uint) error { return RemoveSiteStorage(siteID) }
func (backendContent) ReadTemplate(id, relative string) ([]byte, error) {
	if err := validateTemplateID(id); err != nil {
		return nil, err
	}
	if err := validateRelativeContentPath(relative); err != nil {
		return nil, err
	}
	return readOwnedRegularFile(templateRoot(id), filepath.Join(templateRoot(id), filepath.FromSlash(relative)))
}
func (backendContent) TemplateAvailable(id string) bool {
	return validateTemplateID(id) == nil && cachedTemplateAvailable(id)
}
func (backendContent) ReplaceTemplate(id string, files []TemplateFile) (ContentChange, error) {
	if err := validateTemplateID(id); err != nil {
		return nil, err
	}
	root := templateRoot(id)
	parent := filepath.Dir(root)
	if err := ensureOwnedDir(templateCacheRoot(), parent); err != nil {
		return nil, err
	}
	staged, err := os.MkdirTemp(parent, "."+safeArchiveName(id)+"-download-*")
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(staged)
		}
	}()
	for _, file := range files {
		if err := validateRelativeContentPath(file.Relative); err != nil {
			return nil, err
		}
		if err := writeOwnedNewFile(staged, filepath.Join(staged, filepath.FromSlash(file.Relative)), file.Data, 0o640); err != nil {
			return nil, err
		}
	}
	previous, hadPrevious, err := replaceTemplateRoot(root, staged)
	if err != nil {
		return nil, err
	}
	cleanup = false
	return backendContentChange{commit: func() error {
		if hadPrevious {
			return os.RemoveAll(previous)
		}
		return nil
	}, rollback: func() error { return restoreTemplateRoot(root, previous, hadPrevious) }}, nil
}
func (backendContent) RemoveTemplate(id string) (ContentChange, error) {
	if err := validateTemplateID(id); err != nil {
		return nil, err
	}
	root := templateRoot(id)
	previous, hadPrevious, err := stageTemplateRoot(root)
	if err != nil {
		return nil, err
	}
	return backendContentChange{commit: func() error {
		if hadPrevious {
			return os.RemoveAll(previous)
		}
		return nil
	}, rollback: func() error { return restoreTemplateRoot(root, previous, hadPrevious) }}, nil
}

var safeArchiveName = SafeArchiveName

func removeOwnedPublishRoot(siteID uint, root string) error {
	base := filepath.Join(storageRoot(), "publishes", "site-"+uintString(siteID))
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if rootAbs == baseAbs || !strings.HasPrefix(rootAbs, baseAbs+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to remove publish root outside site storage: %s", root)
	}
	return os.RemoveAll(rootAbs)
}

func replaceTemplateRoot(root, stagedRoot string) (string, bool, error) {
	previousRoot, hadPrevious, err := stageTemplateRoot(root)
	if err != nil {
		return "", false, err
	}
	if err := os.Rename(stagedRoot, root); err != nil {
		if restoreErr := restoreTemplateRoot(root, previousRoot, hadPrevious); restoreErr != nil {
			return "", false, errors.Join(err, restoreErr)
		}
		return "", false, err
	}
	return previousRoot, hadPrevious, nil
}

func stageTemplateRoot(root string) (string, bool, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", false, fmt.Errorf("refusing non-directory remote template storage %s", root)
	}
	temporary, err := os.CreateTemp(filepath.Dir(root), "."+filepath.Base(root)+"-previous-*")
	if err != nil {
		return "", false, err
	}
	previousRoot := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(previousRoot)
		return "", false, err
	}
	if err := os.Remove(previousRoot); err != nil {
		return "", false, err
	}
	if err := os.Rename(root, previousRoot); err != nil {
		return "", false, err
	}
	return previousRoot, true, nil
}

func restoreTemplateRoot(root, previousRoot string, hadPrevious bool) error {
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if hadPrevious {
		return os.Rename(previousRoot, root)
	}
	return nil
}

func cachedTemplateAvailable(templateID string) bool {
	if templateCacheRoot() == filepath.Join(storageRoot(), "templates") {
		return true
	}
	info, err := os.Lstat(filepath.Join(templateRoot(templateID), "manifest.json"))
	return err == nil && info.Mode().IsRegular()
}
