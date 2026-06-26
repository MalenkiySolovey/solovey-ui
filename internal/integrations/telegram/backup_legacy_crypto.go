package telegram

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
)

func EncryptTelegramBackup(plain []byte) ([]byte, []byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	encrypted := make([]byte, 0, len(nonce)+len(plain)+gcm.Overhead())
	encrypted = append(encrypted, nonce...)
	encrypted = gcm.Seal(encrypted, nonce, plain, nil)
	return encrypted, key, nil
}
