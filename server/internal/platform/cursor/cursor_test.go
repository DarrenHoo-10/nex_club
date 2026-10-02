package cursor

import (
	"bytes"
	"testing"
)

func TestSignAndVerifyRoundTrip(t *testing.T) {
	current := bytes.Repeat([]byte{1}, 32)
	previous := bytes.Repeat([]byte{2}, 32)
	payload := []byte(`{"kid":"k1","id":"abc"}`)
	signer := NewSigner("k2", current, map[string][]byte{"k1": previous})
	token := signer.Sign(payload)
	got, err := signer.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload bytes changed: %q", got)
	}
	old := NewSigner("k1", previous, nil)
	oldToken := old.Sign(payload)
	got, err = signer.Verify(oldToken)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("previous key did not verify the original bytes")
	}
	if _, err := NewSigner("k2", current, nil).Verify(oldToken); err == nil {
		t.Fatal("token verified without the previous key")
	}
	if _, err := signer.Verify(token + "x"); err == nil {
		t.Fatal("tampered token verified")
	}
}
