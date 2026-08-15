package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"golang.org/x/crypto/argon2"
)

const (
	Magic       = "SUI-TGBKP\x00"
	Version     = byte(1)
	KDFArgon2ID = byte(1)
	// LegacyMaxBytes bounds the whole-buffer v1 format while retaining
	// compatibility with the historical 50 MiB Telegram backup limit.
	LegacyMaxBytes = int64(64 << 20)

	magicSize     = 10
	versionSize   = 1
	kdfIDSize     = 1
	kdfParamsSize = 16
	saltSize      = 16
	nonceSize     = 12
	headerSize    = magicSize +
		versionSize +
		kdfIDSize +
		kdfParamsSize +
		saltSize +
		nonceSize

	argon2MemoryKiB          = 64 * 1024
	argon2Iterations         = 3
	argon2Parallelism        = 1
	maximumArgon2MemoryKiB   = 1024 * 1024
	maximumArgon2Iterations  = 16
	maximumArgon2Parallelism = 4
	keySize                  = 32
)

var (
	ErrDecryptionFailed = errors.New("decryption_failed")
	ErrInvalidEnvelope  = errors.New("invalid_backup_envelope")
)

type KDFParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

type Header struct {
	Version      byte
	KDFID        byte
	KDFParams    KDFParams
	RawKDFParams [kdfParamsSize]byte
	Salt         [saltSize]byte
	Nonce        [nonceSize]byte
}

var defaultKDFParams = KDFParams{
	MemoryKiB:   argon2MemoryKiB,
	Iterations:  argon2Iterations,
	Parallelism: argon2Parallelism,
}

// Backup envelope layout, fixed for version 1:
//
//	magic          10 bytes  "SUI-TGBKP\x00"
//	version         1 byte   currently 0x01
//	kdf-id          1 byte   0x01 = Argon2id
//	kdf-params     16 bytes  for Argon2id:
//	                         uint32 memoryKiB, uint32 iterations,
//	                         uint8 parallelism, 7 reserved zero bytes
//	salt           16 bytes  random per envelope
//	nonce          12 bytes  AES-GCM nonce, random per envelope
//	ciphertext+tag  N bytes  AES-256-GCM output
//
// AES-GCM authenticates the whole header through nonce as AAD.
func Build(plaintext []byte, passphrase []byte) ([]byte, error) {
	return build(plaintext, passphrase, rand.Reader)
}

func Open(envelope []byte, passphrase []byte) ([]byte, error) {
	header, err := ParseHeader(envelope)
	if err != nil {
		return nil, err
	}
	if header.Version != Version || header.KDFID != KDFArgon2ID {
		return nil, ErrInvalidEnvelope
	}
	params := header.KDFParams
	if err := validateKDFParams(params); err != nil {
		return nil, err
	}
	key := deriveKey(passphrase, header.Salt[:], params)
	defer common.WipeBytes(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, nonceSize)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	headerBytes := envelope[:headerSize]
	ciphertext := envelope[headerSize:]
	plaintext, err := gcm.Open(nil, header.Nonce[:], ciphertext, headerBytes)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}

func ParseHeader(envelope []byte) (Header, error) {
	var header Header
	if len(envelope) < headerSize {
		return header, ErrInvalidEnvelope
	}
	if !IsEnvelope(envelope) {
		return header, ErrInvalidEnvelope
	}
	offset := magicSize
	header.Version = envelope[offset]
	offset += versionSize
	header.KDFID = envelope[offset]
	offset += kdfIDSize
	copy(header.RawKDFParams[:], envelope[offset:offset+kdfParamsSize])
	if header.KDFID == KDFArgon2ID {
		header.KDFParams = KDFParams{
			MemoryKiB:   binary.BigEndian.Uint32(header.RawKDFParams[0:4]),
			Iterations:  binary.BigEndian.Uint32(header.RawKDFParams[4:8]),
			Parallelism: header.RawKDFParams[8],
		}
	}
	offset += kdfParamsSize
	copy(header.Salt[:], envelope[offset:offset+saltSize])
	offset += saltSize
	copy(header.Nonce[:], envelope[offset:offset+nonceSize])
	return header, nil
}

func IsEnvelope(data []byte) bool {
	return len(data) >= magicSize && string(data[:magicSize]) == Magic
}

func build(plaintext []byte, passphrase []byte, random io.Reader) ([]byte, error) {
	if len(passphrase) == 0 {
		return nil, common.NewError("missing_passphrase")
	}
	salt := make([]byte, saltSize)
	nonce := make([]byte, nonceSize)
	defer common.WipeBytes(salt)
	defer common.WipeBytes(nonce)
	if _, err := io.ReadFull(random, salt); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(random, nonce); err != nil {
		return nil, err
	}

	params := defaultKDFParams
	key := deriveKey(passphrase, salt, params)
	defer common.WipeBytes(key)
	if len(key) != keySize {
		return nil, common.NewError("invalid backup envelope key size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, nonceSize)
	if err != nil {
		return nil, err
	}

	header := make([]byte, headerSize)
	copy(header[:magicSize], []byte(Magic))
	offset := magicSize
	header[offset] = Version
	offset += versionSize
	header[offset] = KDFArgon2ID
	offset += kdfIDSize
	binary.BigEndian.PutUint32(header[offset:offset+4], params.MemoryKiB)
	binary.BigEndian.PutUint32(header[offset+4:offset+8], params.Iterations)
	header[offset+8] = params.Parallelism
	offset += kdfParamsSize
	copy(header[offset:offset+saltSize], salt)
	offset += saltSize
	copy(header[offset:offset+nonceSize], nonce)

	envelope := make([]byte, 0, len(header)+len(plaintext)+gcm.Overhead())
	envelope = append(envelope, header...)
	envelope = gcm.Seal(envelope, nonce, plaintext, header)
	return envelope, nil
}

func deriveKey(passphrase []byte, salt []byte, params KDFParams) []byte {
	return argon2.IDKey(passphrase, salt, params.Iterations, params.MemoryKiB, params.Parallelism, keySize)
}

func validateKDFParams(params KDFParams) error {
	if params.MemoryKiB < argon2MemoryKiB || params.MemoryKiB > maximumArgon2MemoryKiB {
		return ErrInvalidEnvelope
	}
	if params.Iterations < argon2Iterations || params.Iterations > maximumArgon2Iterations {
		return ErrInvalidEnvelope
	}
	if params.Parallelism < argon2Parallelism || params.Parallelism > maximumArgon2Parallelism {
		return ErrInvalidEnvelope
	}
	return nil
}
