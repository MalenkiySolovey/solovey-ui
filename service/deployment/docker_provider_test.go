package deployment

import (
	"slices"
	"testing"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

const hardenedDockerMountInfo = `36 25 0:32 / / ro,relatime - overlay overlay ro
37 36 8:1 /data /data rw,relatime - ext4 /dev/sda rw
38 36 8:1 /cert /cert rw,relatime - ext4 /dev/sda rw
39 36 0:41 / /tmp rw,nosuid,nodev,noexec - tmpfs tmpfs rw
40 36 0:42 / /run/solovey-ui rw,nosuid,nodev,noexec - tmpfs tmpfs rw
`

func TestDockerRuntimeProjectionIsTruthfulAndCapabilityExact(t *testing.T) {
	status := []byte("NoNewPrivs:\t1\nCapEff:\t0000000000000000\n")
	uidMap := []byte("         0          0 4294967295\n")
	facts, reasons := projectDockerRuntime(domain.DockerHost, 65532, 65532, status, []byte(hardenedDockerMountInfo), uidMap, false)
	if !facts.NoNewPrivileges || !facts.RootReadOnly || !facts.DataWritable || !facts.CertificateWritable || !facts.TemporaryTmpfs || !facts.RuntimeTmpfs || facts.UserNamespace != "initial" {
		t.Fatalf("unexpected facts: %#v", facts)
	}
	if !slices.Equal(reasons, []string{"docker_network_mode_unattested", "docker_engine_mode_unattested"}) {
		t.Fatalf("unattestable daemon facts were not explicit: %v", reasons)
	}

	// Bits 10, 12 and 13 are NET_BIND_SERVICE, NET_ADMIN and NET_RAW.
	advancedStatus := []byte("NoNewPrivs:\t1\nCapEff:\t0000000000003400\n")
	advanced, advancedReasons := projectDockerRuntime(domain.DockerNetworkAdvanced, 65532, 65532, advancedStatus, []byte(hardenedDockerMountInfo), uidMap, true)
	if !slices.Equal(advanced.EffectiveCapabilities, []string{"NET_ADMIN", "NET_BIND_SERVICE", "NET_RAW"}) ||
		!slices.Equal(advancedReasons, []string{"docker_network_mode_unattested", "docker_engine_mode_unattested"}) {
		t.Fatalf("advanced exact-capability projection facts=%#v reasons=%v", advanced, advancedReasons)
	}
}

func TestDockerRuntimeProjectionRejectsPrivilegeAndMountDrift(t *testing.T) {
	status := []byte("NoNewPrivs:\t0\nCapEff:\t0000000000200000\n") // SYS_ADMIN
	uidMap := []byte("         0       1000      65536\n")
	_, reasons := projectDockerRuntime(domain.DockerHost, 0, 0, status, []byte("36 25 0:32 / / rw - overlay overlay rw\n"), uidMap, false)
	for _, required := range []string{"docker_process_identity_mismatch", "docker_no_new_privileges_missing", "docker_capability_set_mismatch",
		"docker_root_filesystem_writable", "docker_bound_write_scope_mismatch", "docker_tmpfs_mismatch"} {
		if !slices.Contains(reasons, required) {
			t.Fatalf("missing %q in %v", required, reasons)
		}
	}
	if slices.Contains(reasons, "docker_user_namespace_unattested") {
		t.Fatalf("remapped user namespace was incorrectly reported unknown: %v", reasons)
	}
}

func TestDockerMountProjectionHandlesEscapedFieldsAndUnknownCapabilities(t *testing.T) {
	mounts := parseMountInfo([]byte("1 0 0:1 / /data\\040set rw - tmpfs tmpfs rw\n"))
	if !mountHas(mounts, "/data set", "rw", "tmpfs") {
		t.Fatalf("escaped mount field not decoded: %#v", mounts)
	}
	if got := effectiveCapabilities("not-hex"); !slices.Equal(got, []string{"UNKNOWN"}) {
		t.Fatalf("malformed capability evidence accepted: %v", got)
	}
}

func BenchmarkDockerProfileProjection(b *testing.B) {
	status := []byte("NoNewPrivs:\t1\nCapEff:\t0000000000000000\n")
	uidMap := []byte("         0          0 4294967295\n")
	mounts := []byte(hardenedDockerMountInfo)
	b.ReportAllocs()
	for b.Loop() {
		_, reasons := projectDockerRuntime(domain.DockerHost, 65532, 65532, status, mounts, uidMap, false)
		if len(reasons) != 2 {
			b.Fatal(reasons)
		}
	}
}
