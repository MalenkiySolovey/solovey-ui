//go:build !minimal

package telegram_test

import (
	"net/http"
	"sync"
	"testing"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
)

func TestTelegramHTTPClientConcurrentReloadRaceAnchor(t *testing.T) {
	settingService := initSettingTestDB(t)
	setTelegramProxyConfig(t, settingService, integrationtelegram.ProxyConfig{URL: "http://8.8.8.8:8080"})
	service := &telegramservice.Service{Settings: testTelegramSettings{}}
	seed, err := service.HTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	setTelegramProxyConfig(t, settingService, integrationtelegram.ProxyConfig{})

	const workers = 64

	clients := make([]*http.Client, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			client, err := service.HTTPClient()
			if err != nil {
				errs <- err
				return
			}
			clients[index] = client
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
	if clients[0] == nil {
		t.Fatal("first worker received nil telegram http client")
	}
	if clients[0] == seed {
		t.Fatal("telegram http client should be replaced when config changes")
	}
	for i, client := range clients {
		if client != clients[0] {
			t.Fatalf("worker %d received a different telegram http client", i)
		}
	}
}

func TestTelegramHTTPClientReusesClientOnSameConfig(t *testing.T) {
	settingService := initSettingTestDB(t)
	setTelegramProxyConfig(t, settingService, integrationtelegram.ProxyConfig{})

	service := &telegramservice.Service{Settings: testTelegramSettings{}}
	first, err := service.HTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.HTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatal("expected same config to reuse the telegram http client")
	}
}

func TestTelegramHTTPClientReplacesClientOnDifferentConfig(t *testing.T) {
	settingService := initSettingTestDB(t)
	setTelegramProxyConfig(t, settingService, integrationtelegram.ProxyConfig{})
	seed, err := (&telegramservice.Service{Settings: testTelegramSettings{}}).HTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	setTelegramProxyConfig(t, settingService, integrationtelegram.ProxyConfig{URL: "http://8.8.8.8:8080"})

	client, err := (&telegramservice.Service{Settings: testTelegramSettings{}}).HTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("telegram http client should not be nil")
	}
	if client == seed {
		t.Fatal("expected different config to replace the telegram http client")
	}
}

func setTelegramProxyConfig(t *testing.T, settingService *coreservice.SettingService, cfg integrationtelegram.ProxyConfig) {
	t.Helper()
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{
		"telegramProxyURL":      cfg.URL,
		"telegramProxyUsername": cfg.Username,
		"telegramProxyPassword": cfg.Password,
	}
	for key, value := range settings {
		if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}
}
