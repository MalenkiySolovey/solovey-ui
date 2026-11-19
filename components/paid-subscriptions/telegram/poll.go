//go:build !minimal

package telegram

import (
	"context"
	"sync"

	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	"github.com/MalenkiySolovey/solovey-component-paid-subscriptions/customerbot"
	paidprovider "github.com/MalenkiySolovey/solovey-component-paid-subscriptions/provider"
	"github.com/MalenkiySolovey/solovey-component-paid-subscriptions/workflow"
	paidstore "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/store"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

var pollMu sync.Mutex

// PollOnce retains the admitted DB generation throughout bounded provider work.
// The mutex reduces overlap; durable state guards arbitrate financial changes.
func PollOnce(ctx context.Context, runtime *service.Runtime) {
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return
	}
	defer release()
	setting := paidSettings{}
	if enabled, err := setting.GetPaidSubEnabled(); err != nil || !enabled {
		return
	}
	if !pollMu.TryLock() {
		return
	}
	defer pollMu.Unlock()
	ps := newPaymentCoordinator(runtime)
	prov := ps.providerByKind(paidprovider.ProviderCryptoBot)
	if poller, ok := prov.(paidprovider.PollingProvider); ok {
		reconciler, _ := prov.(paidprovider.ReconciliationProvider)
		runCryptoBotWork(ctx, runtime, ps, poller, reconciler)
	}
	if ctx.Err() != nil {
		return
	}
	if err := ps.ExpireStaleOrders(); err != nil {
		logger.Warning("paidsub: expire local orders: ", err)
	}
}

func runCryptoBotWork(ctx context.Context, runtime *service.Runtime, ps *paymentCoordinator, poller paidprovider.PollingProvider, reconciler paidprovider.ReconciliationProvider) {
	admitted := dbsqlite.DB()
	workflow.RunCryptoBot(ctx, admitted, poller, reconciler, func(connection *gorm.DB, orderID uint, chargeID string) (func(context.Context), error) {
		// For recovery, connection is the transaction that also stores invoice
		// evidence and cancellation work. No global DB writes from this callback.
		applied, err := paidstore.ApplyPaidOrderGrant(connection, orderID, chargeID, nil, nowUnix(), "PaidSubBot")
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context) {
			ps.afterPaidCommit(admitted, applied, orderID)
			if applied.Applied && applied.TelegramUserID > 0 {
				notifyPaid(ctx, runtime, applied.TelegramUserID)
			}
		}, nil
	}, warnPaid)
}

func notifyPaid(ctx context.Context, runtime *service.Runtime, tgUserID int64) {
	b, err := newSenderBot(runtime)
	if err != nil {
		return
	}
	defer b.closeIdleConnections()
	_ = b.sendMessage(ctx, tgUserID, customerbot.Text(customerbot.English, "pay_success"), nil)
}
