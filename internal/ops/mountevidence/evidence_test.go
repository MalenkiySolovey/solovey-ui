package mountevidence

import (
	"strings"
	"testing"
)

const fixture = "36 25 0:32 / / rw,relatime - overlay overlay rw,lowerdir=/rom,upperdir=/overlay/upper,workdir=/overlay/work\n" +
	"40 36 0:35 / /overlay rw,relatime - ext4 /dev/mmcblk0p4 rw,data=ordered\n" +
	"44 36 0:41 / /run rw,nosuid,nodev - tmpfs tmpfs rw,size=16384k\n" +
	"45 44 0:41 /solovey /run/solovey-ui rw,nosuid,nodev - tmpfs tmpfs rw,size=16384k\n"

func TestParseSelectsLongestContainingMountAndSealsStatfs(t *testing.T) {
	fact, err := Parse([]byte(fixture), "/run/solovey-ui/server-protection")
	if err != nil {
		t.Fatal(err)
	}
	fact, err = BindStatfs(fact, "/run/solovey-ui/server-protection", false, 0x01021994)
	if err != nil {
		t.Fatal(err)
	}
	if fact.MountID != 45 || fact.MountPoint != "/run/solovey-ui" || fact.Filesystem != "tmpfs" || !fact.Writable() || fact.Validate() != nil {
		t.Fatalf("fact = %#v", fact)
	}
}

func TestParseCarriesOverlayBackingPaths(t *testing.T) {
	fact, err := Parse([]byte(fixture), "/etc/solovey-ui/db")
	if err != nil {
		t.Fatal(err)
	}
	fact, err = BindStatfs(fact, "/etc/solovey-ui/db", false, 0x794c7630)
	if err != nil {
		t.Fatal(err)
	}
	if fact.OptionValue("upperdir") != "/overlay/upper" || fact.OptionValue("workdir") != "/overlay/work" {
		t.Fatalf("overlay options = %#v", fact.SuperOptions)
	}
	if fact.HasOption("volatile") {
		t.Fatal("ordinary overlay unexpectedly carries volatile option")
	}
}

func TestOverlayLabelAndBackingProbeFactsStayPolicyFreeAndSealed(t *testing.T) {
	opaque := OverlayLabelFact{Label: "/hidden/upper", Visibility: OverlayLabelOpaque}
	if err := opaque.Seal(); err != nil || opaque.Validate() != nil {
		t.Fatalf("opaque label = %#v, %v", opaque, err)
	}
	probe := BackingProbeFact{
		Target: "/etc/solovey-ui/db", MountRevision: strings.Repeat("a", 64), Mapping: BackingMappingPhysical,
		BytesWritten: BackingProbeBytes, ExtentCount: 1, PhysicalBytes: BackingProbeBytes,
		FileSynced: true, FilesystemSynced: true, DirectorySynced: true,
	}
	if err := probe.Seal(); err != nil || probe.Validate() != nil {
		t.Fatalf("probe = %#v, %v", probe, err)
	}
	unsupported := BackingProbeFact{
		Target: "/etc/solovey-ui/db", MountRevision: strings.Repeat("b", 64), Mapping: BackingMappingUnsupported,
		BytesWritten: BackingProbeBytes, FileSynced: true, FilesystemSynced: true, DirectorySynced: true,
	}
	if err := unsupported.Seal(); err != nil || unsupported.Validate() != nil {
		t.Fatalf("unsupported mapping fact = %#v, %v", unsupported, err)
	}
	probe.Mapping = "DURABLE"
	if err := probe.Validate(); err == nil {
		t.Fatal("policy-bearing probe classification was accepted")
	}
}

func TestParseRejectsMalformedOrUnboundedEvidence(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("invalid\n"), []byte("1 2 0:1 / / rw - tmpfs tmpfs rw\\00\n")} {
		if _, err := Parse(data, "/run/x"); err == nil {
			t.Fatalf("Parse(%q) unexpectedly succeeded", data)
		}
	}
}

func TestFilesystemMountPointsReturnsExactCanonicalType(t *testing.T) {
	data := fixture + "50 36 0:50 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime - cgroup2 cgroup rw\n"
	points, err := FilesystemMountPoints([]byte(data), "cgroup2")
	if err != nil || len(points) != 1 || points[0] != "/sys/fs/cgroup" {
		t.Fatalf("cgroup2 points = %v, %v", points, err)
	}
}

func TestParseAllowsKernelRootMountWithZeroParentID(t *testing.T) {
	fact, err := Parse([]byte("1 0 8:1 / / rw - ext4 /dev/root rw\n"), "/etc")
	if err != nil {
		t.Fatal(err)
	}
	fact, err = BindStatfs(fact, "/etc", false, 0xEF53)
	if err != nil || fact.ParentID != 0 || fact.Validate() != nil {
		t.Fatalf("root mount fact = %#v, %v", fact, err)
	}
}

func TestParseAllowsLiteralBackslashInFilesystemSpecificOptions(t *testing.T) {
	data := "1 0 0:1 / / rw - 9p C:\\134 rw,aname=drvfs;path=C:\\;uid=1000\n" + fixture
	fact, err := Parse([]byte(data), "/etc/solovey-ui/db")
	if err != nil {
		t.Fatal(err)
	}
	if fact.Filesystem != "overlay" || fact.OptionValue("upperdir") != "/overlay/upper" {
		t.Fatalf("selected fact = %#v", fact)
	}
}
