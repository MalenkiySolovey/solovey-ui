package deployment

import (
	"testing"
	"time"
)

func BenchmarkDeploymentProfileGeneration(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		profiles := Catalog()
		if len(profiles) != 6 {
			b.Fatal("profile catalog changed")
		}
	}
}

func BenchmarkDeploymentDoctorProjection(b *testing.B) {
	now := time.Unix(1_900_000_000, 0).UTC()
	profile, _ := Lookup(NativeHardened)
	posture := Posture{Schema: SchemaV1, Profile: profile.ID, InstalledProfile: profile.ID, ActiveProfile: profile.ID, VerifiedProfile: profile.ID,
		Runtime: profile.Runtime, PanelUID: 1001, PanelGID: 1001, BrokerAvailable: true, BrokerRevision: Revision("broker"),
		ServiceRevision: Revision("service"), DataRevision: Revision("data"), HardeningRevision: Revision("hardening"),
		ObservedAt: now.Unix(), ExpiresAt: now.Add(2 * time.Minute).Unix()}
	systemd := SystemdActualState{Schema: SchemaV1, Version: "systemd-257", ManagerBootRevision: Revision("boot"), DirectiveSupport: Available,
		DirectiveCapabilityRevision: Revision("directives"), Unit: "solovey-ui.service", FragmentRevision: Revision("fragment"), DropInRevision: Revision([]string{}),
		UnitFileState: "enabled", LoadState: "loaded", ActiveState: "active", SubState: "running", User: "solovey-ui", Group: "solovey-ui",
		NoNewPrivileges: true, SandboxRevision: Revision("sandbox"), ExecutableRevision: Revision("executable"), RuntimeDirectoryRevision: Revision("runtime"),
		ResourceRevision: Revision("resources"), Restart: "on-failure", RestartUSec: "5s", WatchdogUSec: "0", BrokerSocketRevision: Revision("sockets"),
		ObservedAt: now.Unix(), ExpiresAt: now.Add(2 * time.Minute).Unix()}
	systemd.Revision = Revision(systemd)
	posture.Systemd = &systemd
	SetPostureRevision(&posture)
	capabilities := Capabilities{Observe: Available, Doctor: Available, Migrate: Available, Rollback: Available}
	capabilities.Revision = Revision(capabilities)
	b.ReportAllocs()
	for b.Loop() {
		report := FinalizeDoctor(DoctorReport{Posture: &posture, Capabilities: capabilities, GeneratedAt: now.Unix()})
		if !report.Healthy || report.Verified != NativeHardened {
			b.Fatal("doctor projection changed")
		}
	}
}
