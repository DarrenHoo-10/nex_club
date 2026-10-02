package ingest

import "testing"

func TestContentHashV1(t *testing.T) {
	if ContentHash("Hello   World", "ａ", "x") != ContentHash("Hello World", "a", "x") {
		t.Fatal("whitespace and NFKC did not collapse")
	}
	if ContentHash("Hello   World", "", "") == ContentHash("hello   world", "", "") {
		t.Fatal("case was folded")
	}
	if ContentHash("ab", "c", "") == ContentHash("a", "bc", "") {
		t.Fatal("fields collided")
	}
	if ContentHash("A", "B", "C") == "" || NormalizationVersion != "hash.v2" {
		t.Fatal("hash empty")
	}
}
