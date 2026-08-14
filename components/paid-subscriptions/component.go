//go:build !minimal

package paidsubscriptions

import (
	"context"
	_ "embed"
	"errors"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	paidadmin "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/admin"
	paidcore "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	paidsettings "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/settings"
	paidtelegram "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/telegram"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

//go:embed component.json
var componentJSON []byte

var componentManifest = manifest.MustFromJSON(componentJSON)
var id = componentManifest.ID

func init() {
	registry.Register(registry.Component{
		Manifest:  componentManifest,
		Lifecycle: &component{},
	})
	registry.RegisterAPIRoutes(id, componenthost.RouteAdapter[paidadmin.Deps]{
		Build:    paidAdminDeps,
		Register: paidadmin.RegisterRoutes,
	}.RegisterRoutes)
}

type component struct {
	mu                            sync.Mutex
	started                       bool
	scheduler                     componenthost.Scheduler
	unregisterSettingContribution func()
	unregisterTelegramActions     func()
	paymentPollEntryID            cron.EntryID
}

func (*component) Migrate(context.Context, lifecycle.Context) error {
	return paidcore.EnsureSchema(dbsqlite.DB())
}

func (*component) MigrateStaged(_ context.Context, db *gorm.DB) error {
	return paidcore.EnsureSchema(db)
}

func (*component) DropData(context.Context, lifecycle.Context) error {
	if err := paidcore.DropSchema(dbsqlite.DB()); err != nil {
		return err
	}
	if dbsqlite.DB() == nil {
		return nil
	}
	return dbsqlite.DB().Where("key IN ?", paidsettings.AllKeys()).Delete(&model.Setting{}).Error
}

func (c *component) Start(_ context.Context, ctx lifecycle.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return nil
	}
	if ctx.Host.Scheduler == nil {
		return errors.New("paid-subscriptions scheduler is unavailable")
	}
	if ctx.Host.API.Runtime == nil {
		return errors.New("paid-subscriptions runtime is unavailable")
	}
	runtime := ctx.Host.API.Runtime
	c.started = true
	var rollback []func()
	if c.unregisterTelegramActions == nil {
		c.unregisterTelegramActions = paidadmin.RegisterTelegramActions(
			func(ctx context.Context, text string) (int, int, error) {
				return paidtelegram.Broadcast(ctx, runtime, text)
			},
			func(ctx context.Context, orderID uint, revoke bool) (string, error) {
				return paidtelegram.RefundOrder(ctx, runtime, orderID, revoke)
			},
		)
		rollback = append(rollback, c.unregisterTelegramActions)
	}
	if c.unregisterSettingContribution == nil {
		c.unregisterSettingContribution = registerSettingContribution()
		rollback = append(rollback, c.unregisterSettingContribution)
	}
	entryID, err := ctx.Host.Scheduler.AddJob("@every 20s", paymentPollJob{runtime: runtime})
	if err != nil {
		c.started = false
		c.unregisterTelegramActions = nil
		c.unregisterSettingContribution = nil
		for i := len(rollback) - 1; i >= 0; i-- {
			if rollback[i] != nil {
				rollback[i]()
			}
		}
		return err
	}
	c.scheduler = ctx.Host.Scheduler
	c.paymentPollEntryID = entryID
	paidtelegram.StartBot(runtime)
	return nil
}

func (c *component) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	scheduler := c.scheduler
	unregisterSettingContribution := c.unregisterSettingContribution
	unregisterTelegramActions := c.unregisterTelegramActions
	entryID := c.paymentPollEntryID
	if scheduler != nil && entryID != 0 {
		if err := scheduler.RemoveJobAndWait(ctx, entryID); err != nil {
			return err
		}
	}
	if err := paidtelegram.StopBot(ctx); err != nil {
		return err
	}
	if unregisterSettingContribution != nil {
		unregisterSettingContribution()
	}
	if unregisterTelegramActions != nil {
		unregisterTelegramActions()
	}
	c.started = false
	c.scheduler = nil
	c.unregisterSettingContribution = nil
	c.unregisterTelegramActions = nil
	c.paymentPollEntryID = 0
	return nil
}

func paidAdminDeps(host componenthost.APIDeps) paidadmin.Deps {
	return paidadmin.Deps{
		RequireScope: host.Auth.RequireScope,
		LoginUser:    host.Auth.LoginUser,
		Audit:        host.Audit.Audit,
	}
}

type paymentPollJob struct {
	runtime *service.Runtime
}

func (j paymentPollJob) Run() {
	paidtelegram.PollOnce(context.Background(), j.runtime)
}

func (j paymentPollJob) RunContext(ctx context.Context) {
	paidtelegram.PollOnce(ctx, j.runtime)
}
