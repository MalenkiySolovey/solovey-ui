//go:build !minimal

package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func httpPublicationFixture(t *testing.T, standalone bool) (*Service, *gorm.DB, fallbackdomain.Site, TargetView) {
	t.Helper()
	db, _ := openFallbackDB(t)
	if err := authority.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	setSetting(t, db, "webPath", "/private-panel/")
	runtime := NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "HTTP fixture"}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	var target TargetView
	if standalone {
		free, ok := freeLoopbackPortCandidate(false)
		if !ok {
			t.Fatal("no loopback port")
		}
		target, err = service.SaveTarget(site.ID, TargetInput{Kind: "standalone", Listen: free.Listen, Port: free.Port, Runtime: "gin"}, "fixture")
		if err != nil {
			t.Fatal(err)
		}
	}
	site, err = service.GetSite(site.ID)
	if err != nil {
		t.Fatal(err)
	}
	return service, db, site, target
}

func publishHTTPFixture(t *testing.T, service *Service, siteID uint) PublishResult {
	t.Helper()
	result, err := service.PublishSite(siteID, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func httpFixtureResponse(t *testing.T, client *http.Client, base, path string) (*http.Response, string) {
	t.Helper()
	response, err := client.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(data)
}

func writeHTTPTestCertificate(t *testing.T, serial int64) (string, string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "decoy.example"}, DNSNames: []string{"decoy.example"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}), 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("certificate fixture")
	}
	return certPath, keyPath, roots
}

func TestHTTPPublicationRevisionRollbackAndTLSMaterial(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(strconv.FormatBool(secure), func(t *testing.T) {
			service, db, site, target := httpPublicationFixture(t, true)
			client := &http.Client{Timeout: 2 * time.Second}
			scheme := "http"
			cert, key := "", ""
			if secure {
				var roots *x509.CertPool
				cert, key, roots = writeHTTPTestCertificate(t, 1)
				setSetting(t, db, "webCertFile", cert)
				setSetting(t, db, "webKeyFile", key)
				var err error
				target, err = service.SaveTarget(site.ID, TargetInput{ID: target.ID, Kind: "standalone", Listen: target.Listen, Port: target.Port, Runtime: "gin", TLS: true}, "fixture")
				if err != nil {
					t.Fatal(err)
				}
				transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "decoy.example", MinVersion: tls.VersionTLS12}, DisableKeepAlives: true}
				t.Cleanup(transport.CloseIdleConnections)
				client.Transport = transport
				scheme = "https"
				sqlDB, err := db.DB()
				if err != nil {
					t.Fatal(err)
				}
				sqlDB.SetMaxOpenConns(1)
				sqlDB.SetMaxIdleConns(1)
			}
			first := publishHTTPFixture(t, service, site.ID)
			base := scheme + "://" + net.JoinHostPort(target.Listen, strconv.Itoa(target.Port))
			response, before := httpFixtureResponse(t, client, base, "/")
			if response.StatusCode != 200 || !strings.Contains(before, site.Name) {
				t.Fatalf("home response %d %q", response.StatusCode, before)
			}
			for _, requestPath := range []string{"/missing/", "/private-panel/", "/.well-known/acme-challenge/probe"} {
				response, _ = httpFixtureResponse(t, client, base, requestPath)
				if response.StatusCode != 404 {
					t.Fatalf("reserved/missing %s: %d", requestPath, response.StatusCode)
				}
			}
			if secure && (!service.runtime.TLSAcceptsServerName(target.Listen, target.Port, "decoy.example") || service.runtime.TLSAcceptsServerName(target.Listen, target.Port, "other.example")) {
				t.Fatal("TLS facts lost exact certificate identity")
			}
			if _, err := service.SavePage(site.ID, PageInput{ID: site.Pages[0].ID, Path: "/", Title: "Updated", Body: "replacement revision", IsHome: true}, "fixture"); err != nil {
				t.Fatal(err)
			}
			second := publishHTTPFixture(t, service, site.ID)
			_, after := httpFixtureResponse(t, client, base, "/")
			if second.Version == first.Version || after == before || !strings.Contains(after, "replacement revision") {
				t.Fatal("new immutable revision was not served")
			}
			if _, err := service.RollbackSite(site.ID, RollbackInput{Version: first.Version}, "fixture"); err != nil {
				t.Fatal(err)
			}
			_, rolledBack := httpFixtureResponse(t, client, base, "/")
			if rolledBack != before {
				t.Fatal("rollback did not restore exact served bytes")
			}
			if secure {
				state := service.runtime.PublicationState()
				setSetting(t, db, "webKeyFile", filepath.Join(t.TempDir(), "missing.key"))
				if err := service.runtime.Rebuild(db); err == nil {
					t.Fatal("invalid TLS material was admitted")
				}
				if service.runtime.PublicationState() != state {
					t.Fatal("invalid TLS candidate replaced accepted publication")
				}
				_, body := httpFixtureResponse(t, client, base, "/")
				if body != before {
					t.Fatal("failed TLS preparation lost old listener")
				}
				setSetting(t, db, "webKeyFile", key)
				nextCert, nextKey, nextRoots := writeHTTPTestCertificate(t, 2)
				setSetting(t, db, "webCertFile", nextCert)
				setSetting(t, db, "webKeyFile", nextKey)
				if err := service.runtime.Rebuild(db); err != nil {
					t.Fatal(err)
				}
				nextTransport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: nextRoots, ServerName: "decoy.example", MinVersion: tls.VersionTLS12}}
				t.Cleanup(nextTransport.CloseIdleConnections)
				response, body = httpFixtureResponse(t, &http.Client{Transport: nextTransport, Timeout: 2 * time.Second}, base, "/")
				if body != before || response.TLS.PeerCertificates[0].SerialNumber.Int64() != 2 {
					t.Fatal("TLS rotation did not use newly loaded certificate")
				}
			}
		})
	}
}

func TestHTTPBindingRejectsForeignStaleCancelledAndReplayedScope(t *testing.T) {
	service, db, site, _ := httpPublicationFixture(t, true)
	result := publishHTTPFixture(t, service, site.ID)
	site, err := service.GetSite(site.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime := service.runtime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	binding, err := runtime.prepareHTTPBinding(db, site.ID, result.Version, site.Targets)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*HTTPBindingScope){
		func(s *HTTPBindingScope) { s.Resource.Host = hostref.Ref{} },
		func(s *HTTPBindingScope) { s.Resource.Owner = "foreign" },
		func(s *HTTPBindingScope) { s.Resource.ResourceID = "foreign:site" },
		func(s *HTTPBindingScope) { s.Resource.DesiredRevision = "stale" },
		func(s *HTTPBindingScope) { s.PublicationRevision = "stale" },
		func(s *HTTPBindingScope) { s.ExpectedGeneration++ },
	}
	for _, mutate := range mutations {
		scope := binding.Scope()
		mutate(&scope)
		if err := binding.Bind(context.Background(), scope); err == nil {
			t.Fatal("foreign or stale binding accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := binding.Bind(ctx, binding.Scope()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled binding: %v", err)
	}
	if err := binding.Bind(context.Background(), binding.Scope()); err != nil {
		t.Fatal(err)
	}
	if err := binding.Bind(context.Background(), binding.Scope()); err == nil {
		t.Fatal("binding replay was admitted")
	}
	stale, err := runtime.prepareHTTPBinding(db, site.ID, result.Version, site.Targets)
	if err != nil {
		t.Fatal(err)
	}
	runtime.generation++
	if err := stale.Bind(context.Background(), stale.Scope()); err == nil {
		t.Fatal("old generation binding was admitted")
	}
	foreign := site.Targets[0]
	foreign.SiteID++
	if _, err := runtime.prepareHTTPBinding(db, site.ID, result.Version, []fallbackdomain.RuntimeTarget{foreign}); err == nil {
		t.Fatal("foreign target admitted")
	}
	var count int64
	if err := db.Model(&authority.ReservationModel{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("binding leaked durable reservation: %d %v", count, err)
	}
}

func TestHTTPBindFailureCompensatesPartialPlan(t *testing.T) {
	service, db, site, target := httpPublicationFixture(t, true)
	publishHTTPFixture(t, service, site.ID)
	listeners := make([]net.Listener, 2)
	for i := range listeners {
		var err error
		listeners[i], err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(listeners, func(i, j int) bool { return listeners[i].Addr().String() < listeners[j].Addr().String() })
	freeAddress := listeners[0].Addr().String()
	freePort := listeners[0].Addr().(*net.TCPAddr).Port
	occupiedPort := listeners[1].Addr().(*net.TCPAddr).Port
	if err := listeners[0].Close(); err != nil {
		t.Fatal(err)
	}
	defer listeners[1].Close()
	before := service.runtime.PublicationState()
	err := service.guardedRuntimeMutation(site.ID, func(tx *gorm.DB) error {
		return tx.Create(&[]fallbackdomain.RuntimeTarget{
			{SiteID: site.ID, Kind: "standalone", Listen: "127.0.0.1", Port: freePort, Runtime: "gin"},
			{SiteID: site.ID, Kind: "standalone", Listen: "127.0.0.1", Port: occupiedPort, Runtime: "gin"},
		}).Error
	})
	if err == nil {
		t.Fatal("occupied target accepted")
	}
	if !service.runtime.Owns(target.Listen, target.Port) {
		t.Fatal("prior listener was lost")
	}
	after := service.runtime.PublicationState()
	if after.PublicationRevision != before.PublicationRevision || after.Resource != before.Resource {
		t.Fatal("failed plan replaced publication identity")
	}
	probe, err := net.Listen("tcp", freeAddress)
	if err != nil {
		t.Fatalf("partially bound listener leaked: %v", err)
	}
	probe.Close()
	var targets int64
	if err := db.Model(&fallbackdomain.RuntimeTarget{}).Where("site_id = ?", site.ID).Count(&targets).Error; err != nil || targets != 1 {
		t.Fatalf("target transaction not restored: %d %v", targets, err)
	}
	_, _ = httpFixtureResponse(t, &http.Client{Timeout: time.Second}, "http://"+net.JoinHostPort(target.Listen, strconv.Itoa(target.Port)), "/")
}

type blockedHTTPWriter struct {
	http.ResponseWriter
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockedHTTPWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return w.ResponseWriter.Write(data)
}

func TestShutdownFailureRetainsAuthorityAndRetryDrainsActiveResponses(t *testing.T) {
	for _, standalone := range []bool{false, true} {
		t.Run(strconv.FormatBool(standalone), func(t *testing.T) {
			service, db, site, target := httpPublicationFixture(t, standalone)
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			if standalone {
				service.runtime.serving.(*backendHTTP).handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					service.runtime.serveHTTPPublic(&blockedHTTPWriter{ResponseWriter: w, entered: entered, release: release}, req)
				})
			}
			publishHTTPFixture(t, service, site.ID)
			insertGuardReservation(t, db, site.ID, neutral.ReservationActive, time.Now().UTC())
			var reservationBefore authority.ReservationModel
			if err := db.First(&reservationBefore).Error; err != nil {
				t.Fatal(err)
			}
			var publish fallbackdomain.Publish
			if err := db.Preload("Files").Where("active = ?", true).First(&publish).Error; err != nil {
				t.Fatal(err)
			}
			completed := make(chan error, 1)
			if standalone {
				go func() {
					client := &http.Client{Timeout: 3 * time.Second}
					response, err := client.Get("http://" + net.JoinHostPort(target.Listen, strconv.Itoa(target.Port)) + "/")
					if err == nil {
						_, err = io.Copy(io.Discard, response.Body)
						response.Body.Close()
					}
					completed <- err
				}()
			} else {
				go func() {
					recorder := httptest.NewRecorder()
					writer := &blockedHTTPWriter{ResponseWriter: recorder, entered: entered, release: release}
					ctx, _ := gin.CreateTestContext(writer)
					ctx.Request = httptest.NewRequest("GET", "/", nil)
					if !service.runtime.ServePublic(ctx, publicsurface.Context{AdminBasePath: "/private-panel/"}) {
						completed <- fmt.Errorf("request was not served")
						return
					}
					completed <- nil
				}()
			}
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("response did not enter serving")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			err := service.runtime.Shutdown(ctx)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("failed drain reported success: %v", err)
			}
			if !service.runtime.Status().Active || !service.runtime.PublicationState().Draining || service.runtime.unregister == nil {
				t.Fatal("failed stop erased recovery evidence")
			}
			if service.runtime.Health(publicsurface.Context{AdminBasePath: "/private-panel/"}).OK {
				t.Fatal("draining runtime claimed healthy")
			}
			if err := service.runtime.Start(db); !errors.Is(err, ErrRuntimeDraining) {
				t.Fatalf("activation during failed drain: %v", err)
			}
			if err := service.runtime.Rebuild(db); !errors.Is(err, ErrRuntimeDraining) {
				t.Fatalf("rebuild during failed drain: %v", err)
			}
			releaseOnce.Do(func() { close(release) })
			select {
			case err := <-completed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("accepted response was not allowed to complete")
			}
			if err := service.runtime.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if service.runtime.Status().Active || service.runtime.unregister != nil {
				t.Fatal("successful retry did not stop runtime")
			}
			var reservationAfter authority.ReservationModel
			if err := db.First(&reservationAfter).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reservationBefore, reservationAfter) {
				t.Fatal("stop changed resource reservation")
			}
			var active int64
			if err := db.Model(&fallbackdomain.Publish{}).Where("active = ?", true).Count(&active).Error; err != nil || active != 1 {
				t.Fatal("stop unpublished durable revision")
			}
			if err := verifyPublishFiles(publish.RootDir, publish.Files); err != nil {
				t.Fatal(err)
			}
			if standalone {
				service.runtime.serving.(*backendHTTP).handler = http.HandlerFunc(service.runtime.serveHTTPPublic)
			}
			for range 2 {
				if err := service.runtime.Start(db); err != nil {
					t.Fatal(err)
				}
				if !service.runtime.Status().Active {
					t.Fatal("explicit restart did not recover publication")
				}
				if err := service.runtime.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestShutdownCancellationDoesNotContinueBehindCaller(t *testing.T) {
	runtime := NewRuntime()
	runtime.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := runtime.Shutdown(ctx)
	cancel()
	runtime.mu.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) || runtime.draining.Load() {
		t.Fatalf("contended cancellation changed runtime: %v", err)
	}
	if err := runtime.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationCancellationAndConcurrentConflict(t *testing.T) {
	service, db, site, _ := httpPublicationFixture(t, false)
	publishHTTPFixture(t, service, site.ID)
	before, _ := os.ReadDir(filepath.Join(storageRoot(), "publishes", "site-"+uintString(site.ID)))
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { done <- authority.WithMutationLock(func() error { close(held); <-release; return nil }) }()
	<-held
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	_, err := service.PublishSiteContext(ctx, site.ID, "fixture")
	cancel()
	close(release)
	if lockErr := <-done; lockErr != nil {
		t.Fatal(lockErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled publication: %v", err)
	}
	after, _ := os.ReadDir(filepath.Join(storageRoot(), "publishes", "site-"+uintString(site.ID)))
	if len(before) != len(after) {
		t.Fatal("cancelled publication leaked candidate files")
	}
	if err := service.UnpublishSite(site.ID, "fixture"); err != nil {
		t.Fatal(err)
	}
	other, err := service.SaveSite(SiteInput{Name: "Concurrent site"}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, id := range []uint{site.ID, other.ID} {
		go func() { _, err := service.PublishSiteContext(context.Background(), id, "fixture"); results <- err }()
	}
	successes := 0
	for range 2 {
		if <-results == nil {
			successes++
		}
	}
	var active int64
	if err := db.Model(&fallbackdomain.Publish{}).Where("active = ?", true).Count(&active).Error; err != nil || successes != 1 || active != 1 {
		t.Fatalf("concurrent publication admission: %d successful, %d active, %v", successes, active, err)
	}
}

type pausedHTTPFacts struct {
	HostFacts
	entered, release chan struct{}
	once             sync.Once
}

func (p *pausedHTTPFacts) CurrentWebTarget(ctx context.Context) (TargetView, error) {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
	case <-ctx.Done():
		return TargetView{}, ctx.Err()
	}
	return p.HostFacts.CurrentWebTarget(ctx)
}

func TestPublicationRejectsTargetChangedDuringPreparation(t *testing.T) {
	service, db, site, target := httpPublicationFixture(t, true)
	first := publishHTTPFixture(t, service, site.ID)
	facts := &pausedHTTPFacts{HostFacts: BackendHostFacts(), entered: make(chan struct{}), release: make(chan struct{})}
	prepared := NewWithHost(db, service.runtime, facts)
	done := make(chan error, 1)
	go func() { _, err := prepared.PublishSiteContext(context.Background(), site.ID, "fixture"); done <- err }()
	select {
	case <-facts.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("publication did not reach target preflight")
	}
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(facts.release) }) })
	free, ok := freeLoopbackPortCandidate(false)
	if !ok {
		t.Fatal("no free target")
	}
	if _, err := service.SaveTarget(site.ID, TargetInput{ID: target.ID, Kind: "standalone", Listen: target.Listen, Port: free.Port, Runtime: "gin"}, "fixture"); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(facts.release) })
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "changed while publication") {
			t.Fatalf("stale candidate: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stale publication did not finish")
	}
	if state := service.runtime.PublicationState(); state.PublicationRevision != first.Version {
		t.Fatal("stale candidate replaced active revision")
	}
}
