# ACME 证书（Let's Encrypt）

BPanel 使用 [lego](https://github.com/go-acme/lego) 申请/续期证书。

## 方式一：HTTP-01

1. 域名 **A/AAAA** 解析到主控公网 IP  
2. **80 端口** 能访问到面板（或 Nginx 反代）  
3. 将 `/.well-known/acme-challenge/` 转到 BPanel（同机 host 网络可直接监听 80 再跳转）  
4. 面板 → 证书 → 挑战选 `http-01` → 填写域名与邮箱 → 申请  

示例 Nginx：

```nginx
server {
  listen 80;
  server_name example.com;
  location /.well-known/acme-challenge/ {
    proxy_pass http://127.0.0.1:8080;
  }
  location / {
    proxy_pass http://127.0.0.1:8080;
  }
}
```

## 方式二：DNS-01（推荐）

| 厂商 | dns_provider | 凭证字段 |
|------|--------------|----------|
| Cloudflare | `cloudflare` | `dns_api_token`（Zone DNS Edit） |
| 阿里云 | `alidns` | `dns_api_key` + `dns_api_secret` |
| 腾讯云 | `tencentcloud` | `dns_api_key`(SecretId) + `dns_api_secret` |

无需开放 80，支持纯内网主控。也可写在 **设置** 或环境变量：

```bash
export CLOUDFLARE_DNS_API_TOKEN=xxxx
export ALICLOUD_ACCESS_KEY=...
export ALICLOUD_SECRET_KEY=...
export TENCENTCLOUD_SECRET_ID=...
export TENCENTCLOUD_SECRET_KEY=...
```

## 证书下发到 Agent + 挂 TLS

1. 申请/上传证书成功后，主控 **自动 bump** 服务器 `config_version`  
2. Agent 拉配置时收到 `certs[]`，写入 `{DATA}/certs/<域名>/fullchain.pem` 与 `privkey.pem`  
3. Xray 配置里路径为 `{{CERTS}}/...`，Agent 展开为本地绝对路径  
4. 创建入站时选择证书 → `enable_tls` → stream 写入 `tlsSettings.certificates`  
5. 面板可点 **下发 Agent** / API `POST /api/certs/{id}/deploy` 强制再推  

```http
POST /api/inbounds
{
  "server_id": "...",
  "protocol": "vless",
  "port": 443,
  "cert_id": 1,
  "enable_tls": true
}
```

## API

```http
POST /api/certs/acme
Authorization: Bearer <jwt>
{
  "domain": "node.example.com",
  "email": "admin@example.com",
  "challenge": "dns-01",
  "dns_provider": "cloudflare",
  "dns_api_token": "optional-if-in-settings",
  "staging": false,
  "auto_renew": true
}
```

```http
POST /api/certs/{id}/renew
```

## 存储位置

| 位置 | 说明 |
|------|------|
| SQLite `certificates` | PEM + 元数据 |
| `{DATA_DIR}/certs/<domain>/fullchain.pem` | 磁盘证书 |
| `{DATA_DIR}/certs/<domain>/privkey.pem` | 磁盘私钥 |
| `{DATA_DIR}/acme/account-*.key` | ACME 账户密钥 |

## 自动续期

后台每 **12 小时** 检查：`auto_renew=1` 且 **30 天内到期** 的证书会重新申请。

## 测试

生产前建议勾选 **staging**，避免触发 LE 速率限制。  
Staging 证书不被浏览器信任，仅用于验证流程。
