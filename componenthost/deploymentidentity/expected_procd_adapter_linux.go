//go:build linux

package deploymentidentity

func init() {
	registerInstalledExpectedOwnerAdapter(installedExpectedOwnerAdapter{
		backend: InstalledApplicationBackendProcd, path: ProcdInstalledContractPath,
		load: func() (ExpectedApplicationOwnerV1, error) {
			contract, err := LoadProcdInstalled()
			if err != nil {
				return ExpectedApplicationOwnerV1{}, err
			}
			return ExpectedProcdApplicationOwner(contract)
		},
	})
}
