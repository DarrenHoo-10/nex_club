package push

import (
	"strings"
	"testing"
)

func TestBearerTokenKeepsExactBytes(t *testing.T) {
	token, ok := BearerToken("bearer nex_abc ")
	if !ok || token != "nex_abc " {
		t.Fatalf("token %q ok %v", token, ok)
	}
	if _, ok := BearerToken("Basic abc"); ok {
		t.Fatal("accepted a non-bearer scheme")
	}
	if TokenHash("nex_abc") == TokenHash("nex_abc ") {
		t.Fatal("trailing space changed the hash")
	}
	if len(TokenHash("nex_abc")) != 64 || strings.ToLower(TokenHash("nex_abc")) != TokenHash("nex_abc") {
		t.Fatal("hash is not lowercase hex")
	}
}
