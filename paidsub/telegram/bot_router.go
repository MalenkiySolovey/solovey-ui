package telegram

import (
	"context"
	"strconv"
	"strings"
)

func (b *Bot) handleUpdate(ctx context.Context, u *tgUpdate) {
	switch {
	case u.PreCheckoutQuery != nil:
		b.handlePreCheckout(ctx, u.PreCheckoutQuery)
	case u.Message != nil && u.Message.SuccessfulPayment != nil:
		b.handleSuccessfulPayment(ctx, u.Message)
	case u.Message != nil:
		b.handleMessage(ctx, u.Message)
	case u.CallbackQuery != nil:
		b.handleCallback(ctx, u.CallbackQuery)
	}
}

func (b *Bot) handleMessage(ctx context.Context, m *tgMessage) {
	if m.From == nil || m.From.ID <= 0 || m.From.IsBot {
		return
	}
	if m.Chat.Type != "private" {
		return
	}
	l := pickLang(m.From.LanguageCode)
	if !b.cmdLimiter.Allow(m.From.ID).Allowed {
		return // silent drop
	}
	cmd, _ := parseCommand(m.Text)
	switch cmd {
	case "/help":
		_ = b.sendMessage(ctx, m.Chat.ID, tr(l, "help"), nil)
	case "/links", "/sub":
		b.cmdLinks(ctx, m.Chat.ID, m.From.ID, l)
	case "/qr":
		b.cmdQR(ctx, m.Chat.ID, m.From.ID, l)
	case "/stats", "/usage":
		b.cmdStats(ctx, m.Chat.ID, m.From.ID, l)
	default: // /start, unknown, or plain text → open menu
		b.cmdStart(ctx, m.Chat.ID, m.From, l)
	}
}

func (b *Bot) handleCallback(ctx context.Context, cq *tgCallbackQuery) {
	if cq.From.ID <= 0 || cq.From.IsBot {
		return
	}
	l := pickLang(cq.From.LanguageCode)
	if !b.cmdLimiter.Allow(cq.From.ID).Allowed {
		_ = b.answerCallback(ctx, cq.ID, tr(l, "rate_limited"))
		return
	}
	_ = b.answerCallback(ctx, cq.ID, "")
	var chatID int64
	if cq.Message != nil {
		chatID = cq.Message.Chat.ID
	}
	if chatID == 0 {
		return
	}
	data := cq.Data
	switch {
	case data == "links":
		b.cmdLinks(ctx, chatID, cq.From.ID, l)
	case data == "qr":
		b.cmdQR(ctx, chatID, cq.From.ID, l)
	case data == "stats":
		b.cmdStats(ctx, chatID, cq.From.ID, l)
	case data == "help":
		_ = b.sendMessage(ctx, chatID, tr(l, "help"), nil)
	case data == "menu":
		b.cmdStart(ctx, chatID, &cq.From, l)
	case data == "payment":
		b.cmdPaymentMenu(ctx, chatID, l)
	case data == "orders":
		b.cmdMyOrders(ctx, chatID, cq.From.ID, l)
	case data == "refund":
		b.cmdRefundMenu(ctx, chatID, cq.From.ID, l)
	case strings.HasPrefix(data, "refund:"):
		if id, ok := parseUintArg(data, "refund:"); ok {
			b.handleRefundRequest(ctx, chatID, cq.From.ID, id, l)
		}
	case data == "buy":
		b.cmdBuy(ctx, chatID, cq.From.ID, l)
	case strings.HasPrefix(data, "tariff:"):
		if id, ok := parseUintArg(data, "tariff:"); ok {
			b.handleTariffSelect(ctx, chatID, cq.From.ID, id, l)
		}
	case strings.HasPrefix(data, "pay:"):
		if tid, kind, ok := parsePayData(data); ok {
			b.handlePay(ctx, chatID, cq.From.ID, tid, kind, l)
		}
	case strings.HasPrefix(data, "paid:"):
		if id, ok := parseUintArg(data, "paid:"); ok {
			b.handleManualPaid(ctx, chatID, cq.From.ID, id, l)
		}
	}
}

func parseUintArg(data, prefix string) (uint, bool) {
	value, err := strconv.ParseUint(strings.TrimPrefix(data, prefix), 10, 64)
	if err != nil || value == 0 {
		return 0, false
	}
	return uint(value), true
}

func parsePayData(data string) (uint, string, bool) {
	parts := strings.Split(strings.TrimPrefix(data, "pay:"), ":")
	if len(parts) != 2 {
		return 0, "", false
	}
	value, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || value == 0 || parts[1] == "" {
		return 0, "", false
	}
	return uint(value), parts[1], true
}

// handlePreCheckout / handleSuccessfulPayment are implemented in payment.go
// (Phase 5). Declared here as no-ops would shadow them; the real methods live
// in payment.go.

// ---- commands ----
