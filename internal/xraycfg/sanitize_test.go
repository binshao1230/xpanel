package xraycfg

import (
	"strings"
	"testing"
)

func TestSanitizeRealityStripsFingerprintAndNormalizesDest(t *testing.T) {
	settings := map[string]any{
		"clients":    []any{map[string]any{"id": "u1", "flow": "xtls-rprx-vision"}},
		"decryption": "none",
		"bpanelMeta": map[string]any{"publicKey": "pk"},
	}
	stream := map[string]any{
		"network":  "ws", // should force tcp for vision
		"security": "reality",
		"realitySettings": map[string]any{
			"dest":        "www.microsoft.com", // no port
			"privateKey":  "abcPRIVATE",
			"serverNames": []any{"www.microsoft.com"},
			"shortIds":    []any{"a1b2c3d4", ""},
			"fingerprint": "chrome",
			"publicKey":   "should-go",
		},
	}
	st, sm, skip := SanitizeInbound("vless", 443, settings, stream)
	if skip != "" {
		t.Fatalf("skip: %s", skip)
	}
	if st["bpanelMeta"] != nil {
		t.Fatal("bpanelMeta should be stripped")
	}
	if sm["network"] != "tcp" {
		t.Fatalf("vision must use tcp, got %v", sm["network"])
	}
	rs := sm["realitySettings"].(map[string]any)
	if _, ok := rs["fingerprint"]; ok {
		t.Fatal("fingerprint must not be on server")
	}
	if _, ok := rs["publicKey"]; ok {
		t.Fatal("publicKey must not be on server")
	}
	if rs["dest"] != "www.microsoft.com:443" {
		t.Fatalf("dest normalize failed: %v", rs["dest"])
	}
}

func TestBuildRealitySniffRouteOnly(t *testing.T) {
	cfg, _, err := Build(BuildOptions{Inbounds: []InboundSpec{{
		Tag: "r", Protocol: "vless", Port: 443,
		Settings: map[string]any{
			"clients":    []map[string]any{{"id": "u", "flow": "xtls-rprx-vision"}},
			"decryption": "none",
		},
		Stream: map[string]any{
			"network":  "tcp",
			"security": "reality",
			"realitySettings": map[string]any{
				"dest": "www.microsoft.com:443", "privateKey": "k",
				"serverNames": []string{"www.microsoft.com"}, "shortIds": []string{"ab"},
			},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	ins := cfg["inbounds"].([]map[string]any)
	var found map[string]any
	for _, in := range ins {
		if in["tag"] == "r" {
			found = in
			break
		}
	}
	if found == nil {
		t.Fatal("inbound missing")
	}
	sn := found["sniffing"].(map[string]any)
	if sn["routeOnly"] != true {
		t.Fatalf("expected routeOnly for reality, got %#v", sn)
	}
}

func TestNormalizeShortIDs(t *testing.T) {
	out := normalizeShortIDs([]any{"A1b2", "zz", "", "a1b2"})
	if len(out) < 2 || out[0] != "a1b2" {
		t.Fatalf("got %v", out)
	}
	// last should be empty optional
	if out[len(out)-1] != "" {
		t.Fatalf("expected trailing empty, got %v", out)
	}
	if strings.Contains(strings.Join(out, ","), "zz") {
		t.Fatal("non-hex should be dropped")
	}
}
