package service

import (
	"net/http"
	"sync"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
)

func TestTelegramHTTPClientConcurrentReloadRaceAnchorIssue21(t *testing.T) {
	settingService := initSettingTestDB(t)
	setTelegramProxyConfigIssue21(t, settingService, integrationtelegram.ProxyConfig{URL: "http://8.8.8.8:8080"})
	service := &TelegramService{}
	seed, err := service.getTelegramHTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	setTelegramProxyConfigIssue21(t, settingService, integrationtelegram.ProxyConfig{})

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
			client, err := service.getTelegramHTTPClient()
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

func TestTelegramHTTPClientReusesClientOnSameConfigIssue21(t *testing.T) {
	settingService := initSettingTestDB(t)
	setTelegramProxyConfigIssue21(t, settingService, integrationtelegram.ProxyConfig{})

	service := &TelegramService{}
	first, err := service.getTelegramHTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.getTelegramHTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatal("expected same config to reuse the telegram http client")
	}
}

func TestTelegramHTTPClientReplacesClientOnDifferentConfigIssue21(t *testing.T) {
	settingService := initSettingTestDB(t)
	setTelegramProxyConfigIssue21(t, settingService, integrationtelegram.ProxyConfig{})
	seed, err := (&TelegramService{}).getTelegramHTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	setTelegramProxyConfigIssue21(t, settingService, integrationtelegram.ProxyConfig{URL: "http://8.8.8.8:8080"})

	client, err := (&TelegramService{}).getTelegramHTTPClient()
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

func setTelegramProxyConfigIssue21(t *testing.T, settingService *SettingService, cfg integrationtelegram.ProxyConfig) {
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
