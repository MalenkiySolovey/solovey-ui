// Package bootstrap owns Panel storage compatibility, administrator bootstrap and schema invariants.
package bootstrap

// Initializer receives Product identity/version policy and logging without owning
// another connection, migration runner or provider runtime.
type Initializer struct {
	DataDirectory      string
	DatabasePath       string
	ApplicationVersion string
	CoreSchemaVersion  string
	CompareVersions    func(string, string) (int, bool)
	Warning            func(...any)
	Warningf           func(string, ...any)
	Infof              func(string, ...any)
}

func (i Initializer) warning(args ...any) {
	if i.Warning != nil {
		i.Warning(args...)
	}
}
func (i Initializer) warningf(format string, args ...any) {
	if i.Warningf != nil {
		i.Warningf(format, args...)
	}
}
func (i Initializer) infof(format string, args ...any) {
	if i.Infof != nil {
		i.Infof(format, args...)
	}
}
