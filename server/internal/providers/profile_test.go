package providers

import "testing"

func TestProfileCannotChangeUnderSameVersion(t *testing.T) {
	s := New(nil, nil, nil)
	p := Profile{Version: "v1", ProviderKey: "test", Model: "model-a", Currency: "USD", InputPerToken: "0.01", OutputPerToken: "0.02", MaxOutputTokens: 100}
	if err := s.RegisterProfile(p); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterProfile(p); err != nil {
		t.Fatal(err)
	}
	a := ProfileFingerprint(p, "https://example.com/v1")
	p.Model = "model-b"
	if err := s.RegisterProfile(p); err == nil {
		t.Fatal("immutable profile overwritten")
	}
	if a == ProfileFingerprint(p, "https://example.com/v1") {
		t.Fatal("model change reused version")
	}
	if ProfileFingerprint(p, "https://example.com/v1") == ProfileFingerprint(p, "https://other.example/v1") {
		t.Fatal("endpoint change reused version")
	}
}
