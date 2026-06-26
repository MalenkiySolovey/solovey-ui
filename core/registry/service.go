package registry

import (
	"github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/service/oomkiller"
	"github.com/sagernet/sing-box/service/resolved"
	"github.com/sagernet/sing-box/service/ssmapi"
)

func ServiceRegistry() *service.Registry {
	registry := service.NewRegistry()

	resolved.RegisterService(registry)
	ssmapi.RegisterService(registry)

	registerDERPService(registry)
	oomkiller.RegisterService(registry)

	return registry
}
