package providers

import (
	"testing"
)

func TestAmountAddDoesNotUseFloat(t *testing.T) {
	a := mustAmount(t, "0.10")
	b := mustAmount(t, "0.20")
	if got := a.Add(b).String(); got != "0.30000000" {
		t.Fatalf("got %s", got)
	}
	if a.MulInt(3).String() != "0.30000000" {
		t.Fatal(a.MulInt(3))
	}
	n := a.Numeric()
	back, err := amountFromNumeric(n)
	if err != nil || back.String() != "0.10000000" {
		t.Fatalf("%v %v", back, err)
	}
	for _, raw := range []string{"", "-1", "1e-3", "1.000000001", "1.", ".1"} {
		if _, err := ParseAmount(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestCanonicalRequestKeyChanges(t *testing.T) {
	base := CanonicalRequestKey("v", "schema", 10, []byte("in"))
	alts := []string{
		CanonicalRequestKey("v2", "schema", 10, []byte("in")),
		CanonicalRequestKey("v", "other", 10, []byte("in")),
		CanonicalRequestKey("v", "schema", 11, []byte("in")),
		CanonicalRequestKey("v", "schema", 10, []byte("in2")),
	}
	for _, alt := range alts {
		if alt == base {
			t.Fatalf("key reused: %s", alt)
		}
	}
}

func mustAmount(t *testing.T, raw string) Amount {
	t.Helper()
	a, err := ParseAmount(raw)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
