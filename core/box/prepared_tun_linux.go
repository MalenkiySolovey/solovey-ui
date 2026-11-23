package box

import (
	"errors"
	"os"

	"github.com/sagernet/sing-tun"
	"golang.org/x/sys/unix"
)

func usePreparedTUN(file *os.File, options tun.Options) (tun.Tun, error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return nil, err
	}
	fd := -1
	var opErr error
	err = raw.Control(func(value uintptr) {
		var request unix.Ifreq
		opErr = unix.IoctlIfreq(int(value), unix.TUNGETIFF, &request)
		// The provider expects an attached, nonpersistent, single-queue
		// layer-3 interface without packet or virtio headers.
		const relevant = unix.IFF_TUN | unix.IFF_TAP | unix.IFF_NO_PI | unix.IFF_VNET_HDR | unix.IFF_MULTI_QUEUE | unix.IFF_PERSIST
		if opErr != nil || request.Name() != options.Name || request.Uint16()&relevant != unix.IFF_TUN|unix.IFF_NO_PI {
			opErr = errors.Join(errPreparedTUN, opErr)
			return
		}
		fd, opErr = unix.FcntlInt(value, unix.F_DUPFD_CLOEXEC, 3)
	})
	if err != nil || opErr != nil {
		return nil, errors.Join(err, opErr)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	options.FileDescriptor = fd
	options.EXP_ExternalConfiguration = true // Provider must not change addresses, DNS or routes on close.
	device, err := tun.New(options)
	if err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	return device, nil
}
