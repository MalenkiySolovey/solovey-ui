//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ZapretLuaBundlePath is the engine's first inherited descriptor, never a
// filename supplied by Panel. nfqws2 reopens it after dropping its UID.
const ZapretLuaBundlePath = "/proc/self/fd/3"

var errZapretManagedLuaDenied = errors.New("installed Zapret profile does not delegate managed Lua; the Host administrator must enable allowManagedLua")
var errZapretManagedLuaStart = errors.New("unable to initialize the selected Zapret Lua revisions; check the declared functions, dependency order, and source syntax")

// With interception off, nfqws2 loads and validates all Lua initializers
// and function references, but never joins an NFQUEUE.
func checkZapretLuaInit(ctx context.Context, command ZapretEngineCommand) error {
	if command.Lua == "" {
		return nil
	}
	bundle, err := openZapretLuaBundle(command.Lua)
	if err != nil {
		return err
	}
	defer bundle.Close()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	args := make([]string, 0, len(command.Args)+2)
	args = append(args, "--intercept=0", fmt.Sprintf("--uid=%d:%d", zapretEngineUID, zapretEngineUID))
	args = append(args, command.Args...)
	cmd := exec.CommandContext(ctx, zapretEnginePath, args...)
	cmd.Dir = zapretEngineRoot
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	cmd.ExtraFiles = []*os.File{bundle}
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", errZapretManagedLuaStart, ctx.Err())
		}
		return errZapretManagedLuaStart
	}
	return nil
}

func openZapretLuaBundle(source string) (*os.File, error) {
	fd, err := unix.MemfdCreate("solovey-zapret-lua", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "solovey-zapret-lua")
	if _, err = file.WriteString(source); err == nil {
		err = file.Chmod(0o444)
	}
	if err == nil {
		_, err = unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_SEAL|unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE)
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
