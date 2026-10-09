package inboundidentity

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	official "github.com/sagernet/sing-box/protocol/snell"
	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing-snell/snellv4"
	"github.com/sagernet/sing-snell/snellv6"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type snellRoute struct {
	adapter.Router
	owner    *Owner
	accepted chan Binding
}

func (r *snellRoute) accept(ctx context.Context, metadata adapter.InboundContext) {
	Admission(ctx, func() {
		stamped := InventoryMetadata(ctx, metadata)
		binding, _ := r.owner.Resolve(stamped.Inbound, stamped.User)
		r.accepted <- binding
	})
}
func (r *snellRoute) RouteConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, close N.CloseHandlerFunc) {
	r.accept(ctx, metadata)
	_, _ = conn.Write([]byte("x"))
	_ = conn.Close()
	if close != nil {
		close(nil)
	}
}
func (r *snellRoute) RoutePacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, close N.CloseHandlerFunc) {
	r.accept(ctx, metadata)
	packet := buf.NewPacket()
	packet.Resize(64, 0) // Reserve the protocol response's front header space.
	if destination, err := conn.ReadPacket(packet); err == nil {
		_ = conn.WritePacket(packet, destination)
	} else {
		packet.Release()
	}
	_ = conn.Close()
	if close != nil {
		close(nil)
	}
}

// Use the pinned official inbound and its real v4/v6 client over local pipes.
// Handshake IO and completion barriers qualify authentication, including the
// constructor's empty-user fallback and retired epochs, without a public port.
func TestSnellOfficialMultiUserEpochAdmission(t *testing.T) {
	for _, version := range []int{5, 6} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			owner := NewOwner()
			t.Cleanup(owner.Close)
			delegate := &snellRoute{owner: owner, accepted: make(chan Binding, 16)}
			router, epoch, err := owner.Prepare(delegate, "snell", []Binding{{Inbound: "snell", Principal: "alice", ClientID: 7}, {Inbound: "snell", Principal: "bob", ClientID: 8}})
			if err != nil {
				t.Fatal(err)
			}
			owner.Publish("snell", epoch)
			options := option.SnellInboundOptions{Version: version, AbstractSnellInboundOptions: option.AbstractSnellInboundOptions{PSK: "fixture-psk-12"}}
			if version == 5 {
				options.ObfsOptions.ObfsMode = "http"
			} else {
				options.V6Options.Mode = "unshaped"
			}
			options.Users = []option.SnellUser{{Name: "alice", UserKey: "key-a"}, {Name: "bob", UserKey: "key-b"}}
			open := func(clientVersion int, key string, udp bool, want uint, anonymous bool) {
				t.Helper()
				serverOptions := options
				if anonymous {
					serverOptions.Users = nil
				}
				inbound, err := official.NewInbound(t.Context(), router, log.NewNOPFactory().Logger(), "snell", serverOptions)
				if err != nil {
					t.Fatal(err)
				}
				server := inbound.(*official.Inbound)
				left, right := net.Pipe()
				defer left.Close()
				defer right.Close()
				deadline := time.Now().Add(3 * time.Second)
				_ = left.SetDeadline(deadline)
				_ = right.SetDeadline(deadline)
				done := make(chan struct{})
				go func() {
					defer close(done)
					server.NewConnection(t.Context(), right, adapter.InboundContext{Source: M.ParseSocksaddr("127.0.0.1:1234")}, nil)
				}()
				var client snell.Method
				if clientVersion == 4 {
					client, err = snellv4.NewClient(snellv4.ClientOptions{PSK: []byte(options.PSK), UserKey: []byte(key), ObfsMode: snell.ObfsModeHTTP})
				} else {
					client, err = snellv6.NewClient(snellv6.ClientOptions{PSK: []byte(options.PSK), UserKey: []byte(key), Mode: snellv6.ModeUnshaped})
				}
				if err != nil {
					t.Fatal(err)
				}
				if udp {
					packet, err := client.DialPacketConn(left)
					if err == nil {
						_, err = packet.WriteTo([]byte("x"), M.ParseSocksaddr("127.0.0.1:443"))
						if err == nil {
							_, _, err = packet.ReadFrom(make([]byte, 16))
						}
						_ = packet.Close()
					}
					if want != 0 && err != nil {
						t.Fatal("accepted UDP fixture failed", err)
					}
				} else {
					conn, err := client.DialConn(left, M.ParseSocksaddr("example.invalid:443"))
					if err == nil {
						_, err = io.ReadFull(conn, make([]byte, 1))
						_ = conn.Close()
					}
					if want != 0 && err != nil {
						t.Fatal("accepted TCP fixture failed", err)
					}
				}
				_ = left.Close()
				_ = right.Close()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("server fixture leaked")
				}
				if want == 0 {
					select {
					case <-delegate.accepted:
						t.Fatal("invalid or retired credential admitted")
					default:
					}
					return
				}
				select {
				case binding := <-delegate.accepted:
					if binding.ClientID != want {
						t.Fatal("wrong authenticated client identity")
					}
				default:
					t.Fatal("no authenticated route")
				}
			}
			clientVersion := 4
			wrongVersion := 6
			if version == 6 {
				clientVersion, wrongVersion = 6, 4
			}
			open(clientVersion, "key-a", false, 7, false)
			open(clientVersion, "key-b", true, 8, false)
			open(clientVersion, "wrong", false, 0, false)
			open(wrongVersion, "key-a", false, 0, false)
			open(clientVersion, "", false, 0, true)
			owner.Revoke("snell")
			open(clientVersion, "key-a", false, 0, false)
			newRouter, replacement, err := owner.Prepare(delegate, "snell", []Binding{{Inbound: "snell", Principal: "alice", ClientID: 7}})
			if err != nil {
				t.Fatal(err)
			}
			owner.Publish("snell", replacement)
			open(clientVersion, "key-a", false, 0, false)
			router = newRouter
			options.Users = options.Users[:1]
			open(clientVersion, "key-a", false, 7, false)
			open(clientVersion, "key-b", false, 0, false)
		})
	}
}
