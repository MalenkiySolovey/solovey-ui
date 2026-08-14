package settingscmd

import (
	"errors"
	"fmt"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func Reset() error {
	err := dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		return err
	}

	settingService := service.SettingService{}
	err = settingService.ResetSettings()
	if err != nil {
		return fmt.Errorf("reset setting failed: %w", err)
	} else {
		fmt.Println("reset setting success")
	}
	return nil
}

func ClearWebDomain() error {
	err := dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		return err
	}

	settingService := service.SettingService{}
	if err := settingService.ClearWebDomainAndAddress(); err != nil {
		return fmt.Errorf("clear panel domain and address failed: %w", err)
	}
	fmt.Println("clear panel domain and address success")
	return Show()
}

func Update(port int, path string, subPort int, subPath string) error {
	err := dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		return err
	}

	settingService := service.SettingService{}
	var result error

	if port > 0 {
		err := settingService.SetPort(port)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("set port failed: %w", err))
		} else {
			fmt.Println("set port success")
		}
	}
	if path != "" {
		err := settingService.SetWebPath(path)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("set path failed: %w", err))
		} else {
			fmt.Println("set path success")
		}
	}
	if subPort > 0 {
		err := settingService.SetSubPort(subPort)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("set sub port failed: %w", err))
		} else {
			fmt.Println("set sub port success")
		}
	}
	if subPath != "" {
		err := settingService.SetSubPath(subPath)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("set sub path failed: %w", err))
		} else {
			fmt.Println("set sub path success")
		}
	}
	return result
}

func Show() error {
	err := dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		return err
	}
	settingService := service.SettingService{}
	allSetting, err := settingService.GetAllSetting()
	if err != nil {
		return fmt.Errorf("get current settings failed: %w", err)
	}
	if allSetting == nil {
		return fmt.Errorf("get current settings failed: settings are unavailable")
	}
	fmt.Println("Current panel settings:")
	fmt.Println("\tPanel port:\t", (*allSetting)["webPort"])
	fmt.Println("\tPanel path:\t", (*allSetting)["webPath"])
	if (*allSetting)["webListen"] != "" {
		fmt.Println("\tPanel IP:\t", (*allSetting)["webListen"])
	}
	if (*allSetting)["webDomain"] != "" {
		fmt.Println("\tPanel Domain:\t", (*allSetting)["webDomain"])
	}
	if (*allSetting)["webURI"] != "" {
		fmt.Println("\tPanel URI:\t", (*allSetting)["webURI"])
	}
	fmt.Println()
	fmt.Println("Current subscription settings:")
	fmt.Println("\tSub port:\t", (*allSetting)["subPort"])
	fmt.Println("\tSub path:\t", (*allSetting)["subPath"])
	if (*allSetting)["subListen"] != "" {
		fmt.Println("\tSub IP:\t", (*allSetting)["subListen"])
	}
	if (*allSetting)["subDomain"] != "" {
		fmt.Println("\tSub Domain:\t", (*allSetting)["subDomain"])
	}
	if (*allSetting)["subURI"] != "" {
		fmt.Println("\tSub URI:\t", (*allSetting)["subURI"])
	}
	return nil
}
