package firewall

import protectiondata "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"

import (
	"context"
	"errors"

	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	helperinvoker "github.com/MalenkiySolovey/solovey-ui/testsupport/serverprotectionhelper"
)

type testHelperInvoker = helperinvoker.Invoker

func newTestHelperInvoker(capabilities *protectionhelper.CapabilitiesResult) *testHelperInvoker {
	return helperinvoker.New(capabilities)
}

type MockRecovery struct {
	HasArtifact func(context.Context, protectiondata.OperationLockModel) (bool, error)
	Rollback    func(context.Context, protectiondata.OperationLockModel) error
	Bundle      func(context.Context, protectiondata.OperationLockModel, string) error
}

func (m MockRecovery) HasMutationArtifact(ctx context.Context, item protectiondata.OperationLockModel) (bool, error) {
	if m.HasArtifact != nil {
		return m.HasArtifact(ctx, item)
	}
	return true, nil
}

func (m MockRecovery) AttemptRollback(ctx context.Context, item protectiondata.OperationLockModel) error {
	if m.Rollback == nil {
		return errors.New("mock rollback is unavailable")
	}
	return m.Rollback(ctx, item)
}

func (m MockRecovery) CreateBundle(ctx context.Context, item protectiondata.OperationLockModel, state string) error {
	if m.Bundle != nil {
		return m.Bundle(ctx, item, state)
	}
	return nil
}
