package ipcertcmd

import (
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

// newIpCertService opens the database and returns an IP-certificate service with
// no live runtime; the CLI never restarts the panel itself.
func newIpCertService() (*service.IpCertificateService, error) {
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		return nil, err
	}
	return &service.IpCertificateService{Settings: &service.SettingService{}}, nil
}
