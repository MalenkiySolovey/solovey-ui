package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

const (
	VersionStream   = byte(2)
	StreamChunkSize = 1 << 20
	MaxStreamBytes  = int64(512 << 20)
)

// SealStream emits independently authenticated, ordered chunks plus an
// authenticated terminal frame. Payload memory is bounded by one chunk; the
// Argon2id KDF retains its separately bounded 64 MiB security cost.
func SealStream(destination io.Writer, source io.Reader, passphrase []byte) (int64, int64, error) {
	if destination == nil || source == nil || len(passphrase) == 0 {
		return 0, 0, common.NewError("missing_passphrase")
	}
	header, key, err := newStreamHeader(passphrase)
	if err != nil {
		return 0, 0, err
	}
	defer common.WipeBytes(key)
	gcm, err := streamGCM(key)
	if err != nil {
		return 0, 0, err
	}
	written, err := writeFull(destination, header)
	if err != nil {
		return 0, written, err
	}
	buffer := make([]byte, StreamChunkSize)
	defer common.WipeBytes(buffer)
	var plainBytes int64
	var counter uint32
	for {
		count, readErr := source.Read(buffer)
		if count > 0 {
			plainBytes += int64(count)
			if plainBytes > MaxStreamBytes || counter == ^uint32(0) {
				return plainBytes, written, errors.New("backup stream exceeds bounds")
			}
			frame, frameErr := sealFrame(gcm, header, buffer[:count], counter)
			if frameErr != nil {
				return plainBytes, written, frameErr
			}
			frameWritten, writeErr := writeFull(destination, frame)
			common.WipeBytes(frame)
			written += frameWritten
			if writeErr != nil {
				return plainBytes, written, writeErr
			}
			counter++
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return plainBytes, written, readErr
		}
	}
	terminal, err := sealFrame(gcm, header, nil, counter)
	if err != nil {
		return plainBytes, written, err
	}
	terminalWritten, err := writeFull(destination, terminal)
	common.WipeBytes(terminal)
	written += terminalWritten
	return plainBytes, written, err
}

func OpenStream(destination io.Writer, source io.Reader, passphrase []byte, maxPlainBytes int64) (int64, int64, error) {
	if destination == nil || source == nil || len(passphrase) == 0 {
		return 0, 0, ErrDecryptionFailed
	}
	if maxPlainBytes <= 0 || maxPlainBytes > MaxStreamBytes {
		maxPlainBytes = MaxStreamBytes
	}
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(source, header); err != nil {
		return 0, 0, ErrInvalidEnvelope
	}
	parsed, err := ParseHeader(header)
	if err != nil || parsed.Version != VersionStream || parsed.KDFID != KDFArgon2ID || validateKDFParams(parsed.KDFParams) != nil {
		return 0, int64(len(header)), ErrInvalidEnvelope
	}
	key := deriveKey(passphrase, parsed.Salt[:], parsed.KDFParams)
	defer common.WipeBytes(key)
	gcm, err := streamGCM(key)
	if err != nil {
		return 0, int64(len(header)), ErrDecryptionFailed
	}
	var plainBytes int64
	cipherBytes := int64(len(header))
	for counter := uint32(0); ; counter++ {
		var size [4]byte
		if _, err := io.ReadFull(source, size[:]); err != nil {
			return plainBytes, cipherBytes, ErrDecryptionFailed
		}
		cipherBytes += 4
		plainSize := binary.BigEndian.Uint32(size[:])
		if plainSize > StreamChunkSize || plainBytes+int64(plainSize) > maxPlainBytes {
			return plainBytes, cipherBytes, ErrInvalidEnvelope
		}
		ciphertext := make([]byte, int(plainSize)+gcm.Overhead())
		if _, err := io.ReadFull(source, ciphertext); err != nil {
			common.WipeBytes(ciphertext)
			return plainBytes, cipherBytes, ErrDecryptionFailed
		}
		cipherBytes += int64(len(ciphertext))
		nonce := streamNonce(parsed.Nonce[:], counter)
		aad := streamAAD(header, counter, plainSize)
		plaintext, openErr := gcm.Open(nil, nonce, ciphertext, aad)
		common.WipeBytes(ciphertext)
		common.WipeBytes(nonce)
		common.WipeBytes(aad)
		if openErr != nil {
			return plainBytes, cipherBytes, ErrDecryptionFailed
		}
		if plainSize == 0 {
			common.WipeBytes(plaintext)
			var trailing [1]byte
			if count, trailingErr := source.Read(trailing[:]); count != 0 || !errors.Is(trailingErr, io.EOF) {
				return plainBytes, cipherBytes, ErrInvalidEnvelope
			}
			return plainBytes, cipherBytes, nil
		}
		written, writeErr := writeFull(destination, plaintext)
		common.WipeBytes(plaintext)
		plainBytes += written
		if writeErr != nil || written != int64(plainSize) {
			return plainBytes, cipherBytes, errors.Join(writeErr, io.ErrShortWrite)
		}
		if counter == ^uint32(0) {
			return plainBytes, cipherBytes, ErrInvalidEnvelope
		}
	}
}

func newStreamHeader(passphrase []byte) ([]byte, []byte, error) {
	salt := make([]byte, saltSize)
	nonce := make([]byte, nonceSize)
	defer common.WipeBytes(salt)
	defer common.WipeBytes(nonce)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, err
	}
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	header := make([]byte, headerSize)
	copy(header[:magicSize], []byte(Magic))
	offset := magicSize
	header[offset], offset = VersionStream, offset+versionSize
	header[offset], offset = KDFArgon2ID, offset+kdfIDSize
	binary.BigEndian.PutUint32(header[offset:offset+4], defaultKDFParams.MemoryKiB)
	binary.BigEndian.PutUint32(header[offset+4:offset+8], defaultKDFParams.Iterations)
	header[offset+8] = defaultKDFParams.Parallelism
	offset += kdfParamsSize
	copy(header[offset:offset+saltSize], salt)
	offset += saltSize
	copy(header[offset:offset+nonceSize], nonce)
	return header, deriveKey(passphrase, salt, defaultKDFParams), nil
}

func streamGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithNonceSize(block, nonceSize)
}

func sealFrame(gcm cipher.AEAD, header, plaintext []byte, counter uint32) ([]byte, error) {
	if len(plaintext) > StreamChunkSize {
		return nil, ErrInvalidEnvelope
	}
	size := uint32(len(plaintext))
	nonce := streamNonce(header[headerSize-nonceSize:], counter)
	aad := streamAAD(header, counter, size)
	frame := make([]byte, 4, 4+len(plaintext)+gcm.Overhead())
	binary.BigEndian.PutUint32(frame, size)
	frame = gcm.Seal(frame, nonce, plaintext, aad)
	common.WipeBytes(nonce)
	common.WipeBytes(aad)
	return frame, nil
}

func streamNonce(base []byte, counter uint32) []byte {
	nonce := make([]byte, nonceSize)
	copy(nonce[:8], base[:8])
	binary.BigEndian.PutUint32(nonce[8:], counter)
	return nonce
}

func streamAAD(header []byte, counter, size uint32) []byte {
	aad := make([]byte, len(header)+8)
	copy(aad, header)
	binary.BigEndian.PutUint32(aad[len(header):], counter)
	binary.BigEndian.PutUint32(aad[len(header)+4:], size)
	return aad
}

func writeFull(destination io.Writer, data []byte) (int64, error) {
	var total int64
	for len(data) > 0 {
		written, err := destination.Write(data)
		total += int64(written)
		data = data[written:]
		if err != nil {
			return total, err
		}
		if written == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
