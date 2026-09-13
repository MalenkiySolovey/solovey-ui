// Package openwrtpackage supplies the cross-component contract fixture used by
// the OpenWrt package boundary tests. It remains outside production packages.
package openwrtpackage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

func WriteOwnerRuntime(parent string) error {
	owner, err := runtimeOwner()
	if err != nil {
		return err
	}
	fact, err := mountevidence.Parse(
		[]byte("41 36 0:41 / / rw - tmpfs tmpfs rw\n"),
		protectionruntime.OpenWrtRuntimeRoot,
	)
	if err != nil {
		return err
	}
	fact, err = mountevidence.BindStatfs(fact, "/tmp/run/solovey-ui/server-protection", false, 0x01021994)
	if err != nil {
		return err
	}
	mount, err := protectionruntime.NewRuntimeMountProof(
		protectionruntime.OpenWrtRuntimeRoot,
		protectionruntime.RuntimeMountVolatile,
		fact,
	)
	if err != nil {
		return err
	}
	runtimeRoot, err := protectionruntime.InstalledOpenWrtRuntimeRoot(owner, mount)
	if err != nil {
		return err
	}
	if err := writeRootJSON(filepath.Join(parent, "procd-application-owner-contract.json"), owner); err != nil {
		return err
	}
	return writeRootJSON(filepath.Join(parent, "server-protection-runtime-root.json"), runtimeRoot)
}

func ValidateOwnerRuntime(parent string) error {
	owner, err := deploymentidentity.LoadProcdFromPath(filepath.Join(parent, "procd-application-owner-contract.json"))
	if err != nil {
		return err
	}
	expectedFixture, err := runtimeOwner()
	if err != nil {
		return err
	}
	if owner != expectedFixture {
		return errors.New("materialized application owner differs from package generation")
	}
	data, err := os.ReadFile(filepath.Join(parent, "server-protection-runtime-root.json"))
	if err != nil {
		return err
	}
	var runtimeRoot protectionruntime.InstalledRuntimeRootV1
	if err := json.Unmarshal(data, &runtimeRoot); err != nil {
		return err
	}
	if err := runtimeRoot.Validate(); err != nil {
		return err
	}
	expectedOwner, err := deploymentidentity.ExpectedProcdApplicationOwner(owner)
	if err != nil {
		return err
	}
	if err := protectionruntime.ValidateInstalledRuntimeRootBinding(runtimeRoot, expectedOwner); err != nil {
		return err
	}
	if runtimeRoot.RuntimeRootContractRevision != owner.RuntimeRootContractRevision {
		return errors.New("runtime-root policy revision differs from the application owner")
	}
	return nil
}

func runtimeOwner() (deploymentidentity.ApplicationOwnerContractProcdV1, error) {
	instanceID := "123e4567-e89b-42d3-a456-426614174000"
	sourceRevision := "src-" + strings.Repeat("1", 64)
	artifactRevision := "art-" + strings.Repeat("a", 64)
	deploymentID := "dep-" + strings.Repeat("3", 64)
	runtimePolicy := protectionruntime.OpenWrtInstalled()
	runtimeRevision, err := runtimePolicy.Revision()
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractProcdV1{}, err
	}
	runtimeBinding, err := protectionruntime.BindOpenWrt(runtimePolicy, instanceID, sourceRevision, artifactRevision, deploymentID)
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractProcdV1{}, err
	}
	return deploymentidentity.NewProcdV1(
		instanceID, sourceRevision, artifactRevision, deploymentID,
		runtimeRevision, runtimeBinding.BindingRevision, "solovey-ui-panel", openwrt.ProcdServiceName, openwrt.ProcdPanelInstance,
		openwrt.PanelExecutablePath, strings.Repeat("a", 64), 32769, 32769,
	)
}

func writeRootJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o444); err != nil {
		return err
	}
	if err := os.Chown(path, 0, 0); err != nil {
		return err
	}
	return os.Chmod(path, 0o444)
}
