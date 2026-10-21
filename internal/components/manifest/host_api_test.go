package manifest

import "testing"

func TestHostAPIRejectsUnknownOrAmbiguousAuthority(t *testing.T) {
	for _, contract := range []*HostAPI{
		{Version: "future"},
		{Version: HostAPIV1, Lifecycle: []string{"runtime"}},
		{Version: HostAPIV1, API: []string{"database"}},
		{Version: HostAPIV1, Lifecycle: []string{PortAuditRecord, PortAuditRecord}},
		{Version: HostAPIV1, API: []string{PortScheduler}},
		{Version: HostAPIV1, API: []string{PortSettingsDrop}},
		{Version: HostAPIV1, Lifecycle: []string{PortHTTPRequest}},
		{Version: HostAPIV1, DropData: []string{"database"}},
	} {
		if err := contract.Validate(); err == nil {
			t.Fatalf("unsupported Host API accepted: %+v", contract)
		}
	}
	contract := &HostAPI{Version: HostAPIV1, Lifecycle: []string{PortScheduler, PortOutboundHTTP, PortAuditRecord},
		API: []string{PortHTTPRequest, PortOutboundHTTP, PortAuditRecord}}
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	copy := contract.Clone()
	copy.Lifecycle[0], copy.API[0] = "runtime", "database"
	if err := contract.Validate(); err != nil {
		t.Fatal("mutating a projection changed the original contract", err)
	}
}
