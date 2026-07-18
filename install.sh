#!/usr/bin/env bash
# BPanel Master 一键安装 / 更新 / 卸载（Linux）
# 用法:
#   curl -sL https://raw.githubusercontent.com/binshao1230/xpanel/main/install.sh | sudo bash
#   curl -sL .../install.sh | sudo bash -s -- update
#   curl -sL .../install.sh | sudo bash -s -- uninstall
set -euo pipefail

# 默认指向当前 GitHub 仓库；若已将仓库改名为 bpanel，可设 BPANEL_REPO=binshao1230/bpanel
REPO="${BPANEL_REPO:-binshao1230/xpanel}"
APP_NAME="bpanel-master"
INSTALL_DIR="${BPANEL_DIR:-/opt/bpanel}"
BIN_PATH="${INSTALL_DIR}/bin/${APP_NAME}"
DATA_DIR="${INSTALL_DIR}/data"
SERVICE_NAME="bpanel-master"
# 旧版 XPanel 路径（改名后 update 自动迁移）
LEGACY_INSTALL_DIR="${XPANEL_DIR:-/opt/xpanel}"
LEGACY_APP_NAME="xpanel-master"
LEGACY_SERVICE_NAME="xpanel-master"
LEGACY_BIN_PATH="${LEGACY_INSTALL_DIR}/bin/${LEGACY_APP_NAME}"
LEGACY_DATA_DIR="${LEGACY_INSTALL_DIR}/data"
DEFAULT_PORT="${PORT:-8080}"
GITHUB_API="https://api.github.com/repos/${REPO}"
RAW_BASE="https://raw.githubusercontent.com/${REPO}/main"

red() { echo -e "\033[31m$*\033[0m"; }
green() { echo -e "\033[32m$*\033[0m"; }
yellow() { echo -e "\033[33m$*\033[0m"; }
info() { echo -e "\033[36m[bpanel]\033[0m $*"; }

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
  # prefer versioned name: bpanel-master-linux-amd64
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

# 从 unit 文件里抠环境变量（兼容无 -P 的 grep）
read_unit_env() {
  local unit="$1" key="$2"
  [[ -f "$unit" ]] || return 0
  sed -n "s/^Environment=${key}=//p" "$unit" 2>/dev/null | head -n1 | tr -d '\r' || true
}

# XPanel → BPanel：迁移数据目录与旧 systemd 单元
migrate_from_xpanel() {
  # 1) 数据：新 data 为空且旧 data 有内容时迁过来
  if [[ -d "${LEGACY_DATA_DIR}" ]]; then
    mkdir -p "${DATA_DIR}"
    local new_empty=1
    if [[ -n "$(ls -A "${DATA_DIR}" 2>/dev/null || true)" ]]; then
      new_empty=0
    fi
    if [[ "$new_empty" -eq 1 ]]; then
      info "迁移旧数据 ${LEGACY_DATA_DIR} → ${DATA_DIR}"
      # 尽量用 cp 保留原目录（用户可自行删 /opt/xpanel）
      cp -a "${LEGACY_DATA_DIR}/." "${DATA_DIR}/"
      green "数据已复制到 ${DATA_DIR}"
    else
      yellow "检测到 ${DATA_DIR} 已有数据，跳过数据迁移（旧目录: ${LEGACY_DATA_DIR}）"
    fi
  fi

  # 2) 停掉旧服务，避免抢端口
  if systemctl list-unit-files "${LEGACY_SERVICE_NAME}.service" 2>/dev/null | grep -q "${LEGACY_SERVICE_NAME}"; then
    info "停止旧服务 ${LEGACY_SERVICE_NAME}"
    systemctl stop "${LEGACY_SERVICE_NAME}" 2>/dev/null || true
    systemctl disable "${LEGACY_SERVICE_NAME}" 2>/dev/null || true
  fi
  if [[ -f "/etc/systemd/system/${LEGACY_SERVICE_NAME}.service" ]]; then
    # 保留一份备份，便于核对 JWT / PUBLIC_URL
    cp -f "/etc/systemd/system/${LEGACY_SERVICE_NAME}.service" \
      "/etc/systemd/system/${LEGACY_SERVICE_NAME}.service.bpanel-migrated.bak" 2>/dev/null || true
  fi
}

write_service() {
  local public_url jwt port
  public_url="${PUBLIC_URL:-}"
  jwt="${JWT_SECRET:-}"
  port="${DEFAULT_PORT}"

  # 优先继承新服务；否则继承旧 xpanel-master 服务配置
  local unit_new="/etc/systemd/system/${SERVICE_NAME}.service"
  local unit_old="/etc/systemd/system/${LEGACY_SERVICE_NAME}.service"
  local unit_bak="/etc/systemd/system/${LEGACY_SERVICE_NAME}.service.bpanel-migrated.bak"

  if [[ -z "$jwt" ]]; then
    jwt="$(read_unit_env "$unit_new" "JWT_SECRET")"
  fi
  if [[ -z "$jwt" ]]; then
    jwt="$(read_unit_env "$unit_old" "JWT_SECRET")"
  fi
  if [[ -z "$jwt" && -f "$unit_bak" ]]; then
    jwt="$(read_unit_env "$unit_bak" "JWT_SECRET")"
  fi
  if [[ -z "$public_url" ]]; then
    public_url="$(read_unit_env "$unit_new" "PUBLIC_URL")"
  fi
  if [[ -z "$public_url" ]]; then
    public_url="$(read_unit_env "$unit_old" "PUBLIC_URL")"
  fi
  if [[ -z "$public_url" && -f "$unit_bak" ]]; then
    public_url="$(read_unit_env "$unit_bak" "PUBLIC_URL")"
  fi

  if [[ -z "$jwt" || "$jwt" == "change-me" ]]; then
    if command -v openssl >/dev/null 2>&1; then
      jwt="$(openssl rand -hex 24)"
    else
      jwt="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    fi
  fi

  if [[ -z "$public_url" ]]; then
    public_url="http://$(curl -fsSL --max-time 3 https://api.ipify.org 2>/dev/null || echo "SERVER_IP"):${port}"
  fi

  cat >"${unit_new}" <<EOF
[Unit]
Description=BPanel Master - multi-server Xray control panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=${INSTALL_DIR}
Environment=ADDR=:${port}
Environment=DATA_DIR=${DATA_DIR}
Environment=PUBLIC_URL=${public_url}
Environment=JWT_SECRET=${jwt}
ExecStart=${BIN_PATH} -addr :${port} -data ${DATA_DIR} -public-url ${public_url} -jwt-secret ${jwt}
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

  # 禁用并移除旧 unit（已备份）
  if [[ -f "$unit_old" ]]; then
    systemctl stop "${LEGACY_SERVICE_NAME}" 2>/dev/null || true
    systemctl disable "${LEGACY_SERVICE_NAME}" 2>/dev/null || true
    rm -f "$unit_old"
    yellow "已替换旧服务单元 ${LEGACY_SERVICE_NAME} → ${SERVICE_NAME}"
  fi

  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
  # 不用裸 restart：unit 刚创建时某些环境会报 not found；先 start 再 restart
  if systemctl cat "${SERVICE_NAME}.service" >/dev/null 2>&1; then
    systemctl restart "${SERVICE_NAME}" || systemctl start "${SERVICE_NAME}"
  else
    systemctl start "${SERVICE_NAME}"
  fi
  if systemctl is-active --quiet "${SERVICE_NAME}"; then
    green "systemd 服务已启动: ${SERVICE_NAME}"
  else
    red "服务启动失败，请检查: systemctl status ${SERVICE_NAME} -l"
    systemctl --no-pager -l status "${SERVICE_NAME}" || true
    journalctl -u "${SERVICE_NAME}" -n 30 --no-pager || true
    return 1
  fi
  echo
  info "面板地址: ${public_url}"
  info "数据目录: ${DATA_DIR}"
  info "查看日志: journalctl -u ${SERVICE_NAME} -f"
  yellow "请打开面板确认数据正常；旧目录 ${LEGACY_INSTALL_DIR} 可在确认后手动删除"
}

do_install() {
  need_root
  require_cmd systemctl
  migrate_from_xpanel
  install_binary
  write_service
}

do_update() {
  need_root
  require_cmd systemctl
  migrate_from_xpanel
  install_binary
  # 始终重写并启用 unit：兼容 XPanel→BPanel 改名、CDN 缓存旧脚本、unit 缺失等情况
  # （write_service 会继承旧 JWT/PUBLIC_URL，停掉 xpanel-master）
  info "写入/更新 systemd 单元 ${SERVICE_NAME}.service …"
  write_service
  green "更新完成"
  systemctl --no-pager -l status "${SERVICE_NAME}" || true
}

do_uninstall() {
  need_root
  systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
  systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
  systemctl stop "${LEGACY_SERVICE_NAME}" 2>/dev/null || true
  systemctl disable "${LEGACY_SERVICE_NAME}" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
  rm -f "/etc/systemd/system/${LEGACY_SERVICE_NAME}.service"
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
