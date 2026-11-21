//go:build linux && !minimal

package api

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	_ "github.com/MalenkiySolovey/solovey-ui/components/zapret"
	"github.com/gin-gonic/gin"
)

// The native ownership gate opts into actual root installer -> service UID
// execution. Ordinary portable API tests do not require a privileged host.
func TestNativeInventoryServiceUIDMigrationAndRoutes(t *testing.T) {
	if os.Getenv("SOLOVEY_TEST_NATIVE_COMPONENTS") == "" {
		t.Skip("native ownership executor required")
	}
	if os.Geteuid() != 0 {
		if os.Geteuid() != 12346 || os.Getegid() != 12345 {
			t.Fatal("unexpected service credential")
		}
		initSessionTestDB(t)
		if _, exists, err := installstate.Load(installstate.DefaultPath()); err != nil || !exists {
			t.Fatalf("inventory read: %v %v", exists, err)
		}
		host := supervisor.New(componenthost.Deps{})
		if err := host.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := host.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = host.Stop(context.Background()) })
		components, err := state.Components()
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(components, func(item state.Component) bool { return item.ID == "zapret" })
		if index < 0 || !components[index].AvailableInBinary || components[index].Enabled || components[index].Active {
			t.Fatal("the Full binary must register Zapret without activating its lifecycle")
		}
		gin.SetMode(gin.TestMode)
		router := gin.New()
		(&APIHandler{ApiService: newComposedApiService()}).initRouter(router.Group("/api"))
		for _, route := range expectedOptionalAPIGetRoutes() {
			if !routeExists(router, http.MethodGet, route) {
				t.Fatalf("missing installed route %s", route)
			}
		}
		if !routeExists(router, http.MethodGet, "/api/components/server-protection/status") {
			t.Fatal("missing protection routes")
		}
		if !routeExists(router, http.MethodGet, "/api/update/components") {
			t.Fatal("missing intrinsic component inventory route")
		}
		return
	}
	dir, err := os.MkdirTemp("/var/tmp", "solovey-component-api-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	installer, err := filepath.Abs("../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, item := range registry.Components() {
		ids = append(ids, item.Manifest.ID)
	}
	slices.Sort(ids)
	want := []string{"fallback-html", "import-xui", "observability-extra", "paid-subscriptions", "remote-outbound-subscriptions", "server-protection", "telegram", "zapret"}
	if !slices.Equal(ids, want) {
		t.Fatalf("active Full registry = %v, want %v", ids, want)
	}
	command := exec.Command("bash", "-c", `source "$1"; INSTALL_DIR="$2/app"; mkdir -p "$INSTALL_DIR"; chmod 755 "$INSTALL_DIR"; REQUESTED_PROFILE=full; read -ra COMPONENT_IDS <<< "$3"; resolve_binary_profile; write_component_metadata 12345`, "bash", installer, dir, strings.Join(ids, " "))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("installer: %v: %s", err, output)
	}
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe")
	if err := os.WriteFile(probe, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	command = exec.Command(probe, "-test.run=^TestNativeInventoryServiceUIDMigrationAndRoutes$", "-test.v")
	command.Dir = dir
	command.Env = append(os.Environ(), installstate.InstalledFileEnv+"="+filepath.Join(dir, "app/components/installed.json"), installstate.ManagementEnv+"=installer")
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 12346, Gid: 12345}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("nonroot startup: %v: %s", err, output)
	}
}
