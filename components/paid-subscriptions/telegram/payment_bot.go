//go:build !minimal

package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/MalenkiySolovey/solovey-component-paid-subscriptions/customerbot"
	paidstore "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/store"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

// customerHandler binds one authenticated poll to the admitted Panel DB.
func (b *Bot) customerHandler(db *gorm.DB) *customerbot.Handler {
	return &customerbot.Handler{
		DB: db,
		Send: customerbot.Sender{
			Message: b.sendMessage, Photo: b.sendPhoto, Invoice: b.sendInvoice,
			AnswerCallback: b.answerCallback, AnswerPreCheckout: b.answerPreCheckout,
		},
		Allow: func(id int64) bool { return b.cmdLimiter.Allow(id).Allowed },
		ClientID: func(id int64) (uint, error) {
			client, err := paidstore.ClientByTelegramUserID(db, id)
			if err != nil {
				return 0, err
			}
			return client.Id, nil
		},
		Trial: &customerbot.TrialRegistration{
			Enabled:     b.setting.GetPaidSubAutoRegister,
			StartLimit:  b.setting.GetPaidSubStartRateLimitPerMin,
			AllowStart:  func(id int64, limit int) bool { return b.startLimiter.AllowWithLimit(id, limit).Allowed },
			Inbounds:    b.setting.GetPaidSubAutoInbounds,
			ClientLimit: b.setting.GetPaidSubMaxClients,
			Limits: func() (int, int, error) {
				days, daysErr := b.setting.GetPaidSubTrialDays()
				gigabytes, volumeErr := b.setting.GetPaidSubTrialVolumeGB()
				return days, gigabytes, errors.Join(daysErr, volumeErr)
			},
			CreateClient: func(ctx context.Context, trial customerbot.TrialClient) (uint, error) {
				return b.createTrialClient(ctx, db, trial)
			},
			OnRegistered: func(clientID uint, tgID int64) {
				_ = (&service.AuditService{Runtime: b.runtime}).Record(service.AuditEvent{
					Actor: "PaidSubBot", Event: "paidsub_registered", Resource: "paidsub", Severity: service.AuditSeverityInfo,
					Details: map[string]any{"clientId": clientID, "tgUserId": tgID},
				})
			},
		},
		Subscription: func(id int64) (string, []string, error) { return b.subscriptionFacts(db, id) },
		ReadUsage:    func(id int64) (customerbot.Usage, error) { return b.usageFacts(db, id) },
		RenderQR:     renderQR, Greeting: b.setting.GetPaidSubGreeting, RefundRevokes: b.setting.GetPaidSubRefundRevoke,
		Provider: b.payments.providerByKind, Purchase: b.payments.createOrder,
		ApplyPayment: func(_ context.Context, orderID uint, chargeID string) (bool, error) {
			applied, _, err := b.payments.applyPaidOrder(db, orderID, chargeID)
			return applied, err
		},
		Refund: b.payments.refundOrder,
		AuditCrossUser: func(tgID int64, orderID uint, action string) {
			auditCrossUserOrderAccess(b.runtime, tgID, orderID, action)
		},
		Notify: service.NotifyPanelEvent, Warn: warnPaid,
	}
}

func warnPaid(message string, err error) {
	if err == nil {
		logger.Warning("paidsub: ", message)
		return
	}
	logger.Warning("paidsub: ", message, ": ", err)
}

func auditCrossUserOrderAccess(runtime *service.Runtime, tgID int64, orderID uint, action string) {
	_ = (&service.AuditService{Runtime: runtime}).Record(service.AuditEvent{
		Actor:    fmt.Sprintf("tg:%d", tgID),
		Event:    "paidsub_cross_user_access",
		Resource: "paidsub",
		Severity: service.AuditSeverityWarn,
		Details:  map[string]any{"orderId": orderID, "action": action},
	})
}
