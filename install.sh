#!/usr/bin/env bash
# XPanel Master 一键安装 / 更新 / 卸载（Linux）
# 用法:
#   curl -sL https://raw.githubusercontent.com/binshao1230/xpanel/main/install.sh | sudo bash
#   curl -sL .../install.sh | sudo bash -s -- update
#   curl -sL .../install.sh | sudo bash -s -- uninstall
set -euo pipefail

REPO="${XPANEL_REPO:-binshao1230/xpanel}"
APP_NAME="xpanel-master"
INSTALL_DIR="${XPANEL_DIR:-/opt/xpanel}"
BIN_PATH="${INSTALL_DIR}/bin/${APP_NAME}"
DATA_DIR="${INSTALL_DIR}/data"
SERVICE_NAME="xpanel-master"
DEFAULT_PORT="${PORT:-8080}"
GITHUB_API="https://api.github.com/repos/${REPO}"
RAW_BASE="https://raw.githubusercontent.com/${REPO}/main"

red() { echo -e "\033[31m$*\033[0m"; }
green() { echo -e "\033[32m$*\033[0m"; }
yellow() { echo -e "\033[33m$*\033[0m"; }
info() { echo -e "\033[36m[xpanel]\033[0m $*"; }

need_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    red "请使用 root 运行（sudo）"
    exit 1
  fi
}

detect_arch() {
  local m
  m="$(uname -m)"
  case "$m" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    armv7l) echo "armv7" ;;
    *) red "不支持的架构: $m"; exit 1 ;;
  esac
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    *) red "仅支持 Linux"; exit 1 ;;
  esac
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || { red "缺少命令: $1"; exit 1; }
}

download() {
  local url="$1" out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 -o "$out" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
  else
    red "需要 curl 或 wget"
    exit 1
  fi
}

latest_tag() {
  local tag=""
  if command -v curl >/dev/null 2>&1; then
    tag="$(curl -fsSL "${GITHUB_API}/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
  fi
  if [[ -z "$tag" ]]; then
    tag="latest"
  fi
  echo "$tag"
}

asset_url() {
  local tag="$1" os="$2" arch="$3"
  # prefer versioned name: xpanel-master-linux-amd64
  local name="${APP_NAME}-${os}-${arch}"
  if [[ "$tag" == "latest" ]]; then
    echo "https://github.com/${REPO}/releases/latest/download/${name}"
  else
    echo "https://github.com/${REPO}/releases/download/${tag}/${name}"
  fi
}

install_binary() {
  local os arch tag url tmp
  os="$(detect_os)"
  arch="$(detect_arch)"
  tag="$(latest_tag)"
  url="$(asset_url "$tag" "$os" "$arch")"
  tmp="$(mktemp)"

  info "下载 ${url}"
  if ! download "$url" "$tmp"; then
    red "下载失败。若尚未发布 Release，请先在仓库打 tag 触发构建，或手动放置二进制到 ${BIN_PATH}"
    rm -f "$tmp"
    exit 1
  fi
  # check not HTML error page
  if head -c 20 "$tmp" | grep -qi '<!DOCTYPE\|<html'; then
    red "下载到的不是二进制（可能 Release 不存在）。tag=${tag}"
    rm -f "$tmp"
    exit 1
  fi

  mkdir -p "${INSTALL_DIR}/bin" "${DATA_DIR}"
  install -m 0755 "$tmp" "${BIN_PATH}"
  rm -f "$tmp"
  green "已安装: ${BIN_PATH} (${tag})"
}

write_service() {
  local public_url jwt
  public_url="${PUBLIC_URL:-}"
  jwt="${JWT_SECRET:-}"
  if [[ -z "$jwt" ]]; then
    if command -v openssl >/dev/null 2>&1; then
      jwt="$(openssl rand -hex 24)"
    else
      jwt="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    fi
  fi

  # preserve existing jwt if service already has one
  if [[ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]]; then
    local old
    old="$(grep -oP 'JWT_SECRET=\K\S+' "/etc/systemd/system/${SERVICE_NAME}.service" 2>/dev/null || true)"
    if [[ -n "$old" && "$old" != "change-me" ]]; then
      jwt="$old"
    fi
  fi

  if [[ -z "$public_url" ]]; then
    # best-effort public IP
    public_url="http://$(curl -fsSL --max-time 3 https://api.ipify.org 2>/dev/null || echo "SERVER_IP"):${DEFAULT_PORT}"
  fi

  cat >"/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=XPanel Master - multi-server Xray control panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=${INSTALL_DIR}
Environment=ADDR=:${DEFAULT_PORT}
Environment=DATA_DIR=${DATA_DIR}
Environment=PUBLIC_URL=${public_url}
Environment=JWT_SECRET=${jwt}
ExecStart=${BIN_PATH} -addr :${DEFAULT_PORT} -data ${DATA_DIR} -public-url ${public_url} -jwt-secret ${jwt}
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
  systemctl restart "${SERVICE_NAME}"
  green "systemd 服务已启动: ${SERVICE_NAME}"
  echo
  info "面板地址: ${public_url}"
  info "数据目录: ${DATA_DIR}"
  info "查看日志: journalctl -u ${SERVICE_NAME} -f"
  yellow "请尽快打开面板完成管理员初始化，并按需修改 PUBLIC_URL / JWT_SECRET"
}

do_install() {
  need_root
  require_cmd systemctl
  install_binary
  write_service
}

do_update() {
  need_root
  require_cmd systemctl
  install_binary
  systemctl restart "${SERVICE_NAME}"
  green "更新完成"
  systemctl --no-pager -l status "${SERVICE_NAME}" || true
}

do_uninstall() {
  need_root
  systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
  systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
  systemctl daemon-reload
  if [[ "${REMOVE_DATA:-0}" == "1" ]]; then
    rm -rf "${INSTALL_DIR}"
    green "已卸载并删除 ${INSTALL_DIR}"
  else
    rm -f "${BIN_PATH}"
    yellow "已卸载服务与二进制；数据保留在 ${DATA_DIR}（删除数据: REMOVE_DATA=1 $0 uninstall）"
  fi
}

cmd="${1:-install}"
case "$cmd" in
  install|"") do_install ;;
  update) do_update ;;
  uninstall) do_uninstall ;;
  *)
    echo "用法: $0 [install|update|uninstall]"
    exit 1
    ;;
esac
