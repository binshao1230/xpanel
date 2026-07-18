package xraycfg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type InboundSpec struct {
	Tag      string
	Protocol string
	Port     int
	Settings map[string]any
	Stream   map[string]any
}

type OutboundSpec struct {
	Tag      string
	Protocol string
	Settings map[string]any
	Stream   map[string]any
}

type RouteSpec struct {
	OutboundTag string
	Domain      []string
	IP          []string
	Port        string
	Network     string
	Protocol    []string
}

type BuildOptions struct {
	Inbounds  []InboundSpec
	Outbounds []OutboundSpec
	Routes    []RouteSpec
	// APIPort enables stats API on 127.0.0.1 (0 = default 10085, -1 disable)
	APIPort int
}

// Build generates xray config with stats API, custom outbounds and routes.
func Build(opts BuildOptions) (map[string]any, string, error) {
	apiPort := opts.APIPort
	if apiPort == 0 {
		apiPort = 10085
	}

	inList := make([]map[string]any, 0, len(opts.Inbounds)+1)
	for i, in := range opts.Inbounds {
		tag := in.Tag
		if tag == "" {
			tag = fmt.Sprintf("in-%d", i+1)
		}
		proto := strings.ToLower(in.Protocol)
		if proto == "ss" {
			proto = "shadowsocks"
		}
		if proto == "dokodemo" || proto == "tunnel" {
			proto = "dokodemo-door"
		}
		if proto == "hy2" || proto == "hysteria2" {
			proto = "hysteria"
		}
		settings := in.Settings
		if settings == nil {
			settings = defaultSettings(proto)
		}
		stream := in.Stream
		if stream == nil {
			stream = map[string]any{"network": "tcp"}
		}
		// strip panel meta again (safety)
		delete(settings, "bpanelMeta")
		delete(settings, "xpanelMeta")

		item := map[string]any{
			"tag":            tag,
			"protocol":       proto,
			"port":           in.Port,
			"settings":       settings,
			"streamSettings": stream,
		}
		if ShouldSniff(proto) {
			item["sniffing"] = map[string]any{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
			}
		}
		inList = append(inList, item)
	}

	outList := []map[string]any{
		{"protocol": "freedom", "tag": "direct", "settings": map[string]any{}},
		{"protocol": "blackhole", "tag": "block", "settings": map[string]any{}},
	}
	for _, ob := range opts.Outbounds {
		item := map[string]any{
			"tag":      ob.Tag,
			"protocol": ob.Protocol,
			"settings": ob.Settings,
		}
		if ob.Settings == nil {
			item["settings"] = map[string]any{}
		}
		if ob.Stream != nil {
			item["streamSettings"] = ob.Stream
		}
		outList = append(outList, item)
	}

	// avoid geosite/geoip dependencies in default rules
	rules := []map[string]any{}
	if apiPort > 0 {
		rules = append([]map[string]any{{
			"type":        "field",
			"inboundTag":  []string{"api"},
			"outboundTag": "api",
		}}, rules...)
	}
	for _, r := range opts.Routes {
		rule := map[string]any{
			"type":        "field",
			"outboundTag": r.OutboundTag,
		}
		if len(r.Domain) > 0 {
			rule["domain"] = r.Domain
		}
		if len(r.IP) > 0 {
			rule["ip"] = r.IP
		}
		if r.Port != "" {
			rule["port"] = r.Port
		}
		if r.Network != "" {
			rule["network"] = r.Network
		}
		if len(r.Protocol) > 0 {
			rule["protocol"] = r.Protocol
		}
		rules = append(rules, rule)
	}

	cfg := map[string]any{
		"log": map[string]any{
			"loglevel": "warning",
		},
		"inbounds":  inList,
		"outbounds": outList,
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules":          rules,
		},
	}

	if apiPort > 0 {
		// dokodemo API inbound
		inList = append([]map[string]any{{
			"listen":   "127.0.0.1",
			"port":     apiPort,
			"protocol": "dokodemo-door",
			"settings": map[string]any{"address": "127.0.0.1"},
			"tag":      "api",
		}}, inList...)
		cfg["inbounds"] = inList
		cfg["stats"] = map[string]any{}
		cfg["api"] = map[string]any{
			"tag":      "api",
			"services": []string{"StatsService", "HandlerService"},
		}
		cfg["policy"] = map[string]any{
			"levels": map[string]any{
				"0": map[string]any{
					"statsUserUplink":   true,
					"statsUserDownlink": true,
				},
			},
			"system": map[string]any{
				"statsInboundUplink":    true,
				"statsInboundDownlink":  true,
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		}
		// api outbound
		outList = append(outList, map[string]any{"protocol": "freedom", "tag": "api", "settings": map[string]any{}})
		// fix: api outbound should be special - xray uses routing to api tag as built-in
		// Actually for Xray, outbound tag "api" is reserved when using api config - no need freedom.
		// Revert extra freedom api outbound - use empty outbounds for api via routing only.
		outList = outList[:len(outList)-1]
		cfg["outbounds"] = outList
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return cfg, hex.EncodeToString(sum[:]), nil
}

// BuildSimple keeps backward-compatible helper.
func BuildSimple(inbounds []InboundSpec) (map[string]any, string, error) {
	return Build(BuildOptions{Inbounds: inbounds})
}

func defaultSettings(protocol string) map[string]any {
	switch protocol {
	case "vless":
		return map[string]any{"clients": []map[string]any{}, "decryption": "none"}
	case "vmess":
		return map[string]any{"clients": []map[string]any{}}
	case "trojan":
		return map[string]any{"clients": []map[string]any{}}
	case "shadowsocks":
		return map[string]any{"method": "aes-256-gcm", "password": "", "network": "tcp,udp"}
	default:
		return map[string]any{}
	}
}

func DefaultVLESSClient(uuid, email string) map[string]any {
	return map[string]any{"id": uuid, "email": email, "flow": ""}
}

// RealityStream builds a basic reality streamSettings skeleton.
func RealityStream(dest, serverNames string, shortIDs []string, publicKey, privateKey string) map[string]any {
	return map[string]any{
		"network":  "tcp",
		"security": "reality",
		"realitySettings": map[string]any{
			"show":        false,
			"dest":        dest,
			"xver":        0,
			"serverNames": []string{serverNames},
			"privateKey":  privateKey,
			"shortIds":    shortIDs,
		},
	}
}

func MustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
