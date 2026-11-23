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

const (
	zapretEngineUID = 65534
	zapretStopMode  = "--internal-stop-zapret-engine"
)

// RunZapretEngineStop is the broker's one-shot child mode. The broker invokes
// it with the engine's credentials and a single inherited pidfd.
func RunZapretEngineStop(args []string) (bool, error) {
	if len(args) != 1 || args[0] != zapretStopMode {
		return false, nil
	}
	if os.Getuid() != zapretEngineUID || os.Geteuid() != zapretEngineUID || os.Getegid() != zapretEngineUID {
		return true, errors.New("unexpected Zapret engine stop credentials")
	}
	err := unix.PidfdSendSignal(3, unix.SIGKILL, nil, 0)
	if errors.Is(err, unix.ESRCH) {
		return true, nil
	}
	return true, err
}

type installedZapretEngine struct {
	process *os.Process
	pidfd   *os.File
	done    chan struct{}
	waitErr error // published by closing done
}

func (e *installedZapretEngine) stop() error {
	if e.pidfd != nil {
		defer e.pidfd.Close()
	}
	select {
	case <-e.done:
		return nil
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// The broker can signal the engine before it drops root. After the drop,
	// use the engine's own UID with the existing SETUID/SETGID permissions.
	if err := e.process.Kill(); errors.Is(err, unix.EPERM) && e.pidfd != nil {
		stop := exec.Command("/proc/self/exe", zapretStopMode)
		stop.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: zapretEngineUID, Gid: zapretEngineUID}}
		stop.ExtraFiles = []*os.File{e.pidfd}
		if err := stop.Start(); err != nil {
			return fmt.Errorf("start Zapret engine retirement: %w", err)
		}
		finished := make(chan error, 1)
		go func() { finished <- stop.Wait() }()
		select {
		case err := <-finished:
			if err != nil {
				return fmt.Errorf("signal the Zapret engine via pidfd: %w", err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	} else if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
