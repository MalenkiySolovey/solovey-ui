//go:build !minimal

package serverprotection

import (
	"testing"

	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	sptest "github.com/MalenkiySolovey/solovey-ui/testsupport/serverprotection"
)

func TestArtifactRecoveryProjectionRetainsRuntimeAuthorityGeneration(t *testing.T) {
	authority := sptest.OpenWrtRuntimeAuthority(t)
	projection, err := artifactRecoveryProjection(authority)
	if err != nil {
		t.Fatal(err)
	}
	if authority.Backend() != protectionruntime.DeploymentBackendProcd || projection.DeploymentBackend != string(authority.Backend()) ||
		projection.ProjectionRevision != authority.ProjectionRevision() || projection.OwnerContractRevision != authority.OwnerContractRevision() ||
		projection.Action.Program != "ubus" {
		t.Fatalf("runtime authority=%#v recovery projection=%#v", authority, projection)
	}
}
