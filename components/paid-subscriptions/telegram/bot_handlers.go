//go:build !minimal

package telegram

import (
	"context"
	"fmt"
	"strings"

	paidcore "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	paidprovider "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/provider"
	paidstore "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/store"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func (b *Bot) cmdStart(ctx context.Context, chatID int64, from *tgUser, l lang) {
	_, err := paidstore.ClientByTelegramUserID(dbsqlite.DB(), from.ID)
	if err != nil {
		// Only a genuine "not found" may lead to auto-registration. A transient
		// DB error must NOT be treated as unbound (that would auto-create and
		// rebind a new client, orphaning an existing subscription).
		if !dbsqlite.IsNotFound(err) {
			logger.Warning("paidsub: client lookup failed: ", err)
			_ = b.sendMessage(ctx, chatID, tr(l, "error"), nil)
			return
		}
		if b.tryAutoRegister(ctx, chatID, from, l) {
			return
		}
		_ = b.sendMessage(ctx, chatID, tr(l, "not_linked"), nil)
		return
	}
	greeting := tr(l, "greeting")
	if custom, _ := b.setting.GetPaidSubGreeting(); strings.TrimSpace(custom) != "" {
		greeting = truncateRunes(custom, 4096)
	}
	_ = b.sendMessage(ctx, chatID, greeting, b.menuKeyboard(l))
}

func (b *Bot) cmdLinks(ctx context.Context, chatID int64, tgID int64, l lang) {
	client, err := paidstore.ClientByTelegramUserID(dbsqlite.DB(), tgID)
	if err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "not_linked"), nil)
		return
	}
	text := b.buildLinksText(client, l)
	for _, chunk := range chunkText(text, 4000) {
		_ = b.sendMessage(ctx, chatID, chunk, nil)
	}
}

func (b *Bot) cmdQR(ctx context.Context, chatID int64, tgID int64, l lang) {
	client, err := paidstore.ClientByTelegramUserID(dbsqlite.DB(), tgID)
	if err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "not_linked"), nil)
		return
	}
	sub, err := b.subURL(client)
	if err != nil || sub == "" {
		_ = b.sendMessage(ctx, chatID, tr(l, "links_none"), nil)
		return
	}
	png, err := renderQR(sub)
	if err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "error"), nil)
		return
	}
	if err := b.sendPhoto(ctx, chatID, png, tr(l, "qr_caption_sub")); err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "error"), nil)
	}
}

func (b *Bot) cmdStats(ctx context.Context, chatID int64, tgID int64, l lang) {
	client, err := paidstore.ClientByTelegramUserID(dbsqlite.DB(), tgID)
	if err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "not_linked"), nil)
		return
	}
	_ = b.sendMessage(ctx, chatID, b.buildStatsText(client, l), b.menuKeyboard(l))
}

// cmdPaymentMenu opens the "Payment" submenu (buy/renew, my purchases, refund).
func (b *Bot) cmdPaymentMenu(ctx context.Context, chatID int64, l lang) {
	_ = b.sendMessage(ctx, chatID, tr(l, "payment_title"), b.paymentMenuKeyboard(l))
}

// cmdMyOrders lists the requesting user's own orders (read-only history).
func (b *Bot) cmdMyOrders(ctx context.Context, chatID int64, tgID int64, l lang) {
	if _, err := paidstore.ClientByTelegramUserID(dbsqlite.DB(), tgID); err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "not_linked"), nil)
		return
	}
	orders, err := b.payments.OrdersForTgUser(tgID, 20)
	if err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "error"), nil)
		return
	}
	if len(orders) == 0 {
		_ = b.sendMessage(ctx, chatID, tr(l, "orders_none"), b.backToPaymentKeyboard(l))
		return
	}
	chunks := chunkText(b.buildOrdersText(orders, l), 4000)
	for i, chunk := range chunks {
		if i == len(chunks)-1 {
			_ = b.sendMessage(ctx, chatID, chunk, b.backToPaymentKeyboard(l))
		} else {
			_ = b.sendMessage(ctx, chatID, chunk, nil)
		}
	}
}

// cmdRefundMenu lists the user's refundable (paid) orders as tappable buttons.
func (b *Bot) cmdRefundMenu(ctx context.Context, chatID int64, tgID int64, l lang) {
	if _, err := paidstore.ClientByTelegramUserID(dbsqlite.DB(), tgID); err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "not_linked"), nil)
		return
	}
	orders, err := b.payments.RefundableOrdersForTgUser(tgID, 20)
	if err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "error"), nil)
		return
	}
	if len(orders) == 0 {
		_ = b.sendMessage(ctx, chatID, tr(l, "refund_none"), b.backToPaymentKeyboard(l))
		return
	}
	names := b.tariffNameMap()
	var rows [][]inlineButton
	for i := range orders {
		o := orders[i]
		rows = append(rows, []inlineButton{{Text: refundOrderButtonLabel(&o, names), CallbackData: fmt.Sprintf("refund:%d", o.Id)}})
	}
	rows = append(rows, []inlineButton{{Text: tr(l, "menu_back"), CallbackData: "payment"}})
	_ = b.sendMessage(ctx, chatID, tr(l, "refund_choose"), &inlineKeyboard{InlineKeyboard: rows})
}

// handleRefundRequest processes a refund button. Stars are refunded
// programmatically through the shared payment coordinator; every
// other provider cannot be refunded via the Bot API, so it sends the admin a
// request. It never acts on another user's order.
func (b *Bot) handleRefundRequest(ctx context.Context, chatID int64, tgID int64, orderID uint, l lang) {
	order, err := b.payments.getOrder(orderID)
	if err != nil {
		return
	}
	if order.TelegramUserId != tgID {
		auditCrossUserOrderAccess(b.runtime, tgID, orderID, "refund")
		return
	}
	if order.Status != paidcore.StatusPaid && order.Status != paidcore.StatusRefundPending {
		_ = b.sendMessage(ctx, chatID, tr(l, "refund_not_eligible"), b.backToPaymentKeyboard(l))
		return
	}
	if err := paidstore.ValidateOrderGrant(*order); err != nil {
		_ = b.sendMessage(ctx, chatID, tr(l, "refund_not_eligible"), b.backToPaymentKeyboard(l))
		return
	}
	if order.Provider != string(paidprovider.ProviderStars) {
		service.NotifyPanelEvent("paidsub_refund_request", map[string]string{
			"orderId":  fmt.Sprintf("%d", order.Id),
			"clientId": fmt.Sprintf("%d", order.ClientId),
			"provider": order.Provider,
		})
		_ = b.sendMessage(ctx, chatID, tr(l, "refund_requested"), b.backToPaymentKeyboard(l))
		return
	}
	// Stars: the admin policy (paidSubRefundRevoke) decides rollback; the user
	// does not choose, to prevent buy → refund → keep-using abuse.
	revoke, err := b.setting.GetPaidSubRefundRevoke()
	if err != nil {
		logger.Warning("paidsub: read refund revoke policy failed: ", err)
		_ = b.sendMessage(ctx, chatID, tr(l, "error"), nil)
		return
	}
	if _, rerr := b.payments.refundOrder(ctx, order.Id, revoke); rerr != nil {
		logger.Warning("paidsub: refundStarPayment failed; manual refund needed")
		service.NotifyPanelEvent("paidsub_refund_failed", map[string]string{
			"orderId":  fmt.Sprintf("%d", order.Id),
			"clientId": fmt.Sprintf("%d", order.ClientId),
		})
		_ = b.sendMessage(ctx, chatID, tr(l, "refund_requested"), b.backToPaymentKeyboard(l))
		return
	}
	service.NotifyPanelEvent("paidsub_refunded", map[string]string{
		"orderId":  fmt.Sprintf("%d", order.Id),
		"clientId": fmt.Sprintf("%d", order.ClientId),
	})
	_ = b.sendMessage(ctx, chatID, tr(l, "refund_done"), b.backToPaymentKeyboard(l))
}

// ---- content builders ----
