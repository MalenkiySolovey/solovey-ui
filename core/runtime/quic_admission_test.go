//go:build with_quic

package runtime

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/quic-go/quicvarint"
	"github.com/sagernet/sing-quic/hysteria"
	"github.com/sagernet/sing-quic/tuic"
)

type quicAdmissionObserver struct {
	deny     atomic.Bool
	rejected chan struct{}
}

func (o *quicAdmissionObserver) ObserveAndAllow(user, _ string) bool {
	if user == "verified-7" && o.deny.Load() {
		select {
		case o.rejected <- struct{}{}:
		default:
		}
		return false
	}
	return true
}

func TestQUICRejectedFlowsStayOutsideAdmissionAndAccounting(t *testing.T) {
	for _, kind := range []string{"hysteria", "hysteria2", "tuic"} {
		t.Run(kind, func(t *testing.T) {
			observer := &quicAdmissionObserver{rejected: make(chan struct{}, 1)}
			f := newQUICFixture(t, kind, observer)
			peer := f.peer(t, 7)
			_ = f.core.instance.StatsTracker().GetStats()
			observer.deny.Store(true)
			if err := peer.echo(t.Context(), f.echo.Addr().String()); err == nil {
				t.Fatal("rejected flow was routed")
			}
			select {
			case <-observer.rejected:
			case <-time.After(time.Second):
				t.Fatal("policy rejection not reached")
			}
			for _, stat := range f.core.instance.StatsTracker().GetStats() {
				if stat.Resource == "user" && stat.Tag == "verified-7" && stat.Traffic != 0 {
					t.Fatal("rejected flow was accounted")
				}
			}
			observed := f.parents(t, 7)
			if len(observed.Parents) != 1 {
				t.Fatal("flow rejection confused authenticated parent capability")
			}
			if _, err := f.core.Disconnect(t.Context(), DisconnectRequest{Generation: observed.Generation, ClientID: 7, Parents: []QUICParentTarget{observed.Parents[0].QUICParentTarget}}); err != nil {
				t.Fatal(err)
			}
			assertQUICRemoteClose(t, peer)
		})
	}
}

func TestQUICAuthenticatedUDPParentKick(t *testing.T) {
	for _, kind := range []string{"hysteria", "hysteria2", "tuic"} {
		t.Run(kind, func(t *testing.T) {
			f := newQUICFixture(t, kind)
			peer := f.peer(t, 7)
			echo, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				buffer := make([]byte, 4096)
				for {
					count, address, err := echo.ReadFrom(buffer)
					if err != nil {
						return
					}
					_, _ = echo.WriteTo(buffer[:count], address)
				}
			}()
			t.Cleanup(func() {
				_ = echo.Close()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("UDP fixture leaked")
				}
			})
			port := uint16(echo.LocalAddr().(*net.UDPAddr).Port)
			var session uint32 = 42
			var packet []byte
			switch kind {
			case "hysteria":
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				stream, err := peer.conn.OpenStreamSync(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer stream.CancelRead(0)
				defer stream.Close()
				_ = stream.SetDeadline(time.Now().Add(3 * time.Second))
				request := hysteria.WriteClientRequest(hysteria.ClientRequest{UDP: true, Host: "127.0.0.1", Port: port}, nil)
				_, err = stream.Write(request.Bytes())
				request.Release()
				if err != nil {
					t.Fatal(err)
				}
				response, err := hysteria.ReadServerResponse(stream)
				if err != nil || !response.OK {
					t.Fatal("UDP request rejected")
				}
				session = response.UDPSessionID
				packet = binary.BigEndian.AppendUint32(nil, session)
				packet = binary.BigEndian.AppendUint16(packet, 9)
				packet = append(packet, "127.0.0.1"...)
				packet = binary.BigEndian.AppendUint16(packet, port)
				packet = binary.BigEndian.AppendUint16(packet, 1)
				packet = append(packet, 0, 1)
				packet = binary.BigEndian.AppendUint16(packet, 2)
				packet = append(packet, "ok"...)
			case "hysteria2":
				address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
				packet = binary.BigEndian.AppendUint32(nil, session)
				packet = binary.BigEndian.AppendUint16(packet, 1)
				packet = append(packet, 0, 1)
				packet = quicvarint.Append(packet, uint64(len(address)))
				packet = append(packet, address...)
				packet = append(packet, "ok"...)
			case "tuic":
				packet = []byte{tuic.Version, tuic.CommandPacket}
				packet = binary.BigEndian.AppendUint16(packet, uint16(session))
				packet = binary.BigEndian.AppendUint16(packet, 1)
				packet = append(packet, 1, 0)
				packet = binary.BigEndian.AppendUint16(packet, 2)
				packet = append(packet, 1, 127, 0, 0, 1)
				packet = binary.BigEndian.AppendUint16(packet, port)
				packet = append(packet, "ok"...)
			}
			if err := peer.conn.SendDatagram(packet); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			for {
				response, err := peer.conn.ReceiveDatagram(ctx)
				if err != nil {
					t.Fatal("authenticated UDP echo:", err)
				}
				if kind == "tuic" && (len(response) < 2 || response[1] != tuic.CommandPacket) {
					continue
				}
				if !bytes.HasSuffix(response, []byte("ok")) {
					t.Fatal("UDP echo payload mismatch")
				}
				break
			}
			observed := f.parents(t, 7)
			udpObserved := false
			for _, flow := range observed.Connections {
				if flow.Network == "udp" && flow.ClientID == 7 {
					udpObserved = true
				}
			}
			if !udpObserved || len(observed.Parents) != 1 {
				t.Fatal("UDP admitted inventory and parent association not proven")
			}
			result, err := f.core.Disconnect(t.Context(), DisconnectRequest{Generation: observed.Generation, ClientID: 7, Parents: []QUICParentTarget{observed.Parents[0].QUICParentTarget}})
			if err != nil || result.ParentsClosed != 1 {
				t.Fatal("UDP parent kick failed")
			}
			assertQUICRemoteClose(t, peer)
			t.Log("authenticated accepted UDP + official flow inventory + exact QUIC parent close PASS")
		})
	}
}
