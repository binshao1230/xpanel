package xraycfg

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StockProtocols are known to work with official Xray-core JSON config.
var StockProtocols = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
	"socks": true, "http": true, "mixed": true,
	"dokodemo-door": true, "tunnel": true,
	"wireguard": true, "hysteria": true,
	// anytls is NOT in official Xray loader yet — including it breaks entire config
}

// DeepCopyMap returns a deep copy via JSON round-trip.
func DeepCopyMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	b, err := json.Marshal(in)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// SanitizeInbound prepares settings/stream for Xray core.
// Returns skipReason non-empty if this inbound must not be deployed (would break whole config).
func SanitizeInbound(protocol string, port int, settings, stream map[string]any) (map[string]any, map[string]any, string) {
	proto := strings.ToLower(strings.TrimSpace(protocol))
	if proto == "ss" {
		proto = "shadowsocks"
	}
	if proto == "dokodemo" || proto == "tunnel" {
		proto = "dokodemo-door"
	}
	if proto == "hy2" || proto == "hysteria2" {
		proto = "hysteria"
	}

	if !StockProtocols[proto] {
		return nil, nil, fmt.Sprintf("protocol %q not supported by stock Xray (skipped so other nodes keep working)", protocol)
	}
	if port <= 0 || port > 65535 {
		return nil, nil, fmt.Sprintf("invalid port %d", port)
	}

	st := DeepCopyMap(settings)
	sm := DeepCopyMap(stream)
	delete(st, "bpanelMeta")
	delete(st, "xpanelMeta")

	// normalize stream network aliases
	if sm == nil {
		sm = map[string]any{}
	}
	netw, _ := sm["network"].(string)
	netw = strings.ToLower(netw)
	switch netw {
	case "", "raw":
		sm["network"] = "tcp"
	case "ws":
		sm["network"] = "ws"
	case "xhttp":
		sm["network"] = "splithttp"
	}

	sec, _ := sm["security"].(string)
	sec = strings.ToLower(sec)
	if sec == "" {
		sec = "none"
		sm["security"] = "none"
	}

	// TLS without certificates will fail xray -test — demote or skip
	if sec == "tls" {
		hasCert := false
		if tls, ok := sm["tlsSettings"].(map[string]any); ok {
			if certs, ok := tls["certificates"].([]any); ok && len(certs) > 0 {
				hasCert = true
			}
			// certificateFile form
			if !hasCert {
				if _, ok := tls["certificateFile"]; ok {
					hasCert = true
				}
			}
		}
		if !hasCert {
			// public proxy protocols need real cert; demoting to none makes them insecure but xray runs
			// for vless/trojan/anytls-like, better skip so operator notices
			switch proto {
			case "vless", "vmess", "trojan", "hysteria":
				return nil, nil, "TLS selected but no certificate bound — pick a cert in ACME or use Reality"
			default:
				sm["security"] = "none"
				delete(sm, "tlsSettings")
			}
		}
	}

	// Reality only with tcp/raw/grpc/splithttp
	if sec == "reality" {
		n, _ := sm["network"].(string)
		switch strings.ToLower(n) {
		case "tcp", "raw", "grpc", "splithttp", "xhttp", "":
			// ok
		default:
			sm["network"] = "tcp"
		}
		if rs, ok := sm["realitySettings"].(map[string]any); ok {
			// remove client-only fields that some versions reject on server
			// keep privateKey, shortIds, serverNames, dest
			delete(rs, "publicKey")
			// fingerprint is used by client; server may ignore — keep unless empty
			if pk, _ := rs["privateKey"].(string); pk == "" || strings.HasPrefix(pk, "REPLACE") {
				return nil, nil, "Reality privateKey missing"
			}
		} else {
			return nil, nil, "Reality stream missing realitySettings"
		}
	}

	switch proto {
	case "vless":
		if _, ok := st["decryption"]; !ok {
			st["decryption"] = "none"
		}
		if !hasClients(st) {
			return nil, nil, "vless has no clients"
		}
	case "vmess", "trojan":
		if !hasClients(st) {
			return nil, nil, proto + " has no clients"
		}
	case "shadowsocks":
		if pw, _ := st["password"].(string); pw == "" {
			return nil, nil, "shadowsocks password empty"
		}
		if _, ok := st["method"]; !ok {
			st["method"] = "aes-256-gcm"
		}
	case "hysteria":
		st["version"] = 2
		if !hasHyUsers(st) {
			return nil, nil, "hysteria has no users"
		}
		sm["network"] = "hysteria"
		if sec == "none" || sec == "" {
			return nil, nil, "hysteria requires TLS certificate"
		}
	case "wireguard":
		if sk, _ := st["secretKey"].(string); sk == "" {
			return nil, nil, "wireguard secretKey empty"
		}
		// empty peers is allowed (server waiting) for some versions; if fails, operator fills peers
	case "socks", "mixed":
		// ok
	case "http":
		// ok
	case "dokodemo-door":
		if _, ok := st["address"]; !ok {
			st["address"] = "127.0.0.1"
		}
	}

	// sniffing-hostile protocols: leave as-is; Build() adds sniffing globally
	return st, sm, ""
}

func hasClients(st map[string]any) bool {
	if c, ok := st["clients"].([]any); ok && len(c) > 0 {
		return true
	}
	// after typed marshal sometimes []map
	if c, ok := st["clients"].([]map[string]any); ok && len(c) > 0 {
		return true
	}
	return false
}

func hasHyUsers(st map[string]any) bool {
	if u, ok := st["users"].([]any); ok && len(u) > 0 {
		return true
	}
	if u, ok := st["clients"].([]any); ok && len(u) > 0 {
		return true
	}
	return false
}

// ShouldSniff returns whether sniffing should be attached for this protocol.
func ShouldSniff(protocol string) bool {
	switch strings.ToLower(protocol) {
	case "wireguard", "hysteria", "tun":
		return false
	default:
		return true
	}
}
