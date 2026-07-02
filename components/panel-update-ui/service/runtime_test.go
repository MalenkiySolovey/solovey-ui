package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRuntimeManagerProtectsUpdateComponentFromSelfManagement(t *testing.T) {
	manager := RuntimeManager{}

	if _, err := manager.Disable(OperationContext{}, UpdateComponentID); err == nil || !strings.Contains(err.Error(), "cannot disable itself") {
		t.Fatalf("Disable(update component) error = %v, want self-disable rejection", err)
	}
	if _, err := manager.Remove(OperationContext{}, UpdateComponentID, false); err == nil || !strings.Contains(err.Error(), "cannot remove itself") {
		t.Fatalf("Remove(update component) error = %v, want self-remove rejection", err)
	}
}

func TestRuntimeManagerDropDataDelegatesToHost(t *testing.T) {
	var gotID string
	manager := RuntimeManager{
		DropData: func(_ context.Context, id string) error {
			gotID = id
			return nil
		},
		Timeout: time.Second,
	}

	if err := manager.dropData("telegram"); err != nil {
		t.Fatal(err)
	}
	if gotID != "telegram" {
		t.Fatalf("dropData id = %q, want telegram", gotID)
	}
}
