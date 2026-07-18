package xraycfg

import "testing"

func TestSanitizeSkipsAnyTLS(t *testing.T) {
	_, _, reason := SanitizeInbound("anytls", 443, map[string]any{
		"password": "x",
	}, map[string]any{"network": "tcp", "security": "tls"})
	if reason == "" {
		t.Fatal("expected anytls to be skipped")
	}
}

func TestSanitizeVLESSOK(t *testing.T) {
	st, sm, reason := SanitizeInbound("vless", 443, map[string]any{
		"clients":    []any{map[string]any{"id": "u1", "email": "a@b.c"}},
		"decryption": "none",
		"bpanelMeta": map[string]any{"publicKey": "pk"},
	}, map[string]any{"network": "tcp", "security": "none"})
	if reason != "" {
		t.Fatal(reason)
	}
	if _, ok := st["bpanelMeta"]; ok {
		t.Fatal("meta not stripped")
	}
	if sm["network"] != "tcp" {
		t.Fatal(sm["network"])
	}
}

func TestSanitizeTLSWithoutCert(t *testing.T) {
	_, _, reason := SanitizeInbound("vless", 443, map[string]any{
		"clients":    []any{map[string]any{"id": "u1"}},
		"decryption": "none",
	}, map[string]any{"network": "ws", "security": "tls", "tlsSettings": map[string]any{"serverName": "a.com"}})
	if reason == "" {
		t.Fatal("expected skip for TLS without cert")
	}
}
