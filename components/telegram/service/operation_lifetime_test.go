//go:build !minimal

package telegram_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

type lifetimeTelegramSettings struct{ testTelegramSettings }

func (lifetimeTelegramSettings) GetTelegramEnabled() (bool, error) { return true, nil }
func (lifetimeTelegramSettings) GetTelegramBotToken() (string, error) {
	return "qualification-fixture", nil
}
func (lifetimeTelegramSettings) GetTelegramChatID() (string, error) { return "41", nil }

type telegramLifetimeTransport func(*http.Request) (*http.Response, error)

func (f telegramLifetimeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNotificationAndDocumentRetainAdmissionUntilSendCompletes(t *testing.T) {
	for _, kind := range []string{"notification", "document"} {
		t.Run(kind, func(t *testing.T) {
			initSettingTestDB(t)
			original := dbsqlite.DB()
			entered, proceed := make(chan struct{}), make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(proceed) }) }
			t.Cleanup(resume)
			service := &telegramservice.Service{Settings: lifetimeTelegramSettings{}, Client: &http.Client{Transport: telegramLifetimeTransport(func(r *http.Request) (*http.Response, error) {
				close(entered)
				select {
				case <-proceed:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
				// The real document pipe must be consumed before acknowledging it.
				if r.Body != nil {
					if _, err := io.Copy(io.Discard, r.Body); err != nil {
						return nil, err
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}, nil
			})}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			finished := make(chan telegramservice.Result, 1)
			go func() {
				if kind == "document" {
					finished <- service.SendDocumentStream(ctx, "fixture.txt", strings.NewReader("bounded fixture"), "")
				} else {
					finished <- service.SendContext(ctx, "bounded fixture")
				}
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			type maintenanceResult struct {
				owner *dbsqlite.Maintenance
				err   error
			}
			maintained := make(chan maintenanceResult, 1)
			go func() { owner, err := dbsqlite.BeginMaintenance(ctx); maintained <- maintenanceResult{owner, err} }()
			for {
				_, release, err := dbsqlite.AcquireOperation(ctx)
				if errors.Is(err, dbsqlite.ErrMaintenance) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				release()
				runtime.Gosched()
			}
			select {
			case got := <-maintained:
				if got.owner != nil {
					got.owner.End()
				}
				t.Fatalf("maintenance crossed finite Telegram send: %v", got.err)
			default:
			}
			if result := service.SendContext(ctx, "late fixture"); result.Success || result.ErrorClass != "maintenance" {
				t.Fatal("late send admitted outbound I/O")
			}
			resume()
			select {
			case result := <-finished:
				if !result.Success {
					t.Fatalf("admitted send failed: %s", result.ErrorClass)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case got := <-maintained:
				if got.err != nil {
					t.Fatal(got.err)
				}
				got.owner.End()
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if dbsqlite.DB() != original {
				t.Fatal("finite send changed generation")
			}
		})
	}
}
