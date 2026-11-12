//go:build !minimal

package service

// Product composition keeps private settings and inventory implementations here.
// The feature consumes HostFacts through NewWithHost.
import (
	"context"
	"errors"
	"strings"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

type backendHostFacts struct {
	snapshot func(context.Context) hostresources.ResourceSnapshot
}

func BackendHostFacts() HostFacts { return backendHostFacts{} }

func New(db *gorm.DB, runtime *Runtime) *Service {
	return NewWithHost(db, runtime, BackendHostFacts())
}

func (backendHostFacts) CurrentWebTarget(ctx context.Context) (TargetView, error) {
	if err := ctx.Err(); err != nil {
		return TargetView{}, err
	}
	settings := coreservice.SettingService{}
	listen, err := settings.GetListen()
	if err != nil {
		return TargetView{}, err
	}
	port, err := settings.GetPort()
	if err != nil {
		return TargetView{}, err
	}
	host, err := settings.GetWebDomain()
	if err != nil {
		return TargetView{}, err
	}
	certFile, err := settings.GetCertFile()
	if err != nil {
		return TargetView{}, err
	}
	keyFile, err := settings.GetKeyFile()
	if err != nil {
		return TargetView{}, err
	}
	return TargetView{
		Kind:     "web-current",
		Host:     strings.TrimSpace(host),
		Listen:   strings.TrimSpace(listen),
		Port:     port,
		RootPath: "/",
		Runtime:  "gin",
		TLS:      certFile != "" && keyFile != "",
		Status:   "available",
		Reason:   "uses the current managed panel web listener",
		Current:  true,
	}, nil
}

func (b backendHostFacts) InboundPorts(ctx context.Context) ([]PortCandidate, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	snapshotter := b.snapshot
	if snapshotter == nil {
		snapshotter = hostresources.Snapshot
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot := snapshotter(ctx)
	if len(snapshot.Errors) > 0 {
		return nil, errors.New("host resource inventory is unavailable")
	}
	out := make([]PortCandidate, 0)
	seen := map[string]bool{}
	for _, inbound := range snapshot.Resources {
		if inbound.Owner != "core" || inbound.Kind != "inbound" || inbound.Port <= 0 {
			continue
		}
		key := portCandidateKey("inbound", inbound.Listen, inbound.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		tag := strings.TrimSpace(inbound.InboundTag)
		if tag == "" {
			tag = strings.TrimSpace(inbound.Name)
		}
		if tag == "" {
			tag = inbound.ID
		}
		out = append(out, PortCandidate{
			Kind:    "inbound",
			Listen:  inbound.Listen,
			Port:    inbound.Port,
			Runtime: "sing-box",
			Status:  "blocked-inbound",
			Reason:  "sing-box inbound " + tag + " already uses this port",
		})
	}
	return out, nil
}

func (backendHostFacts) ReservedPaths(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	settings := coreservice.SettingService{}
	web, err := settings.GetWebPath()
	if err != nil {
		return nil, err
	}
	sub, err := settings.GetSubPath()
	if err != nil {
		return nil, err
	}
	return []string{web, sub}, nil
}
