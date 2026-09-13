//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	componentregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	_ "github.com/MalenkiySolovey/solovey-ui/components/server-protection"
	serverprotectionbroker "github.com/MalenkiySolovey/solovey-ui/components/server-protection/brokerplugin"
	protectiondeployment "github.com/MalenkiySolovey/solovey-ui/components/server-protection/deploymentadapter"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"golang.org/x/sys/unix"
)

const writerCompositionRoot = "SUI_TEST_OPENWRT_WRITER_COMPOSITION_ROOT"

const writerCompositionInstanceID = "00112233-4455-4677-8899-aabbccddeeff"

func TestOpenWrtWriterFeedsInstalledLoaderAndBrokerHelperComposition(t *testing.T) {
	root := os.Getenv(writerCompositionRoot)
	if root == "" {
		reexecWriterComposition(t)
		return
	}
	prepareWriterCompositionRoot(t, root)
	procRoot := filepath.Join(root, "proc")
	if err := os.MkdirAll(procRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("/proc", procRoot, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Chroot(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("/"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("tmpfs", "/tmp", "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "mode=1777,size=16m"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("/tmp/run/solovey-ui/server-protection", 0o700); err != nil {
		t.Fatal(err)
	}

	arguments := os.Args
	os.Args = []string{arguments[0]}
	defer func() { os.Args = arguments }()
	if err := run(); err != nil {
		t.Fatalf("OpenWrt deployment writer: %v", err)
	}
	owner, err := deploymentidentity.LoadBoundProcdInstalled()
	if err != nil || owner.ProcdService != openwrt.ProcdServiceName || owner.ProcdInstance != openwrt.ProcdPanelInstance {
		t.Fatalf("installed OpenWrt owner = %#v, %v", owner, err)
	}
	if owner.InstanceID != writerCompositionInstanceID {
		t.Fatalf("installed OpenWrt instance ID = %q, want stable package identity %q", owner.InstanceID, writerCompositionInstanceID)
	}
	firstOwner := owner
	writeCompositionFile(t, openWrtBuildInfoPath, []byte("commit=439a15051c95cea12e97dcdf939ab1fc6b941063\n"), 0o644)
	writeCompositionFile(t, openwrt.PanelExecutablePath, []byte("next-openwrt-panel-executable\n"), 0o755)
	if err := run(); err != nil {
		t.Fatalf("OpenWrt deployment writer after package generation change: %v", err)
	}
	owner, err = deploymentidentity.LoadBoundProcdInstalled()
	if err != nil {
		t.Fatalf("load regenerated OpenWrt owner: %v", err)
	}
	if owner.InstanceID != firstOwner.InstanceID || owner.SourceRevision == firstOwner.SourceRevision ||
		owner.ArtifactRevision == firstOwner.ArtifactRevision || owner.DeploymentID == firstOwner.DeploymentID {
		t.Fatalf("regenerated owner did not preserve stable instance identity and advance generation: before=%#v after=%#v", firstOwner, owner)
	}
	identity, err := os.ReadFile(openwrt.DefaultOpenWrtInstanceIDPath)
	if err != nil || string(identity) != writerCompositionInstanceID+"\n" {
		t.Fatalf("stable package identity after regeneration = %q, %v", identity, err)
	}
	authority, err := protectionruntime.LoadInstalledRuntimeRootAuthority()
	if err != nil || authority.Path() != protectionruntime.OpenWrtRuntimeRoot ||
		authority.CanonicalPath() != "/tmp/run/solovey-ui/server-protection" || !authority.Installed() ||
		authority.Backend() != protectionruntime.DeploymentBackendProcd || authority.OwnerContractRevision() != owner.Revision {
		t.Fatalf("installed runtime authority = %#v, %v", authority, err)
	}
	recoveryProjection, err := protectiondeployment.RecoveryProjectionForRuntimeAuthority(authority)
	if err != nil || recoveryProjection.DeploymentBackend != string(authority.Backend()) ||
		recoveryProjection.ProjectionRevision != authority.ProjectionRevision() || recoveryProjection.OwnerContractRevision != owner.Revision ||
		recoveryProjection.Action.Program != "ubus" || strings.Contains(strings.Join(recoveryProjection.Action.Args, " "), "systemctl") {
		t.Fatalf("installed OpenWrt recovery projection = %#v, %v", recoveryProjection, err)
	}
	registry := broker.NewRegistry()
	composition, err := sshbroker.ResolveRegisteredSSHComposition(sshbroker.ProcdDropbearComposition())
	if err != nil {
		t.Fatalf("registered SSH composition: %v", err)
	}
	if err := serverprotectionbroker.RegisterBrokerHandlers(registry, composition); err != nil {
		t.Fatalf("broker plugin composition: %v", err)
	}
	wantVerbs := make([]broker.Verb, 0, len(protectionhelper.DefaultCapabilities().Capabilities))
	for _, capability := range protectionhelper.DefaultCapabilities().Capabilities {
		wantVerbs = append(wantVerbs, broker.Verb("server-protection."+string(capability.Operation)))
	}
	verbs := registry.Verbs(broker.RolePanel)
	slices.Sort(wantVerbs)
	slices.Sort(verbs)
	if !slices.Equal(verbs, wantVerbs) {
		t.Fatalf("server-protection broker verbs = %v, semantic capability inventory = %v", verbs, wantVerbs)
	}
	if err := os.MkdirAll("/etc/solovey-ui/db", 0o700); err != nil {
		t.Fatal(err)
	}
	_ = dbsqlite.Close()
	if err := dbsqlite.Init("/etc/solovey-ui/db/solovey-ui.db"); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	registered, ok := componentregistry.ComponentByID("server-protection")
	if !ok {
		t.Fatal("production server-protection component is not registered")
	}
	migrator, ok := registered.Lifecycle.(lifecycle.Migrator)
	if !ok {
		t.Fatal("production server-protection component does not expose its migration lifecycle")
	}
	if err := migrator.Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	// Model a durable metadata row surviving an OpenWrt reboot while the
	// volatile /run fileset is gone. Component startup must run the same bounded
	// owner-local prune before admitting new work.
	repository := protectionrepository.New(dbsqlite.DB())
	if err := repository.SaveArtifact(context.Background(), &protectionrepository.ArtifactModel{
		OperationID: "operation-00000000000000000000000000000401", Revision: "pre-reboot-revision", Scope: "firewall",
		RelativePath: "revisions/pre-reboot-revision", ManifestSHA256: strings.Repeat("a", 64), Bytes: 4096, CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	lifecycleContext := lifecycle.Context{Host: componenthost.Deps{API: componenthost.APIDeps{Runtime: coreservice.NewRuntime(nil)}}}
	if err := registered.Lifecycle.Start(context.Background(), lifecycleContext); err != nil {
		t.Fatalf("production server-protection component Start: %v", err)
	}
	t.Cleanup(func() { _ = registered.Lifecycle.Stop(context.Background()) })
	for _, directory := range []string{
		"/run/solovey-ui/server-protection/revisions",
		"/run/solovey-ui/server-protection/operations",
		"/run/solovey-ui/server-protection/recovery",
		"/run/solovey-ui/server-protection/publication",
	} {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("component storage directory %s = %v, %v", directory, info, err)
		}
	}
	if _, err := os.Stat("/etc/solovey-ui/db/.runtime/server-protection"); !os.IsNotExist(err) {
		t.Fatalf("component derived a storage root below the database path: %v", err)
	}
	if artifacts, err := repository.ListArtifacts(context.Background()); err != nil || len(artifacts) != 0 {
		t.Fatalf("startup retained reboot-stale volatile artifact metadata: %#v, %v", artifacts, err)
	}
}

func reexecWriterComposition(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	arguments := []string{os.Args[0], "-test.run=^TestOpenWrtWriterFeedsInstalledLoaderAndBrokerHelperComposition$", "-test.v"}
	var command *exec.Cmd
	if os.Geteuid() == 0 {
		command = exec.Command("unshare", append([]string{"-m", "--"}, arguments...)...)
	} else {
		command = exec.Command("unshare", append([]string{"-Urm", "--"}, arguments...)...)
	}
	command.Env = append(os.Environ(), writerCompositionRoot+"="+root)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("OpenWrt writer composition subprocess: %v\n%s", err, output)
	}
}

func prepareWriterCompositionRoot(t testing.TB, root string) {
	t.Helper()
	for _, directory := range []string{
		"etc", "etc/solovey-ui", "usr/lib/solovey-ui", "proc", "tmp",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("tmp", filepath.Join(root, "var")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/var/run", filepath.Join(root, "run")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "etc/solovey-ui"), 0o711); err != nil {
		t.Fatal(err)
	}
	writeCompositionFile(t, filepath.Join(root, "etc/passwd"), []byte("root:x:0:0:root:/root:/bin/sh\nsolovey-ui:x:997:997:Solovey UI:/var/lib/solovey-ui:/sbin/nologin\n"), 0o644)
	writeCompositionFile(t, filepath.Join(root, "etc/group"), []byte("root:x:0:\nsolovey-ui:x:997:\n"), 0o644)
	writeCompositionFile(t, filepath.Join(root, "etc/nsswitch.conf"), []byte("passwd: files\ngroup: files\n"), 0o644)
	writeCompositionFile(t, filepath.Join(root, "usr/lib/solovey-ui/BUILD_INFO.txt"), []byte("commit=339a15051c95cea12e97dcdf939ab1fc6b941062\n"), 0o644)
	writeCompositionFile(t, filepath.Join(root, "usr/lib/solovey-ui/solovey-ui"), []byte("openwrt-panel-executable\n"), 0o755)
	writeCompositionFile(t, filepath.Join(root, strings.TrimPrefix(openwrt.DefaultOpenWrtInstanceIDPath, "/")), []byte(writerCompositionInstanceID+"\n"), 0o400)
}

func writeCompositionFile(t testing.TB, name string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(name, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, mode); err != nil {
		t.Fatal(err)
	}
}
