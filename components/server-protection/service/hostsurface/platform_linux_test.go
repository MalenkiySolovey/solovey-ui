//go:build linux

package hostsurface

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	processevidence "github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
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

func TestObservePlatformProjectsRealProcSocketProcessEvidence(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := uint16(listener.Addr().(*net.TCPAddr).Port)

	snapshot, err := observePlatform(context.Background(), hostfacts.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var observed *RawSocket
	for index := range snapshot.Sockets {
		candidate := &snapshot.Sockets[index]
		if candidate.Network == hostfacts.NetworkTCP && candidate.Family == hostfacts.FamilyIPv4 && candidate.Bind == "127.0.0.1" && candidate.Port == port {
			observed = candidate
			break
		}
	}
	if observed == nil || len(observed.Processes) != 1 {
		t.Fatalf("real listener process projection was not found: port=%d observed=%#v", port, observed)
	}
	process := observed.Processes[0]
	if process.PID != os.Getpid() || process.ProviderRevision != processevidence.RevisionV2 || !completeProcessEvidence(process) {
		t.Fatalf("real /proc process projection is incomplete: %#v", process)
	}

	result := Normalize(PlatformSnapshot{Sockets: []RawSocket{*observed}}, hostresources.ResourceSnapshot{}, time.Now().UTC())
	if len(result.Facts) != 1 || result.Facts[0].Classification != hostfacts.ClassificationLocalOnly || result.Facts[0].ConfidenceBP != 9000 || result.Facts[0].Process.EvidenceRevision != process.EvidenceRevision {
		t.Fatalf("real process projection did not survive owner normalization: %#v", result)
	}
}
