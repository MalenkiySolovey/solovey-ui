package udpguard

import (
	"context"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
)

// StateRepository contains only the durable operations consumed by this owner.
type StateRepository interface {
	LoadSettingsRevision(context.Context) (domain.Settings, int, bool, error)
	BeginUDPGuardReceipt(context.Context, string, string, string) (protectionrepository.UDPGuardIdempotencyV1Model, bool, error)
	AmbiguousUDPGuardReceipt(ctx context.Context, id uint) error
	BindUDPGuardReceiptOperation(ctx context.Context, id uint, operationID string, operationRevision int) error
	CompleteUDPGuardReceipt(ctx context.Context, id uint, operationID string, operationRevision int, response any) error
	FirewallAuthority(ctx context.Context) (protectionrepository.FirewallAuthoritySnapshot, error)
	FirewallTransition(ctx context.Context, operationID string) (protectionrepository.FirewallContributionTransitionModel, error)
	FirewallTransitions(ctx context.Context) ([]protectionrepository.FirewallContributionTransitionModel, error)
	SaveUDPGuardState(ctx context.Context, value protectionrepository.UDPGuardStateV1Model) error
	UDPGuardStates(ctx context.Context) ([]protectionrepository.UDPGuardStateV1Model, error)
}
