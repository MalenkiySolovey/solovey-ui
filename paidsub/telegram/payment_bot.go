package telegram

import (
	"context"
	"fmt"
	"strings"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	paidcore "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	paidprovider "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/provider"
	paidstore "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/store"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func (b *Bot) cmdBuy(ctx context.Context, chatID int64, tgID int64, l lang) {
	if _, err := paidstore.ClientByTelegramUserID(dbsqlite.DB(), tgID); err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "not_linked"), nil)
		return
	}
	tariffs, _ := paidstore.ListEnabledTariffs(dbsqlite.DB())
	var rows [][]inlineButton
	for i := range tariffs {
		t := tariffs[i]
		if len(b.payments.enabledProvidersForTariff(&t)) == 0 {
			continue
		}
		rows = append(rows, []inlineButton{{Text: tariffButtonLabel(&t), CallbackData: fmt.Sprintf("tariff:%d", t.Id)}})
	}
	if len(rows) == 0 {
		_ = b.sendMessage(ctx, chatID, tr(l, "buy_none"), nil)
		return
	}
	_ = b.sendMessage(ctx, chatID, tr(l, "buy_title"), &inlineKeyboard{InlineKeyboard: rows})
}

func (b *Bot) handleTariffSelect(ctx context.Context, chatID int64, tgID int64, tariffID uint, l lang) {
	t, err := paidstore.GetTariff(dbsqlite.DB(), tariffID)
	if err != nil || !t.Enabled {
		_ = b.sendMessage(ctx, chatID, tr(l, "buy_none"), nil)
		return
	}
	provs := b.payments.enabledProvidersForTariff(t)
	if len(provs) == 0 {
		_ = b.sendMessage(ctx, chatID, tr(l, "buy_none"), nil)
		return
	}
	if len(provs) == 1 {
		b.startPurchase(ctx, chatID, tgID, t, provs[0], l)
		return
	}
	var rows [][]inlineButton
	for _, prov := range provs {
		rows = append(rows, []inlineButton{{Text: prov.Title(string(l)), CallbackData: fmt.Sprintf("pay:%d:%s", t.Id, prov.Kind())}})
	}
	_ = b.sendMessage(ctx, chatID, tr(l, "buy_choose_provider"), &inlineKeyboard{InlineKeyboard: rows})
}

func (b *Bot) handlePay(ctx context.Context, chatID int64, tgID int64, tariffID uint, kind string, l lang) {
	t, err := paidstore.GetTariff(dbsqlite.DB(), tariffID)
	if err != nil || !t.Enabled {
		_ = b.sendMessage(ctx, chatID, tr(l, "buy_none"), nil)
		return
	}
	prov := b.payments.providerByKind(paidprovider.ProviderKind(kind))
	if prov == nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "buy_none"), nil)
		return
	}
	b.startPurchase(ctx, chatID, tgID, t, prov, l)
}

func (b *Bot) startPurchase(ctx context.Context, chatID int64, tgID int64, t *paidcore.Tariff, prov paidprovider.PaymentProvider, l lang) {
	client, err := paidstore.ClientByTelegramUserID(dbsqlite.DB(), tgID)
	if err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "not_linked"), nil)
		return
	}
	_, inv, err := b.payments.CreateOrder(ctx, client, t, prov.Kind(), tgID)
	if err != nil {
		logger.Warning("paidsub: create order failed: ", err)
		_ = b.sendMessage(ctx, chatID, tr(l, "pay_invoice_failed"), nil)
		return
	}
	switch inv.Method {
	case paidprovider.InvoiceTelegramNative:
		if err := b.sendInvoice(ctx, chatID, inv); err != nil {
			logger.Warning("paidsub: sendInvoice failed: ", err)
			_ = b.sendMessage(ctx, chatID, tr(l, "pay_invoice_failed"), nil)
		}
	case paidprovider.InvoiceURL:
		kb := &inlineKeyboard{InlineKeyboard: [][]inlineButton{{{Text: tr(l, "pay_open"), URL: inv.PayURL}}}}
		_ = b.sendMessage(ctx, chatID, tr(l, "pay_open_hint"), kb)
	case paidprovider.InvoiceManualLink:
		var order *paidcore.PaymentOrder
		// Re-fetch the freshly created order id for the manual button.
		order, _ = b.payments.findOrderByPayload(inv.Payload)
		var rows [][]inlineButton
		rows = append(rows, []inlineButton{{Text: tr(l, "pay_open"), URL: inv.PayURL}})
		if order != nil {
			rows = append(rows, []inlineButton{{Text: tr(l, "pay_manual_btn"), CallbackData: fmt.Sprintf("paid:%d", order.Id)}})
		}
		_ = b.sendMessage(ctx, chatID, tr(l, "pay_open_hint"), &inlineKeyboard{InlineKeyboard: rows})
	}
}

// auditCrossUserOrderAccess records an attempt by a Telegram user to act on an
// order owned by someone else (order-id enumeration/probing on the public bot).
// In practice this is rate-bounded by the bot's per-user command limiter, so it
// leaves a trace without flooding the audit log. MITRE T1110/T1499.
func auditCrossUserOrderAccess(tgID int64, orderID uint, action string) {
	_ = (&service.AuditService{}).Record(service.AuditEvent{
		Actor:    fmt.Sprintf("tg:%d", tgID),
		Event:    "paidsub_cross_user_access",
		Resource: "paidsub",
		Severity: service.AuditSeverityWarn,
		Details:  map[string]any{"orderId": orderID, "action": action},
	})
}

func (b *Bot) handleManualPaid(ctx context.Context, chatID int64, tgID int64, orderID uint, l lang) {
	order, err := b.payments.getOrder(orderID)
	if err != nil {
		return
	}
	if order.TelegramUserId != tgID {
		auditCrossUserOrderAccess(tgID, orderID, "manual_paid") // never act on another user's order
		return
	}
	(&service.TelegramService{}).NotifyTelegramEvent("paidsub_manual_claim", map[string]string{
		"orderId":  fmt.Sprintf("%d", order.Id),
		"clientId": fmt.Sprintf("%d", order.ClientId),
	})
	_ = b.sendMessage(ctx, chatID, tr(l, "pay_manual_sent"), nil)
}

// ---- payment confirmation (Telegram-native) ----

func (b *Bot) handlePreCheckout(ctx context.Context, q *tgPreCheckoutQuery) {
	order, err := b.payments.findOrderByPayload(q.InvoicePayload)
	ok := err == nil &&
		order.Status == paidcore.StatusPending &&
		q.TotalAmount == order.Amount &&
		strings.EqualFold(q.Currency, order.Currency) &&
		(order.TelegramUserId == 0 || q.From.ID == order.TelegramUserId)
	if ok {
		_ = b.answerPreCheckout(ctx, q.ID, true, "")
		return
	}
	_ = b.answerPreCheckout(ctx, q.ID, false, "Order is no longer valid")
}

func (b *Bot) handleSuccessfulPayment(ctx context.Context, m *tgMessage) {
	if m.From == nil {
		return
	}
	l := pickLang(m.From.LanguageCode)
	sp := m.SuccessfulPayment
	order, err := b.payments.findOrderByPayload(sp.InvoicePayload)
	if err != nil {
		logger.Warning("paidsub: successful_payment for unknown order")
		return
	}
	if sp.TotalAmount != order.Amount || !strings.EqualFold(sp.Currency, order.Currency) {
		logger.Warning("paidsub: payment amount/currency mismatch; refusing renewal")
		b.payments.markFailed(order.Id)
		(&service.TelegramService{}).NotifyTelegramEvent("paidsub_payment_mismatch", map[string]string{
			"orderId": fmt.Sprintf("%d", order.Id),
		})
		return
	}
	// Defence in depth: the payer must be the Telegram user the order was created
	// for (the payload + pending status are the primary gate).
	if order.TelegramUserId != 0 && m.From.ID != order.TelegramUserId {
		logger.Warning("paidsub: successful_payment from unexpected telegram user; refusing renewal")
		b.payments.markFailed(order.Id)
		(&service.TelegramService{}).NotifyTelegramEvent("paidsub_payment_mismatch", map[string]string{
			"orderId": fmt.Sprintf("%d", order.Id),
		})
		return
	}
	charge := sp.TelegramPaymentChargeID
	if charge == "" {
		charge = sp.ProviderPaymentChargeID
	}
	applied, _, err := b.payments.ApplyPaidOrder(order.Id, "tg:"+charge, nil)
	if err != nil {
		logger.Warning("paidsub: apply paid order failed: ", err)
		_ = b.sendMessage(ctx, m.Chat.ID, tr(l, "error"), nil)
		return
	}
	if applied {
		_ = b.sendMessage(ctx, m.Chat.ID, tr(l, "pay_success"), b.menuKeyboard(l))
	}
}

// ---- helpers ----

func tariffButtonLabel(t *paidcore.Tariff) string {
	price := ""
	switch {
	case t.Price > 0:
		price = fmt.Sprintf("%.2f %s", float64(t.Price)/100, t.Currency)
	case t.StarsAmount > 0:
		price = fmt.Sprintf("%d \u2b50", t.StarsAmount)
	}
	if price == "" {
		return t.Name
	}
	return fmt.Sprintf("%s: %s", t.Name, price)
}

// formatOrderAmount renders an order amount: Telegram Stars (XTR) are whole
// units; every other currency is stored in minor units (e.g. kopeks/cents).
func formatOrderAmount(amount int64, currency string) string {
	if currency == "XTR" {
		return fmt.Sprintf("%d \u2b50", amount)
	}
	return fmt.Sprintf("%.2f %s", float64(amount)/100, currency)
}
