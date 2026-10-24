//go:build linux

package updatebroker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/systemdexec"
)

const (
	panelService          = "solovey-ui.service"
	brokerService         = "solovey-privileged-broker.service"
	releaseControlTimeout = 10 * time.Second
)

var errReleaseControl = errors.New("fixed release service control is unavailable")

type releaseControlObject interface {
	File() *os.File
	Label() string
	ExecPath(int) string
	Revalidate() error
	Close() error
}

// This port names the installed services itself. No unit, PID, executable path,
// environment or argv is supplied by a Panel request.
type releaseServiceControl struct {
	open func() (releaseControlObject, error)
	run  func(context.Context, releaseControlObject, []string) ([]byte, error)
}

func newReleaseServiceControl() releaseServiceControl {
	return releaseServiceControl{
		open: func() (releaseControlObject, error) { return systemdexec.Open() },
		run:  runReleaseSystemctl,
	}
}

func (control releaseServiceControl) Ready(ctx context.Context) error {
	return control.withObject(ctx, func(object releaseControlObject) error {
		output, err := control.run(ctx, object, []string{"show", "--property=LoadState", "--value", panelService, brokerService})
		if err != nil || !slices.Equal(strings.Fields(string(output)), []string{"loaded", "loaded"}) {
			return errReleaseControl
		}
		return nil
	})
}

func (control releaseServiceControl) RestartPanel(ctx context.Context) error {
	return control.restart(ctx, false)
}

func (control releaseServiceControl) RestartBoth(ctx context.Context) error {
	return control.restart(ctx, true)
}

func (control releaseServiceControl) restart(ctx context.Context, includeBroker bool) error {
	return control.withObject(ctx, func(object releaseControlObject) error {
		units := []string{panelService}
		if includeBroker {
			units = append(units, brokerService)
		}
		// The failed candidate may have exhausted systemd's start limit.
		// Reset only the same fixed services before starting the trusted target.
		if _, err := control.run(ctx, object, append([]string{"reset-failed"}, units...)); err != nil {
			return errReleaseControl
		}
		if _, err := control.run(ctx, object, append([]string{"--no-block", "restart"}, units...)); err != nil {
			return errReleaseControl
		}
		return nil
	})
}

func (control releaseServiceControl) withObject(ctx context.Context, operation func(releaseControlObject) error) error {
	if ctx == nil || ctx.Err() != nil || control.open == nil || control.run == nil {
		return errReleaseControl
	}
	object, err := control.open()
	if err != nil || object == nil {
		return errReleaseControl
	}
	defer func() { _ = object.Close() }()
	if object.File() == nil || object.ExecPath(0) == "" || object.Revalidate() != nil {
		return errReleaseControl
	}
	if operation(object) != nil || object.Revalidate() != nil {
		return errReleaseControl
	}
	return nil
}

func validReleaseControlArgs(args []string) bool {
	for _, fixed := range [][]string{
		{"show", "--property=LoadState", "--value", panelService, brokerService},
		{"reset-failed", panelService}, {"reset-failed", panelService, brokerService},
		{"--no-block", "restart", panelService}, {"--no-block", "restart", panelService, brokerService},
	} {
		if slices.Equal(args, fixed) {
			return true
		}
	}
	return false
}

func runReleaseSystemctl(ctx context.Context, object releaseControlObject, args []string) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || !validReleaseControlArgs(args) || object == nil || object.File() == nil ||
		object.ExecPath(0) == "" || object.Revalidate() != nil {
		return nil, errReleaseControl
	}
	bounded, cancel := context.WithTimeout(ctx, releaseControlTimeout)
	defer cancel()
	command := exec.CommandContext(bounded, object.ExecPath(0), args...)
	command.Args[0] = object.Label()
	command.ExtraFiles = []*os.File{object.File()}
	command.Dir, command.Env = "/", []string{"LANG=C", "LC_ALL=C"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 250 * time.Millisecond
	output := boundedProviderCheckOutput{limit: 16 << 10}
	diagnostics := boundedProviderCheckOutput{limit: 16 << 10}
	command.Stdout, command.Stderr = &output, &diagnostics
	if command.Run() != nil || bounded.Err() != nil || object.Revalidate() != nil {
		return nil, errReleaseControl
	}
	return bytes.Clone(output.Bytes()), nil
}
