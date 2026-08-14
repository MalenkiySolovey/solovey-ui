package service

import (
	"strings"
	"testing"
)

func TestRuntimeManagerProtectsUpdateComponentFromSelfManagement(t *testing.T) {
	manager := RuntimeManager{}

	if _, err := manager.Disable(OperationContext{}, UpdateComponentID); err == nil || !strings.Contains(err.Error(), "cannot disable itself") {
		t.Fatalf("Disable(update component) error = %v, want self-disable rejection", err)
	}
	if _, err := manager.Remove(OperationContext{}, UpdateComponentID); err == nil || !strings.Contains(err.Error(), "cannot remove itself") {
		t.Fatalf("Remove(update component) error = %v, want self-remove rejection", err)
	}
}
