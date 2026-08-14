package provider

import (
	"testing"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
)

func TestCryptoBotPaymentMatchIsExactAndFailClosed(t *testing.T) {
	order := paid.PaymentOrder{Amount: 12345, Currency: "RUB"}
	for _, value := range []string{"123.45", "123.450", "1.2345e2", "NaN", "", "-123.45", "123."} {
		want := value == "123.45"
		if got := cryptoBotPaymentMatches(order, value, "RUB"); got != want {
			t.Fatalf("cryptoBotPaymentMatches(%q) = %v, want %v", value, got, want)
		}
	}
	if cryptoBotPaymentMatches(order, "123.45", "") {
		t.Fatal("missing provider currency must be rejected")
	}
	if cryptoBotPaymentMatches(order, "123.45", "USD") {
		t.Fatal("mismatched provider currency must be rejected")
	}
}

func TestParseDecimalMinorUnits(t *testing.T) {
	for value, want := range map[string]int64{"0": 0, "0.01": 1, "1.2": 120, "123.45": 12345} {
		got, err := parseDecimalMinorUnits(value)
		if err != nil || got != want {
			t.Fatalf("parseDecimalMinorUnits(%q) = %d, %v; want %d", value, got, err, want)
		}
	}
	for _, value := range []string{"", ".1", "1.", "-1", "+1", "1.001", "1e2", "92233720368547759"} {
		if _, err := parseDecimalMinorUnits(value); err == nil {
			t.Fatalf("parseDecimalMinorUnits(%q) should fail", value)
		}
	}
}
