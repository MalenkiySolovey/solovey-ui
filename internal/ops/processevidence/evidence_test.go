package processevidence

import "testing"

func TestParsersPreserveNeutralProcessAndCgroupFacts(t *testing.T) {
	stat := []byte("99 (name with ) parens) S 7 8 9 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 424242 0\n")
	parent, session, start, err := parseStat(stat)
	if err != nil || parent != 7 || session != 9 || start != "424242" {
		t.Fatalf("stat facts = %d %d %q, %v", parent, session, start, err)
	}
	uid, gid, groups, err := parseStatus([]byte("Uid:\t1000 1000 1000 1000\nGid:\t1001 1001 1001 1001\nGroups:\t1001 27\n"))
	if err != nil || uid != 1000 || gid != 1001 || len(groups) != 2 || groups[1] != 27 {
		t.Fatalf("credential facts = %d %d %#v, %v", uid, gid, groups, err)
	}
	cgroups, err := parseCgroups([]byte("0::/services/solovey-ui/panel\n2:cpu,memory:/tenant\n"))
	if err != nil || len(cgroups) != 2 || cgroups[0].Path != "/services/solovey-ui/panel" || len(cgroups[1].Controllers) != 2 {
		t.Fatalf("cgroup facts = %#v, %v", cgroups, err)
	}
}

func TestMalformedEvidenceFailsClosed(t *testing.T) {
	if _, _, _, err := parseStat([]byte("broken")); err == nil {
		t.Fatal("malformed stat accepted")
	}
	if _, _, _, err := parseStatus([]byte("Uid:\t1 2 1 1\nGid:\t1 1 1 1\n")); err == nil {
		t.Fatal("unstable credentials accepted")
	}
	if _, err := parseCgroups([]byte("0::relative\n")); err == nil {
		t.Fatal("relative cgroup accepted")
	}
}
