// Package ssmcache owns credential-bearing SSM files at the deployment's data
// path. SSM itself owns cache schema, authentication and service counters.
package ssmcache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/sagernet/sing-box/service/ssmapi"
)

func Root() string {
	name, err := filepath.Abs(filepath.Join(storage.GetDBFolderPath(), "ssm"))
	if err != nil {
		return ""
	}
	return name
}

// PreparePrivate is used only during owner-approved restore publication. Dry
// construction, validation and restore rehearsal never call it.
func (s *Store) PreparePrivate() error {
	dir, err := s.openRoot(true)
	if err == nil {
		dir.Close()
	}
	return err
}

type Store struct {
	root, relative string
	seed           bool
	access         sync.Mutex
}

func New(name string) (*Store, error) {
	root := Root()
	if root == "" || !filepath.IsAbs(name) || strings.ContainsAny(name, "\x00\r\n") {
		return nil, errors.New("SSM_CACHE_PATH_UNOWNED")
	}
	relative, err := filepath.Rel(root, filepath.Clean(name))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("SSM_CACHE_PATH_UNOWNED")
	}
	if len(relative) > 1024 || len(strings.Split(relative, string(filepath.Separator))) > 8 {
		return nil, errors.New("SSM_CACHE_PATH_UNOWNED")
	}
	store := &Store{root: root, relative: relative, seed: isRestoreSeed(relative)}
	if directory, err := store.openRoot(false); err == nil {
		err = store.checkParents(directory, relative)
		if err == nil {
			err = store.checkExisting(directory, relative)
		}
		if err == nil && store.seed {
			err = store.checkExisting(directory, relative+".state")
		}
		directory.Close()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return store, nil
}

// All ancestor checks stay below the already anchored private owner root.
// Neither validation nor construction creates files or reads credential bytes.
func (s *Store) checkParents(dir *os.Root, name string) error {
	parent := filepath.Dir(name)
	if parent == "." {
		return nil
	}
	partial := ""
	for _, part := range strings.Split(parent, string(filepath.Separator)) {
		partial = filepath.Join(partial, part)
		info, err := dir.Lstat(partial)
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
		}
		f, err := dir.Open(partial)
		if err != nil {
			return errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
		}
		opened, err := f.Stat()
		if err == nil && !os.SameFile(info, opened) {
			err = errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
		}
		if err == nil {
			err = checkPrivate(f)
		}
		f.Close()
		if err != nil {
			return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
		}
	}
	return nil
}

func (s *Store) checkExisting(dir *os.Root, name string) error {
	info, err := dir.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("SSM_CACHE_FILE_INVALID")
	}
	f, err := dir.Open(name)
	if err != nil {
		return errors.New("SSM_CACHE_FILE_INVALID")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() > ssmapi.MaxCacheBytes {
		return errors.New("SSM_CACHE_FILE_INVALID")
	}
	if checkPrivate(f) != nil {
		return errors.New("SSM_CACHE_FILE_NOT_PRIVATE")
	}
	return nil
}

func (s *Store) openRoot(create bool) (*os.Root, error) {
	data, err := os.OpenRoot(filepath.Dir(s.root))
	if err != nil {
		return nil, errors.New("SSM_CACHE_DATA_ROOT_UNAVAILABLE")
	}
	defer data.Close()
	name := filepath.Base(s.root)
	info, err := data.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		created := false
		if err = data.Mkdir(name, 0700); err == nil {
			created = true
		} else if !errors.Is(err, os.ErrExist) {
			return nil, errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
		}
		dir, e := data.OpenRoot(name)
		if e != nil {
			return nil, errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
		}
		file, e := dir.Open(".")
		if e == nil {
			if created {
				e = secureCreated(file)
			}
			if e == nil {
				e = checkPrivate(file)
			}
			file.Close()
		}
		dir.Close()
		if e != nil {
			return nil, errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
		}
		info, err = data.Lstat(name)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
	}
	dir, err := data.OpenRoot(name)
	if err != nil {
		return nil, errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
	}
	file, err := dir.Open(".")
	if err == nil {
		err = checkPrivate(file)
		file.Close()
	}
	if err != nil {
		dir.Close()
		return nil, errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
	}
	return dir, nil
}

func (s *Store) Read(ctx context.Context) ([]byte, error) {
	s.access.Lock()
	defer s.access.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := s.openRoot(false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	name := s.relative
	if err = s.checkParents(dir, name); err != nil {
		return nil, err
	}
	if s.seed {
		if _, err = dir.Lstat(name + ".state"); err == nil {
			name += ".state"
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("SSM_CACHE_FILE_INVALID")
		}
	}
	info, err := dir.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("SSM_CACHE_FILE_INVALID")
	}
	f, err := dir.Open(name)
	if err != nil {
		return nil, errors.New("SSM_CACHE_FILE_INVALID")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() > ssmapi.MaxCacheBytes {
		return nil, errors.New("SSM_CACHE_FILE_INVALID")
	}
	if err = checkPrivate(f); err != nil {
		return nil, errors.New("SSM_CACHE_FILE_NOT_PRIVATE")
	}
	data, err := io.ReadAll(io.LimitReader(f, ssmapi.MaxCacheBytes+1))
	if err != nil || len(data) > ssmapi.MaxCacheBytes {
		return nil, errors.New("SSM_CACHE_FILE_EXCESSIVE")
	}
	if err = ssmapi.ValidateCache(data); err != nil {
		return nil, err
	}
	final, err := f.Stat()
	current, pathErr := dir.Lstat(name)
	if err != nil || pathErr != nil || !os.SameFile(opened, current) || final.Size() != opened.Size() || !final.ModTime().Equal(opened.ModTime()) || checkPrivate(f) != nil {
		return nil, errors.New("SSM_CACHE_FILE_CHANGED")
	}
	return data, ctx.Err()
}

func (s *Store) Write(ctx context.Context, data []byte) error {
	s.access.Lock()
	defer s.access.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ssmapi.ValidateCache(data); err != nil {
		return err
	}
	dir, err := s.openRoot(true)
	if err != nil {
		return err
	}
	defer dir.Close()
	name := s.relative
	if s.seed {
		name += ".state"
	} // immutable restore seed is never overwritten
	parent := filepath.Dir(name)
	if parent != "." {
		partial := ""
		for _, part := range strings.Split(parent, string(filepath.Separator)) {
			partial = filepath.Join(partial, part)
			created := false
			if err = dir.Mkdir(partial, 0700); err == nil {
				created = true
			} else if !errors.Is(err, os.ErrExist) {
				return errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
			}
			info, e := dir.Lstat(partial)
			if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
			}
			f, e := dir.Open(partial)
			if e != nil {
				return errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
			}
			if created {
				e = secureCreated(f)
			}
			if e == nil {
				e = checkPrivate(f)
			}
			f.Close()
			if e != nil {
				return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
			}
		}
	}
	if info, e := dir.Lstat(name); e == nil {
		if !info.Mode().IsRegular() {
			return errors.New("SSM_CACHE_FILE_INVALID")
		}
		f, e := dir.Open(name)
		if e != nil {
			return errors.New("SSM_CACHE_FILE_INVALID")
		}
		e = checkPrivate(f)
		f.Close()
		if e != nil {
			return errors.New("SSM_CACHE_FILE_NOT_PRIVATE")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return errors.New("SSM_CACHE_FILE_INVALID")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
	}
	temporary := filepath.Join(parent, ".ssm-"+hex.EncodeToString(nonce[:])+".tmp")
	f, err := dir.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
	}
	defer dir.Remove(temporary)
	if err = secureCreated(f); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = dir.Rename(temporary, name); err != nil {
		return errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
	}
	return nil
}
