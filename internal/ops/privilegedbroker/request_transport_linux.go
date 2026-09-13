//go:build linux

package privilegedbroker

import (
	"encoding/binary"
	"errors"
	"net"

	"golang.org/x/sys/unix"
)

func preparePeerListener(listener *net.UnixListener) error {
	if listener == nil {
		return errors.New("broker listener is unavailable")
	}
	raw, err := listener.SyscallConn()
	if err != nil {
		return err
	}
	var optionErr error
	if err := raw.Control(func(fd uintptr) {
		optionErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1)
	}); err != nil {
		return err
	}
	return optionErr
}

func preparePeerConnection(connection *net.UnixConn) error {
	if connection == nil {
		return errors.New("broker connection is unavailable")
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return err
	}
	var optionErr error
	if err := raw.Control(func(fd uintptr) {
		optionErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1)
	}); err != nil {
		return err
	}
	return optionErr
}

func readPeerRequest(connection *net.UnixConn, target *Request, limit int) (WriterCredentials, error) {
	if connection == nil || target == nil || limit <= 0 {
		return WriterCredentials{}, errors.New("broker credential frame target is invalid")
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return WriterCredentials{}, err
	}

	frame := make([]byte, 0, limit+4)
	expected := 0
	var writer WriterCredentials
	for expected == 0 || len(frame) < expected {
		var receiveErr error
		err = raw.Read(func(fd uintptr) bool {
			remaining := limit + 4 - len(frame)
			if remaining <= 0 {
				receiveErr = errors.New("broker credential frame exceeds its bound")
				return true
			}
			payload := make([]byte, remaining)
			oob := make([]byte, unix.CmsgSpace(unix.SizeofUcred)+unix.CmsgSpace(16*4))
			n, oobn, flags, _, recvErr := unix.Recvmsg(int(fd), payload, oob, unix.MSG_CMSG_CLOEXEC)
			if errors.Is(recvErr, unix.EAGAIN) || errors.Is(recvErr, unix.EWOULDBLOCK) {
				return false
			}
			if recvErr != nil {
				receiveErr = recvErr
				return true
			}
			if n == 0 || flags&(unix.MSG_CTRUNC|unix.MSG_TRUNC) != 0 {
				receiveErr = errors.New("broker credential frame is incomplete")
				return true
			}
			credentials, credentialErr := credentialsFromControl(oob[:oobn])
			if credentialErr != nil {
				receiveErr = credentialErr
				return true
			}
			if writer.PID == 0 {
				writer = credentials
			} else if writer != credentials {
				receiveErr = errors.New("broker request has multiple writer identities")
				return true
			}
			frame = append(frame, payload[:n]...)
			if len(frame) >= 4 && expected == 0 {
				length := int(binary.BigEndian.Uint32(frame[:4]))
				if length <= 0 || length > limit {
					receiveErr = errors.New("broker frame length is invalid")
					return true
				}
				expected = length + 4
			}
			if expected > 0 && len(frame) > expected {
				receiveErr = errors.New("multiple broker request frames are forbidden")
			}
			return true
		})
		if err != nil || receiveErr != nil {
			return WriterCredentials{}, errors.Join(err, receiveErr)
		}
	}
	if writer.PID <= 1 || len(frame) != expected {
		return WriterCredentials{}, errors.New("broker request writer credentials are unavailable")
	}
	if err := decodeStrict(frame[4:], target); err != nil {
		return WriterCredentials{}, err
	}
	return writer, nil
}

func credentialsFromControl(oob []byte) (WriterCredentials, error) {
	messages, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return WriterCredentials{}, errors.New("broker request control message is malformed")
	}
	var result WriterCredentials
	credentials := 0
	for index := range messages {
		message := &messages[index]
		if message.Header.Level == unix.SOL_SOCKET && message.Header.Type == unix.SCM_RIGHTS {
			if descriptors, parseErr := unix.ParseUnixRights(message); parseErr == nil {
				for _, descriptor := range descriptors {
					_ = unix.Close(descriptor)
				}
			}
			return WriterCredentials{}, errors.New("broker request descriptor transfer is forbidden")
		}
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_CREDENTIALS {
			return WriterCredentials{}, errors.New("broker request control message is unsupported")
		}
		credential, err := unix.ParseUnixCredentials(message)
		if err != nil || credential.Pid <= 1 {
			return WriterCredentials{}, errors.New("broker request writer credentials are malformed")
		}
		credentials++
		result = WriterCredentials{PID: int(credential.Pid), UID: uint32(credential.Uid), GID: uint32(credential.Gid)}
	}
	if credentials != 1 {
		return WriterCredentials{}, errors.New("broker request writer credentials are ambiguous")
	}
	return result, nil
}
