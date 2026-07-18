package master

import (
	"encoding/json"
	"testing"

	xpcrypto "github.com/xpanel/xpanel/internal/crypto"
)

func TestComposeRealityDoesNotMismatchKeys(t *testing.T) {
	s := &ServerApp{}
	priv, pub, err := xpcrypto.X25519Pair()
	if err != nil {
		t.Fatal(err)
	}
	body := &inboundForm{
		Protocol:   "vless",
		Network:    "tcp",
		Security:   "reality",
		PrivateKey: priv,
		// PublicKey intentionally empty — must derive from private, not invent a new pair half
		SNI:  "www.microsoft.com",
		Dest: "www.microsoft.com:443",
	}
	body.Settings = map[string]any{
		"clients":    []map[string]any{{"id": "11111111-1111-1111-1111-111111111111", "flow": "xtls-rprx-vision"}},
		"decryption": "none",
	}
	stream, err := s.composeInboundStream(body)
	if err != nil {
		t.Fatal(err)
	}
	rs := stream["realitySettings"].(map[string]any)
	if rs["privateKey"] != priv {
		t.Fatalf("private key changed")
	}
	meta := body.Settings["xpanelMeta"].(map[string]any)
	if meta["publicKey"] != pub {
		t.Fatalf("public key mismatch: got %v want %s", meta["publicKey"], pub)
	}
}

func TestFillProtocolDefaultsKeepsExistingClients(t *testing.T) {
	s := &ServerApp{}
	body := &inboundForm{
		Protocol: "vless",
		Settings: map[string]any{
			"clients":    []map[string]any{{"id": "keep-me", "email": "a@b.c"}},
			"decryption": "none",
		},
	}
	s.fillProtocolDefaults(body)
	clients := body.Settings["clients"].([]map[string]any)
	if clients[0]["id"] != "keep-me" {
		t.Fatalf("clients were regenerated: %v", clients)
	}
}

func TestJSONRoundTripSettings(t *testing.T) {
	// ensure xpanelMeta survives marshal for share links
	settings := map[string]any{
		"clients":    []any{map[string]any{"id": "u1"}},
		"decryption": "none",
		"xpanelMeta": map[string]any{"publicKey": "pk", "shortId": "abcd"},
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	meta := back["xpanelMeta"].(map[string]any)
	if meta["publicKey"] != "pk" {
		t.Fatal("meta lost")
	}
}
