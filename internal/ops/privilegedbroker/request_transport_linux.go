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
		return WriterCredentials{}, requestReadFailure(receiveTargetInvalid, nil)
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return WriterCredentials{}, requestReadFailure(receiveUnavailable, err)
	}

	frame := make([]byte, 0, limit+4)
	expected := 0
	var writer WriterCredentials
	for expected == 0 || len(frame) < expected {
		var receiveErr error
		err = raw.Read(func(fd uintptr) bool {
			remaining := limit + 4 - len(frame)
			if remaining <= 0 {
				receiveErr = requestReadFailure(frameOversized, nil)
				return true
			}
			payload := make([]byte, remaining)
			oob := make([]byte, unix.CmsgSpace(unix.SizeofUcred)+unix.CmsgSpace(16*4))
			n, oobn, flags, _, recvErr := unix.Recvmsg(int(fd), payload, oob, unix.MSG_CMSG_CLOEXEC)
			if errors.Is(recvErr, unix.EAGAIN) || errors.Is(recvErr, unix.EWOULDBLOCK) {
				return false
			}
			if recvErr != nil {
				receiveErr = requestReadFailure(receiveFailed, recvErr)
				return true
			}
			credentials, credentialErr := receivedRequestCredentials(n, flags, oob[:oobn], len(frame) > 0)
			if credentialErr != nil {
				receiveErr = credentialErr
				return true
			}
			if writer.PID == 0 {
				writer = credentials
			} else if writer != credentials {
				receiveErr = requestReadFailure(writerChanged, nil)
				return true
			}
			frame = append(frame, payload[:n]...)
			if len(frame) >= 4 && expected == 0 {
				length := int(binary.BigEndian.Uint32(frame[:4]))
				if length <= 0 {
					receiveErr = requestReadFailure(frameLengthInvalid, nil)
					return true
				}
				if length > limit {
					receiveErr = requestReadFailure(frameOversized, nil)
					return true
				}
				expected = length + 4
			}
			if expected > 0 && len(frame) > expected {
				receiveErr = requestReadFailure(frameMultiple, nil)
			}
			return true
		})
		if err != nil {
			return WriterCredentials{}, requestReadFailure(receiveFailed, err)
		}
		if receiveErr != nil {
			return WriterCredentials{}, receiveErr
		}
	}
	if writer.PID <= 1 || len(frame) != expected {
		return WriterCredentials{}, requestReadFailure(credentialsMissing, nil)
	}
	if err := decodeStrict(frame[4:], target); err != nil {
		return WriterCredentials{}, requestReadFailure(frameDecodeInvalid, err)
	}
	return writer, nil
}

func receivedRequestCredentials(n, flags int, oob []byte, partial bool) (WriterCredentials, error) {
	// recvmsg may install the descriptors that fit even when MSG_CTRUNC is
	// set. Consume and close all delivered SCM_RIGHTS before any rejection.
	writer, err := credentialsFromControl(oob)
	if flags&unix.MSG_CTRUNC != 0 {
		return WriterCredentials{}, requestReadFailure(controlTruncated, nil)
	}
	if flags&unix.MSG_TRUNC != 0 {
		return WriterCredentials{}, requestReadFailure(frameTruncated, nil)
	}
	if n == 0 {
		class := receiveEmpty
		if partial {
			class = frameIncomplete
		}
		return WriterCredentials{}, requestReadFailure(class, nil)
	}
	return writer, err
}

func credentialsFromControl(oob []byte) (WriterCredentials, error) {
	var result WriterCredentials
	credentials := 0
	var failure error
	for len(oob) > 0 {
		if len(oob) < unix.CmsgLen(0) {
			return WriterCredentials{}, requestReadFailure(controlMalformed, nil)
		}
		header, data, remainder, err := unix.ParseOneSocketControlMessage(oob)
		if err != nil {
			return WriterCredentials{}, requestReadFailure(controlMalformed, nil)
		}
		oob = remainder
		message := &unix.SocketControlMessage{Header: header, Data: data}
		if message.Header.Level == unix.SOL_SOCKET && message.Header.Type == unix.SCM_RIGHTS {
			if descriptors, parseErr := unix.ParseUnixRights(message); parseErr == nil {
				for _, descriptor := range descriptors {
					_ = unix.Close(descriptor)
				}
			}
			failure = requestReadFailure(descriptorForbidden, nil)
			continue
		}
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_CREDENTIALS {
			failure = requestReadFailure(controlUnsupported, nil)
			continue
		}
		if len(data) != unix.SizeofUcred {
			failure = requestReadFailure(credentialsMalformed, nil)
			continue
		}
		credential, err := unix.ParseUnixCredentials(message)
		if err != nil || credential.Pid <= 1 {
			failure = requestReadFailure(credentialsMalformed, nil)
			continue
		}
		credentials++
		result = WriterCredentials{PID: int(credential.Pid), UID: uint32(credential.Uid), GID: uint32(credential.Gid)}
	}
	if failure != nil {
		return WriterCredentials{}, failure
	}
	if credentials == 0 {
		return WriterCredentials{}, requestReadFailure(credentialsMissing, nil)
	}
	if credentials != 1 {
		return WriterCredentials{}, requestReadFailure(credentialsAmbiguous, nil)
	}
	return result, nil
}
