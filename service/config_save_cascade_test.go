package service

import (
	"reflect"
	"testing"
)

func TestConfigSaveCascadePolicies(t *testing.T) {
	tests := []struct {
		name        string
		policy      configSaveCascadePolicy
		wantObjects []string
		wantRestart bool
	}{
		{
			name:        "client inbound membership changed",
			policy:      configSaveCascadeClientInboundMembershipChanged,
			wantObjects: []string{"clients", "inbounds"},
			wantRestart: true,
		},
		{
			name:        "tls changed",
			policy:      configSaveCascadeTLSChanged,
			wantObjects: []string{"tls", "clients", "inbounds"},
			wantRestart: true,
		},
		{
			name:        "inbound changed",
			policy:      configSaveCascadeInboundChanged,
			wantObjects: []string{"inbounds", "clients"},
			wantRestart: true,
		},
		{
			name:        "core runtime changed",
			policy:      configSaveCascadeCoreRuntimeChanged,
			wantObjects: []string{"config"},
			wantRestart: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primary := tt.wantObjects[0]
			plan := newConfigSavePlan(primary)
			plan.ApplyCascade(tt.policy)
			if got := plan.Objects(); !reflect.DeepEqual(got, tt.wantObjects) {
				t.Fatalf("objects = %#v, want %#v", got, tt.wantObjects)
			}
			if got := plan.RequiresCoreRestart(); got != tt.wantRestart {
				t.Fatalf("restart = %v, want %v", got, tt.wantRestart)
			}
		})
	}
}

func TestConfigSaveCascadePolicyWithoutEffects(t *testing.T) {
	plan := newConfigSavePlan(configSaveObjectSettings.String())
	plan.ApplyCascade(configSaveCascadePolicy{})
	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"settings"}) {
		t.Fatalf("objects = %#v, want settings only", got)
	}
	if plan.RequiresCoreRestart() {
		t.Fatal("empty cascade should not require core restart")
	}
}
