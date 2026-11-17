package backup

import "testing"

func TestNonportableRuntimeAuthorityExclusionsAreExplicit(t *testing.T) {
	want := map[string]string{
		"security_sessions":                   "NONPORTABLE_RUNTIME_AUTHORITY",
		"step_up_grants":                      "NONPORTABLE_RUNTIME_AUTHORITY",
		"inbound_endpoint_leases":             "NONPORTABLE_RUNTIME_AUTHORITY",
		"failover_state":                      "NONPORTABLE_RUNTIME_STATE",
		"ssh_managed_artifact_checkpoints_v1": "NONPORTABLE_HOST_AUTHORITY",
		"ssh_reconnect_challenges_v1":         "NONPORTABLE_RUNTIME_AUTHORITY",
	}
	got := map[string]string{}
	for _, table := range CoreTables() {
		if table.AlwaysExclude {
			got[table.Name] = table.ExclusionCode
		}
	}
	if len(got) != len(want) {
		t.Fatalf("nonportable exclusion inventory=%#v, want exactly %#v", got, want)
	}
	for name, reason := range want {
		if got[name] != reason {
			t.Fatalf("nonportable exclusion %q=%q, want %q", name, got[name], reason)
		}
	}
}
