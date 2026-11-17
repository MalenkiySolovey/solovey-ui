package credentials

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSudokuCanonicalSplitKeysBelongToOneMaster(t *testing.T) {
	master, err := GenerateSudokuMaster()
	if err != nil {
		t.Fatal("master generation failed")
	}
	public, err := SudokuPublicKey(master)
	if err != nil || len(public) != 64 {
		t.Fatal("invalid public key")
	}
	other, _ := GenerateSudokuMaster()
	first, err := SplitSudokuMaster(master)
	second, err2 := SplitSudokuMaster(master)
	if err != nil || err2 != nil || first == second || !SudokuSplitMatches(first, master) || !SudokuSplitMatches(second, master) || SudokuSplitMatches(first, other) {
		t.Fatal("split keys lost their master or randomness")
	}
	normalized, err := NormalizeSudokuSplit(" " + strings.ToUpper(first) + " ")
	if err != nil || normalized != first {
		t.Fatal("canonical split normalization failed")
	}
	// Scalar order L is not canonical; zero total key is not a usable master.
	order := "edd3f55c1a631258d69cf7a2def9de1400000000000000000000000000000010"
	for _, value := range []string{"", "private-sentinel", strings.Repeat("0", 64), order, strings.Repeat("f", 64), first} {
		if _, err := SudokuPublicKey(value); err == nil || strings.Contains(err.Error(), value) && value != "" {
			t.Fatal("invalid scalar accepted or disclosed")
		}
	}
	for _, value := range []string{master, strings.Repeat("0", 128), order + first[64:], first[:64] + order, "private-sentinel"} {
		if _, err := NormalizeSudokuSplit(value); err == nil {
			t.Fatal("invalid split accepted")
		}
	}
	bag, _ := json.Marshal(map[string]string{"17": first, "23": second})
	if keys, err := SudokuKeyBag(string(bag)); err != nil || len(keys) != 2 {
		t.Fatal("canonical key bundle rejected")
	}
	for _, value := range []string{`null`, `[]`, `{"01":"` + first + `"}`, `{"0":"` + first + `"}`, `{"record:17":"` + first + `"}`, `{"17":"` + first + `","17":"` + second + `"}`, `{"17":"private-sentinel"}`} {
		if _, err := SudokuKeyBag(value); err == nil {
			t.Fatal("ambiguous or malformed delivery bundle accepted")
		}
	}
}
