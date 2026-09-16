//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"

	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "database-folder" {
		selection, err := openwrt.LoadStorageSelection()
		if err != nil {
			fatal(err)
		}
		fmt.Println(selection.DatabaseFolder())
		return
	}
	if os.Geteuid() != 0 {
		fatal(errors.New("durability proof writer requires root"))
	}
	if len(os.Args) == 2 && os.Args[1] == "prepare-storage" {
		if err := openwrt.PrepareSelectedStorage(); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) != 1 {
		fatal(errors.New("unsupported durability operation"))
	}
	if _, err := openwrt.RefreshDatabaseDurabilityProof(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "solovey-openwrt-durability:", err)
	os.Exit(1)
}
