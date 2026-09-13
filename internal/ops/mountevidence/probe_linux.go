//go:build linux

package mountevidence

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	fsIoctlFiemap       = 0xc020660b
	fiemapFlagSync      = 0x00000001
	fiemapExtentLast    = 0x00000001
	fiemapExtentMerged  = 0x00001000
	fiemapExtentShared  = 0x00002000
	fiemapExtentAllowed = fiemapExtentLast | fiemapExtentMerged | fiemapExtentShared
	fiemapHeaderSize    = 32
	fiemapExtentSize    = 56
)

var ErrMappingUnsupported = errors.New("file extent mapping is unsupported")

// ProbeOverlayBacking creates a short-lived upper-only regular file through
// the overlay, synchronizes it, and records whether the real data inode can
// provide FIEMAP evidence. Mapping support is a neutral capability fact: an
// unsupported mapping is not classified as either durable or volatile here.
func ProbeOverlayBacking(target string, mount Fact) (fact BackingProbeFact, resultErr error) {
	target = filepath.ToSlash(filepath.Clean(target))
	if mount.Validate() != nil || mount.Filesystem != "overlay" || mount.ResolvedTarget != target || !mount.Writable() {
		return BackingProbeFact{}, ErrUnavailable
	}
	file, err := os.CreateTemp(target, ".solovey-mount-evidence-*")
	if err != nil {
		return BackingProbeFact{}, err
	}
	name := file.Name()
	closed, removed := false, false
	defer func() {
		var cleanupErr error
		if !closed {
			cleanupErr = errors.Join(cleanupErr, file.Close())
		}
		if !removed {
			if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
		cleanupErr = errors.Join(cleanupErr, syncProbeDirectory(target))
		if cleanupErr != nil {
			fact = BackingProbeFact{}
			resultErr = errors.Join(resultErr, cleanupErr)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return BackingProbeFact{}, err
	}
	payload := make([]byte, BackingProbeBytes)
	for index := range payload {
		payload[index] = byte(index*131 + 17)
	}
	written, err := file.Write(payload)
	if err != nil {
		return BackingProbeFact{}, err
	}
	if written != len(payload) {
		return BackingProbeFact{}, io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return BackingProbeFact{}, err
	}
	mapping := BackingMappingPhysical
	extents, physicalBytes, err := fiemapPhysicalExtents(file)
	if errors.Is(err, ErrMappingUnsupported) {
		mapping, extents, physicalBytes, err = BackingMappingUnsupported, 0, 0, nil
	}
	if err != nil {
		return BackingProbeFact{}, err
	}
	if _, _, errno := unix.Syscall(unix.SYS_SYNCFS, file.Fd(), 0, 0); errno != 0 {
		return BackingProbeFact{}, errno
	}
	if err := file.Close(); err != nil {
		return BackingProbeFact{}, err
	}
	closed = true
	if err := syncProbeDirectory(target); err != nil {
		return BackingProbeFact{}, err
	}
	if err := os.Remove(name); err != nil {
		return BackingProbeFact{}, err
	}
	removed = true
	if err := syncProbeDirectory(target); err != nil {
		return BackingProbeFact{}, err
	}
	fact = BackingProbeFact{
		Target: target, MountRevision: mount.Revision, Mapping: mapping,
		BytesWritten: BackingProbeBytes, ExtentCount: extents, PhysicalBytes: physicalBytes,
		FileSynced: true, FilesystemSynced: true, DirectorySynced: true,
	}
	return fact, fact.Seal()
}

func fiemapPhysicalExtents(file *os.File) (uint32, uint64, error) {
	buffer := make([]byte, fiemapHeaderSize+MaxBackingProbeExtents*fiemapExtentSize)
	binary.LittleEndian.PutUint64(buffer[8:16], ^uint64(0))
	binary.LittleEndian.PutUint32(buffer[16:20], fiemapFlagSync)
	binary.LittleEndian.PutUint32(buffer[24:28], MaxBackingProbeExtents)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), fsIoctlFiemap, uintptr(unsafe.Pointer(&buffer[0])))
	if errno != 0 {
		if errno == syscall.EOPNOTSUPP || errno == syscall.ENOTTY {
			return 0, 0, errors.Join(ErrMappingUnsupported, errno)
		}
		return 0, 0, errors.Join(ErrUnavailable, errno)
	}
	mapped := binary.LittleEndian.Uint32(buffer[20:24])
	if mapped == 0 || mapped > MaxBackingProbeExtents {
		return 0, 0, ErrUnavailable
	}
	var physicalBytes uint64
	var logicalEnd uint64
	for index := uint32(0); index < mapped; index++ {
		offset := fiemapHeaderSize + int(index)*fiemapExtentSize
		logical := binary.LittleEndian.Uint64(buffer[offset : offset+8])
		physical := binary.LittleEndian.Uint64(buffer[offset+8 : offset+16])
		length := binary.LittleEndian.Uint64(buffer[offset+16 : offset+24])
		flags := binary.LittleEndian.Uint32(buffer[offset+40 : offset+44])
		if logical != logicalEnd || physical == 0 || length == 0 ||
			length > ^uint64(0)-logical || length > ^uint64(0)-physical ||
			length > ^uint64(0)-physicalBytes || flags&^uint32(fiemapExtentAllowed) != 0 {
			return 0, 0, ErrUnavailable
		}
		logicalEnd = logical + length
		physicalBytes += length
		if index+1 == mapped && flags&fiemapExtentLast == 0 {
			return 0, 0, ErrUnavailable
		}
	}
	if logicalEnd < BackingProbeBytes || physicalBytes < BackingProbeBytes {
		return 0, 0, ErrUnavailable
	}
	return mapped, physicalBytes, nil
}

func syncProbeDirectory(name string) error {
	directory, err := os.Open(name) // #nosec G304 -- caller supplies an already observed mount target.
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
