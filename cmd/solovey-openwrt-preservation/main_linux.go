//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "solovey OpenWrt preservation:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 || (os.Args[1] != "prepare" && os.Args[1] != "complete-backup" && os.Args[1] != "fail-backup") {
		return errors.New("only fixed prepare, complete-backup, and fail-backup operations are supported")
	}
	if os.Geteuid() == 0 {
		return errors.New("preservation preparation must run as the panel service account")
	}
	profile := openwrt.DefaultProfile()
	if configstorage.GetDBFolderPath() != profile.DatabaseFolder {
		return errors.New("database storage authority differs from the OpenWrt package profile")
	}
	info, err := os.Lstat(configstorage.GetDBPath())
	if err != nil || !info.Mode().IsRegular() {
		return errors.Join(err, errors.New("live database is unavailable for preservation"))
	}
	if os.Args[1] == "complete-backup" || os.Args[1] == "fail-backup" {
		return openwrt.FinalizeSysupgradePreservationBackup(context.Background(), openwrt.ProductionPreservationEnvironment(), os.Args[1] == "complete-backup")
	}
	if installstate.DefaultPath() != openwrt.DefaultInstallRoot+"/components/installed.json" {
		return errors.New("installed owner inventory differs from the OpenWrt package")
	}
	if _, exists, err := installstate.Load(installstate.DefaultPath()); err != nil || !exists {
		return errors.Join(err, errors.New("installed owner inventory is unavailable for preservation"))
	}
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		return err
	}
	defer dbsqlite.Close()
	metadata, err := openwrt.PrepareSysupgradePreservation(context.Background(), profile, openwrt.ProductionPreservationEnvironment())
	if err != nil {
		return err
	}
	// Native `sysupgrade -b -` reserves stdout for the gzip archive stream.
	// Keep the successful preparation identity on the diagnostic channel.
	fmt.Fprintln(os.Stderr, metadata.Identity)
	return nil
}
