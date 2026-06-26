package box

import (
	"errors"
	"fmt"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/taskmonitor"
	C "github.com/sagernet/sing-box/constant"
	F "github.com/sagernet/sing/common/format"
)

func (s *Box) PreStart() error {
	err := s.preStart()
	if err != nil {
		// A partially initialized third-party lifecycle may panic while closing.
		// Preserve the original startup error and keep cleanup best-effort.
		defer func() {
			v := recover()
			if v != nil {
				s.logger.Error(err.Error())
				s.logger.Error("panic on early close: " + fmt.Sprint(v))
			}
		}()
		_ = s.Close()
		return err
	}
	s.logger.Info("sing-box pre-started (", F.Seconds(time.Since(s.createdAt).Seconds()), "s)")
	return nil
}

func (s *Box) Start() error {
	err := s.start()
	if err != nil {
		return err
	}
	s.logger.Info("sing-box started (", F.Seconds(time.Since(s.createdAt).Seconds()), "s)")
	return nil
}

func (s *Box) preStart() error {
	monitor := taskmonitor.New(s.logger, C.StartTimeout)
	monitor.Start("start logger")
	err := s.logFactory.Start()
	monitor.Finish()
	if err != nil {
		return common.NewError(err, "start logger")
	}
	err = adapter.StartNamed(s.logger, adapter.StartStateInitialize, s.internalService) // cache-file clash-api v2ray-api
	if err != nil {
		return err
	}
	err = adapter.Start(s.logger, adapter.StartStateInitialize, s.network, s.dnsTransport, s.dnsRouter, s.connection, s.router, s.outbound, s.inbound, s.endpoint, s.service)
	if err != nil {
		return err
	}
	err = adapter.Start(s.logger, adapter.StartStateStart, s.outbound, s.dnsTransport, s.dnsRouter, s.network, s.connection, s.router)
	if err != nil {
		return err
	}
	return nil
}

func (s *Box) start() error {
	err := s.preStart()
	if err != nil {
		return err
	}
	err = adapter.StartNamed(s.logger, adapter.StartStateStart, s.internalService)
	if err != nil {
		return err
	}
	err = adapter.Start(s.logger, adapter.StartStateStart, s.inbound, s.endpoint, s.service)
	if err != nil {
		return err
	}
	err = adapter.Start(s.logger, adapter.StartStatePostStart, s.outbound, s.network, s.dnsTransport, s.dnsRouter, s.connection, s.router, s.inbound, s.endpoint, s.service)
	if err != nil {
		return err
	}
	err = adapter.StartNamed(s.logger, adapter.StartStatePostStart, s.internalService)
	if err != nil {
		return err
	}
	err = adapter.Start(s.logger, adapter.StartStateStarted, s.network, s.dnsTransport, s.dnsRouter, s.connection, s.router, s.outbound, s.inbound, s.endpoint, s.service)
	if err != nil {
		return err
	}
	err = adapter.StartNamed(s.logger, adapter.StartStateStarted, s.internalService)
	if err != nil {
		return err
	}
	return nil
}

func (s *Box) Close() error {
	select {
	case <-s.done:
		return nil
	default:
		close(s.done)
	}
	var err error
	s.logger.Info("closing sing-box")
	for _, closeItem := range []struct {
		name    string
		service adapter.Lifecycle
	}{
		{"service", s.service},
		{"endpoint", s.endpoint},
		{"inbound", s.inbound},
		{"outbound", s.outbound},
		{"router", s.router},
		{"connection", s.connection},
		{"dns-router", s.dnsRouter},
		{"dns-transport", s.dnsTransport},
		{"network", s.network},
	} {
		if closeItem.service == nil {
			continue
		}
		func() {
			defer func() {
				if v := recover(); v != nil {
					err = errors.Join(err, common.NewError(fmt.Errorf("panic: %v", v), "close "+closeItem.name))
					s.logger.Error("panic closing ", closeItem.name, ": ", v)
				}
			}()
			s.logger.Trace("close ", closeItem.name)
			startTime := time.Now()
			closeErr := closeItem.service.Close()
			if closeErr != nil {
				closeErr = common.NewError(closeErr, "close "+closeItem.name)
			}
			err = errors.Join(err, closeErr)
			s.logger.Trace("close ", closeItem.name, " completed (", F.Seconds(time.Since(startTime).Seconds()), "s)")
		}()
	}
	for _, lifecycleService := range s.internalService {
		if lifecycleService == nil {
			continue
		}
		func() {
			defer func() {
				if v := recover(); v != nil {
					err = errors.Join(err, common.NewError(fmt.Errorf("panic: %v", v), "close "+lifecycleService.Name()))
					s.logger.Error("panic closing ", lifecycleService.Name(), ": ", v)
				}
			}()
			s.logger.Trace("close ", lifecycleService.Name())
			startTime := time.Now()
			closeErr := lifecycleService.Close()
			if closeErr != nil {
				closeErr = common.NewError(closeErr, "close "+lifecycleService.Name())
			}
			err = errors.Join(err, closeErr)
			s.logger.Trace("close ", lifecycleService.Name(), " completed (", F.Seconds(time.Since(startTime).Seconds()), "s)")
		}()
	}
	s.logger.Trace("close logger")
	startTime := time.Now()
	closeErr := s.logFactory.Close()
	if closeErr != nil {
		closeErr = common.NewError(closeErr, "close logger")
	}
	err = errors.Join(err, closeErr)
	s.logger.Trace("close logger completed (", F.Seconds(time.Since(startTime).Seconds()), "s)")
	s.logger.Info("sing-box closed (live time: ", F.Seconds(time.Since(s.createdAt).Seconds()), "s)")
	if s.statsTracker != nil {
		s.statsTracker.Reset()
	}
	if s.connTracker != nil {
		s.connTracker.Reset()
	}
	return err
}
