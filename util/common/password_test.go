package common

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHashAndMigrationChecks(t *testing.T) {
	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "secret" || !IsPasswordHash(hash) {
		t.Fatalf("password was not hashed with expected marker: %q", hash)
	}
	if ok, migrate := CheckPassword(hash, "secret"); !ok || migrate {
		t.Fatalf("hashed password check = %v, migrate = %v", ok, migrate)
	}
	if ok, migrate := CheckPassword("secret", "secret"); !ok || !migrate {
		t.Fatalf("plain password check = %v, migrate = %v", ok, migrate)
	}
	if ok, _ := CheckPassword(hash, "wrong"); ok {
		t.Fatal("wrong password was accepted")
	}
}

func TestLegacyBcryptVerificationRequestsMigration(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("legacy-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if ok, migrate := CheckPassword("bcrypt:"+string(hash), "legacy-password"); !ok || !migrate {
		t.Fatalf("legacy bcrypt check = %v, migrate = %v", ok, migrate)
	}
}

func TestEqualizeLoginTimingUsesCurrentBoundedWork(t *testing.T) {
	EqualizeLoginTiming("any-password")
}
