# BPanel 系统设计文档

> 多服务器 Xray 管理面板：主控（Master）+ 子节点（Agent）架构。  
> 对标公开产品形态（如 x-ui / 妙妙屋X 类面板），**独立实现**，不依赖其授权逻辑。

## 1. 目标

| 能力 | MVP | 后续 |
|------|-----|------|
| 用户登录 / JWT | ✅ | 2FA、角色细分 |
| 多服务器注册与心跳 | ✅ | 自动重连、Pull 模式 |
| 下发 Xray 配置并热重载 | ✅（-test 校验后进程重启） | API 热更 inbound、配置 diff/回滚 |
| 入站节点 CRUD | ✅ | 出站/路由可视化 |
| 订阅链接（Clash / V2Ray URI） | ✅ | SingBox / Surge 等 |
| 流量统计 | 骨架 | Xray Stats API 聚合 |
| ACME 证书 | 占位 | lego 集成 |
| Docker 一键部署 | ✅ | host 网络可选 |

## 2. 架构

```
┌──────────────────────────────────────────────┐
│                   Master                     │
│  Web UI · REST API · SQLite · 订阅生成        │
│  Agent 会话管理 · 配置编排 · 用户/套餐        │
└───────────────────┬──────────────────────────┘
                    │ HTTPS / WebSocket
         ┌──────────┼──────────┐
         ▼          ▼          ▼
    ┌────────┐ ┌────────┐ ┌────────┐
    │ Agent1 │ │ Agent2 │ │ Agent3 │
    │ xray   │ │ xray   │ │ xray   │
    └────────┘ └────────┘ └────────┘
```

### 2.1 组件职责

- **Master**  
  - 持久化：用户、服务器、节点、Agent token、配置版本  
  - 对外：管理后台 + 客户端订阅 URL  
  - 对内：接受 Agent 注册/心跳，下发 `ConfigBundle`

- **Agent**  
  - 用安装 token 向主控注册，拿到长期 `agent_key`  
  - 定时心跳：上报 IP、版本、xray 状态、简易流量  
  - 拉取或接收配置：写入 `config.json` 并 `xray -test` 后热重载  

### 2.2 连接模式（MVP：HTTP 双向）

| 模式 | 说明 | MVP |
|------|------|-----|
| Agent → Master HTTP | 注册、心跳、拉取配置 | ✅ |
| Master → Agent HTTP | 主动 push（Agent 暴露管理端口） | ✅ 可选 |
| WebSocket 长连接 | 实时指令 | 后续 |
| Pull only | 仅内网出站 | 后续 |

## 3. 协议（`internal/protocol`）

### 3.1 注册 `POST /api/agent/register`

```json
// request
{
  "token": "install-token",
  "hostname": "node-jp-1",
  "agent_version": "0.1.0",
  "os": "linux",
  "arch": "amd64"
}
// response
{
  "server_id": "uuid",
  "agent_key": "long-lived-secret",
  "poll_interval_sec": 15
}
```

### 3.2 心跳 `POST /api/agent/heartbeat`

Header: `X-Agent-Key: <agent_key>`

```json
// request
{
  "server_id": "uuid",
  "public_ip": "1.2.3.4",
  "xray_running": true,
  "config_version": 3,
  "uptime_sec": 3600,
  "traffic": { "up": 0, "down": 0 }
}
// response
{
  "ok": true,
  "desired_config_version": 4,
  "commands": ["reload_config"]  // 可选
}
```

### 3.3 拉配置 `GET /api/agent/config`

Header: `X-Agent-Key`  
Response: `ConfigBundle`

```json
{
  "version": 4,
  "xray_json": { "...": "完整 xray 配置对象" },
  "checksum": "sha256..."
}
```

## 4. 数据模型（SQLite）

- `users` — id, username, password_hash, role, traffic_limit, expire_at  
- `servers` — id, name, install_token, agent_key, last_seen, public_ip, status  
- `inbounds` — id, server_id, protocol, port, settings_json, users_json  
- `nodes` — 订阅展示用节点快照（可由 inbound 同步）  
- `settings` — key/value  

## 5. API 一览（Master 管理端）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/auth/setup | 首次创建管理员 |
| POST | /api/auth/login | 登录拿 JWT |
| GET  | /api/servers | 服务器列表 |
| POST | /api/servers | 创建服务器（生成 install token） |
| GET  | /api/servers/:id/install-cmd | 一键安装命令 |
| POST | /api/inbounds | 创建入站 |
| GET  | /api/subscribe/:token | 客户端订阅 |
| *    | /api/agent/* | Agent 协议 |

## 6. 目录结构

```
bpanel/
  cmd/master/main.go
  cmd/agent/main.go
  internal/
    protocol/     # 共享消息结构
    master/       # HTTP、DB、业务
    agent/        # 心跳、应用配置
    xraycfg/      # 生成 xray JSON
    sub/          # 订阅序列化
  web/static/     # 嵌入式前端
  deploy/
    Dockerfile.master
    Dockerfile.agent
  docker-compose.yml
  docs/design.md
```

## 7. Docker 部署

- `bpanel-master`：端口 8080，卷挂载 `./data`  
- `bpanel-agent`：依赖主控地址 + install token 环境变量  
- 生产建议：Master 用反向代理 TLS；Agent 与 Xray 可用 host 网络以便暴露业务端口  

## 8. 安全

- Agent 长期密钥存在 DB，仅注册时下发一次  
- 管理 API 使用 JWT  
- 配置下发校验 checksum  
- 后续：mTLS、指令签名、审计日志  

## 9. 实现阶段

1. **P0** 主控 + Agent + SQLite + 基础 UI + Docker  
2. **P1** 真实 xray 进程管理（已完成：`internal/xrayproc` + watchdog）  
3. **P2** Stats 流量、证书、路由/出站、套餐限速、WS 通道  

### Xray 进程管理流程

```
拉配置 → 写 *.apply.json → xray run -test -c（扩展名必须是 .json）
         ├─ 失败：保留旧配置/旧进程，另存 *.bad.json
         └─ 成功：写入 xray.json → Stop 旧进程 → Start 新进程
Watchdog 每 10s：若配置存在且进程退出则 EnsureRunning
```

 
