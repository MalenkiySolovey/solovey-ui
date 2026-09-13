package architecture

import (
	"reflect"
	"testing"

	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
)

// The cross-package guard proves the actual public type boundary. Registry
// membership, exact target binding, recovery observation and both released
// compositions are exercised behaviorally in the owning sshbroker and broker
// composition packages; they are deliberately not inferred from filenames or
// implementation tokens here.
func TestResolvedSSHCompositionIsOpaqueToExternalCallers(t *testing.T) {
	typeOfResolved := reflect.TypeOf(sshbroker.ResolvedSSHComposition{})
	for index := 0; index < typeOfResolved.NumField(); index++ {
		field := typeOfResolved.Field(index)
		if field.Anonymous || field.PkgPath == "" {
			t.Errorf("resolved SSH composition exposes mutable field %q (anonymous=%v, pkgPath=%q)", field.Name, field.Anonymous, field.PkgPath)
		}
	}
	for _, method := range []string{"Composition", "Implementation", "ServiceControl", "LogEvidence", "Valid"} {
		if _, ok := typeOfResolved.MethodByName(method); !ok {
			t.Errorf("resolved SSH composition is missing read-only projection %q", method)
		}
	}
	for _, method := range []string{"RecoveryImplementation", "RecoveryLogEvidence", "RecoveryJournaldUnits", "ResolveTrustedLogreadExecutable"} {
		if _, ok := typeOfResolved.MethodByName(method); ok {
			t.Errorf("resolved SSH composition leaks owner-private recovery target through %q", method)
		}
	}
}
