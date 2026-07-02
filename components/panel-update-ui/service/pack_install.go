package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

const maxComponentBundleBytes = 200 << 20

func componentsRoot() string {
	return filepath.Join(filepath.Dir(configstorage.GetDBFolderPath()), "components")
}

func ensureManifestOnlyPack(item manifest.Manifest) error {
	root := componentsRoot()
	target := filepath.Join(root, item.ID)
	if err := os.MkdirAll(target, 0o750); err != nil {
		return err
	}
	manifestPath := filepath.Join(target, "component.json")
	if _, err := os.Stat(manifestPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(manifestPath, data, 0o600)
}

func removeComponentPack(id string) error {
	if err := manifest.ValidateID(id); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(componentsRoot(), id))
}

func installComponentPackFromBundle(ctx context.Context, client HTTPDoer, bundleURL, checksumHex, id string) error {
	if err := manifest.ValidateID(id); err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{Timeout: releaseHTTPTimeout}
	}
	root := componentsRoot()
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	bundle, err := downloadVerifiedBundle(ctx, client, bundleURL, checksumHex, root)
	if err != nil {
		return err
	}
	defer os.Remove(bundle)

	incoming, err := os.MkdirTemp(root, id+".incoming.")
	if err != nil {
		return err
	}
	cleanupIncoming := true
	defer func() {
		if cleanupIncoming {
			_ = os.RemoveAll(incoming)
		}
	}()
	if err := extractComponentPack(bundle, incoming, id); err != nil {
		return err
	}
	if err := validateExtractedComponentPack(incoming, id); err != nil {
		return err
	}

	target := filepath.Join(root, id)
	previous := filepath.Join(root, fmt.Sprintf("%s.previous.%d", id, time.Now().UnixNano()))
	hadPrevious := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, previous); err != nil {
			return err
		}
		hadPrevious = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(incoming, target); err != nil {
		if hadPrevious {
			_ = os.Rename(previous, target)
		}
		return err
	}
	cleanupIncoming = false
	if hadPrevious {
		_ = os.RemoveAll(previous)
	}
	return nil
}

func downloadVerifiedBundle(ctx context.Context, client HTTPDoer, bundleURL, checksumHex, dir string) (string, error) {
	checksumHex = strings.ToLower(strings.TrimSpace(checksumHex))
	if checksumHex == "" {
		return "", errors.New("component bundle checksum is empty")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, bundleURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "solovey-ui-component-installer")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return "", fmt.Errorf("component bundle status %d", response.StatusCode)
	}
	tmp, err := os.CreateTemp(dir, ".component-bundle-*.tar.gz")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	hash := sha256.New()
	limited := &io.LimitedReader{R: response.Body, N: maxComponentBundleBytes + 1}
	written, err := io.Copy(io.MultiWriter(tmp, hash), limited)
	if err != nil {
		return "", err
	}
	if written > maxComponentBundleBytes {
		return "", fmt.Errorf("component bundle is larger than %d bytes", maxComponentBundleBytes)
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != checksumHex {
		return "", fmt.Errorf("component bundle checksum mismatch")
	}
	cleanup = false
	return tmpPath, nil
}

func extractComponentPack(bundlePath, targetDir, id string) error {
	file, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	prefix := "components/" + id + "/"
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(path.Clean(header.Name), "./")
		if name == "components/"+id {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		if rel == "" || rel == "." {
			continue
		}
		if strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
			return fmt.Errorf("unsafe component pack path: %s", header.Name)
		}
		found = true
		outPath := filepath.Join(targetDir, filepath.FromSlash(rel))
		if !strings.HasPrefix(filepath.Clean(outPath), filepath.Clean(targetDir)+string(os.PathSeparator)) && filepath.Clean(outPath) != filepath.Clean(targetDir) {
			return fmt.Errorf("unsafe component pack path: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(outPath, 0o750); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
				return err
			}
			if err := writeTarFile(outPath, reader, header.FileInfo().Mode().Perm()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported component pack entry type for %s", header.Name)
		}
	}
	if !found {
		return fmt.Errorf("component pack %q was not found in bundle", id)
	}
	return nil
}

func writeTarFile(outPath string, reader *tar.Reader, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	if mode&0o111 != 0 {
		mode = 0o700
	} else {
		mode = 0o600
	}
	file, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, reader)
	return err
}

func validateExtractedComponentPack(dir, id string) error {
	data, err := os.ReadFile(filepath.Join(dir, "component.json")) // #nosec G304 -- dir is a freshly created component pack dir.
	if err != nil {
		return err
	}
	var item manifest.Manifest
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	if item.ID != id {
		return fmt.Errorf("component pack id mismatch: expected %s, got %s", id, item.ID)
	}
	return item.Validate()
}
