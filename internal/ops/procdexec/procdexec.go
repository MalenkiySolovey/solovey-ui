// Package procdexec owns the bounded ubus executable authority used by procd
// adapters. It is not a generic process or shell execution facility.
package procdexec

import (
	"errors"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
)

var ErrUnavailable = errors.New("procd ubus executable is unavailable")

const (
	PrimaryPath = "/bin/ubus"
	LegacyPath  = "/sbin/ubus"
)

type Object struct{ object *executableobject.Object }

func Open() (*Object, error) {
	return openCandidates([]string{PrimaryPath, LegacyPath})
}

func openCandidates(candidates []string) (*Object, error) {
	var selected *executableobject.Object
	for _, candidate := range candidates {
		object, err := executableobject.Open(candidate, executableobject.Policy{
			MaxBytes: 64 << 20, AllowSymlink: true, RequireRegular: true, RequireExecutable: true,
			RequireRootOwner: true, ForbiddenMode: 0o022,
			RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022,
		})
		if err != nil {
			continue
		}
		if selected == nil {
			selected = object
			continue
		}
		left, right := selected.Identity(), object.Identity()
		if left.Device != right.Device || left.Inode != right.Inode || left.Digest != right.Digest {
			_ = selected.Close()
			_ = object.Close()
			return nil, errors.New("procd ubus executable authority is ambiguous")
		}
		_ = object.Close()
	}
	if selected != nil {
		return &Object{object: selected}, nil
	}
	return nil, ErrUnavailable
}

func (o *Object) Identity() executableobject.Identity {
	if o == nil || o.object == nil {
		return executableobject.Identity{}
	}
	return o.object.Identity()
}
func (o *Object) Label() string {
	if o == nil || o.object == nil {
		return ""
	}
	return o.object.Label()
}
func (o *Object) File() *os.File {
	if o == nil || o.object == nil {
		return nil
	}
	return o.object.File()
}
func (o *Object) ExecPath(index int) string {
	if o == nil || o.object == nil {
		return ""
	}
	return o.object.ExecPath(index)
}
func (o *Object) Revalidate() error {
	if o == nil || o.object == nil {
		return ErrUnavailable
	}
	return o.object.Revalidate()
}
func (o *Object) Close() error {
	if o == nil || o.object == nil {
		return nil
	}
	return o.object.Close()
}
