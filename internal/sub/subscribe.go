package sub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type Node struct {
	Name     string
	Protocol string
	Address  string
	Port     int
	UUID     string
	Password string
	Network  string
	Security string
	SNI      string
	Path     string
	Host     string
	Flow     string
	// raw share link if imported
	ShareLink string
}

func ToV2RayLinks(nodes []Node) string {
	var lines []string
	for _, n := range nodes {
		if n.ShareLink != "" {
			lines = append(lines, n.ShareLink)
			continue
		}
		if link := n.ShareLinkGen(); link != "" {
			lines = append(lines, link)
		}
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
}

func ToClashYAML(nodes []Node) string {
	var b strings.Builder
	b.WriteString("proxies:\n")
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		name := n.displayName()
		names = append(names, name)
		switch n.Protocol {
		case "vless":
			fmt.Fprintf(&b, "  - name: %q\n    type: vless\n    server: %s\n    port: %d\n    uuid: %s\n    network: %s\n    tls: %v\n    udp: true\n",
				name, n.Address, n.Port, n.UUID, defaultNet(n.Network), n.Security == "tls" || n.Security == "reality")
			if n.SNI != "" {
				fmt.Fprintf(&b, "    servername: %s\n", n.SNI)
			}
			if n.Flow != "" {
				fmt.Fprintf(&b, "    flow: %s\n", n.Flow)
			}
		case "vmess":
			fmt.Fprintf(&b, "  - name: %q\n    type: vmess\n    server: %s\n    port: %d\n    uuid: %s\n    alterId: 0\n    cipher: auto\n    network: %s\n    udp: true\n",
				name, n.Address, n.Port, n.UUID, defaultNet(n.Network))
		case "trojan":
			fmt.Fprintf(&b, "  - name: %q\n    type: trojan\n    server: %s\n    port: %d\n    password: %s\n    udp: true\n",
				name, n.Address, n.Port, n.Password)
			if n.SNI != "" {
				fmt.Fprintf(&b, "    sni: %s\n", n.SNI)
			}
		case "shadowsocks", "ss":
			fmt.Fprintf(&b, "  - name: %q\n    type: ss\n    server: %s\n    port: %d\n    cipher: aes-256-gcm\n    password: %s\n    udp: true\n",
				name, n.Address, n.Port, n.Password)
		default:
			if n.ShareLink != "" {
				fmt.Fprintf(&b, "  # skip unsupported for clash auto-gen: %s\n", name)
			}
		}
	}
	b.WriteString("proxy-groups:\n  - name: PROXY\n    type: select\n    proxies:\n")
	if len(names) == 0 {
		b.WriteString("      - DIRECT\n")
	}
	for _, name := range names {
		fmt.Fprintf(&b, "      - %s\n", name)
	}
	b.WriteString("rules:\n  - MATCH,PROXY\n")
	return b.String()
}

// ToSingBox returns a minimal sing-box client outbound list JSON.
func ToSingBox(nodes []Node) string {
	outbounds := []map[string]any{
		{"type": "direct", "tag": "direct"},
		{"type": "block", "tag": "block"},
	}
	tags := []string{}
	for i, n := range nodes {
		tag := fmt.Sprintf("proxy-%d", i+1)
		if n.Name != "" {
			tag = n.Name
		}
		tags = append(tags, tag)
		switch n.Protocol {
		case "vless":
			ob := map[string]any{
				"type":        "vless",
				"tag":         tag,
				"server":      n.Address,
				"server_port": n.Port,
				"uuid":        n.UUID,
			}
			if n.Flow != "" {
				ob["flow"] = n.Flow
			}
			outbounds = append(outbounds, ob)
		case "trojan":
			outbounds = append(outbounds, map[string]any{
				"type": "trojan", "tag": tag, "server": n.Address, "server_port": n.Port, "password": n.Password,
			})
		case "vmess":
			outbounds = append(outbounds, map[string]any{
				"type": "vmess", "tag": tag, "server": n.Address, "server_port": n.Port, "uuid": n.UUID, "security": "auto",
			})
		case "shadowsocks", "ss":
			outbounds = append(outbounds, map[string]any{
				"type": "shadowsocks", "tag": tag, "server": n.Address, "server_port": n.Port,
				"method": "aes-256-gcm", "password": n.Password,
			})
		}
	}
	outbounds = append(outbounds, map[string]any{
		"type": "selector", "tag": "proxy", "outbounds": append(tags, "direct"),
	})
	doc := map[string]any{
		"outbounds": outbounds,
		"route": map[string]any{
			"final": "proxy",
		},
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return string(b)
}

// ToSurge generates a minimal Surge proxy list.
func ToSurge(nodes []Node) string {
	var b strings.Builder
	b.WriteString("[General]\n")
	b.WriteString("loglevel = notify\n\n[Proxy]\n")
	names := []string{}
	for _, n := range nodes {
		name := n.displayName()
		names = append(names, name)
		switch n.Protocol {
		case "trojan":
			fmt.Fprintf(&b, "%s = trojan, %s, %d, password=%s", name, n.Address, n.Port, n.Password)
			if n.SNI != "" {
				fmt.Fprintf(&b, ", sni=%s", n.SNI)
			}
			b.WriteByte('\n')
		case "ss", "shadowsocks":
			fmt.Fprintf(&b, "%s = ss, %s, %d, encrypt-method=aes-256-gcm, password=%s\n", name, n.Address, n.Port, n.Password)
		case "vmess":
			fmt.Fprintf(&b, "%s = vmess, %s, %d, username=%s\n", name, n.Address, n.Port, n.UUID)
		default:
			// vless not native in older surge — skip or as external
			fmt.Fprintf(&b, "# %s type=%s not fully supported in surge profile\n", name, n.Protocol)
		}
	}
	b.WriteString("\n[Proxy Group]\n")
	b.WriteString("PROXY = select")
	for _, n := range names {
		fmt.Fprintf(&b, ", %s", n)
	}
	b.WriteString("\n\n[Rule]\nFINAL,PROXY\n")
	return b.String()
}

func (n Node) displayName() string {
	if n.Name != "" {
		return n.Name
	}
	return fmt.Sprintf("%s-%s-%d", n.Protocol, n.Address, n.Port)
}

func (n Node) ShareLinkGen() string {
	if n.ShareLink != "" {
		return n.ShareLink
	}
	switch n.Protocol {
	case "vless":
		q := url.Values{}
		q.Set("encryption", "none")
		q.Set("type", defaultNet(n.Network))
		if n.Security != "" {
			q.Set("security", n.Security)
		}
		if n.SNI != "" {
			q.Set("sni", n.SNI)
		}
		if n.Path != "" {
			q.Set("path", n.Path)
		}
		if n.Host != "" {
			q.Set("host", n.Host)
		}
		if n.Flow != "" {
			q.Set("flow", n.Flow)
		}
		return fmt.Sprintf("vless://%s@%s:%d?%s#%s", n.UUID, n.Address, n.Port, q.Encode(), url.QueryEscape(n.displayName()))
	case "trojan":
		q := url.Values{}
		if n.SNI != "" {
			q.Set("sni", n.SNI)
		}
		return fmt.Sprintf("trojan://%s@%s:%d?%s#%s", n.Password, n.Address, n.Port, q.Encode(), url.QueryEscape(n.displayName()))
	case "vmess":
		obj := map[string]any{
			"v": "2", "ps": n.displayName(), "add": n.Address, "port": n.Port,
			"id": n.UUID, "aid": 0, "net": defaultNet(n.Network), "type": "none", "tls": "",
		}
		if n.Security == "tls" {
			obj["tls"] = "tls"
		}
		raw, _ := json.Marshal(obj)
		return "vmess://" + base64.StdEncoding.EncodeToString(raw)
	default:
		return ""
	}
}

func defaultNet(n string) string {
	if n == "" {
		return "tcp"
	}
	return n
}

// ParseShareLink extracts a minimal Node from common URI schemes.
func ParseShareLink(link string) (Node, error) {
	link = strings.TrimSpace(link)
	n := Node{ShareLink: link, Name: "imported"}
	switch {
	case strings.HasPrefix(link, "vless://"):
		u, err := url.Parse(link)
		if err != nil {
			return n, err
		}
		n.Protocol = "vless"
		n.UUID = u.User.Username()
		n.Address = u.Hostname()
		fmt.Sscanf(u.Port(), "%d", &n.Port)
		n.Network = u.Query().Get("type")
		n.Security = u.Query().Get("security")
		n.SNI = u.Query().Get("sni")
		n.Flow = u.Query().Get("flow")
		if u.Fragment != "" {
			n.Name, _ = url.QueryUnescape(u.Fragment)
		}
	case strings.HasPrefix(link, "trojan://"):
		u, err := url.Parse(link)
		if err != nil {
			return n, err
		}
		n.Protocol = "trojan"
		n.Password = u.User.Username()
		n.Address = u.Hostname()
		fmt.Sscanf(u.Port(), "%d", &n.Port)
		n.SNI = u.Query().Get("sni")
		if u.Fragment != "" {
			n.Name, _ = url.QueryUnescape(u.Fragment)
		}
	case strings.HasPrefix(link, "vmess://"):
		raw := strings.TrimPrefix(link, "vmess://")
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(raw)
		}
		if err != nil {
			return n, err
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return n, err
		}
		n.Protocol = "vmess"
		n.Name, _ = m["ps"].(string)
		n.Address, _ = m["add"].(string)
		switch p := m["port"].(type) {
		case float64:
			n.Port = int(p)
		case string:
			fmt.Sscanf(p, "%d", &n.Port)
		}
		n.UUID, _ = m["id"].(string)
		n.Network, _ = m["net"].(string)
	default:
		return n, fmt.Errorf("unsupported link scheme")
	}
	return n, nil
}
