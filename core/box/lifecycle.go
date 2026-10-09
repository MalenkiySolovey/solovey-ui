package box

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/sagernet/sing-box/adapter"
	boxCertificate "github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxService "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	F "github.com/sagernet/sing/common/format"
)

func (s *Box) PreStart() error {
	if err := s.preStart(); err != nil {
		return errors.Join(err, s.Close())
	}
	s.logger.Info("sing-box pre-started (", F.Seconds(time.Since(s.createdAt).Seconds()), "s)")
	return nil
}

func (s *Box) Start() error {
	if err := s.start(); err != nil {
		return errors.Join(err, s.Close())
	}
	s.logger.Info("sing-box started (", F.Seconds(time.Since(s.createdAt).Seconds()), "s)")
	return nil
}

// Initialize transfers constructed child cleanup to the manager, even when its
// first stage fails. Before that boundary Box owns the constructed children.
func (s *Box) startManagers(stage adapter.StartStage, owners ...adapter.Lifecycle) error {
	for _, owner := range owners {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if stage == adapter.StartStateInitialize {
			s.initialized[owner] = true
		}
		if err := adapter.Start(s.ctx, s.logger, stage, owner); err != nil {
			return err
		}
	}
	return nil
}

func (s *Box) preStart() error {
	if err := s.logFactory.Start(); err != nil {
		return common.NewError(err, "start logger")
	}
	if err := adapter.StartNamed(s.ctx, s.logger, adapter.StartStateInitialize, s.internalService); err != nil {
		return err
	}
	if err := s.startManagers(adapter.StartStateInitialize, s.network, s.dnsTransport, s.dnsRouter, s.connection, s.router, s.outbound, s.inbound, s.endpoint, s.service, s.certificateProvider); err != nil {
		return err
	}
	if err := s.startManagers(adapter.StartStateStart, s.outbound, s.dnsTransport, s.network, s.connection); err != nil {
		return err
	}
	if err := adapter.Start(s.ctx, s.logger, adapter.StartStateStart, s.httpClient); err != nil {
		return err
	}
	return s.startManagers(adapter.StartStateStart, s.router, s.dnsRouter)
}

func (s *Box) start() error {
	if err := s.preStart(); err != nil {
		return err
	}
	if err := adapter.StartNamed(s.ctx, s.logger, adapter.StartStateStart, s.internalService); err != nil {
		return err
	}
	// Provider startup precedes inbound TLS startup, which resolves references.
	if err := s.startManagers(adapter.StartStateStart, s.endpoint, s.certificateProvider, s.inbound, s.service); err != nil {
		return err
	}
	if err := s.startManagers(adapter.StartStatePostStart, s.outbound, s.network, s.dnsTransport, s.dnsRouter, s.connection, s.router, s.endpoint, s.certificateProvider, s.inbound, s.service); err != nil {
		return err
	}
	if err := adapter.StartNamed(s.ctx, s.logger, adapter.StartStatePostStart, s.internalService); err != nil {
		return err
	}
	if err := s.startManagers(adapter.StartStateStarted, s.network, s.dnsTransport, s.dnsRouter, s.connection, s.router, s.outbound, s.endpoint, s.certificateProvider, s.inbound, s.service); err != nil {
		return err
	}
	return adapter.StartNamed(s.ctx, s.logger, adapter.StartStateStarted, s.internalService)
}

func (s *Box) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.cancel != nil {
			s.cancel()
		}
		s.closeErr = s.close()
	})
	return s.closeErr
}

func absent(owner any) bool {
	return owner == nil || reflect.ValueOf(owner).Kind() == reflect.Ptr && reflect.ValueOf(owner).IsNil()
}

func (s *Box) close() error {
	if s.inboundIdentity != nil {
		s.inboundIdentity.Close()
	}
	var result error
	closeOne := func(name string, owner io.Closer) {
		if absent(owner) {
			return
		}
		defer func() {
			if v := recover(); v != nil {
				result = errors.Join(result, fmt.Errorf("close %s: panic: %v", name, v))
			}
		}()
		if err := owner.Close(); err != nil {
			result = errors.Join(result, common.NewError(err, "close "+name))
		}
	}
	if s.connTracker != nil {
		s.connTracker.Close()
	}
	// Quiesce generation-owned log callbacks before attached API subscribers
	// close. The pinned Factory has an attachment slot, no detach RPC.
	if s.logFactory != nil {
		s.logFactory.AttachPlatformWriter(nil)
	}
	for _, item := range []struct {
		name  string
		owner adapter.Lifecycle
	}{
		{"service", s.service}, {"inbound", s.inbound}, {"certificate-provider", s.certificateProvider},
		{"endpoint", s.endpoint}, {"outbound", s.outbound}, {"router", s.router},
		{"connection", s.connection}, {"dns-router", s.dnsRouter},
		{"dns-transport", s.dnsTransport}, {"network", s.network},
	} {
		if absent(item.owner) {
			continue
		}
		if !s.initialized[item.owner] {
			for _, child := range constructedChildren(item.owner) {
				closeOne(item.name+" child", child)
			}
		}
		closeOne(item.name, item.owner)
	}
	closeOne("http-client", s.httpClient)
	for _, owner := range s.internalService {
		if !absent(owner) {
			closeOne(owner.Name(), owner)
		}
	}
	closeOne("logger", s.logFactory)
	if s.statsTracker != nil {
		s.statsTracker.Reset()
	}
	return result
}

func constructedChildren(owner adapter.Lifecycle) []io.Closer {
	var children []io.Closer
	switch manager := owner.(type) {
	case *endpoint.Manager:
		for _, child := range manager.Endpoints() {
			children = append(children, child)
		}
	case *inbound.Manager:
		for _, child := range manager.Inbounds() {
			children = append(children, child)
		}
	case *outbound.Manager:
		for _, child := range manager.Outbounds() {
			if closer, ok := child.(io.Closer); ok {
				children = append(children, closer)
			}
		}
	case *boxService.Manager:
		for _, child := range manager.Services() {
			children = append(children, child)
		}
	case *boxCertificate.Manager:
		for _, child := range manager.CertificateProviders() {
			children = append(children, child)
		}
	case *dns.TransportManager:
		for _, child := range manager.Transports() {
			children = append(children, child)
		}
	}
	return children
}
