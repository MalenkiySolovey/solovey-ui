//go:build !(aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris)

package backendfronting

import "os"

func platformFileIdentityV2(os.FileInfo) (uint64, uint64) { return 0, 0 }
