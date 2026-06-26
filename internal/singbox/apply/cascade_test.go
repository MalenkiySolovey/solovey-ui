package apply

import (
	"reflect"
	"testing"
)

func TestCascadePolicies(t *testing.T) {
	tests := []struct {
		name        string
		primary     Object
		policy      CascadePolicy
		wantObjects []string
		wantRestart bool
	}{
		{
			name:        "client inbound membership changed",
			primary:     ObjectClients,
			policy:      CascadeClientInboundMembershipChanged,
			wantObjects: []string{"clients", "inbounds"},
		},
		{
			name:        "tls changed",
			primary:     ObjectTLS,
			policy:      CascadeTLSChanged,
			wantObjects: []string{"tls", "clients", "inbounds"},
			wantRestart: true,
		},
		{
			name:        "inbound changed",
			primary:     ObjectInbounds,
			policy:      CascadeInboundChanged,
			wantObjects: []string{"inbounds", "clients"},
		},
		{
			name:        "core runtime changed",
			primary:     ObjectConfig,
			policy:      CascadeCoreRuntimeChanged,
			wantObjects: []string{"config"},
			wantRestart: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := NewPlan(tt.primary.String())
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

func TestCascadePolicyWithoutEffects(t *testing.T) {
	plan := NewPlan(ObjectSettings.String())
	plan.ApplyCascade(CascadePolicy{})
	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"settings"}) {
		t.Fatalf("objects = %#v, want settings only", got)
	}
	if plan.RequiresCoreRestart() {
		t.Fatal("empty cascade should not require core restart")
	}
}
