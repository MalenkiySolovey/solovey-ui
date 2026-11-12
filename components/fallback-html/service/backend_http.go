//go:build !minimal

package service

// Product composition owns listener acquisition and configured TLS material.
// This is the same standalone HTTP owner previously embedded in Runtime.
import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

type standaloneTargetConfig struct {
	key         string
	address     string
	tls         bool
	certFile    string
	keyFile     string
	tlsRevision string
	certificate *tls.Certificate
}

type runtimeListener struct {
	config  standaloneTargetConfig
	server  *http.Server
	done    chan struct{}
	ready   atomic.Bool
	failure atomic.Value // unexpected Serve error, retained until recovery
}

type backendHTTP struct {
	mu        sync.Mutex
	listeners map[string]*runtimeListener
	retired   []*runtimeListener
	handler   http.Handler
}

type backendPublicationBinding struct {
	runtime  *Runtime
	host     *backendHTTP
	scope    HTTPBindingScope
	desired  map[string]standaloneTargetConfig
	consumed atomic.Bool
}

func NewRuntime() *Runtime {
	r := &Runtime{limiter: newPublicSiteLimiter(), idle: make(chan struct{})}
	close(r.idle)
	r.snapshot.Store((*snapshot)(nil))
	r.serving = &backendHTTP{listeners: map[string]*runtimeListener{}, handler: http.HandlerFunc(r.serveHTTPPublic)}
	return r
}

// Called under the runtime lock after the existing storage owner has read the
// candidate targets and verified the immutable publication bytes.
func (r *Runtime) prepareHTTPBinding(database *gorm.DB, siteID uint, revision string, targets []fallbackdomain.RuntimeTarget) (PublicationBinding, error) {
	host, ok := r.serving.(*backendHTTP)
	if !ok {
		return nil, errors.New("fallback-html HTTP owner is unavailable")
	}
	scope := HTTPBindingScope{Resource: ConfiguredTargetIdentity(siteID, fallbackdomain.RuntimeTarget{}), PublicationRevision: revision, ExpectedGeneration: r.generation}
	if len(targets) == 1 {
		scope.Resource = ConfiguredTargetIdentity(siteID, targets[0])
	}
	desired := make(map[string]standaloneTargetConfig, len(targets))
	certificateFile, keyFile := "", ""
	tlsLoaded := false
	for _, target := range targets {
		if target.SiteID != siteID || target.Kind != "standalone" {
			return nil, errors.New("fallback-html target does not belong to this publication")
		}
		if target.TLS && !tlsLoaded {
			var err error
			certificateFile, keyFile, err = coreservice.ReadWebTLSFiles(database)
			if err != nil {
				return nil, err
			}
			tlsLoaded = true
		}
		config, valid, err := standaloneConfigForTarget(target, certificateFile, keyFile)
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, errors.New("fallback-html publication target is missing its port")
		}
		if _, duplicate := desired[config.key]; duplicate {
			return nil, fmt.Errorf("fallback-html runtime target %s is duplicated", config.address)
		}
		desired[config.key] = config
	}
	return &backendPublicationBinding{runtime: r, host: host, scope: scope, desired: desired}, nil
}

func (b *backendPublicationBinding) Scope() HTTPBindingScope { return b.scope }

func (b *backendPublicationBinding) Bind(ctx context.Context, scope HTTPBindingScope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !reflect.DeepEqual(scope, b.scope) || scope.PublicationRevision == "" || !scope.Resource.Host.IsLocal() || scope.Resource.Owner != "fallback-html" || b.runtime.generation != scope.ExpectedGeneration || b.runtime.draining.Load() {
		return errors.New("fallback-html HTTP binding scope is missing, stale or foreign")
	}
	if !b.consumed.CompareAndSwap(false, true) {
		return errors.New("fallback-html HTTP binding was already consumed")
	}
	ctx, cancel := context.WithTimeout(ctx, runtimeDrainTimeout)
	defer cancel()
	return b.host.apply(ctx, b.desired)
}

func (h *backendHTTP) Owns(listen string, port int) bool {
	return h.OwnsKey(runtimeTargetKey(listen, port))
}

func (h *backendHTTP) OwnsKey(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.listeners[key]
	return state != nil && state.ready.Load() && state.failure.Load() == nil
}

func (h *backendHTTP) apply(ctx context.Context, desired map[string]standaloneTargetConfig) error {
	if err := acquireRuntimeLock(ctx, &h.mu); err != nil {
		return err
	}
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	pending := h.retired[:0]
	for _, state := range h.retired {
		select {
		case <-state.done:
		default:
			pending = append(pending, state)
		}
	}
	h.retired = pending
	next := make(map[string]*runtimeListener, len(desired))
	started := make([]*runtimeListener, 0, len(desired))
	closed := make(map[string]*runtimeListener)
	rollback := func(cause error) error {
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeDrainTimeout)
		defer cancel()
		for _, state := range started {
			h.retired = append(h.retired, state)
			cause = errors.Join(cause, state.server.Close())
		}
		restored := make(map[string]*runtimeListener, len(h.listeners))
		for key, state := range h.listeners {
			restored[key] = state
		}
		var restoreErr error
		for key, state := range closed {
			replacement, err := h.startStandalone(recoveryCtx, state.config)
			if err != nil {
				delete(restored, key)
				restoreErr = errors.Join(restoreErr, fmt.Errorf("restore listener %s: %w", state.config.address, err))
				continue
			}
			restored[key] = replacement
		}
		h.listeners = restored
		return errors.Join(cause, restoreErr)
	}

	keys := make([]string, 0, len(desired))
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		config := desired[key]
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		current := h.listeners[key]
		if current != nil && sameStandaloneConfig(current.config, config) && current.ready.Load() {
			next[key] = current
			continue
		}
		if current != nil {
			continue
		}
		state, err := h.startStandalone(ctx, config)
		if err != nil {
			return rollback(fmt.Errorf("fallback-html cannot publish on %s: %w", config.address, err))
		}
		started = append(started, state)
		next[key] = state
	}

	for key, config := range desired {
		current := h.listeners[key]
		if current == nil || (sameStandaloneConfig(current.config, config) && current.ready.Load()) {
			continue
		}
		closed[key] = current
		h.retired = append(h.retired, current)
		if err := current.server.Close(); err != nil {
			return rollback(fmt.Errorf("replace listener %s: %w", current.config.address, err))
		}
		state, err := h.startStandalone(ctx, config)
		if err != nil {
			return rollback(fmt.Errorf("fallback-html cannot replace listener %s: %w", config.address, err))
		}
		started = append(started, state)
		next[key] = state
	}

	for key, state := range h.listeners {
		if next[key] != state {
			closed[key] = state
			h.retired = append(h.retired, state)
			if err := state.server.Close(); err != nil {
				return rollback(fmt.Errorf("close listener %s: %w", state.config.address, err))
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	h.listeners = next
	return nil
}

func standaloneConfigForTarget(target fallbackdomain.RuntimeTarget, certificateFile, keyFile string) (standaloneTargetConfig, bool, error) {
	if target.Port <= 0 {
		return standaloneTargetConfig{}, false, nil
	}
	listen := strings.TrimSpace(target.Listen)
	if listen == "" {
		listen = "127.0.0.1"
	}
	config := standaloneTargetConfig{
		key:     runtimeTargetKey(listen, target.Port),
		address: net.JoinHostPort(listen, strconv.Itoa(target.Port)),
		tls:     target.TLS,
	}
	if !target.TLS {
		return config, true, nil
	}
	config.certFile, config.keyFile = strings.TrimSpace(certificateFile), strings.TrimSpace(keyFile)
	if config.certFile == "" || config.keyFile == "" {
		return standaloneTargetConfig{}, false, fmt.Errorf("fallback-html cannot publish TLS target %s: panel certificate and key are required", config.address)
	}
	certificate, revision, err := loadRuntimeTLSMaterial(config.certFile, config.keyFile)
	if err != nil {
		return standaloneTargetConfig{}, false, fmt.Errorf("fallback-html TLS target %s is invalid: %w", config.address, err)
	}
	config.certificate = &certificate
	config.tlsRevision = revision
	return config, true, nil
}

func loadRuntimeTLSMaterial(certFile, keyFile string) (tls.Certificate, string, error) {
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	digest := sha256.New()
	for _, der := range certificate.Certificate {
		_, _ = digest.Write(der)
	}
	revision := hex.EncodeToString(digest.Sum(nil))
	return certificate, revision, nil
}

func sameStandaloneConfig(left, right standaloneTargetConfig) bool {
	return left.key == right.key && left.address == right.address && left.tls == right.tls &&
		left.certFile == right.certFile && left.keyFile == right.keyFile && left.tlsRevision == right.tlsRevision
}

func (h *backendHTTP) startStandalone(ctx context.Context, config standaloneTargetConfig) (*runtimeListener, error) {
	var listen net.ListenConfig
	listener, err := listen.Listen(ctx, "tcp", config.address)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Addr: config.address, Handler: h.handler, ReadHeaderTimeout: 5 * time.Second}
	if config.tls {
		server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*config.certificate}, MinVersion: tls.VersionTLS12}
	}
	state := &runtimeListener{config: config, server: server, done: make(chan struct{})}
	state.ready.Store(true)
	go func() {
		defer close(state.done)
		defer state.ready.Store(false)
		var err error
		if config.tls {
			err = server.ServeTLS(listener, "", "")
		} else {
			err = server.Serve(listener)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			state.failure.Store(err)
		}
	}()
	return state, nil
}

func (h *backendHTTP) Drain(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, runtimeDrainTimeout)
	defer cancel()
	if err := acquireRuntimeLock(ctx, &h.mu); err != nil {
		return err
	}
	defer h.mu.Unlock()
	var joined error
	states := append([]*runtimeListener(nil), h.retired...)
	for _, state := range h.listeners {
		states = append(states, state)
	}
	for _, state := range states {
		if err := ctx.Err(); err != nil {
			return errors.Join(joined, err)
		}
		joined = errors.Join(joined, state.server.Shutdown(ctx))
		select {
		case <-state.done:
		case <-ctx.Done():
			joined = errors.Join(joined, ctx.Err())
		}
	}
	if joined == nil {
		h.listeners = map[string]*runtimeListener{}
		h.retired = nil
	}
	return joined
}

func runtimeTLSFiles() (string, string) {
	settings := coreservice.SettingService{}
	certFile, certErr := settings.GetCertFile()
	keyFile, keyErr := settings.GetKeyFile()
	if certErr != nil || keyErr != nil {
		return "", ""
	}
	return strings.TrimSpace(certFile), strings.TrimSpace(keyFile)
}
