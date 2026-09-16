package openwrt

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"gorm.io/gorm"
)

func init() {
	backup.RegisterFileOwner("deployment-identity", backup.FileOwner{Export: exportDeploymentIdentity, Restore: restoreDeploymentIdentity})
}

func exportDeploymentIdentity(_ context.Context, _ *gorm.DB) ([]backup.OwnerFile, error) {
	selection, err := LoadStorageSelection()
	if err != nil || selection.IsDefault() {
		return nil, errors.Join(ErrUnprovenDurableState, err)
	}
	if _, err := InspectDatabaseDurableState(1); err != nil {
		return nil, err
	}
	owner, err := deploymentidentity.LoadProcdInstalled()
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(owner)
	if err != nil {
		return nil, err
	}
	return []backup.OwnerFile{{Key: "source-instance-provenance", Data: data}}, nil
}

func restoreDeploymentIdentity(_ context.Context, _ *gorm.DB, files []backup.OwnerFile, _ bool) error {
	selection, err := LoadStorageSelection()
	if err != nil || selection.IsDefault() {
		return errors.Join(ErrUnprovenDurableState, err)
	}
	if _, err := InspectDatabaseDurableState(1); err != nil {
		return err
	}
	if len(files) != 1 || files[0].Key != "source-instance-provenance" {
		return errors.New("backup deployment identity is absent")
	}
	var source deploymentidentity.ApplicationOwnerContractProcdV1
	if json.Unmarshal(files[0].Data, &source) != nil || source.Validate() != nil {
		return errors.New("backup deployment identity is invalid")
	}
	_, err = deploymentidentity.LoadProcdInstalled()
	// The source identity remains backup provenance. Same-deployment restore
	// keeps its existing UUID; fresh deployment gets its own UUID from root setup.
	// A panel-supplied archive cannot transplant root-owned process authority.
	return err
}
