package openwrt

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

// Pinned fstools source/source options plus the preserved physical filesystem
// class; mount IDs and device identity are fixture facts, not product policy.
const durabilityMounts = "36 25 0:32 / / rw,relatime - overlay overlayfs:/overlay rw,lowerdir=/rom,upperdir=/overlay/upper,workdir=/overlay/work\n" +
	"40 36 7:0 / /overlay rw,relatime - f2fs /dev/loop0 rw\n" +
	"41 36 0:41 / /tmp rw,nosuid,nodev - tmpfs tmpfs rw,size=16384k\n"

func TestDatabaseDurabilityPinnedFSToolsNoPhysicalProbe(t *testing.T) {
	for _, visibility := range []string{mountevidence.OverlayLabelVisible, mountevidence.OverlayLabelOpaque} {
		t.Run(visibility, func(t *testing.T) {
			environment := durabilityFixtureEnvironment(durabilityMounts, visibility)
			proof, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, environment)
			if err != nil {
				t.Fatal(err)
			}
			if proof.ProofClass != PinnedFSToolsOverlay || proof.Overlay == nil || proof.Overlay.BackingMount.Filesystem != "f2fs" {
				t.Fatalf("proof=%#v", proof)
			}
			data, err := json.Marshal(proof)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "overlayBacking") || strings.Contains(string(data), "fileSynced") {
				t.Fatal("fabricated probe evidence")
			}
			var decoded DatabaseDurabilityProofV3
			if err := json.Unmarshal(data, &decoded); err != nil || decoded.Validate() != nil {
				t.Fatalf("round trip: %v", err)
			}
			evidence, err := recheckDatabaseDurabilityProof(decoded, 4096, environment, func(string) (uint64, error) { return 8192, nil })
			if err != nil || !evidence.Persistent {
				t.Fatalf("recheck=%#v %v", evidence, err)
			}
		})
	}
	// The former injected physical callback has been removed altogether. This
	// guard covers both the producer and live recheck and prevents reintroduction.
	if _, exists := reflect.TypeFor[durabilityEnvironment]().FieldByName("ProbeBacking"); exists {
		t.Fatal("critical path regained physical-probe injection")
	}
	for _, file := range []string{"durability.go", "durability_environment_linux.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"ProbeOverlayBacking", "ProbeBacking", "SYS_SYNCFS", "Syncfs(", "exec.Command"} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s regained %s", file, forbidden)
			}
		}
	}
}

func TestDatabaseDurabilityPersistentFamilies(t *testing.T) {
	for _, filesystem := range ApprovedPersistentFilesystemNames() {
		t.Run(filesystem, func(t *testing.T) {
			mounts := strings.Replace(durabilityMounts, "- f2fs /dev/loop0", "- "+filesystem+" /dev/persistent", 1)
			env := durabilityFixtureEnvironment(mounts, mountevidence.OverlayLabelVisible)
			if _, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, env); err != nil {
				t.Fatal(err)
			}
			direct := "40 25 8:1 / / rw - " + filesystem + " /dev/persistent rw\n"
			env = durabilityFixtureEnvironment(direct, mountevidence.OverlayLabelVisible)
			env.ObserveLabel = nil
			env.PersistenceAuthority = PersistenceAuthority{}
			proof, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, env)
			if err != nil || proof.ProofClass != DirectPersistentMount || proof.Overlay != nil {
				t.Fatalf("direct=%#v %v", proof, err)
			}
		})
	}
}

func TestDatabaseDurabilityFailClosedTopology(t *testing.T) {
	cases := map[string]string{
		"unknown-source":      strings.Replace(durabilityMounts, "overlayfs:/overlay", "overlay", 1),
		"read-only":           strings.Replace(durabilityMounts, " / / rw,", " / / ro,", 1),
		"volatile":            strings.Replace(durabilityMounts, "workdir=/overlay/work", "workdir=/overlay/work,volatile", 1),
		"fsync-volatile":      strings.Replace(durabilityMounts, "workdir=/overlay/work", "workdir=/overlay/work,fsync=volatile", 1),
		"missing-work":        strings.Replace(durabilityMounts, ",workdir=/overlay/work", "", 1),
		"relative-upper":      strings.Replace(durabilityMounts, "upperdir=/overlay/upper", "upperdir=overlay/upper", 1),
		"ramoverlay":          strings.ReplaceAll(durabilityMounts, "/overlay", "/tmp/root"),
		"volatile-backing":    strings.Replace(durabilityMounts, "- f2fs /dev/loop0", "- tmpfs tmpfs", 1),
		"unsupported-backing": strings.Replace(durabilityMounts, "- f2fs /dev/loop0", "- ntfs3 /dev/loop0", 1),
		"missing-backing":     strings.Split(durabilityMounts, "\n")[0] + "\n",
		// Source/image/live witness: an initramfs can leave an opaque upper
		// filesystem after switch_root. Its labels and a separate persistent
		// /opt mount do not establish the database's current backing identity.
		"initramfs-hidden-upper": "34 1 0:27 / / rw,noatime - overlay overlay rw,lowerdir=/root,upperdir=/data/root,workdir=/data/work\n" +
			"44 34 179:106 / /opt rw,relatime - ext4 /dev/persistent rw\n",
		"read-only-backing": strings.Replace(durabilityMounts, " / /overlay rw,", " / /overlay ro,", 1),
		"nested-upper":      durabilityMounts + "42 40 0:42 / /overlay/upper rw - tmpfs tmpfs rw\n",
		"nested-work":       durabilityMounts + "42 40 0:42 / /overlay/work rw - ext4 /dev/other rw\n",
		"nested-database":   durabilityMounts + "42 36 0:42 / /etc/solovey-ui/db rw - overlay overlayfs:/overlay rw,upperdir=/overlay/upper,workdir=/overlay/work\n",
	}
	for name, mounts := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, durabilityFixtureEnvironment(mounts, mountevidence.OverlayLabelVisible))
			if !errors.Is(err, ErrUnprovenDurableState) {
				t.Fatalf("accepted topology: %v", err)
			}
		})
	}
	for _, authority := range []PersistenceAuthority{{}, {FSToolsSource: "unqualified-fstools"}} {
		env := durabilityFixtureEnvironment(durabilityMounts, mountevidence.OverlayLabelVisible)
		env.PersistenceAuthority = authority
		if _, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, env); !errors.Is(err, ErrUnprovenDurableState) {
			t.Fatalf("accepted authority %#v", authority)
		}
	}
}

func TestDatabaseDurabilityRejectsTamperedProofAndDrift(t *testing.T) {
	env := durabilityFixtureEnvironment(durabilityMounts, mountevidence.OverlayLabelVisible)
	proof, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, env)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*DatabaseDurabilityProofV3){
		"legacy-schema":    func(p *DatabaseDurabilityProofV3) { p.Schema = "solovey-ui/openwrt-database-durability/v2" },
		"unknown-class":    func(p *DatabaseDurabilityProofV3) { p.ProofClass = "PHYSICAL_PROBE" },
		"missing-platform": func(p *DatabaseDurabilityProofV3) { p.Overlay.Authority = PersistenceAuthority{} },
		"missing-overlay":  func(p *DatabaseDurabilityProofV3) { p.Overlay = nil },
		"false-direct":     func(p *DatabaseDurabilityProofV3) { p.ProofClass = DirectPersistentMount; p.Overlay = nil },
		"wrong-target":     func(p *DatabaseDurabilityProofV3) { p.DatabaseFolder = "/tmp/db" },
		"contradictory-backing": func(p *DatabaseDurabilityProofV3) {
			p.Overlay.BackingMount.Filesystem = "tmpfs"
			_ = p.Overlay.BackingMount.Seal()
		},
		"symlink-label": func(p *DatabaseDurabilityProofV3) {
			p.Overlay.UpperLabel.Mount.ResolvedTarget = "/overlay/elsewhere"
			_ = p.Overlay.UpperLabel.Mount.Seal()
			_ = p.Overlay.UpperLabel.Seal()
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(proof)
			var copy DatabaseDurabilityProofV3
			_ = json.Unmarshal(data, &copy)
			mutate(&copy)
			copy.Revision = copy.revision() // semantic invalidity must survive resealing
			if copy.Validate() == nil {
				t.Fatal("invalid semantic proof accepted")
			}
		})
	}
	for _, replacement := range []struct{ old, new string }{{"36 25 0:32", "37 25 0:32"}, {"40 36 7:0", "44 36 7:0"}, {"- f2fs /dev/loop0", "- tmpfs tmpfs"}} {
		changed := durabilityFixtureEnvironment(strings.Replace(durabilityMounts, replacement.old, replacement.new, 1), mountevidence.OverlayLabelVisible)
		if _, err := recheckDatabaseDurabilityProof(proof, 1, changed, func(string) (uint64, error) { return 2, nil }); !errors.Is(err, ErrUnprovenDurableState) {
			t.Fatalf("drift accepted: %v", err)
		}
	}
	// Drift during creation and stale visible label observations both fail.
	drift := env
	calls := 0
	drift.Observe = func(target string) (mountevidence.Fact, error) {
		calls++
		if calls > 2 {
			return durabilityObserver(strings.Replace(durabilityMounts, "40 36 7:0", "44 36 7:0", 1))(target)
		}
		return env.Observe(target)
	}
	if _, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, drift); !errors.Is(err, ErrUnprovenDurableState) {
		t.Fatalf("mid-observation drift: %v", err)
	}
	if _, err := recheckDatabaseDurabilityProof(proof, 8192, env, func(string) (uint64, error) { return 1, nil }); err == nil {
		t.Fatal("capacity failure ignored")
	}
}

func durabilityFixtureEnvironment(data, visibility string) durabilityEnvironment {
	observe := durabilityObserver(data)
	return durabilityEnvironment{
		Observe: observe, PersistenceAuthority: selectedPersistenceAuthority(),
		ObserveLabel: func(label string) (mountevidence.OverlayLabelFact, error) {
			fact := mountevidence.OverlayLabelFact{Label: label, Visibility: visibility}
			if visibility == mountevidence.OverlayLabelVisible {
				mount, err := observe(label)
				if err != nil {
					return mountevidence.OverlayLabelFact{}, err
				}
				fact.Mount = mount
			}
			return fact, fact.Seal()
		},
	}
}

func durabilityObserver(data string) mountObserver {
	return func(target string) (mountevidence.Fact, error) {
		fact, err := mountevidence.Parse([]byte(data), target)
		if err != nil {
			return mountevidence.Fact{}, err
		}
		magic := int64(0xEF53)
		switch fact.Filesystem {
		case "overlay":
			magic = 0x794c7630
		case "tmpfs":
			magic = 0x01021994
		case "f2fs":
			magic = 0xF2F52010
		}
		return mountevidence.BindStatfs(fact, target, false, magic)
	}
}

func TestOpenWrtPersistentFilesystemPolicyIsExact(t *testing.T) {
	want := []string{"ext2", "ext3", "ext4", "f2fs", "ubifs", "jffs2", "btrfs", "xfs"}
	if !slices.Equal(ApprovedPersistentFilesystemNames(), want) {
		t.Fatal("persistent policy changed")
	}
	for _, filesystem := range []string{"ntfs", "ntfs3", "tmpfs", "ramfs", "overlay", ""} {
		if ApprovedPersistentFilesystem(filesystem) {
			t.Fatalf("unapproved %q", filesystem)
		}
	}
}

func TestDurabilityProductionNoBackingProbeCallers(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || (strings.HasPrefix(entry.Name(), ".") && name != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.Contains(filepath.ToSlash(name), "internal/ops/mountevidence/") {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "ProbeOverlayBacking(") {
			t.Errorf("unbounded probe production caller: %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
