//go:build linux

package helper

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestProcdPanelManifestSelectionIsExactAndUnambiguous(t *testing.T) {
	contract, err := deploymentidentity.NewProcdV1(
		"00112233-4455-4677-8899-aabbccddeeff", "src-"+strings.Repeat("1", 64), "art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat("3", 64),
		strings.Repeat("4", 64), strings.Repeat("5", 64), "solovey-ui-panel", "solovey-ui", "panel",
		"/usr/lib/solovey-ui/solovey-ui", strings.Repeat("6", 64), 997, 997,
	)
	if err != nil {
		t.Fatal(err)
	}
	client := broker.ClientManifest{
		Name: "panel", UID: contract.ProcessUID, GID: contract.ProcessGID, Executable: contract.ExecutablePath,
		ExecutableDigest: contract.ExecutableSHA256, Device: 1, Inode: 2, Roles: []broker.Role{broker.RolePanel},
		CgroupPolicy: broker.CgroupOptional, CgroupAuthorityRevision: broker.CgroupAuthorityRevisionV1,
		ProcdService: contract.ProcdService, ProcdInstance: contract.ProcdInstance,
		ProcdCommand: []string{"/usr/lib/solovey-ui/solovey-broker-readiness"}, ProcdUser: "solovey-ui", ProcdGroup: "solovey-ui",
		ProcdRelation: broker.ProcdRelationMain,
	}
	manifest := broker.Manifest{Schema: broker.ManifestSchemaProcd, ApplicationOwnerRevision: contract.Revision, Clients: []broker.ClientManifest{client}}
	if selected, err := procdPanelManifestClient(manifest, contract); err != nil || selected.Name != client.Name {
		t.Fatalf("exact procd panel client was not selected: selected=%#v err=%v", selected, err)
	}
	manifest.Clients = append(manifest.Clients, client)
	if _, err := procdPanelManifestClient(manifest, contract); err == nil {
		t.Fatal("duplicate procd panel identities were accepted")
	}
	manifest.Clients = []broker.ClientManifest{client}
	manifest.Clients[0].ProcdInstance = "other"
	if _, err := procdPanelManifestClient(manifest, contract); err == nil {
		t.Fatal("wrong procd instance was accepted")
	}
}

func TestProcdOwnerRequestMatchingUsesOnlyAuthenticatedOwnerIdentity(t *testing.T) {
	contract, err := deploymentidentity.NewProcdV1(
		"00112233-4455-4677-8899-aabbccddeeff", "src-"+strings.Repeat("1", 64), "art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat("3", 64),
		strings.Repeat("4", 64), strings.Repeat("5", 64), "solovey-ui-panel", "solovey-ui", "panel",
		"/usr/lib/solovey-ui/solovey-ui", strings.Repeat("6", 64), 997, 997,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := ListenerOwnerObserveRequest{
		ExpectedInstanceID: contract.InstanceID, ExpectedSourceRevision: contract.SourceRevision,
		ExpectedArtifactRevision: contract.ArtifactRevision, ExpectedDeploymentID: contract.DeploymentID,
		ExpectedOwnerContractRevision: contract.Revision, ExpectedRuntimeRootBindingRevision: contract.RuntimeRootBindingRevision,
	}
	if !ownerRequestMatchesProcdContract(request, contract) {
		t.Fatal("matching procd owner identity was rejected")
	}
	request.ExpectedDeploymentID = "dep-" + strings.Repeat("9", 64)
	if ownerRequestMatchesProcdContract(request, contract) {
		t.Fatal("mismatched procd owner identity was accepted")
	}
}
