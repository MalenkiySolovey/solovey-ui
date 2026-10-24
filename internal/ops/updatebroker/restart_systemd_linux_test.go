//go:build linux

package updatebroker

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"
)

type restartControlFixture struct {
	file      *os.File
	closed    int
	checks    int
	failCheck int
}

func (object *restartControlFixture) File() *os.File { return object.file }
func (*restartControlFixture) Label() string         { return "fixed-systemctl-fixture" }
func (*restartControlFixture) ExecPath(int) string   { return "/proc/self/fd/3" }
func (object *restartControlFixture) Close() error   { object.closed++; return nil }
func (object *restartControlFixture) Revalidate() error {
	object.checks++
	if object.failCheck == object.checks {
		return errors.New("changed object")
	}
	return nil
}

func TestReleaseServiceControlUsesOnlyFixedUnitsAndClosesAuthority(t *testing.T) {
	for _, action := range []string{"ready", "panel", "both"} {
		t.Run(action, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "control")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			object := &restartControlFixture{file: file}
			var calls [][]string
			control := releaseServiceControl{open: func() (releaseControlObject, error) { return object, nil },
				run: func(_ context.Context, actual releaseControlObject, args []string) ([]byte, error) {
					if actual != object || !validReleaseControlArgs(args) {
						t.Fatal("lost fixed executable or operation")
					}
					calls = append(calls, slices.Clone(args))
					return []byte("loaded\n\nloaded\n"), nil
				}}
			var expected [][]string
			switch action {
			case "ready":
				err = control.Ready(t.Context())
				expected = [][]string{{"show", "--property=LoadState", "--value", panelService, brokerService}}
			case "panel":
				err = control.RestartPanel(t.Context())
				expected = [][]string{{"reset-failed", panelService}, {"--no-block", "restart", panelService}}
			case "both":
				err = control.RestartBoth(t.Context())
				expected = [][]string{{"reset-failed", panelService, brokerService}, {"--no-block", "restart", panelService, brokerService}}
			}
			if err != nil || !reflect.DeepEqual(calls, expected) || object.closed != 1 || object.checks != 2 {
				t.Fatal("service control lost unit scope, order or executable lifetime", calls, err)
			}
		})
	}
}

func TestReleaseServiceControlFencesFailuresAndCancelledWork(t *testing.T) {
	for _, failure := range []string{"cancelled", "open", "revalidate-before", "revalidate-after", "reset"} {
		t.Run(failure, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "control")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			object := &restartControlFixture{file: file}
			if failure == "revalidate-before" {
				object.failCheck = 1
			}
			if failure == "revalidate-after" {
				object.failCheck = 2
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if failure == "cancelled" {
				cancel()
			}
			opened, calls := 0, 0
			control := releaseServiceControl{open: func() (releaseControlObject, error) {
				opened++
				if failure == "open" {
					return nil, errors.New("unavailable")
				}
				return object, nil
			}, run: func(context.Context, releaseControlObject, []string) ([]byte, error) {
				calls++
				if failure == "reset" {
					return nil, errors.New("failed reset")
				}
				return nil, nil
			}}
			if control.RestartPanel(ctx) == nil {
				t.Fatal("failure was promoted to successful restart")
			}
			if failure == "cancelled" && (opened != 0 || calls != 0) ||
				failure == "open" && (calls != 0 || object.closed != 0) ||
				failure == "revalidate-before" && calls != 0 || failure == "reset" && calls != 1 ||
				failure != "cancelled" && failure != "open" && object.closed != 1 {
				t.Fatal("failure escaped its executable or mutation boundary", opened, calls, object.closed)
			}
		})
	}
}

func TestReleaseServiceControlRejectsUnknownOrAmbiguousReadiness(t *testing.T) {
	for _, output := range []string{"", "loaded", "loaded\nnot-found", "loaded\nloaded\nloaded", "masked\nloaded"} {
		t.Run(output, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "control")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			object := &restartControlFixture{file: file}
			control := releaseServiceControl{open: func() (releaseControlObject, error) { return object, nil },
				run: func(_ context.Context, _ releaseControlObject, args []string) ([]byte, error) {
					if len(args) == 0 || args[0] != "show" {
						t.Fatal("readiness mutated a service")
					}
					return []byte(output), nil
				}}
			if control.Ready(t.Context()) == nil || object.closed != 1 {
				t.Fatal("unknown service readiness was accepted")
			}
		})
	}
}

func TestReleaseServiceControlRejectsOtherCommandsAndUnitInjection(t *testing.T) {
	for _, args := range [][]string{{"restart", panelService}, {"--no-block", "restart", "ssh.service"},
		{"--no-block", "restart", panelService, "--root=/tmp"}, {"reset-failed"}, {"show", "--all"}} {
		if validReleaseControlArgs(args) {
			t.Fatal("non-owner command was admitted", args)
		}
		if _, err := runReleaseSystemctl(t.Context(), nil, args); err == nil {
			t.Fatal("invalid command reached execution")
		}
	}
}

// The isolated systemd runner creates the two fixed units only after proving
// they are absent. This helper executes the production port under its hardened
// capability set; it is never an application command or RPC handler.
func TestReleaseServiceControlActualFixedUnits(t *testing.T) {
	action := os.Getenv("SOLOVEY_R7_SERVICE_CONTROL_FIXTURE")
	if action == "" {
		t.Skip("isolated real-systemd fixture is required")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root fixture principal is required")
	}
	control := newReleaseServiceControl()
	if err := control.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	var err error
	switch action {
	case "panel":
		err = control.RestartPanel(t.Context())
	case "both":
		err = control.RestartBoth(t.Context())
	default:
		t.Fatal("unknown isolated fixture action")
	}
	if err != nil {
		t.Fatal(err)
	}
}
