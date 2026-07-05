package cmd

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	admincmd "github.com/MalenkiySolovey/solovey-ui/cmd/internal/admin"
	backupcmd "github.com/MalenkiySolovey/solovey-ui/cmd/internal/backup"
	componentdoctorcmd "github.com/MalenkiySolovey/solovey-ui/cmd/internal/componentdoctor"
	ipcertcmd "github.com/MalenkiySolovey/solovey-ui/cmd/internal/ipcert"
	settingscmd "github.com/MalenkiySolovey/solovey-ui/cmd/internal/settings"
	optionalcmd "github.com/MalenkiySolovey/solovey-ui/cmd/optional"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
)

func ParseCmd() {
	var showVersion bool
	flag.BoolVar(&showVersion, "v", false, "show version")

	adminCmd := flag.NewFlagSet("admin", flag.ExitOnError)
	settingCmd := flag.NewFlagSet("setting", flag.ExitOnError)
	migrateCmd := flag.NewFlagSet("migrate", flag.ExitOnError)

	var username string
	var password string
	var port int
	var path string
	var subPort int
	var subPath string
	var settingReset bool
	var settingShow bool
	var settingClearDomain bool
	var adminReset bool
	var adminShow bool
	var repairFKOrphans bool
	settingCmd.BoolVar(&settingReset, "reset", false, "reset all settings")
	settingCmd.BoolVar(&settingShow, "show", false, "show current settings")
	settingCmd.BoolVar(&settingClearDomain, "clearDomain", false, "clear panel domain, listen address and web URI")
	settingCmd.IntVar(&port, "port", 0, "set panel port")
	settingCmd.StringVar(&path, "path", "", "set panel path")
	settingCmd.IntVar(&subPort, "subPort", 0, "set sub port")
	settingCmd.StringVar(&subPath, "subPath", "", "set sub path")
	migrateCmd.BoolVar(&repairFKOrphans, "repair-fk-orphans", false, "delete safe foreign-key orphans during migration")

	adminCmd.BoolVar(&adminShow, "show", false, "show first admin credentials")
	adminCmd.BoolVar(&adminReset, "reset", false, "reset first admin credentials")
	adminCmd.StringVar(&username, "username", "", "set login username")
	adminCmd.StringVar(&password, "password", "", "set login password")

	oldUsage := flag.Usage
	flag.Usage = func() {
		oldUsage()
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("    admin          set/reset/show first admin credentials")
		fmt.Println("    backup         export a Solovey UI database backup")
		fmt.Println("    doctor         run local diagnostics")
		for _, line := range optionalcmd.UsageLines() {
			fmt.Println(line)
		}
		fmt.Println("    ip-cert        issue/renew/status/disable an IP-address TLS certificate")
		fmt.Println("    uri            Show panel URI")
		fmt.Println("    migrate        migrate form older version")
		fmt.Println("    setting        set/reset/clear/show settings")
		fmt.Println()
		adminCmd.Usage()
		fmt.Println()
		settingCmd.Usage()
		fmt.Println()
		migrateCmd.Usage()
	}

	flag.Parse()
	if showVersion {
		fmt.Println("Solovey UI Panel\t", configidentity.GetVersion())
		info, ok := debug.ReadBuildInfo()
		if ok {
			for _, dep := range info.Deps {
				if dep.Path == "github.com/sagernet/sing-box" {
					fmt.Println("Sing-Box\t", dep.Version)
					break
				}
			}
		}
		return
	}
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		return
	}

	if exitCode, handled := optionalcmd.Run(args[0], args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv); handled {
		os.Exit(exitCode)
	}

	switch args[0] {
	case "admin":
		err := adminCmd.Parse(args[1:])
		if err != nil {
			fmt.Println(err)
			return
		}
		switch {
		case adminShow:
			admincmd.Show()
		case adminReset:
			admincmd.Reset()
		default:
			admincmd.Update(username, password)
			admincmd.Show()
		}

	case "backup":
		os.Exit(backupcmd.Run(args[1:], os.Stdout, os.Stderr))

	case "uri":
		settingscmd.PanelURI()

	case "migrate":
		if err := migrateCmd.Parse(args[1:]); err != nil {
			fmt.Println(err)
			return
		}
		if err := migration.MigrateDbWithOptions(migration.Options{RepairForeignKeyOrphans: repairFKOrphans}); err != nil {
			fmt.Println("migrate failed:", err)
			os.Exit(1)
		}

	case "ip-cert":
		os.Exit(ipcertcmd.Run(args[1:]))

	case "doctor":
		os.Exit(componentdoctorcmd.Run(args[1:], os.Stdout))

	case "setting":
		err := settingCmd.Parse(args[1:])
		if err != nil {
			fmt.Println(err)
			return
		}
		switch {
		case settingShow:
			settingscmd.Show()
		case settingReset:
			settingscmd.Reset()
		case settingClearDomain:
			settingscmd.ClearWebDomain()
		default:
			settingscmd.Update(port, path, subPort, subPath)
			settingscmd.Show()
		}
	default:
		fmt.Println("Invalid subcommands")
		flag.Usage()
	}
}
