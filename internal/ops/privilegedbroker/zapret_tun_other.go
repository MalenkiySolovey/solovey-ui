//go:build !linux || minimal

package privilegedbroker

import (
	"context"
	"errors"
	"os"
)

func ReceiveZapretTUN(context.Context, string) (*os.File, error) {
	return nil, errors.New("installed Zapret TUN is unavailable on this build")
}
