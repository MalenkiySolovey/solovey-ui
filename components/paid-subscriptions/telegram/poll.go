//go:build !minimal

package telegram

import (
	"context"
	"sync"

	paidprovider "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/provider"
	paidstore "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/store"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

var pollMu sync.Mutex

const cryptoBotPollGraceSeconds int64 = 24 * 60 * 60

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
	pollCryptoBot(ctx, runtime, ps)
	if ctx.Err() != nil {
		return
	}
	if err := ps.ExpireStaleOrders(); err != nil {
		logger.Warning("paidsub: expire local orders: ", err)
	}
}

func pollCryptoBot(ctx context.Context, runtime *service.Runtime, ps *paymentCoordinator) {
	prov := ps.providerByKind(paidprovider.ProviderCryptoBot)
	poller, ok := prov.(paidprovider.PollingProvider)
	if !ok {
		return
	}
	reconciler, _ := prov.(paidprovider.ReconciliationProvider)
	runCryptoBotWork(ctx, runtime, ps, poller, reconciler)
}

func runCryptoBotWork(ctx context.Context, runtime *service.Runtime, ps *paymentCoordinator, poller paidprovider.PollingProvider, reconciler paidprovider.ReconciliationProvider) {
	db := dbsqlite.DB()
	seen := make(map[uint]bool)
	// Ten batches, each at most100. A persistent cursor wraps deterministically;
	// transient low-ID failures cannot starve higher IDs or survive a restart reset.
	for batch := 0; batch < 10 && ctx.Err() == nil; batch++ {
		orders, err := paidstore.WorkOrders(db, "poll", 100)
		if err != nil {
			logger.Warning("paidsub: load poll work: ", err)
			break
		}
		if len(orders) == 0 || seen[orders[0].Id] {
			break
		}
		for _, order := range orders {
			seen[order.Id] = true
		}
		outcome, err := poller.Poll(ctx, orders)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warning("paidsub: provider poll unavailable: ", err)
		} else {
			for _, result := range outcome.Paid {
				applied, tgID, err := ps.ApplyPaidOrder(result.OrderID, result.ProviderChargeID, nil)
				if err != nil {
					logger.Warning("paidsub: apply provider payment: ", err)
					continue
				}
				if applied && tgID > 0 {
					notifyPaid(ctx, runtime, tgID)
				}
			}
			if err := paidstore.ReviewProviderOrders(db, outcome.ReviewOrderIDs); err != nil {
				logger.Warning("paidsub: review provider work: ", err)
			}
			if err := paidstore.ExpireVerifiedProviderOrders(db, outcome.TerminalOrderIDs, nowUnix(), cryptoBotPollGraceSeconds); err != nil {
				logger.Warning("paidsub: expire verified invoices: ", err)
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err := paidstore.AdvanceProviderCursor(db, "poll", orders[len(orders)-1].Id); err != nil {
			logger.Warning("paidsub: persist poll cursor: ", err)
			break
		}
	}
	if reconciler == nil || ctx.Err() != nil {
		return
	}
	unresolved, err := paidstore.WorkOrders(db, "reconcile", 100)
	if err != nil {
		logger.Warning("paidsub: load recovery work: ", err)
	} else if len(unresolved) > 0 {
		recovered, err := reconciler.Reconcile(ctx, unresolved)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warning("paidsub: provider reconciliation unavailable: ", err)
		} else {
			for _, result := range recovered {
				applied, err := paidstore.RecoverProviderOrder(db, result, nowUnix(), cryptoBotPollGraceSeconds)
				if err != nil {
					logger.Warning("paidsub: persist provider recovery: ", err)
					continue
				}
				ps.afterPaidCommit(applied, result.OrderID)
				if applied.Applied && applied.TelegramUserID > 0 {
					notifyPaid(ctx, runtime, applied.TelegramUserID)
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err := paidstore.AdvanceProviderCursor(db, "reconcile", unresolved[len(unresolved)-1].Id); err != nil {
			logger.Warning("paidsub: persist recovery cursor: ", err)
		}
	}
	work, err := paidstore.CancellationWork(db, 100)
	if err != nil {
		logger.Warning("paidsub: load cancellation work: ", err)
		return
	}
	for _, item := range work {
		resolved, err := reconciler.CancelInvoice(ctx, item.Ref)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warning("paidsub: cancellation remains queued: ", err)
		} else if resolved {
			if err := paidstore.CompleteCancellation(db, item.Id); err != nil {
				logger.Warning("paidsub: persist cancellation: ", err)
			}
		}
		if err := paidstore.AdvanceProviderCursor(db, "cancel", item.Id); err != nil {
			logger.Warning("paidsub: persist cancellation cursor: ", err)
			return
		}
	}
}

func notifyPaid(ctx context.Context, runtime *service.Runtime, tgUserID int64) {
	b, err := newSenderBot(runtime)
	if err != nil {
		return
	}
	defer b.closeIdleConnections()
	_ = b.sendMessage(ctx, tgUserID, tr(langEN, "pay_success"), nil)
}
