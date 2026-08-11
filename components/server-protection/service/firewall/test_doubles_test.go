package firewall

import (
	"context"
	"errors"

	helperinvoker "github.com/MalenkiySolovey/solovey-ui/components/server-protection/internal/normalci/helperinvoker"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type testHelperInvoker = helperinvoker.Invoker

func newTestHelperInvoker(capabilities *protectionhelper.CapabilitiesResult) *testHelperInvoker {
	return helperinvoker.New(capabilities)
}

type MockRecovery struct {
	HasArtifact func(context.Context, protectionrepository.OperationLockModel) (bool, error)
	Rollback    func(context.Context, protectionrepository.OperationLockModel) error
	Bundle      func(context.Context, protectionrepository.OperationLockModel, string) error
}

func (m MockRecovery) HasMutationArtifact(ctx context.Context, item protectionrepository.OperationLockModel) (bool, error) {
	if m.HasArtifact != nil {
		return m.HasArtifact(ctx, item)
	}
	return true, nil
}

func (m MockRecovery) AttemptRollback(ctx context.Context, item protectionrepository.OperationLockModel) error {
	if m.Rollback == nil {
		return errors.New("mock rollback is unavailable")
	}
	return m.Rollback(ctx, item)
}

func (m MockRecovery) CreateBundle(ctx context.Context, item protectionrepository.OperationLockModel, state string) error {
	if m.Bundle != nil {
		return m.Bundle(ctx, item, state)
	}
	return nil
}
