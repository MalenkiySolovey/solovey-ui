//go:build !minimal

package telegram

import "testing"

func TestTelegramBackupSecretBagZeroesOwnedPassphrase(t *testing.T) {
	passphrase := []byte("correct horse battery staple")
	bag := telegramBackupSecretBag{}
	bag.setPassphrase(passphrase)
	bag.zero()
	for _, value := range passphrase {
		if value != 0 {
			t.Fatalf("passphrase was not zeroed: %q", string(passphrase))
		}
	}
	if bag.passphrase != nil {
		t.Fatal("passphrase should be released after zeroization")
	}
}
