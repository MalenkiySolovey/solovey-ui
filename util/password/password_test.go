package password

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestPolicyUnicodeByteAndBlocklistBoundaries(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		value string
		err   error
	}{
		"minimum":        {value: "fifteen chars ok"},
		"spaces":         {value: "spaces are allowed here"},
		"unicode":        {value: strings.Repeat("соловей", 3)},
		"64 ASCII":       {value: strings.Repeat("a", 64)},
		"64 four-byte":   {value: strings.Repeat("🕊", 64)},
		"too few":        {value: "short password", err: ErrPasswordTooShort},
		"too many bytes": {value: strings.Repeat("🕊", 65), err: ErrPasswordTooLong},
		"blocked":        {value: "PasswordPassword", err: ErrPasswordBlocklist},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateNew(test.value)
			if !errors.Is(err, test.err) {
				t.Fatalf("ValidateNew() error = %v, want %v (runes=%d bytes=%d)", err, test.err, utf8.RuneCountInString(test.value), len(test.value))
			}
		})
	}
}

func TestArgon2PHCRoundTripAndStrictCostParser(t *testing.T) {
	hash, err := Hash(t.Context(), "a password value")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatalf("unexpected PHC: %q", hash)
	}
	valid, rehash, err := Verify(t.Context(), hash, "a password value")
	if err != nil || !valid || rehash {
		t.Fatalf("Verify() = %v, %v, %v", valid, rehash, err)
	}
	valid, _, err = Verify(t.Context(), hash, "wrong")
	if err != nil || valid {
		t.Fatalf("wrong password Verify() = %v, %v", valid, err)
	}
	for _, malformed := range []string{
		"$argon2id$v=19$m=131072,t=3,p=1$c2FsdHNhbHRzYWx0MTIzNA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=20$m=65536,t=3,p=1$c2FsdHNhbHRzYWx0MTIzNA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0MTIzNA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$m=65536,t=3,p=1$bad$bad",
	} {
		if _, _, err := Verify(t.Context(), malformed, "password"); err == nil {
			t.Fatalf("malformed/excessive PHC accepted: %q", malformed)
		}
	}
}

func TestDerivationAdmissionHasTwoSlotsAndNoWaitQueue(t *testing.T) {
	held := make([]func(), 0, 2)
	for range 2 {
		release, err := acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, release)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := acquire(context.Background()); !errors.Is(err, ErrBusy) {
			t.Errorf("third acquire error = %v, want ErrBusy", err)
		}
	}()
	wg.Wait()
	for _, release := range held {
		release()
	}
}

func BenchmarkArgon2idCurrentProfileVerification(b *testing.B) {
	hash, err := Hash(context.Background(), "benchmark password value")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ReportMetric(float64(ArgonMemoryKiB)/1024, "MiB/derivation")
	b.ResetTimer()
	for range b.N {
		valid, needsRehash, verifyErr := Verify(context.Background(), hash, "benchmark password value")
		if verifyErr != nil || !valid || needsRehash {
			b.Fatalf("Verify() = %v, %v, %v", valid, needsRehash, verifyErr)
		}
	}
}
