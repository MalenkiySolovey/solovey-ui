//go:build linux

package capabilities

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
)

const maxObservationBusMessage = 64 << 10

// observationBusConn bounds framing before godbus consumes a message. Its
// pinned decoder performs final header alignment outside its error recovery;
// a deadline/EOF at that point would panic in the library's read goroutine.
// Only complete, bounded frames reach that decoder. This adapter knows wire
// boundaries, not D-Bus method, ownership or authorization semantics.
type observationBusConn struct {
	net.Conn
	framed    atomic.Bool
	frame     []byte // Auth and then godbus's single read worker own reads in order.
	authBytes int
}

func (c *observationBusConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err == nil && n == len(p) && bytes.Equal(p, []byte("BEGIN\r\n")) {
		// Auth starts the read worker only after this write returns.
		c.framed.Store(true)
	}
	return n, err
}

func (c *observationBusConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if !c.framed.Load() {
		remaining := maxObservationBusMessage - c.authBytes
		if remaining <= 0 {
			return 0, errors.New("observation bus authentication exceeds the metadata limit")
		}
		if len(p) > remaining {
			p = p[:remaining]
		}
		n, err := c.Conn.Read(p)
		c.authBytes += n
		return n, err
	}
	if len(c.frame) == 0 {
		var header [16]byte
		if _, err := io.ReadFull(c.Conn, header[:]); err != nil {
			return 0, err
		}
		var order binary.ByteOrder
		switch header[0] {
		case 'l':
			order = binary.LittleEndian
		case 'B':
			order = binary.BigEndian
		default:
			return 0, errors.New("invalid observation bus byte order")
		}
		// The fixed header declares the body and header-array sizes. The body
		// starts at the next 8-byte boundary after that array (pinned godbus
		// Message.EncodeToWithFDs/DecodeMessageWithFDs contract).
		body := uint64(order.Uint32(header[4:8]))
		fields := uint64(order.Uint32(header[12:16]))
		size := uint64(16) + ((fields + 7) &^ uint64(7)) + body
		if size > maxObservationBusMessage {
			return 0, errors.New("observation bus message exceeds the bounded metadata limit")
		}
		frame := make([]byte, int(size))
		copy(frame, header[:])
		if _, err := io.ReadFull(c.Conn, frame[16:]); err != nil {
			return 0, err
		}
		if err := validateObservationBusFrame(frame); err != nil {
			return 0, err
		}
		c.frame = frame
	}
	n := copy(p, c.frame)
	c.frame = c.frame[n:]
	return n, nil
}

// Reuse the pinned decoder on memory inside a recovery boundary. Complete
// framing alone cannot make a malformed header's internal alignment safe.
// Library validation owns its grammar; no partial bytes or raw errors escape
// into the asynchronous decoder when it rejects an observation message.
func validateObservationBusFrame(frame []byte) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("invalid observation bus message")
		}
	}()
	reader := bytes.NewReader(frame)
	if _, decodeErr := dbus.DecodeMessage(reader); decodeErr != nil || reader.Len() != 0 {
		return errors.New("invalid observation bus message")
	}
	return nil
}
