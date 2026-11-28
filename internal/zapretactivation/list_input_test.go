package zapretactivation

import (
	"errors"
	"slices"
	"testing"
)

func TestPackedListChecksExactStreamAndWorkspace(t *testing.T) {
	lines := []string{"alpha.example.test", "beta.example.test"}
	packed, err := packList(lines)
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(size, count int) error {
		if size != 37 || count != len(lines) {
			t.Fatalf("unexpected reservation: %d bytes and %d lines", size, count)
		}
		return nil
	}
	if got, err := packed.Expand(reserve); err != nil || !slices.Equal(got, lines) {
		t.Fatalf("list did not round trip: %v, %v", got, err)
	}
	guard := errors.New("workspace denied")
	if _, err := packed.Expand(func(int, int) error { return guard }); !errors.Is(err, guard) {
		t.Fatalf("did not honor the reservation: %v", err)
	}
	for name, damage := range map[string]func(*PackedList){
		"longer":          func(p *PackedList) { p.Bytes-- },
		"shorter":         func(p *PackedList) { p.Bytes++ },
		"wrong count":     func(p *PackedList) { p.Lines++ },
		"trailing stream": func(p *PackedList) { p.Gzip = append(slices.Clone(p.Gzip), p.Gzip...) },
		"checksum":        func(p *PackedList) { p.Gzip = slices.Clone(p.Gzip); p.Gzip[len(p.Gzip)-8] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			altered := packed
			damage(&altered)
			if _, err := altered.Expand(func(int, int) error { return nil }); err == nil {
				t.Fatal("accepted a list that does not match its envelope")
			}
		})
	}
}
