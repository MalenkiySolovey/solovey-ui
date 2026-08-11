//go:build windows

package helper

import "os"

func platformFileIdentity(os.FileInfo) (uint64, uint64) { return 0, 0 }
func platformRootOwned(os.FileInfo) bool                { return true }
