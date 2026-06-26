package service

import "github.com/MalenkiySolovey/solovey-ui/util/common"

type telegramBackupSecretBag struct {
	payload    []byte
	passphrase []byte
}

func (b *telegramBackupSecretBag) setPayload(payload []byte) {
	b.zeroPayload()
	b.payload = payload
}

func (b *telegramBackupSecretBag) setPassphrase(passphrase []byte) {
	b.zeroPassphrase()
	b.passphrase = passphrase
}

func (b *telegramBackupSecretBag) zeroPayload() {
	common.WipeBytes(b.payload)
	b.payload = nil
}

func (b *telegramBackupSecretBag) zeroPassphrase() {
	common.WipeBytes(b.passphrase)
	b.passphrase = nil
}

func (b *telegramBackupSecretBag) zero() {
	b.zeroPassphrase()
	b.zeroPayload()
}
