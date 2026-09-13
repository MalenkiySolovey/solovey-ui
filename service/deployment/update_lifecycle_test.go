package deployment

import "testing"

func TestDeploymentProvidersProjectUpdateLifecycleSemantics(t *testing.T) {
	fixtures := []struct {
		name string
		got  UpdateLifecycle
		want UpdateLifecycle
	}{
		{"systemd", (&SystemdBrokerProvider{}).UpdateLifecycle(), UpdateLifecycleSelfManaged},
		{"procd-package", (&OpenWrtPackageManagedProvider{}).UpdateLifecycle(), UpdateLifecyclePackageManaged},
		{"docker", (&DockerProvider{}).UpdateLifecycle(), UpdateLifecycleOperatorManaged},
		{"unavailable", (UnavailableProvider{}).UpdateLifecycle(), UpdateLifecycleUnavailable},
	}
	for _, fixture := range fixtures {
		if fixture.got != fixture.want {
			t.Errorf("%s update lifecycle=%q want=%q", fixture.name, fixture.got, fixture.want)
		}
	}
}

func TestOnlyDockerProjectsReleasedUpdateDisplayAlias(t *testing.T) {
	docker := NewManager(Repository{}, &DockerProvider{})
	if got := docker.UpdatePresentation().LegacyMode; got != "docker-operator-managed" {
		t.Fatalf("Docker compatibility mode=%q", got)
	}
	for name, provider := range map[string]Provider{
		"systemd":       &SystemdBrokerProvider{},
		"procd-package": &OpenWrtPackageManagedProvider{},
		"unavailable":   UnavailableProvider{},
	} {
		if got := NewManager(Repository{}, provider).UpdatePresentation().LegacyMode; got != "" {
			t.Fatalf("%s unexpectedly projects Docker compatibility mode %q", name, got)
		}
	}
}
