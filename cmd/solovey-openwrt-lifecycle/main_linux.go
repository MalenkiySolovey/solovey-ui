//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
)

const (
	initPath       = "/etc/init.d/solovey-ui"
	transitionWait = 30 * time.Second
)

var brokerArguments = []string{openwrt.BrokerExecutablePath, "--transport=standalone-owned",
	"--ssh-implementation=dropbear", "--ssh-service-control=procd", "--ssh-log-evidence=logread",
	"--deployment-backend=package-managed", "--update-mode=package-managed"}

func main() {
	if err := run(os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "solovey OpenWrt lifecycle:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) != 2 {
		return errors.New("exactly one lifecycle action is required")
	}
	if err := configurePackageEnvironment(); err != nil {
		return err
	}
	switch arguments[1] {
	case "broker-entry":
		return brokerEntry()
	case "panel-entry":
		return panelEntry()
	case "startup-restore":
		return restoreBeforePackageStart()
	case "reconcile":
		return reconcilePackageGeneration()
	case "pre-remove":
		return verifyPreRemove()
	case "restore":
		return restorePreservedDatabase()
	default:
		return errors.New("lifecycle action is unsupported")
	}
}

func configurePackageEnvironment() error {
	selection, err := openwrt.LoadStorageSelection()
	if err != nil {
		return err
	}
	expected := map[string]string{
		"SUI_DB_FOLDER":                 selection.DatabaseFolder(),
		"SUI_DEPLOYMENT_KIND":           "openwrt-package-managed",
		"SUI_COMPONENTS_INSTALLED_FILE": openwrt.DefaultInstallRoot + "/components/installed.json",
		"SUI_LOGICAL_FILE_BACKUP":       "",
		"SUI_CACHE_FOLDER":              "",
		"SUI_STAGING_FOLDER":            "",
	}
	if !selection.IsDefault() {
		expected["SUI_LOGICAL_FILE_BACKUP"] = "OWNER_FILES_V1"
		expected["SUI_CACHE_FOLDER"] = openwrt.DefaultTemporaryRoot + "/cache"
		expected["SUI_STAGING_FOLDER"] = openwrt.DefaultTemporaryRoot + "/staging"
	}
	for name, value := range expected {
		if current := os.Getenv(name); current != "" && current != value {
			return fmt.Errorf("OpenWrt package environment %s differs from its fixed profile", name)
		}
		if err := os.Setenv(name, value); err != nil {
			return err
		}
	}
	return nil
}

func brokerEntry() error {
	if os.Geteuid() != 0 {
		return errors.New("broker entry requires root")
	}
	if err := validateCurrentPackageAuthority(true); err != nil {
		return err
	}
	return syscall.Exec(openwrt.BrokerExecutablePath, brokerArguments, packageExecEnvironment())
}

func panelEntry() error {
	if os.Geteuid() == 0 {
		return errors.New("panel entry requires the package service account")
	}
	if err := validateCurrentPackageAuthority(false); err != nil {
		return err
	}
	return syscall.Exec(openwrt.ReadinessExecutablePath, []string{openwrt.ReadinessExecutablePath}, packageExecEnvironment())
}

func packageExecEnvironment() []string {
	environment := []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=C",
		"LC_ALL=C",
		"SUI_DB_FOLDER=" + os.Getenv("SUI_DB_FOLDER"),
		"SUI_DEPLOYMENT_KIND=openwrt-package-managed",
		"SUI_COMPONENTS_INSTALLED_FILE=" + openwrt.DefaultInstallRoot + "/components/installed.json",
	}
	if value := os.Getenv("SUI_LOGICAL_FILE_BACKUP"); value != "" {
		environment = append(environment, "SUI_LOGICAL_FILE_BACKUP="+value)
	}
	for _, name := range []string{"SUI_CACHE_FOLDER", "SUI_STAGING_FOLDER"} {
		if value := os.Getenv(name); value != "" {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

func validateCurrentPackageAuthority(requireBrokerManifest bool) error {
	owner, err := deploymentidentity.LoadProcdInstalled()
	if err != nil || owner.ProcdService != openwrt.ProcdServiceName || owner.ProcdInstance != openwrt.ProcdPanelInstance ||
		owner.ExecutablePath != openwrt.PanelExecutablePath {
		return errors.Join(errors.New("current OpenWrt owner contract is invalid"), err)
	}
	if _, err := openwrt.InspectDatabaseDurableState(1); err != nil {
		return err
	}
	if requireBrokerManifest {
		manifest, err := broker.LoadManifest(broker.RuntimeManifestPath)
		if err != nil || manifest.Schema != broker.ManifestSchemaProcd || manifest.ApplicationOwnerRevision != owner.Revision {
			return errors.Join(errors.New("current OpenWrt broker manifest is invalid"), err)
		}
	}
	return nil
}

func reconcilePackageGeneration() error {
	if os.Geteuid() != 0 {
		return errors.New("package generation reconciliation requires root")
	}
	if err := runFixed(initPath, "stop"); err != nil {
		return err
	}
	if err := waitMappedProcessesAbsent(transitionWait); err != nil {
		return err
	}
	if err := runFixed(initPath, "start"); err != nil {
		return err
	}
	if err := waitCurrentGeneration(transitionWait); err != nil {
		stopErr := runFixed(initPath, "stop")
		absentErr := waitMappedProcessesAbsent(transitionWait)
		return errors.Join(err, stopErr, absentErr)
	}
	return nil
}

func restoreBeforePackageStart() error {
	if os.Geteuid() != 0 {
		return errors.New("startup restoration dispatch requires root")
	}
	return runRestoreAsPanel()
}

func verifyPreRemove() error {
	if os.Geteuid() != 0 {
		return errors.New("pre-remove verification requires root")
	}
	if err := verifyProcdControlPlane(runFixed); err != nil {
		return err
	}
	if err := waitMappedProcessesAbsent(transitionWait); err != nil {
		return err
	}
	if err := verifyBootLinksRemoved("/etc/rc.d"); err != nil {
		return err
	}
	return completePreRemove(context.Background(), protectionhelper.RemoveManagedTableForPackageRemoval, func() error {
		return cleanupPackageRuntime(openwrt.DefaultBrokerSocketRoot, 0)
	})
}

func completePreRemove(ctx context.Context, removeManagedTable func(context.Context) error, removeRuntime func() error) error {
	if removeManagedTable == nil || removeRuntime == nil {
		return errors.New("OpenWrt pre-remove cleanup contract is incomplete")
	}
	if err := removeManagedTable(ctx); err != nil {
		return fmt.Errorf("remove package-owned firewall table: %w", err)
	}
	if err := removeRuntime(); err != nil {
		return err
	}
	return nil
}

func verifyProcdControlPlane(run func(string, ...string) error) error {
	if err := run("/bin/ubus", "-S", "call", "service", "list", `{"name":"`+openwrt.ProcdServiceName+`"}`); err != nil {
		return fmt.Errorf("OpenWrt procd control plane is unavailable: %w", err)
	}
	return nil
}

func verifyBootLinksRemoved(directory string) error {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || len(entries) > 4096 {
		return errors.Join(errors.New("OpenWrt boot-link inventory is unavailable"), err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if len(name) == len("S00solovey-ui") && (name[0] == 'S' || name[0] == 'K') &&
			name[1] >= '0' && name[1] <= '9' && name[2] >= '0' && name[2] <= '9' && strings.HasSuffix(name, "solovey-ui") {
			return errors.New("OpenWrt package service remains enabled before removal")
		}
	}
	return nil
}

func cleanupPackageRuntime(root string, expectedUID uint32) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect OpenWrt package runtime root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("OpenWrt package runtime root is not a directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != expectedUID {
		return errors.New("OpenWrt package runtime root has an unexpected owner")
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove OpenWrt package runtime root: %w", err)
	}
	return nil
}

func restorePreservedDatabase() error {
	selection, err := openwrt.LoadStorageSelection()
	if err != nil {
		return err
	}
	if !selection.IsDefault() {
		return nil
	} // Explicit logical restore owns this deployment; firmware preservation is unqualified.
	if os.Geteuid() == 0 {
		return errors.New("database restoration must run as the package service account")
	}
	return openwrt.RestoreSysupgradePreservationIfNeeded(context.Background(), openwrt.ProductionPreservationEnvironment())
}

func runRestoreAsPanel() error {
	account, err := user.Lookup(openwrt.PanelAccountName)
	if err != nil {
		return err
	}
	uid, uidErr := strconv.ParseUint(account.Uid, 10, 32)
	gid, gidErr := strconv.ParseUint(account.Gid, 10, 32)
	if uidErr != nil || gidErr != nil || uid == 0 || gid == 0 {
		return errors.New("OpenWrt package account is invalid")
	}
	command := exec.Command(openwrt.LifecycleExecutablePath, "restore") // #nosec G204 -- fixed package lifecycle command.
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "SUI_DB_FOLDER=" + os.Getenv("SUI_DB_FOLDER"),
		"SUI_DEPLOYMENT_KIND=openwrt-package-managed", "SUI_COMPONENTS_INSTALLED_FILE=" + openwrt.DefaultInstallRoot + "/components/installed.json"}
	command.Dir = "/"
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{uint32(gid)}}}
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return command.Run()
}

func runFixed(program string, arguments ...string) error {
	command := exec.Command(program, arguments...) // #nosec G204 -- callers use fixed package-owned commands.
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return command.Run()
}

func waitMappedProcessesAbsent(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		mapped, err := mappedPackageProcesses()
		if err == nil && len(mapped) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.Join(fmt.Errorf("mapped OpenWrt package processes remain: %v", mapped), err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func mappedPackageProcesses() ([]int, error) {
	directory, err := os.Open("/proc")
	if err != nil {
		return nil, errors.Join(errors.New("process inventory is unavailable"), err)
	}
	defer directory.Close()
	names, err := directory.Readdirnames(65537)
	if err != nil && !errors.Is(err, io.EOF) || len(names) > 65536 {
		return nil, errors.Join(errors.New("process inventory is unavailable or exceeds its bound"), err)
	}
	result := make([]int, 0, 4)
	for _, name := range names {
		pid, parseErr := strconv.Atoi(name)
		if parseErr != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		executable, readErr := os.Readlink("/proc/" + name + "/exe")
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return nil, readErr
		}
		executable = strings.TrimSuffix(executable, " (deleted)")
		if executable == openwrt.DefaultInstallRoot || strings.HasPrefix(executable, openwrt.DefaultInstallRoot+"/") {
			result = append(result, pid)
		}
	}
	return result, nil
}

func waitCurrentGeneration(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		if err := inspectCurrentGeneration(); err == nil {
			return nil
		} else {
			last = err
		}
		if time.Now().After(deadline) {
			return errors.Join(errors.New("current OpenWrt package generation did not become ready"), last)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func inspectCurrentGeneration() error {
	owner, err := deploymentidentity.LoadProcdInstalled()
	if err != nil || owner.ProcdService != openwrt.ProcdServiceName || owner.ProcdInstance != openwrt.ProcdPanelInstance ||
		owner.ExecutablePath != openwrt.PanelExecutablePath {
		return errors.Join(errors.New("current OpenWrt owner contract is invalid"), err)
	}
	if _, err := openwrt.InspectDatabaseDurableState(1); err != nil {
		return err
	}
	manifest, err := broker.LoadManifest(broker.RuntimeManifestPath)
	if err != nil || manifest.Schema != broker.ManifestSchemaProcd || manifest.ApplicationOwnerRevision != owner.Revision {
		return errors.Join(errors.New("current OpenWrt broker manifest is invalid"), err)
	}
	var panel broker.ClientManifest
	for _, client := range manifest.Clients {
		if client.Name == "panel" {
			panel = client
		}
	}
	if panel.Name == "" {
		return errors.New("current panel generation is absent from the broker manifest")
	}
	inspector := broker.NewProcdInspector()
	panelInstance, err := inspector.Inspect(context.Background(), panel)
	if err != nil {
		return err
	}
	panelFact, err := processevidence.Observe(panelInstance.PID)
	if err != nil {
		return err
	}
	brokerExpected := broker.ClientManifest{Roles: []broker.Role{broker.RolePanel}, ProcdService: openwrt.ProcdServiceName,
		ProcdInstance: openwrt.ProcdBrokerInstance, ProcdCommand: []string{openwrt.LifecycleExecutablePath, "broker-entry"},
		ProcdUser: "root", ProcdGroup: "root", ProcdRelation: broker.ProcdRelationMain}
	brokerInstance, err := inspector.Inspect(context.Background(), brokerExpected)
	if err != nil {
		return err
	}
	brokerFact, err := processevidence.Observe(brokerInstance.PID)
	if err != nil {
		return err
	}
	objectPolicy := executableobject.Policy{MaxBytes: 512 << 20,
		RequireRegular: true, RequireExecutable: true, RequireRootOwner: true, ForbiddenMode: 0o022,
		RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022}
	panelObject, err := executableobject.Open(openwrt.PanelExecutablePath, objectPolicy)
	if err != nil {
		return err
	}
	defer panelObject.Close()
	brokerObject, err := executableobject.Open(openwrt.BrokerExecutablePath, objectPolicy)
	if err != nil {
		return err
	}
	defer brokerObject.Close()
	if err := panelObject.Revalidate(); err != nil {
		return err
	}
	if err := brokerObject.Revalidate(); err != nil {
		return err
	}
	return openwrt.ValidatePackageGenerationReadiness(openwrt.PackageGenerationEvidence{
		Owner: owner, Manifest: manifest, PanelInstance: panelInstance, PanelProcess: panelFact, PanelObject: panelObject.Identity(),
		BrokerInstance: brokerInstance, BrokerProcess: brokerFact, BrokerObject: brokerObject.Identity(),
	})
}
