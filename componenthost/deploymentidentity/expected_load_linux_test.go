//go:build linux

package deploymentidentity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledApplicationOwnerProjectionUsesProductionProofReaders(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("exact installed-proof ownership fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	systemdPath := filepath.ToSlash(filepath.Join(root, "systemd.json"))
	procdPath := filepath.ToSlash(filepath.Join(root, "procd.json"))
	adapters := []installedExpectedOwnerAdapter{
		{backend: InstalledApplicationBackendSystemd, path: systemdPath, load: func() (ExpectedApplicationOwnerV1, error) {
			contract, err := LoadFromPath(systemdPath)
			if err != nil {
				return ExpectedApplicationOwnerV1{}, err
			}
			return ExpectedSystemdApplicationOwner(contract)
		}},
		{backend: InstalledApplicationBackendProcd, path: procdPath, load: func() (ExpectedApplicationOwnerV1, error) {
			contract, err := LoadProcdFromPath(procdPath)
			if err != nil {
				return ExpectedApplicationOwnerV1{}, err
			}
			return ExpectedProcdApplicationOwner(contract)
		}},
	}

	writeInstalledOwnerFixture(t, systemdPath, testSystemdOwnerContract(t, "3"))
	systemdProjection, err := loadInstalledApplicationOwnerProjection(adapters)
	if err != nil || systemdProjection.Backend != InstalledApplicationBackendSystemd {
		t.Fatalf("production Systemd projection = %#v, %v", systemdProjection, err)
	}
	if err := os.Remove(systemdPath); err != nil {
		t.Fatal(err)
	}
	writeInstalledOwnerFixture(t, procdPath, testProcdOwnerContract(t, "4"))
	if err := systemdProjection.recheck(adapters); err == nil {
		t.Fatal("production proof reader did not fence Systemd-to-procd switch")
	}
	procdProjection, err := loadInstalledApplicationOwnerProjection(adapters)
	if err != nil || procdProjection.Backend != InstalledApplicationBackendProcd {
		t.Fatalf("production procd projection = %#v, %v", procdProjection, err)
	}
	writeInstalledOwnerFixture(t, procdPath, testProcdOwnerContract(t, "5"))
	if err := procdProjection.recheck(adapters); err == nil {
		t.Fatal("production proof reader did not fence procd owner revision change")
	}
	if err := os.Remove(procdPath); err != nil {
		t.Fatal(err)
	}
	writeInstalledOwnerFixture(t, systemdPath, testSystemdOwnerContract(t, "6"))
	if err := procdProjection.recheck(adapters); err == nil {
		t.Fatal("production proof reader did not fence procd-to-Systemd switch")
	}
}

func TestInstalledApplicationOwnerProjectionFencesBackendAndOwnerSwitches(t *testing.T) {
	root := t.TempDir()
	systemdPath := filepath.ToSlash(filepath.Join(root, "systemd.json"))
	procdPath := filepath.ToSlash(filepath.Join(root, "procd.json"))
	systemdOwner := testExpectedSystemdOwner(t, "3")
	procdOwner := testExpectedProcdOwner(t, "4")
	adapters := []installedExpectedOwnerAdapter{
		{backend: InstalledApplicationBackendSystemd, path: systemdPath, load: func() (ExpectedApplicationOwnerV1, error) { return systemdOwner, nil }},
		{backend: InstalledApplicationBackendProcd, path: procdPath, load: func() (ExpectedApplicationOwnerV1, error) { return procdOwner, nil }},
	}
	if err := os.WriteFile(systemdPath, []byte("systemd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projection, err := loadInstalledApplicationOwnerProjection(adapters)
	if err != nil || projection.Backend != InstalledApplicationBackendSystemd || projection.Owner != systemdOwner {
		t.Fatalf("selected projection = %#v, %v", projection, err)
	}
	if err := projection.recheck(adapters); err != nil {
		t.Fatalf("stable projection recheck: %v", err)
	}

	if err := os.Remove(systemdPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(procdPath, []byte("procd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := projection.recheck(adapters); err == nil {
		t.Fatal("Systemd-to-procd proof switch was not fenced")
	}

	if err := os.Remove(procdPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(systemdPath, []byte("systemd-v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	systemdOwner = testExpectedSystemdOwner(t, "5")
	if err := projection.recheck(adapters); err == nil {
		t.Fatal("application-owner generation switch was not fenced")
	}

	if err := os.WriteFile(procdPath, []byte("procd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadInstalledApplicationOwnerProjection(adapters); err == nil {
		t.Fatal("ambiguous installed backend proofs were accepted")
	}
}

func testExpectedSystemdOwner(t testing.TB, deploymentDigit string) ExpectedApplicationOwnerV1 {
	t.Helper()
	contract := testSystemdOwnerContract(t, deploymentDigit)
	owner, err := ExpectedSystemdApplicationOwner(contract)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func testSystemdOwnerContract(t testing.TB, deploymentDigit string) ApplicationOwnerContractV1 {
	t.Helper()
	contract, err := NewSystemdV1(
		"00112233-4455-4677-8899-aabbccddeeff",
		"src-"+strings.Repeat("1", 64), "art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat(deploymentDigit, 64),
		strings.Repeat("6", 64), strings.Repeat("7", 64), "solovey-ui", "solovey-ui.service",
		"/etc/systemd/system/solovey-ui.service", strings.Repeat("8", 64), "/system.slice/solovey-ui.service",
		"/usr/lib/solovey-ui/solovey-ui", strings.Repeat("9", 64), 997, 997,
	)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func testExpectedProcdOwner(t testing.TB, deploymentDigit string) ExpectedApplicationOwnerV1 {
	t.Helper()
	contract := testProcdOwnerContract(t, deploymentDigit)
	owner, err := ExpectedProcdApplicationOwner(contract)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func testProcdOwnerContract(t testing.TB, deploymentDigit string) ApplicationOwnerContractProcdV1 {
	t.Helper()
	contract, err := NewProcdV1(
		"00112233-4455-4677-8899-aabbccddeeff",
		"src-"+strings.Repeat("1", 64), "art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat(deploymentDigit, 64),
		strings.Repeat("6", 64), strings.Repeat("7", 64), "solovey-ui-panel", "solovey-ui", "panel",
		"/usr/lib/solovey-ui/solovey-ui", strings.Repeat("9", 64), 997, 997,
	)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func writeInstalledOwnerFixture(t testing.TB, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, append(data, '\n'), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0o444); err != nil {
		t.Fatal(err)
	}
}
