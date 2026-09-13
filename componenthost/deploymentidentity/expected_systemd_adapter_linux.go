//go:build linux

package deploymentidentity

func init() {
	registerInstalledExpectedOwnerAdapter(installedExpectedOwnerAdapter{
		backend: InstalledApplicationBackendSystemd, path: InstalledContractPath,
		load: func() (ExpectedApplicationOwnerV1, error) {
			contract, err := LoadInstalled()
			if err != nil {
				return ExpectedApplicationOwnerV1{}, err
			}
			return ExpectedSystemdApplicationOwner(contract)
		},
	})
}
