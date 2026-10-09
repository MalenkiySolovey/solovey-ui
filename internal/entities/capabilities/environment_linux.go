//go:build linux

package capabilities

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const resolve1Name = "org.freedesktop.resolve1"
const dependencyProbeTimeout = 250 * time.Millisecond

func observeResolve1() (bool, Resolve1NameState) {
	address := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS")
	if address == "" {
		address = "unix:path=/var/run/dbus/system_bus_socket"
	}
	return observeResolve1At(address)
}

// Only local Unix bus addresses are observed. No well-known name is requested,
// exported, activated or released; raw errors/addresses/PIDs are not projected.
func observeResolve1At(address string) (bool, Resolve1NameState) {
	endpoint, ok := systemBusUnixEndpoint(address)
	if !ok {
		return false, Resolve1NameUnknown
	}
	ctx, cancel := context.WithTimeout(context.Background(), dependencyProbeTimeout)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(ctx, "unix", endpoint)
	if err != nil {
		return false, Resolve1NameUnknown
	}
	defer raw.Close()
	deadline, _ := ctx.Deadline()
	if raw.SetDeadline(deadline) != nil {
		return false, Resolve1NameUnknown
	}
	connection, err := dbus.NewConn(&observationBusConn{Conn: raw}, dbus.WithContext(ctx))
	if err != nil {
		return false, Resolve1NameUnknown
	}
	defer connection.Close()
	// A local system bus uses EXTERNAL peer credentials. Do not let a
	// metadata-only observation fall back to cookie-file authentication.
	if connection.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Geteuid()))}) != nil || connection.Hello() != nil {
		return false, Resolve1NameUnknown
	}
	var owner string
	call := connection.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, resolve1Name)
	if call.Err != nil {
		if failure, typed := call.Err.(dbus.Error); typed && failure.Name == "org.freedesktop.DBus.Error.NameHasNoOwner" {
			return true, Resolve1NameUnclaimed
		}
		return true, Resolve1NameUnknown
	}
	if call.Store(&owner) != nil {
		return true, Resolve1NameUnknown
	}
	var pid uint32
	if connection.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, owner).Store(&pid) != nil {
		return true, Resolve1NameUnknown
	}
	if pid == uint32(os.Getpid()) {
		return true, Resolve1NameCurrentProcess
	}
	return true, Resolve1NameOtherProcess
}

func systemBusUnixEndpoint(address string) (string, bool) {
	keys, ok := strings.CutPrefix(address, "unix:")
	if !ok || strings.Contains(keys, ";") {
		return "", false
	}
	var path, abstract string
	seen := map[string]bool{}
	for _, pair := range strings.Split(keys, ",") {
		key, value, found := strings.Cut(pair, "=")
		if !found || seen[key] {
			return "", false
		}
		seen[key] = true
		decoded, err := url.PathUnescape(value)
		if err != nil || strings.ContainsRune(decoded, 0) {
			return "", false
		}
		switch key {
		case "path":
			path = decoded
		case "abstract":
			abstract = decoded
		case "guid":
		default:
			return "", false
		}
	}
	if path != "" && abstract == "" && filepath.IsAbs(path) {
		return path, true
	}
	if abstract != "" && path == "" {
		return "@" + abstract, true
	}
	return "", false
}
