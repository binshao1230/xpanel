#!/usr/bin/env bash
set -euo pipefail
PREFIX="${PREFIX:-/opt/bpanel}"
BIN_URL="${BIN_URL:-}"
mkdir -p "$PREFIX/data" "$PREFIX/bin"
if [[ -n "$BIN_URL" ]]; then
  curl -fsSL "$BIN_URL" -o "$PREFIX/bin/bpanel-master"
else
  echo "Set BIN_URL to master binary URL, or copy bpanel-master to $PREFIX/bin/"
  exit 1
fi
chmod +x "$PREFIX/bin/bpanel-master"
cat >/etc/systemd/system/bpanel-master.service <<EOF
[Unit]
Description=BPanel Master
After=network.target

[Service]
Type=simple
Environment=ADDR=:8080
Environment=DATA_DIR=$PREFIX/data
Environment=PUBLIC_URL=
Environment=JWT_SECRET=change-me
ExecStart=$PREFIX/bin/bpanel-master -addr \${ADDR} -data \${DATA_DIR}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now bpanel-master
echo "Master installed. Open http://SERVER_IP:8080"
