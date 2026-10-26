# Wiki-X OIDC Provider 运维手册

本文面向 Wiki-X 站长和运维人员, 说明如何把 Wiki-X 配成 OIDC Provider (OP), 让其他站点用 Wiki-X 账号登录. 接入方请看 [OIDC 接入方指南](OIDC-CLIENT.md).

## 1 能力范围

Wiki-X 是 OIDC Provider, 本机用户表是唯一身份源. 支持:

- Authorization Code + PKCE S256
- Discovery 和 JWKS
- RS256 签名的 ID token 和 access token
- `openid` / `profile` / `email` scope
- UserInfo
- Refresh token 轮换
- Token Revocation
- 后台手工登记 OIDC 应用, 不做动态客户端注册
- 授权同意页和 `prompt=none/consent`

不支持:

- 动态客户端注册
- `client_credentials`
- 登出端点 (`end_session_endpoint`)
- 多签名密钥轮换
- 用户自行管理已授权应用

## 2 前置条件

- 必须配置 PostgreSQL 主库; Git 模式也需要数据库
- 站点有公开 HTTPS 地址, 例如 `https://wiki.example.com`
- `site.base_url` 或 `oidc.issuer` 配成接入方访问到的公网地址
- 服务器时间使用 NTP 校准
- 反向代理部署时配置 `auth.trusted_proxies`
- HTTPS 部署时设置 `auth.secure_cookies: true`

## 3 配置

`.env` 推荐:

```dotenv
OIDC_ENABLED=true
OIDC_ISSUER=https://wiki.example.com
OIDC_KEY_ENCRYPTION_KEY="<32 字节 base64>"
OIDC_ACCESS_TTL="5m"
OIDC_ID_TTL="5m"
OIDC_REFRESH_TTL="168h"
OIDC_CODE_TTL="2m"
```

YAML 对应:

```yaml
oidc:
  enabled: true
  issuer: https://wiki.example.com
  key_encryption_key: "<32 字节 base64>"
  access_ttl: 5m
  id_ttl: 5m
  refresh_ttl: 168h
  code_ttl: 2m
```

说明:

- `enabled`: 开启 OIDC Provider
- `issuer`: 必须和接入方看到的公网地址完全一致, 不能带路径、query 或 fragment; 留空时使用 `site.base_url`
- `key_encryption_key`: 加密数据库里的 RSA 私钥, 必须是 32 字节 base64; 用 `openssl rand -base64 32` 生成
- `access_ttl`: access token 有效期, 默认 5 分钟
- `id_ttl`: ID token 有效期, 默认 5 分钟
- `refresh_ttl`: refresh token 和授权会话有效期, 默认 7 天
- `code_ttl`: 授权码有效期, 默认 2 分钟

生成加密密钥:

```bash
openssl rand -base64 32
```

把输出填进 `OIDC_KEY_ENCRYPTION_KEY`, 不要提交到仓库. 更换这个 key 会导致已有私钥无法解密; 必须先备份, 然后清空 `oidc_signing_keys` 让服务重新生成密钥.

## 4 首次启动

```bash
go run ./cmd/wiki_x setup
go run ./cmd/wiki_x serve
```

`setup` 会执行迁移 016 和 017. 启动时:

1. 读取 active 签名密钥
2. 如果没有密钥, 生成 RSA 2048 密钥
3. 用 `OIDC_KEY_ENCRYPTION_KEY` 做 AES-GCM 加密后写入 `oidc_signing_keys`
4. 旧版本留下的明文私钥会自动加密回写, inactive 行也会迁移
5. 私钥解密失败或加密 key 变更时拒绝启动

验证:

```bash
curl -s https://wiki.example.com/.well-known/openid-configuration
curl -s https://wiki.example.com/.well-known/jwks.json
```

监听端口也可以先用本机地址验证:

```bash
curl -s http://127.0.0.1:8080/.well-known/openid-configuration
```

## 5 后台登记应用

用拥有 `manage_users` 权限的账号登录, 打开 `/admin/oidc`.

需要填写:

- 应用名: 显示在同意页, 最多 120 个字符
- 提供者名: 显示在应用名旁边, 最多 120 个字符
- 图标 URL: 留空, 站内绝对路径, 或 http(s) 地址
- 回调地址: 每行一个, 必须和接入方配置完全一致

回调地址规则:

- 生产环境必须使用 HTTPS
- 本机调试允许 `http://localhost`, `http://127.0.0.1`, `http://[::1]`
- 不支持通配符和模糊匹配
- 不接受带用户名密码、fragment 或反斜杠的地址

创建成功后页面只显示一次:

- App ID: `wx_` 开头的客户端 ID, 之后仍能在列表中看到
- App Secret: 只显示一次, 数据库只保存 SHA-256 哈希

把 App ID、App Secret、回调地址和 discovery 地址交给接入方.

### 5.1 重置 Secret

点击 `Reset secret` 会:

- 生成新的 App Secret, 只显示一次
- 旧 Secret 立即失效
- 撤销该应用全部授权会话
- 删除未使用授权码
- 写 `oidc.client.reset_secret` 审计

接入方必须同步更新 Secret.

### 5.2 禁用和删除

禁用应用会:

- 撤销已有授权会话、grant 和 refresh token
- 删除未使用授权码
- 拒绝新的授权和 token 请求
- 写 `oidc.client.update` 审计

删除应用是软撤销:

- 客户端行保留, `revoked_at` 非空
- 后台列表不再显示
- 可在 `/admin/revoked` 恢复
- 恢复后应用保持禁用, 需要重置 Secret 再启用
- 写 `oidc.client.delete` 和后续 `oidc.client.restore` 审计

## 6 接入方需要的信息

把下面这些交给接入方:

- Issuer: `https://wiki.example.com`
- Discovery: `https://wiki.example.com/.well-known/openid-configuration`
- Authorization: `https://wiki.example.com/oauth2/authorize`
- Token: `https://wiki.example.com/oauth2/token`
- UserInfo: `https://wiki.example.com/oauth2/userinfo`
- Revocation: `https://wiki.example.com/oauth2/revoke`
- JWKS: `https://wiki.example.com/.well-known/jwks.json`
- App ID
- App Secret
- 已登记的回调地址
- scope: `openid profile email`

详细接入流程见 [OIDC 接入方指南](OIDC-CLIENT.md).

## 7 Scope 与 Claims

| scope | 返回内容 |
|---|---|
| `openid` | `sub` |
| `profile` | `preferred_username`, `name` |
| `email` | `email`, `email_verified` |

ID token 还包含 `iss`, `aud`, `azp`, `auth_time` (授权码交换时), `nonce`, `iat`, `exp`, `jti`.

`sub` 是数据库用户 ID 的字符串, 用户名或邮箱变化不会改变 `sub`.

## 8 端点行为

### 8.1 Discovery

`GET /.well-known/openid-configuration`

返回 issuer、端点、支持的 response type、grant type、scope、RS256 和 S256.

### 8.2 JWKS

`GET /.well-known/jwks.json`

只暴露当前 active RSA 公钥. 默认缓存 1 小时.

### 8.3 Authorization

`GET /oauth2/authorize`

必需参数:

- `client_id`
- `redirect_uri`
- `response_type=code`
- `scope` (必须包含 `openid`)
- `state` (最长 512)
- `nonce` (最长 256)
- `code_challenge`
- `code_challenge_method=S256`

可选:

- `prompt=none`: 未登录返回 `login_required`, 无既往同意返回 `consent_required`
- `prompt=consent`: 强制显示同意页

未登录时会跳转登录页, 登录后继续原授权请求.

### 8.4 Consent

`POST /oauth2/authorize`

同意页提交使用会话 CSRF. 用户拒绝返回 `access_denied`. 同意记录按 `(user_id, client_id)` 保存 scope 集合; 新增 scope 会重新弹同意页.

### 8.5 Token

`POST /oauth2/token`

客户端认证:

- `client_secret_basic` (推荐)
- `client_secret_post`

支持:

- `grant_type=authorization_code`
- `grant_type=refresh_token`

授权码必须同时提交相同的 `redirect_uri` 和 PKCE verifier. 授权码单次使用, 过期或重复使用返回 `invalid_grant`.

### 8.6 UserInfo

`GET|POST /oauth2/userinfo`

使用 `Authorization: Bearer <access_token>`. 会检查:

- JWT 签名, issuer, 过期时间, `token_use=access`
- 客户端仍然存在且启用
- 会话和 grant 未撤销
- 用户未禁用或删除

### 8.7 Revocation

`POST /oauth2/revoke`

使用客户端认证, 提交 `token`. 支持 access token 和 refresh token. 撤销会作用于整个授权会话.

## 9 安全语义

- App Secret 只存 SHA-256, 后台不会回显
- 授权码只存哈希, 2 分钟过期且单次使用
- ID/access token 使用 RS256, 私钥 AES-GCM 加密后入库
- 授权响应和错误跳转带 `Cache-Control: no-store`
- Refresh token 每次使用后轮换
- 10 秒内重复提交 refresh token 只拒绝请求, 不撤销会话; 超过 10 秒按重放处理并撤销整个授权会话
- 禁用账号后不能刷新 token 或访问 UserInfo, 已签发 access token 最多再用 5 分钟
- 禁用客户端后 UserInfo 和 refresh 立即拒绝
- 删除客户端会撤销全部会话和未使用授权码

## 10 过期数据清理

`serve` 每小时执行一次 OIDC 清理:

- 删除超过 1 小时的授权码
- 删除过期超过 1 天的 refresh token
- 删除撤销超过 7 天的 refresh token
- 删除过期超过 1 天的 session
- 删除创建超过 30 天且没有 session 的 grant

## 11 备份与恢复

必须同时备份:

- PostgreSQL 数据库
- `OIDC_KEY_ENCRYPTION_KEY`

只恢复数据库但丢失加密 key, 已有私钥无法解密, 需要清空 `oidc_signing_keys` 重新生成; 所有接入方会短暂遇到 token 校验失败, 需要重新拉取 JWKS.

恢复后检查:

```bash
curl -s https://wiki.example.com/.well-known/openid-configuration
curl -s https://wiki.example.com/.well-known/jwks.json
```

确认 `issuer` 和 `kid` 没变.

## 12 多实例部署

- 所有实例使用相同的 `OIDC_KEY_ENCRYPTION_KEY`
- 所有实例连接同一个主库
- 签名密钥读取和首次生成在数据库里用 advisory lock 串行化
- 不要给不同实例配置不同 issuer

## 13 故障排查

| 现象 | 先查 |
|---|---|
| discovery 404 | `OIDC_ENABLED` 是否为 true, 服务是否成功初始化 OIDC |
| 启动报解密失败 | `OIDC_KEY_ENCRYPTION_KEY` 是否被更换或填错 |
| 启动报 key 不存在 | 数据库是否执行了迁移 016, 账号是否有写权限 |
| token 401 invalid_client | App Secret 是否被重置, 客户端是否禁用, Basic 是否直接使用原始值 (不要额外 URL 编码) |
| 授权页跳登录 | 浏览器是否已有 Wiki-X 会话, `auth.secure_cookies` 是否和 http/https 匹配 |
| redirect_uri 不匹配 | 后台登记值和接入方提交值是否逐字符一致 |
| state/nonce 错误 | 接入方是否原样回传, 是否每次请求重新生成 |
| iss 不匹配 | `OIDC_ISSUER` 是否和接入方看到的公网地址一致 |
| JWKS 拉到旧 kid | 客户端缓存, 或清空密钥后没有重新拉 JWKS |
| 禁用后仍能访问资源 | 资源服务只本地验签 access token; 需要等 5 分钟过期或接 UserInfo/撤销检查 |

## 14 审计动作

- `oidc.client.create`
- `oidc.client.update`
- `oidc.client.reset_secret`
- `oidc.client.delete`
- `oidc.client.restore`

后台操作都需要 `manage_users` 权限和 CSRF.
