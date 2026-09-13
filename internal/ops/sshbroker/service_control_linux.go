//go:build linux

package sshbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/procdexec"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/systemdexec"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

type sshServiceControlObservation struct {
	ID       string
	Evidence []byte
	Revision string
	// State is an owner-local classification of the native supervisor
	// projection.  It is intentionally not a generic process state model.
	State string
}

var (
	errOpenWrtInitScriptAbsent   = errors.New("OpenWrt init script is absent")
	errOpenWrtInitScriptRejected = errors.New("OpenWrt init script is not an accepted platform authority")
	errUbusUnavailable           = errors.New("OpenWrt ubus executable is unavailable")
	errProcdUnavailable          = errors.New("OpenWrt procd control plane is unavailable")
	errServiceNotRegistered      = errors.New("OpenWrt service is not registered")
	errServiceRegisteredStopped  = errors.New("OpenWrt service is registered but stopped")
	errControlOperationRejected  = errors.New("OpenWrt service control operation was rejected")
)

type procdServiceState string

const (
	procdServiceNotRegistered     procdServiceState = "SERVICE_NOT_REGISTERED"
	procdServiceRegisteredStopped procdServiceState = "SERVICE_REGISTERED_STOPPED"
	procdServiceRunning           procdServiceState = "SERVICE_RUNNING"
)

// sshServiceControlAdapter owns supervisor evidence and lifecycle only. SSH
// policy/configuration implementations receive this capability; they do not
// own systemd or procd commands.
type sshServiceControlAdapter interface {
	Kind() ServiceControl
	Observe(context.Context) (sshServiceControlObservation, error)
	Reload(context.Context) error
}

type systemdSSHServiceControl struct {
	systemctl *systemdexec.Object
	units     []string
}

type procdSSHServiceControl struct {
	init    *executableobject.Object
	ubus    *procdexec.Object
	service string
}

type sshExecutableObject interface {
	Label() string
	File() *os.File
	ExecPath(int) string
	Revalidate() error
}

func runSSHCapabilityCommand(ctx context.Context, object sshExecutableObject, args ...string) ([]byte, error) {
	return runSSHCapabilityInputCommand(ctx, object, nil, args...)
}

func runSSHCapabilityInputCommand(ctx context.Context, object sshExecutableObject, stdin []byte, args ...string) ([]byte, error) {
	if object == nil || object.File() == nil || object.ExecPath(0) == "" || object.Revalidate() != nil {
		return nil, errors.New("fixed SSH executable object is unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, object.ExecPath(0), args...)
	command.Args[0] = object.Label()
	command.ExtraFiles = []*os.File{object.File()}
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	var output boundedBuffer
	command.Stdout, command.Stderr = &output, &output
	err := command.Run()
	if output.truncated || bounded.Err() != nil {
		return output.data.Bytes(), errors.New("fixed SSH capability command exceeded its bound")
	}
	return output.data.Bytes(), err
}

func newSystemdSSHServiceControl(units ...string) (sshServiceControlAdapter, error) {
	systemctl, err := systemdexec.Open()
	if err != nil {
		return nil, err
	}
	return systemdSSHServiceControl{systemctl: systemctl, units: append([]string(nil), units...)}, nil
}

func newProcdSSHServiceControl(service string) (sshServiceControlAdapter, error) {
	if !safeToken(service, 64) {
		return nil, errors.New("procd SSH service identity is invalid")
	}
	init, err := openOpenWrtInitScript(service)
	if err != nil {
		return nil, err
	}
	ubus, err := procdexec.Open()
	if err != nil {
		_ = init.Close()
		return nil, fmt.Errorf("%w: %v", errUbusUnavailable, err)
	}
	return procdSSHServiceControl{init: init, ubus: ubus, service: service}, nil
}

// openOpenWrtInitScript binds the exact OpenWrt service-control object that
// Solovey will execute for reload.  OpenWrt owns /etc and /etc/init.d; the
// generic Solovey whole-ancestry policy is therefore not applicable here.
// The object and its immediate service directory remain constrained so an
// untrusted principal cannot substitute the control authority.
func openOpenWrtInitScript(service string) (*executableobject.Object, error) {
	if !safeToken(service, 64) {
		return nil, errors.New("OpenWrt init-script service identity is invalid")
	}
	return openOpenWrtInitScriptAt(filepath.Join("/etc/init.d", service))
}

func openOpenWrtInitScriptAt(path string) (*executableobject.Object, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || !safeToken(filepath.Base(path), 64) {
		return nil, fmt.Errorf("%w: path is invalid", errOpenWrtInitScriptRejected)
	}
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %v", errOpenWrtInitScriptAbsent, err)
		}
		return nil, fmt.Errorf("%w: %v", errOpenWrtInitScriptRejected, err)
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %v", errOpenWrtInitScriptAbsent, err)
		}
		return nil, fmt.Errorf("%w: %v", errOpenWrtInitScriptRejected, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ok || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%w: immediate init.d directory is not platform-owned", errOpenWrtInitScriptRejected)
	}
	object, err := executableobject.Open(path, executableobject.Policy{
		MaxBytes: 1 << 20, RequireRegular: true, RequireExecutable: true,
		RequireRootOwner: true, ForbiddenMode: 0o022,
		RequireTrustedAncestry: false,
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %v", errOpenWrtInitScriptAbsent, err)
		}
		return nil, fmt.Errorf("%w: %v", errOpenWrtInitScriptRejected, err)
	}
	return object, nil
}

// procdUbusCandidates is the fixed OpenWrt capability set. OpenWrt 25.12
// installs ubus at /bin/ubus; /sbin/ubus remains an explicit legacy-compatible
// candidate rather than a PATH lookup or distro-specific discovery rule.
func procdUbusCandidates() []string {
	return []string{procdexec.PrimaryPath, procdexec.LegacyPath}
}

func (systemdSSHServiceControl) Kind() ServiceControl { return ServiceControlSystemd }
func (c systemdSSHServiceControl) Observe(ctx context.Context) (sshServiceControlObservation, error) {
	for _, unit := range c.units {
		active, err := runSSHCapabilityCommand(ctx, c.systemctl, "is-active", unit)
		if err != nil || strings.TrimSpace(string(active)) != "active" {
			continue
		}
		properties, err := runSSHCapabilityCommand(ctx, c.systemctl, "show", unit, "--property=Id,LoadState,ActiveState,SubState,MainPID,ControlGroup,FragmentPath,ExecMainStartTimestampMonotonic")
		if err != nil || !bytes.Contains(properties, []byte("LoadState=loaded")) || !bytes.Contains(properties, []byte("ActiveState=active")) {
			continue
		}
		return sshServiceControlObservation{ID: unit, Evidence: properties, Revision: domain.Revision(string(properties)), State: "SERVICE_RUNNING"}, nil
	}
	return sshServiceControlObservation{}, errors.New("selected SSH systemd service is not active")
}
func (c systemdSSHServiceControl) Reload(ctx context.Context) error {
	observation, err := c.Observe(ctx)
	if err != nil {
		return err
	}
	_, err = runSSHCapabilityCommand(ctx, c.systemctl, "reload", observation.ID)
	return err
}

func (procdSSHServiceControl) Kind() ServiceControl { return ServiceControlProcd }
func (c procdSSHServiceControl) Observe(ctx context.Context) (sshServiceControlObservation, error) {
	raw, err := runSSHCapabilityCommand(ctx, c.ubus, "-S", "call", "service", "list", `{"name":"`+c.service+`"}`)
	if err != nil {
		return sshServiceControlObservation{}, fmt.Errorf("%w: %v", errProcdUnavailable, err)
	}
	state, err := classifyProcdService(raw, c.service)
	observation := sshServiceControlObservation{ID: c.service, Evidence: raw, Revision: domain.Revision(string(raw)), State: string(state)}
	if err != nil {
		return observation, fmt.Errorf("%w: %v", errControlOperationRejected, err)
	}
	switch state {
	case procdServiceNotRegistered:
		return observation, errServiceNotRegistered
	case procdServiceRegisteredStopped:
		return observation, errServiceRegisteredStopped
	case procdServiceRunning:
		return observation, nil
	default:
		return observation, fmt.Errorf("%w: unknown service state", errControlOperationRejected)
	}
}
func (c procdSSHServiceControl) Reload(ctx context.Context) error {
	_, err := runSSHCapabilityCommand(ctx, c.init, "reload")
	if err != nil {
		return fmt.Errorf("%w: %v", errControlOperationRejected, err)
	}
	return nil
}

func classifyProcdService(raw []byte, service string) (procdServiceState, error) {
	if !safeToken(service, 64) || len(raw) == 0 || len(raw) > maxCommandOutput {
		return "", errors.New("procd service projection is malformed")
	}
	var services map[string]json.RawMessage
	if err := json.Unmarshal(raw, &services); err != nil || services == nil {
		return "", errors.New("procd service projection is malformed")
	}
	if len(services) == 0 {
		return procdServiceNotRegistered, nil
	}
	entry, ok := services[service]
	if !ok {
		return "", errors.New("requested procd service is absent from a non-empty projection")
	}
	if len(services) != 1 {
		return "", errors.New("procd service projection contains multiple services")
	}
	if len(entry) == 0 || string(entry) == "null" {
		return "", errors.New("procd service entry is malformed")
	}
	var projection struct {
		Instances map[string]struct {
			Running bool `json:"running"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(entry, &projection); err != nil {
		return "", errors.New("procd service entry is malformed")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(entry, &fields); err != nil || fields == nil {
		return "", errors.New("procd service entry is malformed")
	}
	if _, ok := fields["instances"]; !ok {
		return "", errors.New("procd service instances projection is absent")
	}
	if fields["instances"] == nil || string(fields["instances"]) == "null" || len(projection.Instances) > maxDropbearSections {
		return "", errors.New("procd service instances projection is malformed")
	}
	for _, instance := range projection.Instances {
		if instance.Running {
			return procdServiceRunning, nil
		}
	}
	return procdServiceRegisteredStopped, nil
}
