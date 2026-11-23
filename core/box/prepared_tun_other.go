//go:build !linux

package box

import (
	"os"

	"github.com/sagernet/sing-tun"
)

func usePreparedTUN(*os.File, tun.Options) (tun.Tun, error) {
	return nil, errPreparedTUN
}
