package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	paidcore "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	paidprovider "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/provider"
	paidstore "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/store"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

var errAlreadyApplied = paidstore.ErrOrderAlreadyFinalized
var errRefundNotApplicable = errors.New("order is not refundable")

// isAlreadyRefunded reports whether a refundStarPayment error means the charge
// was already refunded (e.g. by a concurrent refund via the other path).
// Telegram is idempotent at the charge level, so this is a success — not a
// failure — and must not be reported to the admin/user as "refund failed".
func isAlreadyRefunded(err error) bool {
	var apiErr *integrationtelegram.APIError
	if errors.As(err, &apiErr) {
		return strings.Contains(strings.ToUpper(apiErr.Description), "ALREADY_REFUNDED")
	}
	return false
}

// paymentCoordinator orchestrates orders, invoices and renewals. Logic is scoped to
// the resolved client; amounts are snapshotted server-side from the tariff.
type paymentCoordinator struct {
	setting service.SettingService
}

func newPaymentCoordinator() *paymentCoordinator { return &paymentCoordinator{} }

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
					NewHTTPClient: service.NewPaidSubHTTPClient,
					Notify: func(event string, details map[string]string) {
						(&service.TelegramService{}).NotifyTelegramEvent(event, details)
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

// enabledProvidersForTariff returns providers usable for a tariff: Stars needs
// StarsAmount>0, fiat providers need Price>0. Zero-price tariffs are not
// purchasable (anti free-renewal).
func (p *paymentCoordinator) enabledProvidersForTariff(t *paidcore.Tariff) []paidprovider.PaymentProvider {
	var kinds []paidprovider.ProviderKind
	if t.StarsAmount > 0 {
		kinds = append(kinds, paidprovider.ProviderStars)
	}
	if t.Price > 0 {
		kinds = append(kinds, paidprovider.ProviderYooKassa, paidprovider.ProviderStripe, paidprovider.ProviderPayMaster, paidprovider.ProviderCryptoBot, paidprovider.ProviderExternal)
	}
	var out []paidprovider.PaymentProvider
	for _, k := range kinds {
		if prov := p.providerByKind(k); prov != nil {
			out = append(out, prov)
		}
	}
	return out
}

// CreateOrder snapshots the price from the tariff, persists a pending order, and
// asks the provider to prepare an invoice.
func (p *paymentCoordinator) CreateOrder(ctx context.Context, client *model.Client, tariff *paidcore.Tariff, kind paidprovider.ProviderKind, tgUserId int64) (*paidcore.PaymentOrder, *paidprovider.Invoice, error) {
	prov := p.providerByKind(kind)
	if prov == nil {
		return nil, nil, fmt.Errorf("provider not available")
	}
	var amount int64
	var currency string
	if kind == paidprovider.ProviderStars {
		if tariff.StarsAmount <= 0 {
			return nil, nil, fmt.Errorf("tariff has no stars price")
		}
		amount = tariff.StarsAmount
		currency = "XTR"
	} else {
		if tariff.Price <= 0 {
			return nil, nil, fmt.Errorf("tariff has no price")
		}
		amount = tariff.Price
		currency = tariff.Currency
	}
	ttlMin, _ := p.setting.GetPaidSubOrderTTLMinutes()
	now := nowUnix()
	order := paidstore.NewPendingOrder(client, tariff, kind, amount, currency, tgUserId, common.Random(32), now, ttlMin)
	db := dbsqlite.DB()
	if err := db.Create(order).Error; err != nil {
		return nil, nil, err
	}
	inv, err := prov.CreateInvoice(ctx, order, tariff, client)
	if err != nil {
		return nil, nil, err
	}
	_ = paidstore.SaveInvoiceResult(db, order.Id, inv)
	return order, inv, nil
}

func (p *paymentCoordinator) getOrder(id uint) (*paidcore.PaymentOrder, error) {
	return paidstore.GetOrder(dbsqlite.DB(), id)
}

func (p *paymentCoordinator) findOrderByPayload(payload string) (*paidcore.PaymentOrder, error) {
	return paidstore.FindOrderByPayload(dbsqlite.DB(), payload)
}

func (p *paymentCoordinator) markFailed(id uint) {
	paidstore.MarkOrderFailed(dbsqlite.DB(), id)
}

// ApplyPaidOrder finalizes a pending order and renews the client exactly once.
// The conditional UPDATE ... WHERE status='pending' (checked via RowsAffected)
// is atomic under SQLite write serialization, so concurrent confirmations (a
// redelivered Telegram update or a poll race) are safe no-ops. Returns whether
// a renewal was applied and the bound Telegram user id (for notification).
func (p *paymentCoordinator) ApplyPaidOrder(orderID uint, chargeID string, raw []byte) (bool, int64, error) {
	db := dbsqlite.DB()
	result, err := paidstore.ApplyPaidOrderGrant(db, orderID, chargeID, raw, nowUnix(), "PaidSubBot")
	if errors.Is(err, errAlreadyApplied) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}

	// Post-commit: re-add the (re-enabled) user to its inbounds in the running
	// core. A restart failure does not roll back the paid renewal (logged).
	if len(result.InboundIDs) > 0 {
		if rErr := (&service.InboundService{}).RestartInbounds(dbsqlite.DB(), result.InboundIDs); rErr != nil {
			logger.Warning("paidsub: restart inbounds after renewal failed: ", rErr)
		}
	}
	_ = (&service.AuditService{}).Record(service.AuditEvent{
		Actor:    "PaidSubBot",
		Event:    "paidsub_paid",
		Resource: "paidsub",
		Severity: service.AuditSeverityInfo,
		Details:  map[string]any{"orderId": orderID},
	})
	return result.Applied, result.TelegramUserID, nil
}

// ExpireStaleOrders marks pending non-polled orders past their TTL as expired.
// Polled providers (CryptoBot) are deliberately EXCLUDED: their confirmation is
// out-of-band, so a payment can land after the short local TTL and must remain
// pending to be caught by the next poll. They are reaped instead by
// ExpireStalePolledOrders on a long grace window.
func (p *paymentCoordinator) ExpireStaleOrders() error {
	return paidstore.ExpireStaleOrders(dbsqlite.DB(), nowUnix())
}

// ExpireStalePolledOrders reaps pending polled-provider (CryptoBot) orders whose
// creation is older than graceSeconds — a hard ceiling far beyond the local
// order TTL so a late out-of-band payment is still caught by polling, while
// genuinely abandoned invoices do not accumulate forever.
func (p *paymentCoordinator) ExpireStalePolledOrders(graceSeconds int64) error {
	return paidstore.ExpireStalePolledOrders(dbsqlite.DB(), nowUnix(), graceSeconds)
}

// ---- order history & refunds ----

// OrdersForTgUser returns the most recent orders belonging to a Telegram user,
// scoped strictly by telegram_user_id (never another user's orders).
func (p *paymentCoordinator) OrdersForTgUser(tgUserId int64, limit int) ([]paidcore.PaymentOrder, error) {
	return paidstore.OrdersForTelegramUser(dbsqlite.DB(), tgUserId, limit)
}

// RefundableOrdersForTgUser returns a user's paid (refundable) orders.
func (p *paymentCoordinator) RefundableOrdersForTgUser(tgUserId int64, limit int) ([]paidcore.PaymentOrder, error) {
	return paidstore.RefundableOrdersForTelegramUser(dbsqlite.DB(), tgUserId, limit)
}

// finalizeRefund marks a paid order as refunded exactly once and, when revoke is
// true, rolls back the days/traffic that order granted. The conditional UPDATE
// ... WHERE status='paid' (checked via RowsAffected) makes a double refund a
// safe no-op (returns errAlreadyApplied). Affected inbounds are restarted
// post-commit so the running core re-evaluates the reduced limits. The client is
// never disabled by a refund.
func (p *paymentCoordinator) finalizeRefund(orderID uint, revoke bool) error {
	db := dbsqlite.DB()
	inboundIds, err := paidstore.FinalizeRefundGrant(db, orderID, revoke, nowUnix(), "PaidSubBot")
	if err != nil {
		return err
	}
	if len(inboundIds) > 0 {
		if rErr := (&service.InboundService{}).RestartInbounds(dbsqlite.DB(), inboundIds); rErr != nil {
			logger.Warning("paidsub: restart inbounds after refund failed: ", rErr)
		}
	}
	_ = (&service.AuditService{}).Record(service.AuditEvent{
		Actor:    "PaidSubBot",
		Event:    "paidsub_refunded",
		Resource: "paidsub",
		Severity: service.AuditSeverityInfo,
		Details:  map[string]any{"orderId": orderID, "revoke": revoke},
	})
	return nil
}

// RefundOrder is the admin-initiated refund (panel Orders tab). For Stars it
// returns the money via refundStarPayment FIRST, then marks the order refunded
// (so the admin can cleanly retry if Telegram rejects the call); for every other
// provider the money must be refunded in the provider's own dashboard, so this
// only marks the order refunded (status "refunded_manual"). revoke is the
// admin's per-refund choice to roll back the granted days/traffic.
func (p *paymentCoordinator) refundOrder(ctx context.Context, orderID uint, revoke bool) (string, error) {
	order, err := p.getOrder(orderID)
	if err != nil {
		return "", err
	}
	if order.Status != paidcore.StatusPaid {
		return "", errRefundNotApplicable
	}
	// Defensive: a paid order always has Amount > 0 (CreateOrder rejects zero),
	// so a non-positive amount means a corrupted row — never act on it.
	if order.Amount <= 0 {
		return "", errRefundNotApplicable
	}
	if order.Provider == string(paidprovider.ProviderStars) {
		sender, err := newSenderBot()
		if err != nil {
			return "", err
		}
		charge := strings.TrimPrefix(order.ProviderChargeID, "tg:")
		if charge == "" {
			return "", fmt.Errorf("order has no Stars charge id")
		}
		// An "already refunded" response means a concurrent refund (e.g. the bot
		// path) returned the money first — treat it as success, not a failure.
		if err := sender.refundStarPayment(ctx, order.TelegramUserId, charge); err != nil && !isAlreadyRefunded(err) {
			return "", fmt.Errorf("stars refund failed")
		}
		if err := p.finalizeRefund(orderID, revoke); err != nil && !errors.Is(err, errAlreadyApplied) {
			return "", err
		}
		return "refunded", nil
	}
	if err := p.finalizeRefund(orderID, revoke); err != nil && !errors.Is(err, errAlreadyApplied) {
		return "", err
	}
	return "refunded_manual", nil
}

// RefundOrder performs the admin-facing refund operation. Telegram Stars need
// the bot transport; other providers are finalized locally after an external
// dashboard refund.
func RefundOrder(ctx context.Context, orderID uint, revoke bool) (string, error) {
	return newPaymentCoordinator().refundOrder(ctx, orderID, revoke)
}

// ---- bot purchase flow ----
