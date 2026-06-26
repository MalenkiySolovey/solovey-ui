package config

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
)

func (a *Handler) Reorder(c *gin.Context, loginUser string) {
	obj := c.Request.FormValue("object")
	data := c.Request.FormValue("data")

	objs, err := a.ConfigService.Reorder(obj, json.RawMessage(data), loginUser)
	if err != nil {
		a.JSONMsg(c, "reorder", err)
		return
	}
	if err := a.LoadReorderData(c, objs, loginUser); err != nil {
		a.JSONMsg(c, obj, err)
	}
}

func (a *Handler) LoadReorderData(c *gin.Context, objs []string, loginUser string) error {
	data := make(map[string]interface{}, 0)
	id := c.Query("id")
	seen := make(map[string]struct{}, len(objs))

	for _, obj := range objs {
		if _, ok := seen[obj]; ok {
			continue
		}
		seen[obj] = struct{}{}

		switch obj {
		case "admins", "users":
			users, err := a.UserService.GetUsers()
			if err != nil {
				return err
			}
			result := make([]gin.H, 0, len(*users))
			for _, user := range *users {
				result = append(result, gin.H{
					"id":        user.Id,
					"sortOrder": user.SortOrder,
					"username":  user.Username,
					"lastLogin": user.LastLogins,
					"isCurrent": user.Username == loginUser,
				})
			}
			data["users"] = result
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
			clients, err := a.ClientService.Get(id)
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
