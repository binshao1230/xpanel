package agent

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/xpanel/xpanel/internal/protocol"
)

// queryXrayStats uses `xray api statsquery` when available.
func queryXrayStats(bin string, apiPort int) (protocol.TrafficSnapshot, []protocol.UserTraffic) {
	snap := protocol.TrafficSnapshot{}
	users := []protocol.UserTraffic{}
	if bin == "" || apiPort <= 0 {
		return snap, users
	}
	// xray api statsquery --server=127.0.0.1:10085
	cmd := exec.Command(bin, "api", "statsquery", "--server=127.0.0.1:"+strconv.Itoa(apiPort))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return snap, users
	}
	text := string(out)
	// try JSON first
	var doc struct {
		Stat []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"stat"`
	}
	if err := json.Unmarshal(out, &doc); err == nil && len(doc.Stat) > 0 {
		userMap := map[string]*protocol.UserTraffic{}
		for _, st := range doc.Stat {
			val, _ := strconv.ParseInt(st.Value, 10, 64)
			name := st.Name
			if strings.Contains(name, "inbound>>>") && strings.HasSuffix(name, ">>>traffic>>>uplink") {
				snap.Up += val
			}
			if strings.Contains(name, "inbound>>>") && strings.HasSuffix(name, ">>>traffic>>>downlink") {
				snap.Down += val
			}
			// user>>>email>>>traffic>>>uplink
			if strings.HasPrefix(name, "user>>>") {
				parts := strings.Split(name, ">>>")
				if len(parts) >= 4 {
					email := parts[1]
					ut := userMap[email]
					if ut == nil {
						ut = &protocol.UserTraffic{Email: email}
						userMap[email] = ut
					}
					if parts[len(parts)-1] == "uplink" {
						ut.Up = val
					} else if parts[len(parts)-1] == "downlink" {
						ut.Down = val
					}
				}
			}
		}
		for _, ut := range userMap {
			users = append(users, *ut)
		}
		return snap, users
	}

	// fallback: parse text lines "name: ... value: N"
	re := regexp.MustCompile(`(?m)name:\s*(\S+).*?value:\s*(\d+)`)
	matches := re.FindAllStringSubmatch(text, -1)
	userMap := map[string]*protocol.UserTraffic{}
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		name := m[1]
		val, _ := strconv.ParseInt(m[2], 10, 64)
		if strings.Contains(name, "inbound>>>") && strings.Contains(name, "uplink") {
			snap.Up += val
		}
		if strings.Contains(name, "inbound>>>") && strings.Contains(name, "downlink") {
			snap.Down += val
		}
		if strings.HasPrefix(name, "user>>>") {
			parts := strings.Split(name, ">>>")
			if len(parts) >= 2 {
				email := parts[1]
				ut := userMap[email]
				if ut == nil {
					ut = &protocol.UserTraffic{Email: email}
					userMap[email] = ut
				}
				if strings.Contains(name, "uplink") {
					ut.Up = val
				} else if strings.Contains(name, "downlink") {
					ut.Down = val
				}
			}
		}
	}
	for _, ut := range userMap {
		users = append(users, *ut)
	}
	return snap, users
}
