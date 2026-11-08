package updatebroker

import (
	"encoding/json"
	"testing"
)

func TestOrdinaryUpdateBindingCannotAuthorizeAnotherTarget(t *testing.T) {
	identity := providerReleaseFixture(t, "shtorm-extended")
	op := "update-operation:ordinary"
	bound := ordinaryUpdateBinding(op, identity, identity.CoreProvider)
	raw, err := json.Marshal(bound)
	var reopened OrdinaryUpdateV1
	if err != nil || json.Unmarshal(raw, &reopened) != nil || !reopened.Matches(op, identity) {
		t.Fatal("root checkpoint lost its source across persistence", err)
	}
	for _, mutate := range []func(*OrdinaryUpdateV1){
		func(v *OrdinaryUpdateV1) { v.OperationID += "-other" },
		func(v *OrdinaryUpdateV1) { v.ManifestDigest = updateDigest("other-manifest") },
		func(v *OrdinaryUpdateV1) { v.ArtifactSetDigest = updateDigest("other-set") },
		func(v *OrdinaryUpdateV1) { v.Sequence++ },
		func(v *OrdinaryUpdateV1) { v.Version = "2026.4.1" },
		func(v *OrdinaryUpdateV1) { v.BinaryProfile = "core" },
		func(v *OrdinaryUpdateV1) { v.CoreProvider = providerReleaseFixture(t, "sing-box").CoreProvider },
		func(v *OrdinaryUpdateV1) {
			v.CoreProvider.ModuleSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		},
	} {
		changed := bound
		mutate(&changed)
		changed.Revision = ordinaryUpdateRevision(changed)
		if changed.Matches(op, identity) {
			t.Fatal("another target acquired the prepared binding")
		}
	}
	bound.Revision = updateDigest("forged-revision")
	if ValidateOrdinaryUpdate(bound) == nil {
		t.Fatal("unchecked source evidence accepted")
	}
}
