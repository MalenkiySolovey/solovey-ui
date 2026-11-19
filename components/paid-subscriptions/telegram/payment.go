//go:build !minimal

package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	paidcore "github.com/MalenkiySolovey/solovey-component-paid-subscriptions"
	paidprovider "github.com/MalenkiySolovey/solovey-component-paid-subscriptions/provider"
	commerce "github.com/MalenkiySolovey/solovey-component-paid-subscriptions/store"
	"github.com/MalenkiySolovey/solovey-component-paid-subscriptions/workflow"
	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
	paidstore "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/store"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

var errAlreadyApplied = commerce.ErrOrderAlreadyFinalized
var errRefundNotApplicable = workflow.ErrRefundNotApplicable

// isAlreadyRefunded reports whether a refundStarPayment error means the charge
// was already refunded (e.g. by a concurrent refund via the other path).
// Telegram is idempotent at the charge level, so this is a success — not a
// failure — and must not be reported to the admin/user as "refund failed".
func isAlreadyRefunded(err error) bool {
	var apiErr *integrationtelegram.APIError
	if errors.As(err, &apiErr) && apiErr.Code == 400 {
		reason := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(apiErr.Description)), "BAD REQUEST: ")
		return reason == "CHARGE_ALREADY_REFUNDED" || reason == "STAR_PAYMENT_ALREADY_REFUNDED"
	}
	return false
}

// paymentCoordinator orchestrates orders, invoices and renewals. Logic is scoped to
// the resolved client; amounts are snapshotted server-side from the tariff.
type paymentCoordinator struct {
	setting paidSettings
	runtime *service.Runtime
}

func newPaymentCoordinator(runtime ...*service.Runtime) *paymentCoordinator {
	coordinator := &paymentCoordinator{}
	if len(runtime) > 0 {
		coordinator.runtime = runtime[0]
	}
	return coordinator
}

// providerByKind builds a configured provider if it is enabled and has its
// token set; otherwise nil.
func (p *paymentCoordinator) providerByKind(kind paidprovider.ProviderKind) paidprovider.PaymentProvider {
	s := &p.setting
	switch kind {
	case paidprovider.ProviderStars:
		if on, _ := s.GetPaidSubStarsEnabled(); on {
			return paidprovider.NewTelegramProvider(paidprovider.ProviderStars, "")
		}
	case paidprovider.ProviderYooKassa:
		if on, _ := s.GetPaidSubYooKassaEnabled(); on {
			if tok, _ := s.GetPaidSubYooKassaToken(); tok != "" {
				return paidprovider.NewTelegramProvider(paidprovider.ProviderYooKassa, tok)
			}
		}
	case paidprovider.ProviderStripe:
		if on, _ := s.GetPaidSubStripeEnabled(); on {
			if tok, _ := s.GetPaidSubStripeToken(); tok != "" {
				return paidprovider.NewTelegramProvider(paidprovider.ProviderStripe, tok)
			}
		}
	case paidprovider.ProviderPayMaster:
		if on, _ := s.GetPaidSubPayMasterEnabled(); on {
			if tok, _ := s.GetPaidSubPayMasterToken(); tok != "" {
				return paidprovider.NewTelegramProvider(paidprovider.ProviderPayMaster, tok)
			}
		}
	case paidprovider.ProviderCryptoBot:
		if on, _ := s.GetPaidSubCryptoBotEnabled(); on {
			if tok, _ := s.GetPaidSubCryptoBotToken(); tok != "" {
				return paidprovider.NewCryptoBotProvider(tok, paidprovider.CryptoBotDeps{
					NewHTTPClient: func(timeout time.Duration) (*http.Client, error) {
						return newPaidSubHTTPClient(p.runtime, timeout)
					},
					Notify: func(event string, details map[string]string) {
						service.NotifyPanelEvent(event, details)
					},
				})
			}
		}
	case paidprovider.ProviderExternal:
		if on, _ := s.GetPaidSubExternalEnabled(); on {
			if tmpl, _ := s.GetPaidSubExternalUrlTemplate(); tmpl != "" {
				return paidprovider.NewExternalProvider(tmpl)
			}
		}
	}
	return nil
}

// createOrder keeps the Panel operation admitted across provider I/O, with no
// SQL transaction held by the transport. Paid reloads the tariff and owns the
// immutable quote, intent reuse and ambiguity handling.
func (p *paymentCoordinator) createOrder(ctx context.Context, clientID, tariffID uint, kind paidprovider.ProviderKind, telegramID int64) (*paidcore.PaymentOrder, *paidprovider.Invoice, error) {
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	ttl, err := p.setting.GetPaidSubOrderTTLMinutes()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid paid subscription order TTL")
	}
	return workflow.CreateInvoice(ctx, dbsqlite.DB(), workflow.PurchaseRequest{
		ClientID: clientID, TariffID: tariffID, TelegramUserID: telegramID, TTLMinutes: ttl,
	}, p.providerByKind(kind), workflow.PurchasePorts{
		CheckClient: func(db *gorm.DB, id uint) error { return db.Select("id").First(&model.Client{}, id).Error },
		NewIdentity: func() (string, error) { return common.SecureRandom(32) },
		OnCreated: func(order paidcore.PaymentOrder) {
			_ = (&service.AuditService{Runtime: p.runtime}).Record(service.AuditEvent{
				Actor: "PaidSubBot", Event: "paidsub_order_created", Resource: "paidsub", Severity: service.AuditSeverityInfo,
				Details: map[string]any{"orderId": order.Id, "provider": order.Provider},
			})
		},
	})
}

// ApplyPaidOrder finalizes a pending order and renews the client exactly once.
// The conditional UPDATE ... WHERE status='pending' (checked via RowsAffected)
// is atomic under SQLite write serialization, so concurrent confirmations (a
// redelivered Telegram update or a poll race) are safe no-ops. Returns whether
// a renewal was applied and the bound Telegram user id (for notification).
func (p *paymentCoordinator) applyPaidOrder(db *gorm.DB, orderID uint, chargeID string) (bool, int64, error) {
	result, err := paidstore.ApplyPaidOrderGrant(db, orderID, chargeID, nil, nowUnix(), "PaidSubBot")
	if errors.Is(err, errAlreadyApplied) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	p.afterPaidCommit(db, result, orderID)
	return result.Applied, result.TelegramUserID, nil
}

func (p *paymentCoordinator) afterPaidCommit(db *gorm.DB, result paidstore.AppliedOrderResult, orderID uint) {
	if !result.Applied {
		return
	}

	// Post-commit: re-add the (re-enabled) user to its inbounds in the running
	// core. A restart failure does not roll back the paid renewal (logged).
	if len(result.InboundIDs) > 0 {
		if rErr := (&service.InboundService{Runtime: p.runtime}).RestartInbounds(db, result.InboundIDs); rErr != nil {
			logger.Warning("paidsub: restart inbounds after renewal failed: ", rErr)
		}
	}
	_ = (&service.AuditService{Runtime: p.runtime}).Record(service.AuditEvent{
		Actor:    "PaidSubBot",
		Event:    "paidsub_paid",
		Resource: "paidsub",
		Severity: service.AuditSeverityInfo,
		Details:  map[string]any{"orderId": orderID},
	})
}

// ExpireStaleOrders marks pending non-polled orders past their TTL as expired.
// Polled providers (CryptoBot) are deliberately EXCLUDED: their confirmation is
// out-of-band, so a payment can land after the short local TTL and must remain
// pending until an authenticated provider outcome is available.
func (p *paymentCoordinator) ExpireStaleOrders() error {
	return commerce.ExpireStaleOrders(dbsqlite.DB(), nowUnix())
}

// finalizeRefund marks a paid order as refunded exactly once and, when revoke is
// true, rolls back the days/traffic that order granted. The conditional UPDATE
// ... WHERE status='paid' (checked via RowsAffected) makes a double refund a
// safe no-op (returns errAlreadyApplied). Affected inbounds are restarted
// post-commit so the running core re-evaluates the reduced limits. The client is
// never disabled by a refund.
func (p *paymentCoordinator) finalizeRefund(db *gorm.DB, orderID uint, claim string, revoke bool) error {
	var inboundIds []uint
	var err error
	if claim == "" {
		inboundIds, err = paidstore.FinalizeRefundGrant(db, orderID, revoke, nowUnix(), "PaidSubBot")
	} else {
		inboundIds, err = paidstore.FinalizeClaimedRefundGrant(db, orderID, claim, nowUnix(), "PaidSubBot")
	}
	if err != nil {
		return err
	}
	p.afterRefundCommit(db, inboundIds, orderID, revoke)
	return nil
}

func (p *paymentCoordinator) afterRefundCommit(db *gorm.DB, inboundIds []uint, orderID uint, revoke bool) {
	if len(inboundIds) > 0 {
		if rErr := (&service.InboundService{Runtime: p.runtime}).RestartInbounds(db, inboundIds); rErr != nil {
			logger.Warning("paidsub: restart inbounds after refund failed: ", rErr)
		}
	}
	_ = (&service.AuditService{Runtime: p.runtime}).Record(service.AuditEvent{
		Actor:    "PaidSubBot",
		Event:    "paidsub_refunded",
		Resource: "paidsub",
		Severity: service.AuditSeverityInfo,
		Details:  map[string]any{"orderId": orderID, "revoke": revoke},
	})
}

// refundOrder retains DB admission across the remote Stars refund. Paid
// preserves the durable claim; the Panel finalizes its entitlement transaction.
func (p *paymentCoordinator) refundOrder(ctx context.Context, orderID uint, revoke bool) (string, error) {
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return workflow.RefundOrder(ctx, dbsqlite.DB(), orderID, revoke, workflow.RefundPorts{
		NewIdentity: func() (string, error) { return common.SecureRandom(32) },
		RefundStars: func(ctx context.Context, userID int64, charge string) error {
			sender, err := newSenderBot(p.runtime)
			if err != nil {
				return err
			}
			defer sender.closeIdleConnections()
			if err := sender.refundStarPayment(ctx, userID, charge); err != nil && !isAlreadyRefunded(err) {
				return fmt.Errorf("stars refund failed")
			}
			return nil
		},
		Finalize: p.finalizeRefund,
		ReleaseFailed: func(orderID uint) {
			logger.Warning("paidsub: refund claim release deferred to lease expiry, order ", orderID)
		},
	})
}

// RefundOrder performs the admin-facing refund operation. Telegram Stars need
// the bot transport; other providers are finalized locally after an external
// dashboard refund.
func RefundOrder(ctx context.Context, runtime *service.Runtime, orderID uint, revoke bool) (string, error) {
	return newPaymentCoordinator(runtime).refundOrder(ctx, orderID, revoke)
}

// ---- bot purchase flow ----
