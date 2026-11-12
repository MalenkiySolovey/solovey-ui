//go:build !minimal

package fallbackhtml

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/gin-gonic/gin"
)

type heldLifecycleResponse struct {
	http.ResponseWriter
	entered, release chan struct{}
	once             sync.Once
}

func (w *heldLifecycleResponse) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return w.ResponseWriter.Write(data)
}

func TestComponentStopAndDropPropagateFailedDrainAndRetainRecoveryHooks(t *testing.T) {
	supervisor, _ := admissionFixture(t, true)
	if err := supervisor.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	previous := fallbackservice.DefaultRuntime
	runtime := fallbackservice.NewRuntime()
	fallbackservice.DefaultRuntime = runtime
	t.Cleanup(func() {
		if err := (component{}).Stop(context.Background()); err != nil {
			t.Error(err)
		}
		fallbackservice.DefaultRuntime = previous
	})
	setComponentTestSetting(t, "webPath", "/private-panel/")
	if err := (component{}).Start(t.Context(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	service := fallbackservice.New(dbsqlite.DB(), runtime)
	site, err := service.SaveSite(fallbackservice.SiteInput{Name: "Lifecycle preservation"}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishSite(site.ID, "fixture"); err != nil {
		t.Fatal(err)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	go func() {
		writer := &heldLifecycleResponse{ResponseWriter: httptest.NewRecorder(), entered: entered, release: release}
		ctx, _ := gin.CreateTestContext(writer)
		ctx.Request = httptest.NewRequest("GET", "/", nil)
		runtime.ServePublic(ctx, publicsurface.Context{AdminBasePath: "/private-panel/"})
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("response did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	err = (component{}).Stop(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("component hid drain failure: %v", err)
	}
	if _, ok := neutral.Default.ProviderV2(id); !ok || runtimeHooks.unregisterBackup == nil || runtimeHooks.unregisterHealth == nil || runtimeHooks.unregisterResources == nil || runtimeHooks.restoreHookName == "" {
		t.Fatal("failed stop erased recovery owners")
	}
	if err := (component{}).Start(context.Background(), lifecycle.Context{}); !errors.Is(err, fallbackservice.ErrRuntimeDraining) {
		t.Fatalf("failed-drain restart: %v", err)
	}
	if _, ok := neutral.Default.ProviderV2(id); !ok || runtimeHooks.unregisterHealth == nil {
		t.Fatal("failed restart erased recovery hooks")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
	err = backendDrop(ctx, dbsqlite.DB())
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DropData hid drain failure: %v", err)
	}
	var active int64
	if err := dbsqlite.DB().Model(&fallbackdomain.Publish{}).Where("active = ?", true).Count(&active).Error; err != nil || active != 1 {
		t.Fatal("failed drop deleted active publication", err)
	}
	once.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("accepted response was lost")
	}
	if err := (component{}).Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := neutral.Default.ProviderV2(id); ok || runtimeHooks.unregisterHealth != nil {
		t.Fatal("successful stop retained runtime registration")
	}
	if !dbsqlite.DB().Migrator().HasTable(&fallbackdomain.Site{}) {
		t.Fatal("Stop performed DropData")
	}
}

func TestTLSProviderPublicationAndReservationUseOneSQLiteConnection(t *testing.T) {
	supervisor, _ := admissionFixture(t, true)
	if err := supervisor.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	previous := fallbackservice.DefaultRuntime
	runtime := fallbackservice.NewRuntime()
	fallbackservice.DefaultRuntime = runtime
	t.Cleanup(func() {
		if err := (component{}).Stop(context.Background()); err != nil {
			t.Error(err)
		}
		fallbackservice.DefaultRuntime = previous
	})
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(fixture.Close)
	certificate := fixture.Certificate()
	if len(certificate.DNSNames) == 0 {
		t.Fatal("TLS fixture has no DNS name")
	}
	name := certificate.DNSNames[0]
	material := fixture.TLS.Certificates[0]
	encodedKey, err := x509.MarshalPKCS8PrivateKey(material.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	certFile, keyFile := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: material.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}), 0600); err != nil {
		t.Fatal(err)
	}
	setComponentTestSetting(t, "webPath", "/private-panel/")
	setComponentTestSetting(t, "webCertFile", certFile)
	setComponentTestSetting(t, "webKeyFile", keyFile)
	if err := (component{}).Start(t.Context(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	service := fallbackservice.New(db, runtime)
	site, err := service.SaveSite(fallbackservice.SiteInput{Name: "TLS connection scope"}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	target, err := service.SaveTarget(site.ID, fallbackservice.TargetInput{Kind: "standalone", Host: name, Listen: "127.0.0.1", Port: freeProviderPort(t), Runtime: "gin", TLS: true}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := service.PublishSiteContext(ctx, site.ID, "fixture"); err != nil {
		t.Fatal(err)
	}
	if !runtime.TLSAcceptsServerName(target.Listen, target.Port, name) {
		t.Fatal("installed TLS certificate was not verified")
	}
	setComponentTestSetting(t, "webKeyFile", filepath.Join(directory, "missing.key"))
	if !runtime.TLSAcceptsServerName(target.Listen, target.Port, name) {
		t.Fatal("candidate TLS settings replaced effective facts")
	}
	provider := targetProvider{db: db, runtime: runtime, observer: providerObserver(t, runtime)}
	actionable := onlyActionableTarget(t, provider)
	reference, err := neutral.ReferenceV2FromTarget(actionable)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Reserve(ctx, neutral.ReserveRequestV1{RequestID: "tls-one-connection", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 60}); err != nil {
		t.Fatal(err)
	}
}
