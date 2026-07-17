# XPanel ↔ 妙妙屋X 功能对照

学习公开 README / 模块划分（`internal/acme|traffic|speedtest|mcp|notify|...`），**独立实现**同类能力，不做授权绕过。

| 能力域 | 妙妙屋X | XPanel 现状 (v0.5) |
|--------|---------|-------------------|
| 主控 + 多 Agent | ✅ WS/HTTP/Pull | ✅ auto/WS/HTTP/Pull + 配置推送 |
| Xray 进程托管 | ✅ | ✅ test→重启 + watchdog |
| 入站管理 | ✅ 多协议 | ✅ VLESS/VMess/Trojan/SS + Reality 字段 |
| 出站 / 路由 | ✅ 可视化 | ✅ CRUD，编入下发配置 |
| 套餐 / 用户 | ✅ | ✅ 套餐绑定、流量/到期、角色 |
| 流量统计 | ✅ Stats + 30 天 | ✅ Agent 上报 + 日汇总 API |
| 证书 | ✅ ACME 多 DNS | ✅ 手动/ACME（CF/Ali/腾讯）+ **下发 Agent + 入站 TLS** + 自动续期 |
| 订阅多客户端 | ✅ 十余种 | ✅ base64 / Clash / Sing-box |
| 外部节点导入 | ✅ | ✅ URI 导入节点池 |
| 节点同步订阅 | ✅ | ✅ 入站变更自动进订阅 |
| 一键安装 Agent | ✅ | ✅ Docker/二进制命令 |
| 主题 | ✅ 多主题 | ✅ 亮/暗 |
| 测速 | ✅ mihomo | ✅ TCP/TLS 探测 + 批量入站 |
| TG Bot / MCP | ✅ | ✅ **MCP tools API**；TG 预留 |
| ACME 自动签发 | ✅ | ✅ HTTP-01 + CF / AliDNS / TencentCloud |
| Agent 加密通道 | ✅ | ⏳ AgentKey 鉴权（建议反代 TLS） |
| Webhook 通知 | ✅ | ✅ 上下线 webhook |
| 备份 | ✅ | ✅ export/import（settings/plans/nodes） |

说明：完整 1:1 复制需要数月工程量；本版本把**面板主路径**打通到可运营，其余按路线图迭代。
