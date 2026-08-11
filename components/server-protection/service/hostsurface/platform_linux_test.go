//go:build linux

package hostsurface

import (
	"strings"
	"testing"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
)

func TestParseProcNetExcludesConnectedUDPClientPorts(t *testing.T) {
	data := []byte(strings.Join([]string{
		"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode",
		"0: 00000000:C001 0100007F:0035 01 0:0 0:0 0 1000 0 101",
		"1: 00000000:14E9 00000000:0000 07 0:0 0:0 0 1000 0 102",
	}, "\n"))
	rows := parseProcNet(data, hostfacts.NetworkUDP, hostfacts.FamilyIPv4)
	if len(rows) != 1 || rows[0].Port != 5353 || rows[0].Inode != "102" {
		t.Fatalf("UDP listener rows = %#v", rows)
	}
}

func TestProcStartTimeHandlesSpacesAndParenthesesInComm(t *testing.T) {
	// state is field 3 and starttime is field 22.
	suffix := []string{"S", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "424242", "20"}
	value := "123 (worker name) with paren) " + strings.Join(suffix, " ")
	if got := procStartTime(value); got != "424242" {
		t.Fatalf("starttime = %q", got)
	}
}
