package release

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"strings"
)

const (
	MaxPanelArchiveEntries = 64
	MaxPanelMemberBytes    = int64(512 << 20)
	MaxPanelPayloadBytes   = int64(1 << 30)
	maxPanelExpandedBytes  = MaxPanelPayloadBytes + 1<<20
)

var ErrPanelArchive = errors.New("panel release archive is malformed, ambiguous or exceeds bounds")

// PanelArchiveReader owns the portable release-container grammar. It performs
// no extraction, filesystem access, publisher verification or installation.
// Consumers must reach EOF to verify the gzip trailer and bounded zero padding.
type PanelArchiveReader struct {
	gzip                              *gzip.Reader
	input                             *io.LimitedReader
	tar                               *tar.Reader
	seen                              map[string]bool
	entries, files                    int
	total, remaining, padding         int64
	current, finished, failed, closed bool
}

type PanelArchiveMember struct {
	Relative string
	Size     int64
}

func NewPanelArchiveReader(input io.Reader) (*PanelArchiveReader, error) {
	if input == nil {
		return nil, ErrPanelArchive
	}
	compressed, err := gzip.NewReader(input)
	if err != nil {
		return nil, ErrPanelArchive
	}
	bounded := &io.LimitedReader{R: compressed, N: maxPanelExpandedBytes + 1}
	return &PanelArchiveReader{gzip: compressed, input: bounded, tar: tar.NewReader(bounded), seen: map[string]bool{}}, nil
}

func (r *PanelArchiveReader) Next() (PanelArchiveMember, error) {
	if r == nil || r.closed || r.failed {
		return PanelArchiveMember{}, ErrPanelArchive
	}
	if r.finished {
		return PanelArchiveMember{}, io.EOF
	}
	r.current = false
	for {
		// archive/tar also accepts physical EOF without the two end records.
		// Canonical packages include them; distinguish those records from an
		// unread member body/padding discarded by Next.
		minimumEndBytes := r.remaining + r.padding + 2*512
		before := r.input.N
		r.remaining, r.padding = 0, 0
		header, err := r.tar.Next()
		if errors.Is(err, io.EOF) {
			if r.files == 0 || before-r.input.N < minimumEndBytes || r.finish() != nil {
				return r.reject()
			}
			r.finished = true
			return PanelArchiveMember{}, io.EOF
		}
		if err != nil {
			return r.reject()
		}
		r.entries++
		if r.entries > MaxPanelArchiveEntries || !strings.HasPrefix(header.Name, "solovey-ui/") ||
			header.Size < 0 || header.Size > MaxPanelMemberBytes || r.seen[header.Name] {
			return r.reject()
		}
		r.seen[header.Name] = true
		relative := strings.TrimPrefix(header.Name, "solovey-ui/")
		if header.Typeflag == tar.TypeDir {
			if header.Size != 0 || header.Linkname != "" || (relative != "" && relative != "systemd/") {
				return r.reject()
			}
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Linkname != "" || !PanelArchivePath(relative) ||
			r.total > MaxPanelPayloadBytes-header.Size {
			return r.reject()
		}
		r.total += header.Size
		r.files++
		r.current = true
		r.remaining = header.Size
		r.padding = (512 - header.Size%512) % 512
		return PanelArchiveMember{Relative: relative, Size: header.Size}, nil
	}
}

func (r *PanelArchiveReader) Read(buffer []byte) (int, error) {
	if r == nil || r.closed || r.failed {
		return 0, ErrPanelArchive
	}
	if r.finished {
		return 0, io.EOF
	}
	if !r.current {
		return 0, ErrPanelArchive
	}
	n, err := r.tar.Read(buffer)
	r.remaining -= int64(n)
	if r.input.N == 0 {
		r.failed = true
		return n, ErrPanelArchive
	}
	if err != nil && !errors.Is(err, io.EOF) {
		r.failed = true
		return n, ErrPanelArchive
	}
	return n, err
}

func (r *PanelArchiveReader) Close() error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	if r.gzip.Close() != nil {
		return ErrPanelArchive
	}
	return nil
}

func (r *PanelArchiveReader) reject() (PanelArchiveMember, error) {
	r.failed = true
	return PanelArchiveMember{}, ErrPanelArchive
}

func (r *PanelArchiveReader) finish() error {
	var buffer [32 << 10]byte
	for {
		n, err := r.input.Read(buffer[:])
		for _, value := range buffer[:n] {
			if value != 0 {
				return ErrPanelArchive
			}
		}
		if r.input.N == 0 || (err != nil && !errors.Is(err, io.EOF)) {
			return ErrPanelArchive
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

// PanelArchivePath is a relative regular-member label, never a filesystem path
// capability. The extraction owner still checks its opened destination/object.
func PanelArchivePath(relative string) bool {
	if strings.Contains(relative, "..") || strings.Contains(relative, "\\") {
		return false
	}
	if strings.HasPrefix(relative, "systemd/") {
		return artifactName.MatchString(strings.TrimPrefix(relative, "systemd/"))
	}
	return artifactName.MatchString(relative)
}
