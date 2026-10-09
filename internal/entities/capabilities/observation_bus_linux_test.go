//go:build linux

package capabilities

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func observationSignalFrame(t *testing.T, order binary.ByteOrder) []byte {
	t.Helper()
	for n := 1; n <= 8; n++ {
		var data bytes.Buffer
		msg := &dbus.Message{Type: dbus.TypeSignal, Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldPath:      dbus.MakeVariant(dbus.ObjectPath("/fixture")),
			dbus.FieldInterface: dbus.MakeVariant("fixture.Observation"),
			dbus.FieldMember:    dbus.MakeVariant(string(bytes.Repeat([]byte{'M'}, n))),
			dbus.FieldSignature: dbus.MakeVariant(dbus.SignatureOf("fixture")),
		}, Body: []interface{}{"fixture"}}
		if err := msg.EncodeTo(&data, order); err != nil {
			t.Fatal(err)
		}
		frame := data.Bytes()
		if order.Uint32(frame[12:16])%8 != 0 {
			return frame
		}
	}
	t.Fatal("fixture did not produce nonzero final header padding")
	return nil
}

func TestObservationBusFramesBothByteOrdersAndBoundsSize(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		client, peer := net.Pipe()
		wrapped := &observationBusConn{Conn: client}
		wrapped.framed.Store(true)
		frame := observationSignalFrame(t, order)
		done := make(chan error, 1)
		go func() { _, err := peer.Write(frame); _ = peer.Close(); done <- err }()
		msg, err := dbus.DecodeMessage(wrapped)
		_ = client.Close()
		if err != nil || len(msg.Body) != 1 || msg.Body[0] != "fixture" {
			t.Fatalf("complete observation message was not decoded: %v", err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	wrapped := &observationBusConn{Conn: client}
	wrapped.framed.Store(true)
	var header [16]byte
	header[0] = 'l'
	binary.LittleEndian.PutUint32(header[12:16], ^uint32(0))
	done := make(chan struct{})
	go func() { _, _ = peer.Write(header[:]); close(done) }()
	if _, err := dbus.DecodeMessage(wrapped); err == nil {
		t.Fatal("oversized observation message was accepted")
	}
	<-done
}

func TestObservationBusRejectsMalformedCompleteFrameWithoutPanic(t *testing.T) {
	frame := observationSignalFrame(t, binary.LittleEndian)
	for _, malformed := range [][]byte{
		frame[:16+binary.LittleEndian.Uint32(frame[12:16])],
		append([]byte{'?'}, frame[1:]...),
		append([]byte(nil), frame[:15]...),
	} {
		if err := validateObservationBusFrame(malformed); err == nil {
			t.Fatal("malformed observation message accepted")
		}
	}
}

func TestResolve1PartialMessageAtHeaderAlignmentFailsClosed(t *testing.T) {
	frame := observationSignalFrame(t, binary.LittleEndian)
	path := filepath.Join(t.TempDir(), "partial")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		peer, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer peer.Close()
		_ = peer.SetDeadline(time.Now().Add(2 * time.Second))
		reader := bufio.NewReader(peer)
		_, err = reader.ReadByte() // authentication's NUL byte
		if err == nil {
			_, err = reader.ReadString('\n')
		}
		if err == nil {
			_, err = io.WriteString(peer, "REJECTED EXTERNAL\r\n")
		}
		if err == nil {
			_, err = reader.ReadString('\n')
		}
		if err == nil {
			_, err = io.WriteString(peer, "OK 0123456789abcdef0123456789abcdef\r\n")
		}
		if err == nil {
			_, err = reader.ReadString('\n')
		}
		if err == nil {
			end := 16 + binary.LittleEndian.Uint32(frame[12:16])
			_, err = peer.Write(frame[:end]) // omit final padding and body
		}
		if err == nil {
			_, err = io.Copy(io.Discard, reader)
		}
		done <- err
	}()
	start := time.Now()
	available, state := observeResolve1At("unix:path=" + path)
	if available || state != Resolve1NameUnknown || time.Since(start) > 2*time.Second {
		t.Fatal("partial bus message was not bounded/fail-closed")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("partial bus fixture leaked its peer")
	}
}
