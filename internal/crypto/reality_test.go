package crypto

import "testing"

func TestX25519PairAndPublicFromPrivate(t *testing.T) {
	priv, pub, err := X25519Pair()
	if err != nil {
		t.Fatal(err)
	}
	if priv == "" || pub == "" {
		t.Fatal("empty keys")
	}
	derived, err := PublicFromPrivate(priv)
	if err != nil {
		t.Fatal(err)
	}
	if derived != pub {
		t.Fatalf("public mismatch:\n got %s\nwant %s", derived, pub)
	}
}

func TestPublicFromPrivateInvalid(t *testing.T) {
	if _, err := PublicFromPrivate("not-a-key"); err == nil {
		t.Fatal("expected error")
	}
}
