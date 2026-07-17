#!/usr/bin/env bash
set -euo pipefail
PREFIX="${PREFIX:-/opt/xpanel}"
BIN_URL="${BIN_URL:-}"
mkdir -p "$PREFIX/data" "$PREFIX/bin"
if [[ -n "$BIN_URL" ]]; then
  curl -fsSL "$BIN_URL" -o "$PREFIX/bin/xpanel-master"
else
  echo "Set BIN_URL to master binary URL, or copy xpanel-master to $PREFIX/bin/"
  exit 1
fi
chmod +x "$PREFIX/bin/xpanel-master"
cat >/etc/systemd/system/xpanel-master.service <<EOF
[Unit]
Description=XPanel Master
After=network.target

[Service]
Type=simple
Environment=ADDR=:8080
Environment=DATA_DIR=$PREFIX/data
Environment=PUBLIC_URL=
Environment=JWT_SECRET=change-me
ExecStart=$PREFIX/bin/xpanel-master -addr \${ADDR} -data \${DATA_DIR}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now xpanel-master
echo "Master installed. Open http://SERVER_IP:8080"
