package paidsub

import (
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in      string
		wantCmd string
		wantArg string
	}{
		{"/start", "/start", ""},
		{"/start@MyBot code123", "/start", "code123"},
		{"/QR", "/qr", ""},
		{"  /stats  ", "/stats", ""},
		{"hello there", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		cmd, arg := parseCommand(tc.in)
		if cmd != tc.wantCmd || arg != tc.wantArg {
			t.Errorf("parseCommand(%q) = (%q,%q), want (%q,%q)", tc.in, cmd, arg, tc.wantCmd, tc.wantArg)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:       "0 B",
		1023:    "1023 B",
		1024:    "1.00 KiB",
		1536:    "1.50 KiB",
		1 << 20: "1.00 MiB",
		1 << 30: "1.00 GiB",
		-5:      "0 B",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	if got := progressBar(0); got != "[░░░░░░░░░░]" {
		t.Errorf("progressBar(0) = %q", got)
	}
	if got := progressBar(100); got != "[██████████]" {
		t.Errorf("progressBar(100) = %q", got)
	}
	if got := progressBar(150); got != "[██████████]" {
		t.Errorf("progressBar(150) clamp = %q", got)
	}
	if got := progressBar(50); got != "[█████░░░░░]" {
		t.Errorf("progressBar(50) = %q", got)
	}
}

func TestPickLang(t *testing.T) {
	if pickLang("ru") != langRU {
		t.Error("ru should map to langRU")
	}
	if pickLang("ru-RU") != langRU {
		t.Error("ru-RU should map to langRU")
	}
	if pickLang("en-US") != langEN {
		t.Error("en-US should map to langEN")
	}
	if pickLang("") != langEN {
		t.Error("empty should map to langEN")
	}
}

func TestChunkText(t *testing.T) {
	if got := chunkText("short", 100); len(got) != 1 || got[0] != "short" {
		t.Errorf("chunkText short = %v", got)
	}
	big := "aaaa\nbbbb\ncccc\ndddd"
	chunks := chunkText(big, 9)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %v", chunks)
	}
	for _, ch := range chunks {
		if len(ch) > 9 {
			t.Errorf("chunk too long: %q", ch)
		}
	}
	// A single line longer than max must be hard-split so no chunk exceeds max.
	for _, ch := range chunkText(strings.Repeat("x", 25), 10) {
		if len([]rune(ch)) > 10 {
			t.Errorf("oversized line not hard-split: %q (%d)", ch, len([]rune(ch)))
		}
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(3, 60)
	now := int64(1000)
	for i := 0; i < 3; i++ {
		if !rl.allow(1, now) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if rl.allow(1, now) {
		t.Fatal("4th request in window should be denied")
	}
	// A different key is independent.
	if !rl.allow(2, now) {
		t.Fatal("different key should be allowed")
	}
	// New window resets the count.
	if !rl.allow(1, now+60) {
		t.Fatal("request after window should be allowed")
	}
}
