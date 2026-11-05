package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	client "github.com/enfein/mieru/v3/apis/client"
	common "github.com/enfein/mieru/v3/apis/common"
	model "github.com/enfein/mieru/v3/apis/model"
	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

func run() error {
	var input struct {
		Host, User, Password, Transport string
		Port                            int
	}
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		return err
	}
	transport := pb.TransportProtocol_TCP
	if input.Transport == "UDP" {
		transport = pb.TransportProtocol_UDP
	}
	c := client.NewClient()
	config := &client.ClientConfig{Profile: &pb.ClientProfile{
		ProfileName: proto.String("d12-qualification"), User: &pb.User{Name: proto.String(input.User), Password: proto.String(input.Password)},
		Mtu: proto.Int32(1400), Servers: []*pb.ServerEndpoint{{IpAddress: proto.String(input.Host), PortBindings: []*pb.PortBinding{{Port: proto.Int32(int32(input.Port)), Protocol: transport.Enum()}}}}}}
	if err := c.Store(config); err != nil {
		return err
	}
	if err := c.Start(); err != nil {
		return err
	}
	defer c.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer tcp.Close()
	go func() {
		conn, e := tcp.Accept()
		if e == nil {
			defer conn.Close()
			io.Copy(conn, conn)
		}
	}()
	var dest model.NetAddrSpec
	if err = dest.From(tcp.Addr()); err != nil {
		return err
	}
	dest.Net = "tcp"
	conn, err := c.DialContext(ctx, dest)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	const payload = "d12-authenticated-tcp"
	if _, err = conn.Write([]byte(payload)); err != nil {
		return err
	}
	data := make([]byte, len(payload))
	if _, err = io.ReadFull(conn, data); err != nil || string(data) != payload {
		return fmt.Errorf("tcp echo rejected: %w", err)
	}
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer udp.Close()
	go func() {
		b := make([]byte, 256)
		n, a, e := udp.ReadFrom(b)
		if e == nil {
			udp.WriteTo(b[:n], a)
		}
	}()
	if err = dest.From(udp.LocalAddr()); err != nil {
		return err
	}
	dest.Net = "udp"
	stream, err := c.DialContext(ctx, dest)
	if err != nil {
		return err
	}
	defer stream.Close()
	packet := common.NewUDPAssociateWrapper(common.NewPacketOverStreamTunnel(stream))
	defer packet.Close()
	packet.SetDeadline(time.Now().Add(5 * time.Second))
	const datagram = "d12-authenticated-udp"
	if _, err = packet.WriteTo([]byte(datagram), udp.LocalAddr()); err != nil {
		return err
	}
	data = make([]byte, 256)
	n, _, err := packet.ReadFrom(data)
	if err != nil || string(data[:n]) != datagram {
		return fmt.Errorf("udp echo rejected: %w", err)
	}
	fmt.Println(`{"authenticated":true,"tcp":true,"udp":true}`)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "qualification traffic failed:", err)
		os.Exit(1)
	}
}
