package architecture

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPortableUpdateConsumesLifecycleSemanticsNotDeploymentIdentity(t *testing.T) {
	root := moduleRoot(t)
	updateRoot := filepath.Join(root, "service", "update")
	var violations []string
	walkGoFiles(t, updateRoot, func(path string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range portableUpdateDeploymentMarkers(string(data)) {
			violations = append(violations, filepath.ToSlash(mustRel(t, root, path))+" contains "+forbidden)
		}
	})
	if len(violations) > 0 {
		t.Fatalf("portable update owns deployment/package identity:\n%s", strings.Join(violations, "\n"))
	}
}

func portableUpdateDeploymentMarkers(source string) []string {
	var markers []string
	for _, forbidden := range []string{
		"SUI_DEPLOYMENT_KIND", "openwrt-package-managed", "docker-operator-managed",
		"docker_runtime_operator_managed", "docker_socket_not_used",
	} {
		if strings.Contains(strings.ToLower(source), strings.ToLower(forbidden)) {
			markers = append(markers, forbidden)
		}
	}
	// Match package-manager tokens, including apt-get, without rejecting ordinary
	// configuration-owner vocabulary such as Capture or adapter.
	markers = append(markers, regexp.MustCompile(`(?i)\b(?:apk|opkg|apt|dnf)\b`).FindAllString(source, -1)...)
	return markers
}

func TestPortableUpdateDeploymentMarkers(t *testing.T) {
	if got := portableUpdateDeploymentMarkers("CaptureProviderConfiguration adapter Snapshot"); len(got) != 0 {
		t.Fatalf("ordinary owner vocabulary matched deployment identity: %v", got)
	}
	for _, marker := range []string{"apt-get", "apt", "apk", "opkg", "dnf", "SUI_DEPLOYMENT_KIND", "docker_socket_not_used"} {
		if got := portableUpdateDeploymentMarkers(marker); len(got) != 1 {
			t.Fatalf("deployment identity %q was not detected: %v", marker, got)
		}
	}
}

func TestSemanticOwnersDoNotImportDeploymentAssetPackages(t *testing.T) {
	root := moduleRoot(t)
	semanticRoots := []string{
		filepath.Join("internal", "sshmanagement"),
		filepath.Join("service", "sshmanagement"),
		filepath.Join("service", "update"),
		filepath.Join("componenthost", "resources"),
		filepath.Join("components", "server-protection", "domain"),
		filepath.Join("components", "server-protection", "service"),
	}
	forbidden := []string{modulePath + "/deploy/openwrt", modulePath + "/deploy/systemd", modulePath + "/deploy/docker"}
	var violations []string
	for _, relativeRoot := range semanticRoots {
		walkGoFiles(t, filepath.Join(root, relativeRoot), func(path string) {
			if strings.HasSuffix(path, "_test.go") {
				return
			}
			for _, imported := range fileImports(t, path) {
				for _, deploymentPackage := range forbidden {
					if imported == deploymentPackage || strings.HasPrefix(imported, deploymentPackage+"/") {
						violations = append(violations, filepath.ToSlash(mustRel(t, root, path))+" imports "+imported)
					}
				}
			}
		})
	}
	if len(violations) > 0 {
		t.Fatalf("semantic owner imports deployment assets:\n%s", strings.Join(violations, "\n"))
	}
}
