package deployment

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProfileCatalogIsDeterministicExplicitAndCapabilityHonest(t *testing.T) {
	first, second := Catalog(), Catalog()
	if Revision(first) != Revision(second) || len(first) != 6 {
		t.Fatalf("catalog is not deterministic: %#v %#v", first, second)
	}
	fresh := map[Runtime]ProfileID{}
	for _, profile := range first {
		if profile.Revision == "" || profile.Runtime == RuntimeDocker && profile.BrokerRequired || profile.PanelRoot && profile.ID != NativeLegacyRoot {
			t.Fatalf("dishonest profile: %#v", profile)
		}
		if profile.FreshInstallDefault {
			if prior := fresh[profile.Runtime]; prior != "" {
				t.Fatalf("multiple fresh defaults for %s: %s and %s", profile.Runtime, prior, profile.ID)
			}
			fresh[profile.Runtime] = profile.ID
		}
		if profile.ID == NativeNetworkAdvanced || profile.ID == DockerNetworkAdvanced {
			if profile.Support != TierExperimental || len(profile.NetworkCapabilities) != 3 {
				t.Fatalf("advanced profile is not capability-explicit: %#v", profile)
			}
		} else if len(profile.NetworkCapabilities) != 0 {
			t.Fatalf("default profile has excess capabilities: %#v", profile)
		}
	}
	if fresh[RuntimeNative] != NativeHardened || fresh[RuntimeDocker] != DockerHost {
		t.Fatalf("fresh defaults=%v", fresh)
	}
}

func TestSystemdProfilesAndBrokerHaveBoundedAuthority(t *testing.T) {
	root := repositoryRoot(t)
	mainUnit := readArtifact(t, filepath.Join(root, "solovey-ui.service"))
	if !strings.Contains(mainUnit, "Environment=SUI_COMPONENTS_INSTALLED_FILE=/usr/local/solovey-ui/components/installed.json") {
		t.Fatal("primary native service unit does not bind shipped installed-component metadata")
	}
	hardened := readArtifact(t, filepath.Join(root, "deploy", "systemd", "solovey-ui-native-hardened.service"))
	for _, required := range []string{"User=solovey-ui", "Group=solovey-ui", "NoNewPrivileges=true", "ProtectSystem=strict", "ProtectHome=true",
		"PrivateDevices=true", "RestrictNamespaces=true", "CapabilityBoundingSet=\n", "AmbientCapabilities=\n", "TasksMax=4096",
		"Environment=SUI_COMPONENTS_INSTALLED_FILE=/usr/local/solovey-ui/components/installed.json",
		"Requires=solovey-privileged-broker.socket solovey-privileged-proof.socket"} {
		if !strings.Contains(hardened, required) {
			t.Fatalf("hardened unit misses %q", required)
		}
	}
	for _, forbidden := range []string{"User=root", "CAP_SYS_ADMIN", "Environment=PATH=", "ExecStart=/bin/sh", "PrivateDevices=false"} {
		if strings.Contains(hardened, forbidden) {
			t.Fatalf("hardened unit contains %q", forbidden)
		}
	}
	advanced := readArtifact(t, filepath.Join(root, "deploy", "systemd", "solovey-ui-native-network-advanced.service"))
	if !strings.Contains(advanced, "generated contract; unavailable") || !strings.Contains(advanced, "CapabilityBoundingSet=\n") ||
		!strings.Contains(advanced, "Environment=SUI_COMPONENTS_INSTALLED_FILE=/usr/local/solovey-ui/components/installed.json") ||
		strings.Contains(advanced, "CAP_NET_ADMIN") || strings.Contains(advanced, "DeviceAllow=/dev/net/tun") || strings.Contains(advanced, "CAP_SYS_ADMIN") {
		t.Fatal("advanced native contract grants network authority to the panel")
	}
	legacy := readArtifact(t, filepath.Join(root, "deploy", "systemd", "solovey-ui-native-legacy-root.service"))
	if !strings.Contains(legacy, "User=root") || !strings.Contains(legacy, "native-legacy-root compatibility") || !strings.Contains(legacy, "Requires=solovey-privileged-broker.socket") ||
		!strings.Contains(legacy, "Environment=SUI_COMPONENTS_INSTALLED_FILE=/usr/local/solovey-ui/components/installed.json") {
		t.Fatal("legacy profile is not explicit broker-backed compatibility")
	}
	brokerUnit := readArtifact(t, filepath.Join(root, "deploy", "systemd", "solovey-privileged-broker.service"))
	for _, required := range []string{"ExecStart=/usr/local/solovey-ui/releases/current/solovey-privileged-broker", "TasksMax=128", "LimitNOFILE=4096",
		"ReadWritePaths=/var/lib/solovey-ui-broker /var/lib/solovey-ui /usr/local/solovey-ui/releases", "-/etc/nginx -/run/nginx -/var/lib/nginx -/var/log/nginx", "/etc/systemd/system", "ProtectSystem=strict",
		"CapabilityBoundingSet=CAP_CHOWN CAP_DAC_OVERRIDE CAP_FOWNER CAP_NET_ADMIN CAP_NET_RAW CAP_SETGID CAP_SETUID CAP_SYS_PTRACE",
		"AmbientCapabilities=CAP_CHOWN CAP_DAC_OVERRIDE CAP_FOWNER CAP_NET_ADMIN CAP_NET_RAW CAP_SETGID CAP_SETUID CAP_SYS_PTRACE"} {
		if !strings.Contains(brokerUnit, required) {
			t.Fatalf("broker unit misses %q", required)
		}
	}
	if strings.Contains(brokerUnit, "CAP_SYS_ADMIN") || strings.Contains(brokerUnit, "ExecStart=/bin/") {
		t.Fatal("broker unit has generic or excess privilege")
	}
	for _, socket := range []string{"solovey-privileged-broker.socket", "solovey-privileged-proof.socket"} {
		unit := readArtifact(t, filepath.Join(root, "deploy", "systemd", socket))
		for _, required := range []string{"Accept=no", "SocketUser=root", "SocketGroup=solovey-ui", "SocketMode=0660", "RemoveOnStop=true"} {
			if !strings.Contains(unit, required) {
				t.Fatalf("%s misses %q", socket, required)
			}
		}
		if strings.Contains(unit, "ListenStream=0.0.0.0") || strings.Contains(unit, "ListenStream=[::]") {
			t.Fatalf("%s exposes TCP", socket)
		}
	}
}

func TestSystemdCIValidationFailsOnSpecifiedUnitErrors(t *testing.T) {
	root := repositoryRoot(t)
	script := readArtifact(t, filepath.Join(root, "tests", "installer", "systemd-contracts.sh"))
	for _, required := range []string{"--root=", "--recursive-errors=no", "security --offline=yes", "solovey-ui-native-network-advanced.service", "solovey-ui-native-legacy-root.service"} {
		if !strings.Contains(script, required) {
			t.Fatalf("systemd validation fixture misses %q", required)
		}
	}
}

func TestDockerProfilesHaveNoRootControlPath(t *testing.T) {
	root := repositoryRoot(t)
	base := readArtifact(t, filepath.Join(root, "docker-compose.yml"))
	for _, required := range []string{"user: \"65532:65532\"", "network_mode: host", "read_only: true", "cap_drop: [\"ALL\"]",
		"no-new-privileges:true", "/tmp:rw,noexec,nosuid,nodev,size=64m", "/run/solovey-ui:rw,noexec,nosuid,nodev,size=16m",
		"pids_limit: 4096", "mem_reservation:", "ulimits:", "nofile:", "soft: 16384", "hard: 16384", "restart: unless-stopped",
		"stop_grace_period:", "max-size: \"10m\"", "@sha256:", "healthcheck:", "SUI_COMPONENTS_INSTALLED_FILE: /app/components/installed.json"} {
		if !strings.Contains(base, required) {
			t.Fatalf("default Compose misses %q", required)
		}
	}
	dockerfile := readArtifact(t, filepath.Join(root, "Dockerfile"))
	dockerignore := readArtifact(t, filepath.Join(root, ".dockerignore"))
	for _, generated := range []string{"app/components_generated.go", "cmd/optional_commands_generated.go"} {
		if !containsExactLine(dockerignore, generated) {
			t.Fatalf("Docker build context retains sync-only generated source %q", generated)
		}
	}
	for _, required := range []string{
		"type=cache,id=solovey-ui-go-build,target=/root/.cache/go-build,sharing=locked",
		"type=cache,id=solovey-ui-go-mod,target=/go/pkg/mod,sharing=locked",
		"SUI_COMPONENTS_INSTALLED_FILE=/app/components/installed.json",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Fatalf("Dockerfile misses bounded reusable build cache %q", required)
		}
	}
	for _, forbidden := range []string{"privileged: true", "SYS_ADMIN", "/var/run/docker.sock", "DOCKER_HOST", "pid: host", "ipc: host", "stdin_open: true", "tty: true", "watchtower", "privileged-broker.sock", "2375"} {
		if strings.Contains(strings.ToLower(base), strings.ToLower(forbidden)) {
			t.Fatalf("default Compose contains forbidden %q", forbidden)
		}
	}
	bridge := readArtifact(t, filepath.Join(root, "deploy", "docker", "docker-compose.bridge.yml"))
	if !strings.Contains(bridge, "docker-bridge-explicit") || !strings.Contains(bridge, "SOLOVEY_UI_PANEL_PORT:?") || strings.Contains(bridge, "privileged:") {
		t.Fatal("bridge profile is not explicit/bounded")
	}
	advanced := readArtifact(t, filepath.Join(root, "deploy", "docker", "docker-compose.network-advanced.yml"))
	for _, exact := range []string{"NET_ADMIN", "NET_BIND_SERVICE", "NET_RAW", "/dev/net/tun:/dev/net/tun", "docker-network-advanced"} {
		if !strings.Contains(advanced, exact) {
			t.Fatalf("advanced Compose misses %q", exact)
		}
	}
	if strings.Contains(advanced, "SYS_ADMIN") || strings.Contains(advanced, "privileged: true") {
		t.Fatal("advanced Compose grants excess authority")
	}
	if !strings.Contains(dockerfile, "USER 65532:65532") || strings.Contains(dockerfile, "apk add --no-cache nftables") || strings.Contains(dockerfile, "solovey-privileged-broker") {
		t.Fatal("Docker image has a root control path")
	}
}

func containsExactLine(value, expected string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		if line == expected {
			return true
		}
	}
	return false
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller path unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readArtifact(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
