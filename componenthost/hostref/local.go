// Package hostref preserves the public Local Host import path.
// Deprecated: import github.com/MalenkiySolovey/solovey-host/hostref.
package hostref

import canonical "github.com/MalenkiySolovey/solovey-host/hostref"

type Ref = canonical.Ref

func Local() Ref { return canonical.Local() }
