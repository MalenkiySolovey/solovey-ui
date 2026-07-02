package envelope

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func TestTelegramBackupEnvelopeRoundTripPayloadSizes(t *testing.T) {
	passphrase := []byte("correct horse battery staple")
	for _, size := range []int{0, 1, 1024, 1 << 20, 50 << 20} {
		t.Run(byteSizeName(size), func(t *testing.T) {
			plaintext := patternedBytes(size)
			envelope, err := Build(plaintext, passphrase)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(envelope, plaintext) {
				t.Fatal("envelope equals plaintext")
			}
			decrypted, err := Open(envelope, passphrase)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decrypted, plaintext) {
				t.Fatalf("decrypted payload mismatch for size %d", size)
			}
			common.WipeBytes(decrypted)
			common.WipeBytes(plaintext)
		})
	}
}

func TestTelegramBackupEnvelopeRejectsTamperingEveryByte(t *testing.T) {
	passphrase := []byte("correct horse battery staple")
	envelope, err := Build([]byte("sqlite"), passphrase)
	if err != nil {
		t.Fatal(err)
	}
	for i := range envelope {
		tampered := append([]byte(nil), envelope...)
		tampered[i] ^= 0x80
		plaintext, err := Open(tampered, passphrase)
		if err == nil {
			common.WipeBytes(plaintext)
			t.Fatalf("tampered byte %d decrypted successfully", i)
		}
	}
}

func TestTelegramBackupEnvelopeWrongPassphraseFails(t *testing.T) {
	envelope, err := Build([]byte("sqlite"), []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := Open(envelope, []byte("wrong horse battery staple"))
	if !errors.Is(err, ErrDecryptionFailed) {
		common.WipeBytes(plaintext)
		t.Fatalf("expected decryption failure, got plaintext=%q err=%v", string(plaintext), err)
	}
}

func TestTelegramBackupEnvelopeDifferentPasswordsDifferWithSameSaltAndNonce(t *testing.T) {
	random := bytes.Repeat([]byte{0x42}, saltSize+nonceSize)
	env1, err := build([]byte("sqlite"), []byte("correct horse battery staple"), bytes.NewReader(random))
	if err != nil {
		t.Fatal(err)
	}
	env2, err := build([]byte("sqlite"), []byte("different horse battery staple"), bytes.NewReader(random))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(env1, env2) {
		t.Fatal("different passphrases produced identical envelopes with identical salt and nonce")
	}
	header1, err := ParseHeader(env1)
	if err != nil {
		t.Fatal(err)
	}
	header2, err := ParseHeader(env2)
	if err != nil {
		t.Fatal(err)
	}
	if header1.Salt != header2.Salt || header1.Nonce != header2.Nonce {
		t.Fatal("test reader did not produce identical salt and nonce")
	}
}

func TestHeaderParsingKnownAndUnknownKDF(t *testing.T) {
	envelope, err := Build([]byte("sqlite"), []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	header, err := ParseHeader(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if header.Version != Version || header.KDFID != KDFArgon2ID {
		t.Fatalf("unexpected header: %#v", header)
	}
	if header.KDFParams != defaultKDFParams {
		t.Fatalf("unexpected KDF params: %#v", header.KDFParams)
	}

	unknown := append([]byte(nil), envelope...)
	unknown[magicSize+versionSize] = 99
	header, err = ParseHeader(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if header.KDFID != 99 {
		t.Fatalf("unexpected unknown KDF id: %d", header.KDFID)
	}
	if _, err := Open(unknown, []byte("correct horse battery staple")); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected invalid envelope for unknown KDF, got %v", err)
	}
}

func TestTelegramBackupKDFMeetsMinimumDuration(t *testing.T) {
	start := time.Now()
	key := deriveKey(
		[]byte("correct horse battery staple"),
		bytes.Repeat([]byte{1}, saltSize),
		defaultKDFParams,
	)
	elapsed := time.Since(start)
	common.WipeBytes(key)
	if elapsed < 100*time.Millisecond {
		t.Fatalf("Argon2id KDF completed too quickly: %s", elapsed)
	}
	t.Logf("Argon2id KDF duration: %s", elapsed)
}

func BenchmarkTelegramBackupKDF(b *testing.B) {
	passphrase := []byte("correct horse battery staple")
	salt := bytes.Repeat([]byte{1}, saltSize)
	for i := 0; i < b.N; i++ {
		key := deriveKey(passphrase, salt, defaultKDFParams)
		if len(key) != keySize {
			b.Fatalf("unexpected key length: %d", len(key))
		}
		common.WipeBytes(key)
	}
}

func patternedBytes(size int) []byte {
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	return buf
}

func byteSizeName(size int) string {
	switch size {
	case 0:
		return "0"
	case 1:
		return "1"
	case 1024:
		return "1KiB"
	case 1 << 20:
		return "1MiB"
	case 50 << 20:
		return "50MiB"
	default:
		return "size"
	}
}

type shortReader struct{}

func (shortReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestTelegramBackupEnvelopeRandomFailure(t *testing.T) {
	if _, err := build([]byte("sqlite"), []byte("correct horse battery staple"), shortReader{}); err == nil {
		t.Fatal("expected random reader failure")
	}
}
