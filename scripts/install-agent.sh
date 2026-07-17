#!/usr/bin/env bash
set -euo pipefail
: "${MASTER_URL:?MASTER_URL required}"
: "${INSTALL_TOKEN:?INSTALL_TOKEN required}"
PREFIX="${PREFIX:-/opt/xpanel-agent}"
BIN_URL="${BIN_URL:-}"
XRAY_BIN="${XRAY_BIN:-xray}"
MODE="${AGENT_MODE:-auto}"
mkdir -p "$PREFIX/data" "$PREFIX/bin"
if [[ -n "$BIN_URL" ]]; then
  curl -fsSL "$BIN_URL" -o "$PREFIX/bin/xpanel-agent"
  chmod +x "$PREFIX/bin/xpanel-agent"
fi
cat >/etc/systemd/system/xpanel-agent.service <<EOF
[Unit]
Description=XPanel Agent
After=network.target

[Service]
Type=simple
Environment=MASTER_URL=$MASTER_URL
Environment=INSTALL_TOKEN=$INSTALL_TOKEN
Environment=DATA_DIR=$PREFIX/data
Environment=XRAY_BIN=$XRAY_BIN
Environment=AGENT_MODE=$MODE
ExecStart=$PREFIX/bin/xpanel-agent -master \${MASTER_URL} -token \${INSTALL_TOKEN} -data \${DATA_DIR} -xray-bin \${XRAY_BIN} -mode \${AGENT_MODE}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now xpanel-agent
echo "Agent installed → $MASTER_URL"
