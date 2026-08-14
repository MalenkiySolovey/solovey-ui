package admincmd

import (
	"fmt"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func Reset() error {
	err := dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		return err
	}

	// Generate a random password instead of the well-known admin/admin so a reset
	// never leaves the panel on default credentials. Print it once for the
	// operator (it is stored only as a bcrypt hash).
	password, err := common.SecureRandom(16)
	if err != nil {
		return fmt.Errorf("generate admin password: %w", err)
	}
	userService := service.UserService{}
	if err := userService.UpdateFirstUser("admin", password); err != nil {
		return fmt.Errorf("reset admin credentials failed: %w", err)
	}
	fmt.Println("reset admin credentials success")
	fmt.Println("\tUsername:\tadmin")
	fmt.Printf("\tPassword:\t%s\n", password)
	fmt.Println("Save this password now; it cannot be recovered later.")
	return nil
}

func Update(username string, password string) error {
	err := dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		return err
	}

	if username != "" || password != "" {
		userService := service.UserService{}
		err := userService.UpdateFirstUser(username, password)
		if err != nil {
			return fmt.Errorf("reset admin credentials failed: %w", err)
		} else {
			fmt.Println("reset admin credentials success")
		}
	}
	return nil
}

func Show() error {
	err := dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		return err
	}
	userService := service.UserService{}
	userModel, err := userService.GetFirstUser()
	if err != nil {
		return fmt.Errorf("get current user info failed: %w", err)
	}
	if userModel == nil {
		return fmt.Errorf("get current user info failed: user is unavailable")
	}
	username := userModel.Username
	if username == "" || userModel.Password == "" {
		fmt.Println("current username or password is empty")
	}
	fmt.Println("First admin credentials:")
	fmt.Println("\tUsername:\t", username)
	fmt.Println("\tPassword is hashed; use 'solovey-ui admin -reset' or 'solovey-ui admin -username/-password' to set a new one")
	return nil
}
