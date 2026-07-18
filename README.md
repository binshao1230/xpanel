# BPanel

多服务器 **Xray 主控面板**（Master + Agent），独立实现，支持 Docker / 一键脚本 / WebSocket。  
（原名 XPanel，产品与二进制已统一为 **BPanel**）

- 设计：[docs/design.md](docs/design.md)
- 功能对照：[docs/feature-parity.md](docs/feature-parity.md)
- ACME：[docs/acme.md](docs/acme.md)

## 功能（v0.6 · 对标妙妙屋X 风格）

- **界面**：妙妙屋 terracotta 像素主题 · light/dark/system；侧滑节点编辑；Toast / 模板创建
- **主控 Web**：仪表盘 · 服务器 · 入站 · 出站/路由 · 中转隧道 · 套餐 · 用户/续费 · 邀请码 · 外部节点 · 证书 · Nginx · 流量 · 测速 · URI 分享 · 订阅 · 设置
- **Agent**：auto / WebSocket / HTTP / Pull；Xray 进程托管；证书下发；实时网速
- **入站**：VLESS Reality（真实 x25519）· TLS · 多协议；URI 一键复制
- **中转**：端口转发 / dokodemo 隧道
- **出站**：WARP WireGuard 骨架 + 分流规则
- **套餐 / 邀请码注册**、用户续费视图
- **订阅**：base64 / Clash / Sing-box / Surge；短码 `/s/{code}`
- **ACME**：HTTP-01 + CF/Ali/腾讯；自动续期；下发 Agent
- **伪装探针** `/probe`、Webhook、MCP、备份导出

## Linux 一键安装（推荐）

### 主控

```bash
curl -sL https://raw.githubusercontent.com/binshao1230/xpanel/main/install.sh | sudo bash
```

更新 / 卸载：

```bash
curl -sL https://raw.githubusercontent.com/binshao1230/xpanel/main/install.sh | sudo bash -s -- update
curl -sL https://raw.githubusercontent.com/binshao1230/xpanel/main/install.sh | sudo bash -s -- uninstall
# 连同数据删除：
# REMOVE_DATA=1 curl -sL .../install.sh | sudo bash -s -- uninstall
```

安装后访问：`http://服务器IP:8080`，完成管理员初始化。

可选环境变量：

| 变量 | 说明 | 默认 |
|------|------|------|
| `PORT` | 监听端口 | `8080` |
| `PUBLIC_URL` | 对外访问地址（订阅/安装命令） | 自动探测 |
| `JWT_SECRET` | 会话密钥 | 随机生成 |
| `BPANEL_DIR` | 安装目录 | `/opt/bpanel` |
| `BPANEL_REPO` | GitHub 仓库 `owner/name` | `binshao1230/xpanel` |

### Agent（在节点机上）

面板添加服务器后复制 Token，或：

```bash
curl -sL https://raw.githubusercontent.com/binshao1230/xpanel/main/install-agent.sh | sudo bash -s -- \
  -m http://主控IP:8080 \
  -t 你的INSTALL_TOKEN \
  --with-xray
```

参数：

- `-m` 主控 URL（必填）
- `-t` 安装 Token（必填）
- `--with-xray` 自动安装官方 Xray-core
- `-M auto|websocket|http|pull` 连接模式
- `-x /path/to/xray` 指定 xray 路径

## Docker

```bash
docker compose up -d --build master
```

详见 `docker-compose.yml`。

## 本地开发

```bash
go mod tidy
go run ./cmd/master -addr :8080 -data ./data/master
go run ./cmd/agent -master http://127.0.0.1:8080 -token <TOKEN> -data ./data/agent -mode auto
```

## 目录

```
cmd/master · cmd/agent
internal/protocol · master · agent · acme · xraycfg · xrayproc · sub
web/static          嵌入式前端
install.sh          主控一键安装
install-agent.sh    Agent 一键安装
deploy/             Dockerfile
docs/               设计与对照
```

## 许可

MIT

## 免责声明

仅供学习交流，请遵守当地法律法规。
