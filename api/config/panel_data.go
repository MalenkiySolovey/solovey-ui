package config

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

func (a *Handler) LoadData(c *gin.Context) {
	data, err := a.GetData(c)
	if err != nil {
		a.JSONMsg(c, "", err)
		return
	}
	a.JSONObj(c, data, nil)
}

func (a *Handler) GetData(c *gin.Context) (interface{}, error) {
	data := make(map[string]interface{})
	lu := c.Query("lu")
	isUpdated, err := a.ConfigService.CheckChanges(lu)
	if err != nil {
		return "", err
	}
	onlines, err := a.StatsService.GetOnlines()

	sysInfo := a.ServerService.GetSingboxInfo()
	if sysInfo["running"] == false {
		logs := a.ServerService.GetLogs("1", "debug")
		if len(logs) > 0 {
			data["lastLog"] = logs[0]
		}
	}

	if err != nil {
		return "", err
	}
	if isUpdated {
		hostname := a.Hostname(c)
		var loadSettings service.PanelLoadSettings
		var clients, tlsConfigs, inbounds, outbounds, endpoints, services any
		var group errgroup.Group
		group.Go(func() error {
			settings, err := a.SettingService.LoadPanelSettingsForData(hostname)
			loadSettings = settings
			return err
		})
		group.Go(func() error {
			result, err := a.ClientService.GetAll()
			clients = result
			return err
		})
		group.Go(func() error {
			result, err := a.TlsService.GetAll()
			tlsConfigs = result
			return err
		})
		group.Go(func() error {
			result, err := a.InboundService.GetAll()
			inbounds = result
			return err
		})
		group.Go(func() error {
			result, err := a.OutboundService.GetAll()
			outbounds = result
			return err
		})
		group.Go(func() error {
			result, err := a.EndpointService.GetAll()
			endpoints = result
			return err
		})
		group.Go(func() error {
			result, err := a.ServicesService.GetAll()
			services = result
			return err
		})
		if err := group.Wait(); err != nil {
			return "", err
		}
		data["config"] = json.RawMessage(loadSettings.Config)
		data["clients"] = clients
		data["tls"] = tlsConfigs
		data["inbounds"] = inbounds
		data["outbounds"] = outbounds
		data["endpoints"] = endpoints
		data["services"] = services
		data["subURI"] = loadSettings.SubURI
		if loadSettings.SubJsonURI != "" {
			data["subJsonURI"] = loadSettings.SubJsonURI
		}
		if loadSettings.SubClashURI != "" {
			data["subClashURI"] = loadSettings.SubClashURI
		}
		if loadSettings.SubXrayURI != "" {
			data["subXrayURI"] = loadSettings.SubXrayURI
		}
		data["enableTraffic"] = loadSettings.TrafficAge > 0
		data["onlines"] = onlines
	} else {
		data["onlines"] = onlines
	}

	return data, nil
}

func (a *Handler) LoadPartialData(c *gin.Context, objs []string) error {
	data := make(map[string]interface{})
	id := c.Query("id")

	for _, obj := range objs {
		switch obj {
		case "inbounds":
			inbounds, err := a.InboundService.Get(id)
			if err != nil {
				return err
			}
			data[obj] = inbounds
		case "outbounds":
			outbounds, err := a.OutboundService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = outbounds
		case "endpoints":
			endpoints, err := a.EndpointService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = endpoints
		case "services":
			services, err := a.ServicesService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = services
		case "tls":
			tlsConfigs, err := a.TlsService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = tlsConfigs
		case "clients":
			clients, err := a.ClientService.GetWithLocalLinks(id, a.Hostname(c))
			if err != nil {
				return err
			}
			data[obj] = clients
		case "config":
			config, err := a.SettingService.GetConfig()
			if err != nil {
				return err
			}
			data[obj] = json.RawMessage(config)
		case "settings":
			settings, err := a.SettingService.GetAllSetting()
			if err != nil {
				return err
			}
			data[obj] = settings
		}
	}

	a.JSONObj(c, data, nil)
	return nil
}

func (a *Handler) GetSettings(c *gin.Context) {
	data, err := a.SettingService.GetAllSetting()
	if err != nil {
		a.JSONMsg(c, "", err)
		return
	}
	a.JSONObj(c, data, err)
}

func (a *Handler) GetSettingsSchema(c *gin.Context) {
	a.JSONObj(c, a.SettingService.GetSettingSchema(), nil)
}

func (a *Handler) CheckChanges(c *gin.Context) {
	actor := c.Query("a")
	chngKey := c.Query("k")
	count := c.Query("c")
	changes := a.ConfigService.GetChanges(actor, chngKey, count)
	a.JSONObj(c, changes, nil)
}
