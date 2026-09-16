package openwrt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const pinnedOpenWrtCommit = "f0a60eee2fe051741c643ea6118718aae1ef17fb"
const pinnedFirewall4Commit = "b6e5157527d361f99ad52eaa6da273cb0f2dfd59"

func TestPinnedOpenWrt2512APKLifecycleAndSysupgradeSemantics(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "..", "..", ".tmp", "openwrt-v25.12.5"))
	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Skip("pinned OpenWrt v25.12.5 source checkout is not present")
	} else if err != nil {
		t.Fatal(err)
	}
	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("git", "-c", "safe.directory="+root, "-C", root, "rev-parse", "HEAD").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != pinnedOpenWrtCommit {
		t.Fatalf("pinned OpenWrt source revision = %q, %v", strings.TrimSpace(string(output)), err)
	}
	packagePack := pinnedSource(t, root, "include/package-pack.mk")
	packageRoot := pinnedSource(t, root, "package/Makefile")
	baseFilesMake := pinnedSource(t, root, "package/base-files/Makefile")
	boot := pinnedSource(t, root, "package/base-files/files/etc/init.d/boot")
	functions := pinnedSource(t, root, "package/base-files/files/lib/functions.sh")
	rcCommon := pinnedSource(t, root, "package/base-files/files/etc/rc.common")
	sysupgrade := pinnedSource(t, root, "package/base-files/files/sbin/sysupgrade")

	requireOrdered(t, packagePack,
		`echo "add_group_and_user"`, `echo "default_postinst"`, `postinst-pkg`,
		`echo 'export PKG_UPGRADE=1'`, `preinst`, `pre-upgrade`,
		`echo 'export PKG_UPGRADE=1'`, `post-install`, `post-upgrade`,
		`echo "default_prerm"`, `prerm-pkg`, `pre-deinstall`)
	for _, exact := range []string{
		`APK_SCRIPTS_$(1)+=--script "pre-upgrade:$$(ADIR_$(1))/pre-upgrade"`,
		`APK_SCRIPTS_$(1)+=--script "post-upgrade:$$(ADIR_$(1))/post-upgrade"`,
		`APK_SCRIPTS_$(1)+=--script "pre-deinstall:$$(ADIR_$(1))/pre-deinstall"`,
	} {
		if !strings.Contains(packagePack, exact) {
			t.Fatalf("pinned APK lifecycle lacks %q", exact)
		}
	}
	requireOrdered(t, functions, `default_prerm()`, `"$i" disable`, `"$i" stop`)
	requireOrdered(t, functions, `default_postinst()`, `"$i" enable`, `"$i" start`, `return $ret`)
	if !strings.Contains(functions, `for file in $(ls $1/*.sh 2>/dev/null); do`) {
		t.Fatal("pinned include() no longer selects only .sh upgrade hooks")
	}
	if !strings.Contains(rcCommon, `procd_kill "$(basename ${basescript:-$initscript})" "$1"`) {
		t.Fatal("pinned rc.common no longer delegates stop completion to procd_kill")
	}
	requireOrdered(t, sysupgrade, `include /lib/upgrade`, `run_hooks "$CONFFILES" $sysupgrade_init_conffiles`,
		`create_backup_archive "$CONF_TAR"`, `ubus call system sysupgrade "$(json_dump)"`)
	if !strings.Contains(packageRoot, `rm -rf $(TARGET_DIR)/run`) ||
		!strings.Contains(baseFilesMake, `$(LN) tmp $(1)/var`) ||
		!strings.Contains(baseFilesMake, `$(LN) /tmp/run $(1)/var/run`) {
		t.Fatal("pinned OpenWrt runtime roots are no longer boot/runtime-owned")
	}
	requireOrdered(t, boot, `mkdir -p /var/run`, `ln -s /var/run /run`)
}

func TestOpenWrtBrokerAuthorityIsRegeneratedIntoTheRuntimeRoot(t *testing.T) {
	sourceRoot := filepath.Join("..", "..")
	prepare := pinnedSource(t, sourceRoot, "deploy/openwrt/solovey-openwrt-prepare")
	writer := pinnedSource(t, sourceRoot, "cmd/solovey-openwrt-broker-manifest/main_linux.go")
	lifecycle := pinnedSource(t, sourceRoot, "cmd/solovey-openwrt-lifecycle/main_linux.go")
	protocol := pinnedSource(t, sourceRoot, "internal/ops/privilegedbroker/protocol.go")

	requireOrdered(t, prepare, `mkdir -m 0750 /run/solovey-ui`, `chown root:solovey-ui /run/solovey-ui`,
		`chmod 0750 /run/solovey-ui`, `mkdir -m 0700 /run/solovey-ui/diagnostics`,
		`chown root:root /run/solovey-ui/diagnostics`, `chmod 0700 /run/solovey-ui/diagnostics`,
		`solovey-openwrt-broker-manifest || exit 1`)
	if !strings.Contains(protocol, `RuntimeManifestPath  = StandaloneSocketRoot + "/broker-clients.json"`) ||
		!strings.Contains(writer, `filepath.Dir(broker.RuntimeManifestPath)`) ||
		!strings.Contains(writer, `os.Rename(temporary, broker.RuntimeManifestPath)`) ||
		!strings.Contains(lifecycle, `broker.LoadManifest(broker.RuntimeManifestPath)`) {
		t.Fatal("OpenWrt broker authority is not consistently bound to its regenerated runtime location")
	}
	if strings.Contains(writer, `filepath.Dir(broker.DefaultManifest)`) || strings.Contains(lifecycle, `broker.LoadManifest(broker.DefaultManifest)`) {
		t.Fatal("OpenWrt broker authority still consumes the persistent systemd manifest location")
	}
}

func TestOpenWrtPackageHooksOwnGenerationTransition(t *testing.T) {
	sourceRoot := filepath.Join("..", "..")
	packageMakefile := pinnedSource(t, sourceRoot, "deploy/openwrt/package/solovey-ui/Makefile")
	initScript := pinnedSource(t, sourceRoot, "deploy/openwrt/solovey-ui.init")
	lifecycle := pinnedSource(t, sourceRoot, "cmd/solovey-openwrt-lifecycle/main_linux.go")

	preinst := definitionBody(t, packageMakefile, "Package/solovey-ui/preinst")
	if strings.Index(preinst, `[ -n "$${IPKG_INSTROOT}" ] && exit 0`) < 0 ||
		strings.Index(preinst, `[ "$${PKG_UPGRADE:-0}" = 1 ] || exit 0`) < 0 {
		t.Fatal("OpenWrt preinst does not fail closed for staged roots and non-upgrade installs")
	}
	requireOrdered(t, preinst, `[ "$${PKG_UPGRADE:-0}" = 1 ] || exit 0`, `installed_init_first_line=$$(sed -n '1p' /etc/init.d/solovey-ui)`, `*"$$(printf '\r')")`, `/etc/init.d/solovey-ui stop`, `attempt=0`, `attempt=$$((attempt + 1))`, `exit 1`)
	if !strings.Contains(preinst, `-lt 30`) || !strings.Contains(preinst, `sleep 1`) {
		t.Fatal("OpenWrt preinst process drain is not bounded")
	}
	postinst := definitionBody(t, packageMakefile, "Package/solovey-ui/postinst")
	if !strings.Contains(postinst, LifecycleExecutablePath+" reconcile") || strings.Contains(postinst, "/etc/init.d/solovey-ui start") {
		t.Fatal("OpenWrt postinst does not hand off to the lifecycle owner")
	}
	prerm := definitionBody(t, packageMakefile, "Package/solovey-ui/prerm")
	if !strings.Contains(prerm, LifecycleExecutablePath+" pre-remove") {
		t.Fatal("OpenWrt prerm does not use the package-specific pre-remove hook")
	}

	if !strings.Contains(initScript, `procd_set_param command "$SOLOVEY_ROOT/solovey-openwrt-lifecycle" broker-entry`) ||
		!strings.Contains(initScript, `procd_set_param command "$SOLOVEY_ROOT/solovey-openwrt-lifecycle" panel-entry`) ||
		!strings.Contains(initScript, `procd_set_param respawn 3600 3 5`) ||
		!strings.Contains(initScript, `procd_set_param respawn 3600 5 5`) {
		t.Fatal("OpenWrt procd instances do not respawn through the fixed lifecycle entrypoints")
	}
	if !strings.Contains(initScript, "service_triggers()") ||
		!strings.Contains(initScript, "procd_add_reload_trigger firewall") ||
		!strings.Contains(initScript, "reload_service()") ||
		!strings.Contains(initScript, "procd_send_signal solovey-ui panel HUP") {
		t.Fatal("OpenWrt firewall lifecycle events do not accelerate managed-table reconciliation")
	}
	requireOrdered(t, initScript, `"$SOLOVEY_PREPARE" || return 1`, `"$SOLOVEY_LIFECYCLE" startup-restore || return 1`, `procd_open_instance root-broker`, `procd_open_instance panel`)
	startupRestore := functionBody(t, lifecycle, "func restoreBeforePackageStart() error")
	if !strings.Contains(startupRestore, "os.Geteuid() != 0") || !strings.Contains(startupRestore, "runRestoreAsPanel()") {
		t.Fatal("OpenWrt startup restore is not a root-dispatched service-account transition")
	}
	preRemove := functionBody(t, lifecycle, "func verifyPreRemove() error")
	if !strings.Contains(preRemove, "protectionhelper.RemoveManagedTableForPackageRemoval") {
		t.Fatal("OpenWrt package removal omits the authenticated managed-table cleanup seam")
	}
	completeRemove := functionBody(t, lifecycle, "func completePreRemove(ctx context.Context, removeManagedTable func(context.Context) error, removeRuntime func() error) error")
	requireOrdered(t, completeRemove, "removeManagedTable(ctx)", "removeRuntime()")
	for _, forbidden := range []string{"mkdir", "chown", "chmod"} {
		if strings.Contains(initScript, forbidden) {
			t.Fatalf("OpenWrt init script regained creator operation %q", forbidden)
		}
	}
	for _, function := range []struct {
		name string
	}{
		{name: "brokerEntry"},
		{name: "panelEntry"},
	} {
		body := functionBody(t, lifecycle, "func "+function.name+"() error")
		if !strings.Contains(body, "syscall.Exec(") || !strings.Contains(body, "packageExecEnvironment()") ||
			strings.Contains(body, "os.Environ()") || strings.Contains(body, "runFixed(") || strings.Contains(body, "os.Mkdir") || strings.Contains(body, "os.Chown") || strings.Contains(body, "os.Chmod") {
			t.Fatalf("direct %s respawn entrypoint is not validation-only and exec-only", function.name)
		}
	}
	environment := functionBody(t, lifecycle, "func packageExecEnvironment() []string")
	for _, required := range []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "SUI_DB_FOLDER=", "SUI_DEPLOYMENT_KIND=openwrt-package-managed", "SUI_COMPONENTS_INSTALLED_FILE="} {
		if !strings.Contains(environment, required) {
			t.Errorf("OpenWrt lifecycle child environment lacks %q", required)
		}
	}
}

func TestOpenWrtRuntimeDependenciesUseProviderCapabilities(t *testing.T) {
	workspaceRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	openwrtRoot := filepath.Join(workspaceRoot, ".tmp", "openwrt-v25.12.5")
	if _, err := os.Stat(openwrtRoot); os.IsNotExist(err) {
		t.Skip("pinned OpenWrt v25.12.5 source checkout is not present")
	} else if err != nil {
		t.Fatal(err)
	}
	var err error
	openwrtRoot, err = filepath.Abs(openwrtRoot)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("git", "-c", "safe.directory="+openwrtRoot, "-C", openwrtRoot, "rev-parse", "HEAD").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != pinnedOpenWrtCommit {
		t.Fatalf("pinned OpenWrt source revision = %q, %v", strings.TrimSpace(string(output)), err)
	}

	packageRecipe := pinnedSource(t, workspaceRoot, "workbench/s-ui-personal/deploy/openwrt/package/solovey-ui/Makefile")
	dependencies := packageDependencies(t, packageRecipe, "solovey-ui")
	wantDependencies := []string{"+procd", "+dropbear", "+ubus", "+uci", "+logd", "+nftables"}
	if strings.Join(dependencies, " ") != strings.Join(wantDependencies, " ") {
		t.Fatalf("package dependency closure = %q, want %q", dependencies, wantDependencies)
	}
	for _, forbidden := range []string{"+nftables-json", "+nftables-nojson", "+firewall4", "+jansson"} {
		if containsString(dependencies, forbidden) {
			t.Fatalf("package selects implementation policy through %q", forbidden)
		}
	}
	providers := map[string][]string{
		"package/system/procd/Makefile":              {"define Package/procd", "+ubusd +ubus"},
		"package/network/services/dropbear/Makefile": {"define Package/dropbear", "$(1)/usr/sbin/dropbear"},
		"package/system/ubus/Makefile":               {"define Package/ubus", "$(1)/bin/"},
		"package/system/uci/Makefile":                {"define Package/uci/install", "$(1)/sbin", "$(PKG_BUILD_DIR)/uci"},
		"package/system/ubox/Makefile":               {"define Package/logd", "/sbin/logread:/usr/libexec/logread-ubox", "usr/libexec/logread-ubox"},
	}
	for relative, required := range providers {
		source := pinnedSource(t, openwrtRoot, relative)
		for _, fact := range required {
			if !strings.Contains(source, fact) {
				t.Errorf("pinned provider %s lacks runtime fact %q", relative, fact)
			}
		}
	}

	nftables := pinnedSource(t, openwrtRoot, "package/network/utils/nftables/Makefile")
	common := definitionBody(t, nftables, "Package/nftables/Default")
	noJSON := definitionBody(t, nftables, "Package/nftables-nojson")
	withJSON := definitionBody(t, nftables, "Package/nftables-json")
	install := definitionBody(t, nftables, "Package/nftables/install/Default")
	if !strings.Contains(common, "PROVIDES:=nftables") {
		t.Fatal("pinned nftables variants no longer expose the common nftables provider capability")
	}
	for name, body := range map[string]string{"nftables-nojson": noJSON, "nftables-json": withJSON} {
		if !strings.Contains(body, "$(Package/nftables/Default)") {
			t.Fatalf("%s no longer inherits the common provider contract", name)
		}
		if !strings.Contains(nftables, "Package/"+name+"/install = $(Package/nftables/install/Default)") {
			t.Fatalf("%s no longer installs through the common nft payload", name)
		}
	}
	if !strings.Contains(install, "$(PKG_INSTALL_DIR)/usr/sbin/nft") || !strings.Contains(install, "$(1)/usr/sbin/") {
		t.Fatal("common nftables provider payload no longer installs /usr/sbin/nft")
	}
	if !strings.Contains(noJSON, "VARIANT:=nojson") || !strings.Contains(noJSON, "DEFAULT_VARIANT:=1") || !strings.Contains(noJSON, "CONFLICTS:=nftables-json") {
		t.Fatal("pinned no-JSON nftables provider lost its variant, default, or conflict contract")
	}
	if !strings.Contains(withJSON, "VARIANT:=json") || !strings.Contains(withJSON, "DEPENDS+=+jansson") {
		t.Fatal("pinned JSON nftables provider lost its JSON-only dependency contract")
	}
	if strings.Count(nftables, "+jansson") != 1 || strings.Count(nftables, "--with-json") != 1 ||
		!strings.Contains(nftables, "ifeq ($(BUILD_VARIANT),json)\n  CONFIGURE_ARGS += --with-json\nendif") {
		t.Fatal("pinned JSON additions are no longer isolated to the JSON variant")
	}

	packagePack := pinnedSource(t, openwrtRoot, "include/package-pack.mk")
	for _, fact := range []string{
		"When multiple variants inside the same package have the same provide, a",
		"default variant must be set using DEFAULT_VARIANT:=1",
		"define GetProviderPriority",
		"$(if $(1),100,",
		`--info "provides:$$(Package/$(1)/PROVIDES)"`,
		`--info "provider-priority:$$(Package/$(1)/PRIORITY)"`,
		`--info "depends:$$(foreach depends`,
	} {
		if !strings.Contains(packagePack, fact) {
			t.Fatalf("pinned APK provider machinery lacks %q", fact)
		}
	}
}

func TestPinnedOpenWrtProviderResolutionAcceptsAnExistingJSONVariant(t *testing.T) {
	perl, err := exec.LookPath("perl")
	if err != nil {
		t.Skip("pinned OpenWrt package metadata execution requires Perl")
	}
	workspaceRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	openwrtRoot := filepath.Join(workspaceRoot, ".tmp", "openwrt-v25.12.5")
	if _, err := os.Stat(openwrtRoot); os.IsNotExist(err) {
		t.Skip("pinned OpenWrt v25.12.5 source checkout is not present")
	} else if err != nil {
		t.Fatal(err)
	}
	openwrtRoot, err = filepath.Abs(openwrtRoot)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("git", "-c", "safe.directory="+openwrtRoot, "-C", openwrtRoot, "rev-parse", "HEAD").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != pinnedOpenWrtCommit {
		t.Fatalf("pinned OpenWrt source revision = %q, %v", strings.TrimSpace(string(output)), err)
	}

	packageRecipe := pinnedSource(t, workspaceRoot, "workbench/s-ui-personal/deploy/openwrt/package/solovey-ui/Makefile")
	dependencies := packageDependencies(t, packageRecipe, "solovey-ui")
	if !containsString(dependencies, "+nftables") {
		t.Fatal("Solovey package does not consume the provider-level nftables capability")
	}
	metadata := strings.Join([]string{
		"Source-Makefile: package/network/utils/nftables/Makefile",
		"Package: nftables-nojson",
		"Version: 1.1.6-r2",
		"Conflicts: nftables-json",
		"Provides: nftables",
		"Build-Variant: nojson",
		"Default-Variant: nojson",
		"Section: net",
		"Category: Network",
		"Submenu: Firewall",
		"Title: nftables no JSON support",
		"Description: no JSON provider",
		"@@",
		"Package: nftables-json",
		"Version: 1.1.6-r2",
		"Provides: nftables",
		"Build-Variant: json",
		"Section: net",
		"Category: Network",
		"Submenu: Firewall",
		"Title: nftables with JSON support",
		"Description: JSON provider",
		"@@",
		"Source-Makefile: package/solovey-ui/Makefile",
		"Package: solovey-ui",
		"Version: 2026.3.1-r1",
		"Depends: +nftables",
		"Section: net",
		"Category: Network",
		"Submenu: Firewall",
		"Title: Solovey UI",
		"Description: provider consumer",
		"@@",
		"",
	}, "\n")
	metadataPath := filepath.Join(t.TempDir(), "packageinfo")
	if err := os.WriteFile(metadataPath, []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(perl, filepath.Join(openwrtRoot, "scripts", "package-metadata.pl"), "config", metadataPath)
	command.Dir = openwrtRoot
	generated, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pinned package metadata resolver failed: %v\n%s", err, generated)
	}
	config := string(generated)
	consumer := kconfigPackageBody(t, config, "solovey-ui")
	if !strings.Contains(consumer, "select PACKAGE_nftables-nojson if PACKAGE_nftables-json<PACKAGE_solovey-ui") {
		t.Fatalf("provider-neutral dependency no longer selects the default only when an alternate provider is absent:\n%s", consumer)
	}
	if strings.Contains(consumer, "select PACKAGE_nftables-json") || strings.Contains(consumer, "select PACKAGE_nftables-nojson\n") {
		t.Fatalf("provider-neutral dependency forces an implementation variant:\n%s", consumer)
	}
	noJSON := kconfigPackageBody(t, config, "nftables-nojson")
	if !strings.Contains(noJSON, "depends on m || (PACKAGE_nftables-json != y)") {
		t.Fatalf("pinned default provider no longer preserves the JSON conflict rule:\n%s", noJSON)
	}
}

func TestPinnedFirewall4CoexistenceAndExternalLossSemantics(t *testing.T) {
	workspaceRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	openwrtRoot := filepath.Join(workspaceRoot, ".tmp", "openwrt-v25.12.5")
	firewall4Root := filepath.Join(workspaceRoot, ".tmp", "firewall4-b6e515")
	for _, root := range []string{openwrtRoot, firewall4Root} {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			t.Skip("pinned OpenWrt/firewall4 source checkout is not present")
		} else if err != nil {
			t.Fatal(err)
		}
	}
	var err error
	openwrtRoot, err = filepath.Abs(openwrtRoot)
	if err != nil {
		t.Fatal(err)
	}
	firewall4Root, err = filepath.Abs(firewall4Root)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("git", "-c", "safe.directory="+firewall4Root, "-C", firewall4Root, "rev-parse", "HEAD").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != pinnedFirewall4Commit {
		t.Fatalf("pinned firewall4 source revision = %q, %v", strings.TrimSpace(string(output)), err)
	}
	recipe := pinnedSource(t, openwrtRoot, "package/network/config/firewall4/Makefile")
	if !strings.Contains(recipe, "PKG_SOURCE_VERSION:="+pinnedFirewall4Commit) || !strings.Contains(recipe, "+nftables-json") {
		t.Fatal("OpenWrt 25.12.5 firewall4 recipe no longer pins the inspected source or JSON nft dependency")
	}
	fw4 := strings.ReplaceAll(pinnedSource(t, firewall4Root, "root/sbin/fw4"), "\r\n", "\n")
	template := strings.ReplaceAll(pinnedSource(t, firewall4Root, "root/usr/share/firewall4/templates/ruleset.uc"), "\r\n", "\n")
	ruleTemplate := strings.ReplaceAll(pinnedSource(t, firewall4Root, "root/usr/share/firewall4/templates/rule.uc"), "\r\n", "\n")
	initScript := pinnedSource(t, firewall4Root, "root/etc/init.d/firewall")

	if !strings.Contains(template, "table inet fw4\nflush table inet fw4") || strings.Contains(template, "flush ruleset") {
		t.Fatal("ordinary firewall4 render no longer confines replacement to table inet fw4")
	}
	if !strings.Contains(ruleTemplate, "meta time >= {{ fw4.datestamp(rule.start_date) }}") || !strings.Contains(ruleTemplate, "meta time <= {{ fw4.datestamp(rule.stop_date) }}") {
		t.Fatal("locked firewall4 no longer consumes nft absolute-time comparisons")
	}
	stopBody := shellFunctionBody(t, fw4, "stop")
	if !strings.Contains(stopBody, "nft delete table inet fw4") || strings.Contains(stopBody, "nft list tables") {
		t.Fatal("firewall4 stop no longer deletes only table inet fw4")
	}
	flushBody := shellFunctionBody(t, fw4, "flush")
	requireOrdered(t, flushBody, "nft list tables", `nft delete table "$family" "$table"`)
	if !strings.Contains(shellFunctionBody(t, initScript, "stop_service"), "fw4 flush") {
		t.Fatal("OpenWrt firewall service stop no longer delegates to the all-table fw4 flush path")
	}
	restartCase := `restart)
		QUIET=1 print | nft ${VERBOSE} -c -f $STDIN || die "The rendered ruleset contains errors, not doing firewall restart."
		stop || rm -f $STATE
		start`
	if !strings.Contains(fw4, restartCase) {
		t.Fatal("firewall4 restart no longer validates then stops/restarts through the pinned bounded path")
	}
}

func packageDependencies(t testing.TB, recipe, packageName string) []string {
	t.Helper()
	body := definitionBody(t, recipe, "Package/"+packageName)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "DEPENDS:=") {
			return strings.Fields(strings.TrimPrefix(line, "DEPENDS:="))
		}
	}
	t.Fatalf("package %s has no dependency contract", packageName)
	return nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func kconfigPackageBody(t testing.TB, source, packageName string) string {
	t.Helper()
	startToken := "config PACKAGE_" + packageName + "\n"
	start := strings.Index(source, startToken)
	if start < 0 {
		t.Fatalf("generated Kconfig lacks package %s", packageName)
	}
	start += len(startToken)
	end := strings.Index(source[start:], "\n\tconfig PACKAGE_")
	if end < 0 {
		end = strings.Index(source[start:], "\nendmenu")
	}
	if end < 0 {
		t.Fatalf("generated Kconfig package %s has no bounded body", packageName)
	}
	return source[start : start+end]
}

func pinnedSource(t testing.TB, root, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func requireOrdered(t testing.TB, source string, values ...string) {
	t.Helper()
	position := 0
	for _, value := range values {
		index := strings.Index(source[position:], value)
		if index < 0 {
			t.Fatalf("pinned source lacks ordered fact %q", value)
		}
		position += index + len(value)
	}
}

func definitionBody(t testing.TB, source, name string) string {
	t.Helper()
	start := strings.Index(source, "define "+name)
	if start < 0 {
		t.Fatalf("source lacks definition %q", name)
	}
	start += len("define " + name)
	end := strings.Index(source[start:], "endef")
	if end < 0 {
		t.Fatalf("definition %q has no endef", name)
	}
	return source[start : start+end]
}

func functionBody(t testing.TB, source, signature string) string {
	t.Helper()
	start := strings.Index(source, signature)
	if start < 0 {
		t.Fatalf("source lacks function %q", signature)
	}
	start += len(signature)
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("function %q has no closing brace", signature)
	}
	return source[start : start+end]
}

func shellFunctionBody(t testing.TB, source, name string) string {
	t.Helper()
	signature := name + "() {"
	start := strings.Index(source, signature)
	if start < 0 {
		t.Fatalf("source lacks shell function %q", name)
	}
	start += len(signature)
	end := strings.Index(source[start:], "\n}")
	if end < 0 {
		t.Fatalf("shell function %q has no closing brace", name)
	}
	return source[start : start+end]
}
