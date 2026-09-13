//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"

	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
)

func main() {
	if len(os.Args) != 1 || os.Geteuid() != 0 {
		fatal(errors.New("durability proof writer requires root and accepts no arguments"))
	}
	if _, err := openwrt.RefreshDatabaseDurabilityProof(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "solovey-openwrt-durability:", err)
	os.Exit(1)
}
