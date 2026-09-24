//go:build windows

package helper

import "os"

func platformRootOwned(os.FileInfo) bool { return true }
