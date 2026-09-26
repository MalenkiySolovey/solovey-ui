//go:build !linux

package executableobject

import (
	"errors"
	"os"
)

type Object struct{}

func Open(string, Policy) (*Object, error)    { return nil, ErrUnavailable }
func ValidateMetadata(Identity, Policy) error { return ErrUnavailable }
func (Object) Identity() Identity             { return Identity{} }
func (Object) Label() string                  { return "" }
func (Object) ResolvedPath() string           { return "" }
func (Object) File() *os.File                 { return nil }
func (Object) ExecPath(int) string            { return "" }
func (Object) Revalidate() error {
	return errors.New("coherent executable-object security is unavailable")
}
func (Object) Close() error { return nil }
