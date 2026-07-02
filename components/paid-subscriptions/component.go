//go:build !minimal

package paidsubscriptions

import (
	"context"
	_ "embed"
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

	"github.com/robfig/cron/v3"
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
	scheduler                     componenthost.Scheduler
	unregisterSettingContribution func()
	unregisterTelegramActions     func()
	paymentPollEntryID            cron.EntryID
}

func (*component) Migrate(context.Context, lifecycle.Context) error {
	return paidcore.EnsureSchema(dbsqlite.DB())
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
	var rollback []func()
	if c.unregisterTelegramActions == nil {
		c.unregisterTelegramActions = paidadmin.RegisterTelegramActions(paidtelegram.Broadcast, paidtelegram.RefundOrder)
		rollback = append(rollback, c.unregisterTelegramActions)
	}
	if c.unregisterSettingContribution == nil {
		c.unregisterSettingContribution = registerSettingContribution()
		rollback = append(rollback, c.unregisterSettingContribution)
	}
	c.mu.Unlock()
	if ctx.Host.Scheduler != nil {
		entryID, err := ctx.Host.Scheduler.AddJob("@every 20s", paymentPollJob{})
		if err != nil {
			c.rollbackStartHooks(rollback)
			return err
		}
		c.mu.Lock()
		c.scheduler = ctx.Host.Scheduler
		c.paymentPollEntryID = entryID
		c.mu.Unlock()
	}
	paidtelegram.StartBot()
	return nil
}

func (c *component) rollbackStartHooks(rollback []func()) {
	c.mu.Lock()
	c.unregisterTelegramActions = nil
	c.unregisterSettingContribution = nil
	c.mu.Unlock()
	for i := len(rollback) - 1; i >= 0; i-- {
		if rollback[i] != nil {
			rollback[i]()
		}
	}
}

func (c *component) Stop(ctx context.Context) error {
	c.mu.Lock()
	scheduler := c.scheduler
	unregisterSettingContribution := c.unregisterSettingContribution
	unregisterTelegramActions := c.unregisterTelegramActions
	entryID := c.paymentPollEntryID
	c.scheduler = nil
	c.unregisterSettingContribution = nil
	c.unregisterTelegramActions = nil
	c.paymentPollEntryID = 0
	c.mu.Unlock()
	if scheduler != nil && entryID != 0 {
		scheduler.RemoveJob(entryID)
	}
	err := paidtelegram.StopBot(ctx)
	if unregisterSettingContribution != nil {
		unregisterSettingContribution()
	}
	if unregisterTelegramActions != nil {
		unregisterTelegramActions()
	}
	return err
}

func paidAdminDeps(host componenthost.APIDeps) paidadmin.Deps {
	return paidadmin.Deps{
		LoginUser: host.Auth.LoginUser,
		Audit:     host.Audit.Audit,
	}
}

type paymentPollJob struct{}

func (paymentPollJob) Run() {
	paidtelegram.PollOnce(context.Background())
}
