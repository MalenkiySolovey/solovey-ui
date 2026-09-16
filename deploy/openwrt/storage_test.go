package openwrt

import (
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

const selectedMountFixture = "34 1 0:27 / / rw - overlay overlay rw,lowerdir=/root,upperdir=/data/root,workdir=/data/work\n" +
	"44 34 179:106 / /opt rw,relatime - ext4 /dev/mmcblk0p10 rw\n"

func directStorageFixture() StorageSelection {
	return StorageSelection{Schema: StorageSelectionSchema, DurableRoot: "/opt/solovey-ui", ProofClass: DirectPersistentMount, MountPoint: "/opt"}
}

func selectedEnvironment(mounts string) durabilityEnvironment {
	env := durabilityFixtureEnvironment(mounts, mountevidence.OverlayLabelOpaque)
	observe := env.Observe
	env.Observe = func(target string) (mountevidence.Fact, error) {
		if _, err := mountevidence.ParseUniqueMount([]byte(mounts), target); err != nil {
			return mountevidence.Fact{}, err
		}
		return observe(target)
	}
	env.Storage = directStorageFixture()
	env.ObserveBlockSource = func(mountevidence.Fact) error { return nil }
	return env
}

func TestSelectedDirectStorageAndStockBoundary(t *testing.T) {
	env := selectedEnvironment(selectedMountFixture)
	proof, err := createDatabaseDurabilityProof(env.Storage.DatabaseFolder(), env)
	if err != nil || proof.Validate() != nil || proof.ProofClass != DirectPersistentMount || proof.Overlay != nil {
		t.Fatalf("selected direct proof: %#v %v", proof, err)
	}
	if _, err := recheckDatabaseDurabilityProof(proof, 1, env, func(string) (uint64, error) { return 100, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, env); !errors.Is(err, ErrUnprovenDurableState) {
		t.Fatalf("wrong injected root accepted: %v", err)
	}
	env.Storage = StorageSelection{}
	if _, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, env); !errors.Is(err, ErrUnprovenDurableState) {
		t.Fatalf("hidden root accepted: %v", err)
	}
	if DefaultProfile().DatabaseFolder != "/etc/solovey-ui/db" || (StorageSelection{}).InstanceIDPath() != DefaultOpenWrtInstanceIDPath {
		t.Fatal("stock paths changed")
	}
}

func TestSelectedDirectStorageNegatives(t *testing.T) {
	for name, mounts := range map[string]string{
		"unmounted":    strings.Split(selectedMountFixture, "\n")[0] + "\n",
		"readonly":     strings.Replace(selectedMountFixture, "/opt rw,", "/opt ro,", 1),
		"volatile":     strings.Replace(selectedMountFixture, "ext4 /dev/mmcblk0p10", "tmpfs tmpfs", 1),
		"no-device":    strings.Replace(selectedMountFixture, "179:106", "0:106", 1),
		"no-source":    strings.Replace(selectedMountFixture, "/dev/mmcblk0p10", "none", 1),
		"bind-subtree": strings.Replace(selectedMountFixture, "179:106 / /opt", "179:106 /subtree /opt", 1),
		"nested-mount": selectedMountFixture + "45 44 8:2 / /opt/solovey-ui/db rw - ext4 /dev/other rw\n",
		"ambiguous":    selectedMountFixture + "45 34 8:2 / /opt rw - ext4 /dev/other rw\n",
	} {
		t.Run(name, func(t *testing.T) {
			env := selectedEnvironment(mounts)
			if _, err := createDatabaseDurabilityProof(env.Storage.DatabaseFolder(), env); !errors.Is(err, ErrUnprovenDurableState) {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
	for _, missing := range []bool{false, true} {
		env := selectedEnvironment(selectedMountFixture)
		env.ObserveBlockSource = func(mountevidence.Fact) error { return errors.New("backing missing") }
		if missing {
			env.ObserveBlockSource = nil
		}
		if _, err := createDatabaseDurabilityProof(env.Storage.DatabaseFolder(), env); !errors.Is(err, ErrUnprovenDurableState) {
			t.Fatal("unproven backing accepted")
		}
	}
	env := selectedEnvironment(selectedMountFixture)
	observe, calls := env.Observe, 0
	env.Observe = func(target string) (mountevidence.Fact, error) {
		f, err := observe(target)
		calls++
		if calls >= 2 {
			f.MountID++
			_ = f.Seal()
		}
		return f, err
	}
	if _, err := createDatabaseDurabilityProof(env.Storage.DatabaseFolder(), env); !errors.Is(err, ErrUnprovenDurableState) {
		t.Fatal("admission drift accepted")
	}
}

func TestStorageSelectionParsing(t *testing.T) {
	for _, data := range []string{`{}`, `null`, `{"durableRoot":"/opt"}`, `{"schema":"solovey-ui/deployment-storage/v1","durableRoot":"/opt/solovey-ui","proofClass":"FRIENDLYWRT","mountPoint":"/opt"}`} {
		if _, err := parseStorageSelection([]byte(data)); err == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	for _, root := range []string{"/etc/solovey-ui", "/opt", "/opt/../etc", "/opt/a\nb", "/opt/a b"} {
		s := directStorageFixture()
		s.DurableRoot = root
		if s.Validate() == nil {
			t.Fatalf("invalid root accepted: %q", root)
		}
	}
	// The same semantics support another deployment-selected mount; no distro
	// name or /opt literal participates in the implementation.
	s := directStorageFixture()
	s.MountPoint = "/srv/state"
	s.DurableRoot = "/srv/state/solovey-ui"
	if s.Validate() != nil {
		t.Fatal("generic direct mount selection rejected")
	}
}
