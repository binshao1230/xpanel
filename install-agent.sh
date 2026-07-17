#!/usr/bin/env bash
# XPanel Agent 一键安装（Linux）
# 用法:
#   curl -sL https://raw.githubusercontent.com/binshao1230/xpanel/main/install-agent.sh | \
#     sudo bash -s -- -m http://主控IP:8080 -t INSTALL_TOKEN
# 可选: -x /usr/local/bin/xray  -M auto|websocket|http  --with-xray
set -euo pipefail

REPO="${XPANEL_REPO:-binshao1230/xpanel}"
APP_NAME="xpanel-agent"
INSTALL_DIR="${XPANEL_AGENT_DIR:-/opt/xpanel-agent}"
BIN_PATH="${INSTALL_DIR}/bin/${APP_NAME}"
DATA_DIR="${INSTALL_DIR}/data"
SERVICE_NAME="xpanel-agent"
GITHUB_API="https://api.github.com/repos/${REPO}"

MASTER_URL=""
INSTALL_TOKEN=""
XRAY_BIN="${XRAY_BIN:-xray}"
AGENT_MODE="${AGENT_MODE:-auto}"
WITH_XRAY=0

red() { echo -e "\033[31m$*\033[0m"; }
green() { echo -e "\033[32m$*\033[0m"; }
yellow() { echo -e "\033[33m$*\033[0m"; }
info() { echo -e "\033[36m[xpanel-agent]\033[0m $*"; }

usage() {
  cat <<EOF
用法: $0 -m MASTER_URL -t INSTALL_TOKEN [选项]
  -m, --master URL       主控地址，如 http://1.2.3.4:8080
  -t, --token TOKEN      面板添加服务器后生成的安装 Token
  -x, --xray-bin PATH    xray 可执行文件路径（默认 PATH 中的 xray）
  -M, --mode MODE        auto|websocket|http|pull（默认 auto）
      --with-xray        自动下载官方 Xray-core 到 ${INSTALL_DIR}/bin/xray
  -h, --help             帮助
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -m|--master) MASTER_URL="$2"; shift 2 ;;
    -t|--token) INSTALL_TOKEN="$2"; shift 2 ;;
    -x|--xray-bin) XRAY_BIN="$2"; shift 2 ;;
    -M|--mode) AGENT_MODE="$2"; shift 2 ;;
    --with-xray) WITH_XRAY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) red "未知参数: $1"; usage; exit 1 ;;
  esac
done

if [[ "$(id -u)" -ne 0 ]]; then
  red "请使用 root 运行（sudo）"
  exit 1
fi
if [[ -z "$MASTER_URL" || -z "$INSTALL_TOKEN" ]]; then
  red "必须提供 -m MASTER_URL 与 -t INSTALL_TOKEN"
  usage
  exit 1
fi
command -v systemctl >/dev/null 2>&1 || { red "需要 systemd"; exit 1; }

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) red "不支持的架构"; exit 1 ;;
  esac
}

download() {
  local url="$1" out="$2"
  curl -fsSL --retry 3 -o "$out" "$url"
}

latest_tag() {
  local tag
  tag="$(curl -fsSL "${GITHUB_API}/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1 || true)"
  echo "${tag:-latest}"
}

install_agent_bin() {
  local arch tag url tmp
  arch="$(detect_arch)"
  tag="$(latest_tag)"
  local name="${APP_NAME}-linux-${arch}"
  if [[ "$tag" == "latest" ]]; then
    url="https://github.com/${REPO}/releases/latest/download/${name}"
  else
    url="https://github.com/${REPO}/releases/download/${tag}/${name}"
  fi
  tmp="$(mktemp)"
  info "下载 ${url}"
  download "$url" "$tmp"
  if head -c 20 "$tmp" | grep -qi '<!DOCTYPE\|<html'; then
    red "下载失败：Release 中可能还没有 ${name}"
    rm -f "$tmp"
    exit 1
  fi
  mkdir -p "${INSTALL_DIR}/bin" "${DATA_DIR}"
  install -m 0755 "$tmp" "${BIN_PATH}"
  rm -f "$tmp"
  green "Agent 已安装: ${BIN_PATH}"
}

install_xray() {
  local arch xarch url tmp dir
  arch="$(detect_arch)"
  case "$arch" in
    amd64) xarch="64" ;;
    arm64) xarch="arm64-v8a" ;;
    *) red "无法自动下载该架构 xray"; return 1 ;;
  esac
  # latest release asset name pattern
  url="$(curl -fsSL https://api.github.com/repos/XTLS/Xray-core/releases/latest \
    | sed -n "s/.*\"browser_download_url\"[[:space:]]*:[[:space:]]*\"\\([^\"]*Xray-linux-${xarch}\\.zip\\)\".*/\\1/p" \
    | head -n1)"
  if [[ -z "$url" ]]; then
    yellow "无法解析 Xray 下载地址，请手动安装 xray"
    return 1
  fi
  tmp="$(mktemp -d)"
  info "下载 Xray: $url"
  download "$url" "$tmp/xray.zip"
  command -v unzip >/dev/null 2>&1 || { apt-get update -y && apt-get install -y unzip; } 2>/dev/null || true
  unzip -qo "$tmp/xray.zip" -d "$tmp/out"
  install -m 0755 "$tmp/out/xray" "${INSTALL_DIR}/bin/xray"
  # geo files if present in zip
  [[ -f "$tmp/out/geoip.dat" ]] && install -m 0644 "$tmp/out/geoip.dat" "${INSTALL_DIR}/bin/geoip.dat"
  [[ -f "$tmp/out/geosite.dat" ]] && install -m 0644 "$tmp/out/geosite.dat" "${INSTALL_DIR}/bin/geosite.dat"
  XRAY_BIN="${INSTALL_DIR}/bin/xray"
  rm -rf "$tmp"
  green "Xray 已安装: ${XRAY_BIN}"
}

write_service() {
  cat >"/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=XPanel Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
Environment=MASTER_URL=${MASTER_URL}
Environment=INSTALL_TOKEN=${INSTALL_TOKEN}
Environment=DATA_DIR=${DATA_DIR}
Environment=XRAY_BIN=${XRAY_BIN}
Environment=AGENT_MODE=${AGENT_MODE}
ExecStart=${BIN_PATH} -master ${MASTER_URL} -token ${INSTALL_TOKEN} -data ${DATA_DIR} -xray-bin ${XRAY_BIN} -mode ${AGENT_MODE}
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
  systemctl restart "${SERVICE_NAME}"
  green "Agent 服务已启动"
  info "日志: journalctl -u ${SERVICE_NAME} -f"
}

cmd="${1:-}"
if [[ "$cmd" == "uninstall" ]]; then
  systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
  systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
  systemctl daemon-reload
  rm -f "${BIN_PATH}"
  yellow "Agent 已卸载（数据保留 ${DATA_DIR}）"
  exit 0
fi

command -v curl >/dev/null 2>&1 || { red "需要 curl"; exit 1; }
install_agent_bin
if [[ "$WITH_XRAY" -eq 1 ]]; then
  install_xray || true
fi
if ! command -v "${XRAY_BIN}" >/dev/null 2>&1 && [[ ! -x "${XRAY_BIN}" ]]; then
  yellow "未找到 xray（${XRAY_BIN}）。可用 --with-xray 自动安装，或稍后指定 -x"
fi
write_service
