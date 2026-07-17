package xraycfg

import "github.com/xpanel/xpanel/internal/acme"

// ApplyTLSFiles sets streamSettings to use TLS with certificate files (agent paths).
func ApplyTLSFiles(stream map[string]any, domain string) map[string]any {
	if stream == nil {
		stream = map[string]any{"network": "tcp"}
	}
	certFile, keyFile := acme.AgentRelativePaths(domain)
	stream["security"] = "tls"
	stream["tlsSettings"] = map[string]any{
		"serverName": domain,
		"certificates": []map[string]any{
			{
				"certificateFile": certFile,
				"keyFile":         keyFile,
			},
		},
	}
	return stream
}

// MergeTLSIfNeeded applies cert if stream asks for tls or certDomain is set.
func MergeTLSIfNeeded(stream map[string]any, certDomain string) map[string]any {
	if certDomain == "" {
		return stream
	}
	if stream == nil {
		stream = map[string]any{"network": "tcp"}
	}
	sec, _ := stream["security"].(string)
	// Reality keeps its own security; only inject for empty/none/tls
	if sec == "reality" {
		return stream
	}
	return ApplyTLSFiles(stream, certDomain)
}
