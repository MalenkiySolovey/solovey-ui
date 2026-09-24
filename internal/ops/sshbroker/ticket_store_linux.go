//go:build linux

package sshbroker

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
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	maxProofTicketEntries        = 128
	maxProofTicketBytes          = 16 << 10
	maxProofTicketAggregateBytes = maxProofTicketEntries * maxProofTicketBytes
	proofTicketTemporaryPrefix   = ".ticket-"
	proofTicketNameHexLength     = sha256.Size * 2
	proofTicketNameLength        = proofTicketNameHexLength + len(".json")
	proofTicketRootMode          = os.FileMode(0o700)
	proofTicketFileMode          = os.FileMode(0o600)
)

// proofTicketFile and proofTicketFilesystem are deliberately local to the
// SSH broker ticket owner. The seam exists to exercise publication faults; it
// is not a general filesystem or persistence abstraction.
type proofTicketFile interface {
	io.Reader
	io.Writer
	Name() string
	Stat() (os.FileInfo, error)
	Sync() error
	Close() error
	Chmod(os.FileMode) error
	Chown(int, int) error
}

type proofTicketFilesystem struct {
	lstat         func(string) (os.FileInfo, error)
	mkdir         func(string, os.FileMode) error
	readDir       func(string) ([]os.DirEntry, error)
	openFile      func(string, int, os.FileMode) (proofTicketFile, error)
	createTemp    func(string, string) (proofTicketFile, error)
	remove        func(string) error
	rename        func(string, string) error
	syncDirectory func(string) error
}

func realProofTicketFilesystem() proofTicketFilesystem {
	return proofTicketFilesystem{
		lstat:   os.Lstat,
		mkdir:   os.Mkdir,
		readDir: os.ReadDir,
		openFile: func(path string, flag int, mode os.FileMode) (proofTicketFile, error) {
			return os.OpenFile(path, flag, mode)
		},
		createTemp: func(directory, pattern string) (proofTicketFile, error) {
			return os.CreateTemp(directory, pattern)
		},
		remove:        os.Remove,
		rename:        os.Rename,
		syncDirectory: syncDirectory,
	}
}

func (f proofTicketFilesystem) valid() bool {
	return f.lstat != nil && f.mkdir != nil && f.readDir != nil && f.openFile != nil && f.createTemp != nil &&
		f.remove != nil && f.rename != nil && f.syncDirectory != nil
}

type proofTicketStore struct {
	root              string
	fs                proofTicketFilesystem
	validateOwnership bool
	now               func() time.Time
}

type proofTicketEntry struct {
	name     string
	path     string
	ticket   proofTicketV1
	fileSize int64
}

func newProofTicketStore(root string, fs proofTicketFilesystem, validateOwnership bool) *proofTicketStore {
	if !fs.valid() {
		fs = realProofTicketFilesystem()
	}
	return &proofTicketStore{root: filepath.Clean(root), fs: fs, validateOwnership: validateOwnership, now: time.Now}
}

func productionProofTicketStore() *proofTicketStore {
	return newProofTicketStore(TicketRoot, realProofTicketFilesystem(), true)
}

func ensureTicketRoot() error {
	return productionProofTicketStore().ensureRoot()
}

func ticketPathFor(root, operationID string) string {
	sum := sha256.Sum256([]byte(operationID))
	return filepath.Join(root, hex.EncodeToString(sum[:])+".json")
}

func (s *proofTicketStore) ensureRoot() error {
	if s == nil || !s.fs.valid() || s.root == "" || !filepath.IsAbs(s.root) {
		return errors.New("SSH proof root is invalid")
	}
	info, err := s.fs.lstat(s.root)
	if errors.Is(err, os.ErrNotExist) {
		if err := s.fs.mkdir(s.root, proofTicketRootMode); err != nil {
			return fmt.Errorf("create SSH proof root: %w", err)
		}
		info, err = s.fs.lstat(s.root)
	}
	if err != nil || info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != proofTicketRootMode.Perm() {
		return errors.New("SSH proof root is unsafe")
	}
	if s.validateOwnership && !proofTicketRootOwned(info, true) {
		return errors.New("SSH proof root is not root-owned")
	}
	// Sync on every open as well as after creation. If a previous attempt
	// created the child but failed before syncing its parent, the next attempt
	// repairs that durability hole before acknowledging any ticket operation.
	if err := s.fs.syncDirectory(filepath.Dir(s.root)); err != nil {
		return fmt.Errorf("sync SSH proof parent: %w", err)
	}
	return nil
}

func proofTicketRootOwned(info os.FileInfo, validateOwnership bool) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if !validateOwnership {
		return true
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && stat.Gid == 0
}

func proofTicketOwnedByRoot(info os.FileInfo, validateOwnership bool) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != proofTicketFileMode.Perm() {
		return false
	}
	return proofTicketRootOwned(info, validateOwnership)
}

func proofTicketDirectoryEntryOwned(info os.FileInfo, validateOwnership bool) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if info.IsDir() {
		return false
	}
	return proofTicketOwnedByRoot(info, validateOwnership)
}

func (s *proofTicketStore) write(ticket proofTicketV1) error {
	if s == nil || !safeToken(ticket.OperationID, 128) {
		return errors.New("SSH proof ticket identity is invalid")
	}
	data, err := encodeProofTicket(ticket)
	if err != nil {
		return err
	}
	entries, err := s.scan()
	if err != nil {
		return err
	}
	target := ticketPathFor(s.root, ticket.OperationID)
	activeCount := len(entries)
	activeBytes := int64(0)
	existingSize := int64(0)
	for _, entry := range entries {
		activeBytes += entry.fileSize
		if entry.path == target {
			existingSize = entry.fileSize
		} else {
			_ = entry
		}
	}
	if existingSize == 0 {
		if activeCount >= maxProofTicketEntries {
			return errors.New("SSH proof ticket capacity is exhausted by live authority")
		}
		activeCount++
	}
	prospectiveBytes := activeBytes - existingSize + int64(len(data))
	if activeCount > maxProofTicketEntries || prospectiveBytes > maxProofTicketAggregateBytes {
		return errors.New("SSH proof ticket capacity is exhausted by live authority")
	}
	temporary, err := s.fs.createTemp(s.root, proofTicketTemporaryPrefix)
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	published := false
	defer func() {
		_ = temporary.Close()
		if !published {
			_ = s.fs.remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(proofTicketFileMode); err != nil {
		return err
	}
	if s.validateOwnership {
		if err := temporary.Chown(0, 0); err != nil {
			return err
		}
	}
	written, err := temporary.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := s.fs.rename(temporaryName, target); err != nil {
		return err
	}
	published = true
	if err := s.fs.syncDirectory(s.root); err != nil {
		return err
	}
	return nil
}

func writeTicket(ticket proofTicketV1) error {
	return productionProofTicketStore().write(ticket)
}

func encodeProofTicket(ticket proofTicketV1) ([]byte, error) {
	if err := validateProofTicket(ticket); err != nil {
		return nil, err
	}
	data, err := json.Marshal(ticket)
	if err != nil || len(data) > maxProofTicketBytes {
		return nil, errors.New("SSH proof ticket is too large")
	}
	return data, nil
}

func validateProofTicket(ticket proofTicketV1) error {
	if ticket.Schema != 2 || !safeToken(ticket.OperationID, 128) || !digest(ticket.MarkerDigest) || !safeToken(ticket.Verifier, 128) ||
		!safeToken(ticket.EndpointID, 256) || !safeToken(ticket.PrincipalID, 256) ||
		(ticket.AuthenticationClass != "publickey" && ticket.AuthenticationClass != "certificate") ||
		!digest(ticket.BinaryRevision) || !digest(ticket.ServiceRevision) || !digest(ticket.Configuration) ||
		ticket.IssuedAt <= 0 || ticket.IssuedAtMillis <= 0 || ticket.IssuedAtMillis/1000 != ticket.IssuedAt ||
		ticket.ExpiresAt <= ticket.IssuedAt || ticket.ProofedAt < 0 || ticket.ConsumedAt < 0 ||
		ticket.ConsumedAt != 0 && ticket.ProofedAt == 0 || ticket.EvidenceRevision != "" && !digest(ticket.EvidenceRevision) {
		return errors.New("SSH proof ticket is malformed")
	}
	if ticket.ProofedAt != 0 && ticket.ProofedAt < ticket.IssuedAt || ticket.ConsumedAt != 0 && ticket.ConsumedAt < ticket.IssuedAt {
		return errors.New("SSH proof ticket lifecycle is malformed")
	}
	return nil
}

func (s *proofTicketStore) scan() ([]proofTicketEntry, error) {
	if err := s.ensureRoot(); err != nil {
		return nil, err
	}
	entries, err := s.fs.readDir(s.root)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	result := make([]proofTicketEntry, 0, len(entries))
	removed := false
	now := s.now()
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(s.root, name)
		info, statErr := s.fs.lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return nil, statErr
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if strings.HasPrefix(name, proofTicketTemporaryPrefix) {
			// Only remove our own regular 0600 temporary files. Foreign or
			// unsafe entries are ignored and never become capacity debt.
			if proofTicketDirectoryEntryOwned(info, s.validateOwnership) {
				if err := s.fs.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					return nil, err
				}
				removed = true
			}
			continue
		}
		if !isProofTicketName(name) || !proofTicketDirectoryEntryOwned(info, s.validateOwnership) {
			continue
		}
		if info.Size() < 0 || info.Size() > maxProofTicketBytes {
			if err := s.fs.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			removed = true
			continue
		}
		data, readErr := s.readFile(path)
		if readErr != nil {
			return nil, readErr
		}
		var ticket proofTicketV1
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		decodeErr := decoder.Decode(&ticket)
		if decodeErr == nil {
			var extra any
			decodeErr = decoder.Decode(&extra)
			if errors.Is(decodeErr, io.EOF) {
				decodeErr = nil
			}
		}
		if decodeErr != nil || validateProofTicket(ticket) != nil || filepath.Base(ticketPathFor(s.root, ticket.OperationID)) != name {
			// A root-owned malformed record cannot be used as authority. Remove
			// it deterministically so unrelated valid tickets remain readable;
			// foreign/unowned records were skipped above and are untouched.
			if err := s.fs.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			removed = true
			continue
		}
		if ticket.ExpiresAt <= now.Unix() || ticket.ConsumedAt != 0 {
			if err := s.fs.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			removed = true
			continue
		}
		result = append(result, proofTicketEntry{name: name, path: path, ticket: ticket, fileSize: info.Size()})
	}
	if removed {
		if err := s.fs.syncDirectory(s.root); err != nil {
			return nil, fmt.Errorf("sync SSH proof pruning: %w", err)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].name < result[right].name })
	if len(result) > maxProofTicketEntries {
		return nil, errors.New("SSH proof ticket capacity is exhausted by live authority")
	}
	totalBytes := int64(0)
	for _, entry := range result {
		totalBytes += entry.fileSize
	}
	if totalBytes > maxProofTicketAggregateBytes {
		return nil, errors.New("SSH proof ticket byte capacity is exhausted by live authority")
	}
	return result, nil
}

func (s *proofTicketStore) readFile(path string) ([]byte, error) {
	file, err := s.fs.openFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	data, readErr := io.ReadAll(io.LimitReader(file, maxProofTicketBytes+1))
	closeErr := file.Close()
	if statErr != nil || readErr != nil || closeErr != nil || info == nil || info.Size() != int64(len(data)) || len(data) > maxProofTicketBytes {
		return nil, errors.Join(statErr, readErr, closeErr, errors.New("SSH proof ticket read failed"))
	}
	return data, nil
}

func isProofTicketName(name string) bool {
	if len(name) != proofTicketNameLength || !strings.HasSuffix(name, ".json") {
		return false
	}
	for _, character := range strings.TrimSuffix(name, ".json") {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

func (s *proofTicketStore) readOne(operationID string) (proofTicketV1, error) {
	if !safeToken(operationID, 128) {
		return proofTicketV1{}, errors.New("SSH proof ticket identity is invalid")
	}
	entries, err := s.scan()
	if err != nil {
		return proofTicketV1{}, err
	}
	target := ticketPathFor(s.root, operationID)
	for _, entry := range entries {
		if entry.path == target && entry.ticket.OperationID == operationID {
			return entry.ticket, nil
		}
	}
	return proofTicketV1{}, os.ErrNotExist
}

func readTicket(operationID string) (proofTicketV1, error) {
	return productionProofTicketStore().readOne(operationID)
}

func (s *proofTicketStore) readAll() ([]proofTicketV1, error) {
	entries, err := s.scan()
	if err != nil {
		return nil, err
	}
	result := make([]proofTicketV1, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.ticket)
	}
	return result, nil
}

func readTickets() ([]proofTicketV1, error) {
	return productionProofTicketStore().readAll()
}
