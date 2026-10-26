# Wiki-X OIDC 接入方指南

本文面向需要接入 Wiki-X 登录的第三方站点或应用. Wiki-X 在这里是 OIDC Provider, 你的应用是 Relying Party (RP). 站长的配置和运维说明见 [OIDC Provider 运维手册](OIDC-PROVIDER.md).

## 1 先向站长要什么

- Issuer, 例如 `https://wiki.example.com`
- App ID, 例如 `wx_ab12cd34ef56gh78`
- App Secret, 只显示一次
- 已登记的回调地址
- 允许的 scope: `openid profile email`

Wiki-X 不支持动态客户端注册. 每个应用都必须由站长在 `/admin/oidc` 手工创建. App Secret 丢失只能重置, 重置后旧 Secret 立即失效, 该应用已有会话也会被撤销.

## 2 Discovery

```bash
curl -s https://wiki.example.com/.well-known/openid-configuration
```

关键字段:

```json
{
  "issuer": "https://wiki.example.com",
  "authorization_endpoint": "https://wiki.example.com/oauth2/authorize",
  "token_endpoint": "https://wiki.example.com/oauth2/token",
  "userinfo_endpoint": "https://wiki.example.com/oauth2/userinfo",
  "revocation_endpoint": "https://wiki.example.com/oauth2/revoke",
  "jwks_uri": "https://wiki.example.com/.well-known/jwks.json",
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code", "refresh_token"],
  "id_token_signing_alg_values_supported": ["RS256"],
  "scopes_supported": ["openid", "profile", "email"],
  "token_endpoint_auth_methods_supported": ["client_secret_basic", "client_secret_post"],
  "code_challenge_methods_supported": ["S256"]
}
```

不要硬编码端点, 优先读 discovery; 但 `issuer` 必须和 token 里的 `iss` 完全一致.

## 3 授权请求

### 3.1 生成参数

每次登录都要重新生成:

- `state`: 随机值, 自己保存并比对, 建议至少 16 字节
- `nonce`: 随机值, 自己保存, 用于校验 ID token
- `code_verifier`: 43-128 个 URL 安全字符
- `code_challenge`: `BASE64URL(SHA256(code_verifier))`, 不带 padding

PKCE 必须使用 `S256`, Wiki-X 不接受 `plain`.

### 3.2 跳转授权端点

```text
GET https://wiki.example.com/oauth2/authorize
  ?client_id=wx_xxx
  &redirect_uri=https%3A%2F%2Fapp.example.com%2Fcallback
  &response_type=code
  &scope=openid%20profile%20email
  &state=<random-state>
  &nonce=<random-nonce>
  &code_challenge=<s256-challenge>
  &code_challenge_method=S256
```

可选:

- `prompt=none`: 静默检查. 未登录返回 `login_required`, 无既往同意返回 `consent_required`
- `prompt=consent`: 强制重新显示同意页

用户未登录时会先跳到 Wiki-X 登录页, 登录后回到原授权请求.

### 3.3 回调

成功:

```text
https://app.example.com/callback?code=<code>&state=<state>
```

失败:

```text
https://app.example.com/callback?error=access_denied&error_description=...&state=<state>
```

回调处理必须:

- 先比对 `state`, 不一致直接拒绝
- 检查 `error` 参数
- 不要把 `code` 写进日志或浏览器历史
- 回调地址必须和后台登记值逐字符一致

## 4 换取 token

`POST /oauth2/token`, `Content-Type: application/x-www-form-urlencoded`.

推荐 Basic 认证:

```bash
curl -s https://wiki.example.com/oauth2/token \
  -u 'wx_xxx:APP_SECRET' \
  -d 'grant_type=authorization_code' \
  -d 'code=CODE' \
  -d 'redirect_uri=https://app.example.com/callback' \
  -d 'code_verifier=VERIFIER'
```

也支持表单认证:

```bash
curl -s https://wiki.example.com/oauth2/token \
  -d 'grant_type=authorization_code' \
  -d 'client_id=wx_xxx' \
  -d 'client_secret=APP_SECRET' \
  -d 'code=CODE' \
  -d 'redirect_uri=https://app.example.com/callback' \
  -d 'code_verifier=VERIFIER'
```

成功响应:

```json
{
  "access_token": "<jwt>",
  "token_type": "Bearer",
  "expires_in": 300,
  "id_token": "<jwt>",
  "refresh_token": "<opaque-token>",
  "scope": "email openid profile"
}
```

注意:

- 授权码只能用一次
- `redirect_uri` 必须和授权请求完全一致
- 必须提交生成 challenge 时用的同一个 `code_verifier`
- 失败返回 `invalid_grant`, 不要重复使用同一个 code

## 5 校验 ID token

ID token 是 RS256 JWT. 校验步骤:

1. 从 `jwks_uri` 拉取 JWKS, 按 header 里的 `kid` 找 RSA 公钥
2. 验证 RS256 签名
3. 验证 `iss` 等于配置的 issuer
4. 验证 `aud` 包含你的 App ID
5. 验证 `azp` 等于你的 App ID
6. 验证 `exp` 未过期, `iat` 不在未来太远
7. 验证 `nonce` 等于本次登录生成的值
8. 用 `sub` 作为稳定用户标识

常见 claims:

| claim | 说明 |
|---|---|
| `sub` | 用户数据库 ID 字符串, 用户名变化也不变 |
| `iss` | issuer |
| `aud` | App ID |
| `azp` | App ID |
| `auth_time` | 用户完成登录的时间, 授权码交换时有, refresh 时可能没有 |
| `nonce` | 授权请求里的 nonce |
| `preferred_username` | 请求 `profile` 时返回 |
| `name` | 请求 `profile` 时返回 |
| `email` | 请求 `email` 时返回 |
| `email_verified` | 请求 `email` 时返回 |

不要依赖 `at_hash`、`c_hash` 或 `groups`, Wiki-X 当前不返回这些.

## 6 UserInfo

```bash
curl -s https://wiki.example.com/oauth2/userinfo \
  -H 'Authorization: Bearer ACCESS_TOKEN'
```

返回 `sub`, `azp`, 以及按 scope 返回的 `preferred_username`、`name`、`email`、`email_verified`.

UserInfo 会实时检查:

- access token 签名和过期时间
- 客户端是否启用
- 授权会话/grant 是否撤销
- 用户是否禁用或删除

## 7 Refresh token

```bash
curl -s https://wiki.example.com/oauth2/token \
  -u 'wx_xxx:APP_SECRET' \
  -d 'grant_type=refresh_token' \
  -d 'refresh_token=REFRESH_TOKEN'
```

响应会返回新的 access token、ID token 和 refresh token.

规则:

- 每次刷新都会轮换 refresh token, 只保存最新值
- 10 秒内重复提交同一个 refresh token 会返回 `invalid_grant`, 但不会撤销会话; 这是为了容忍并发/超时重试
- 超过 10 秒再次使用旧 token 会被视为重放, 整个授权会话和 refresh token 会被撤销
- 收到 `invalid_grant` 时不要无限重试, 重新走授权登录

## 8 撤销 token

```bash
curl -s https://wiki.example.com/oauth2/revoke \
  -u 'wx_xxx:APP_SECRET' \
  -d 'token=ACCESS_OR_REFRESH_TOKEN'
```

成功返回 `200` 和 `{}`. 撤销 access token 或 refresh token 都会撤销整个授权会话.

Wiki-X 没有 `end_session_endpoint`. 用户在你的站点退出时, 清掉你自己的会话即可; 如果要同时撤销 Wiki-X 授权, 调用 revocation endpoint.

## 9 错误处理

| 错误 | 含义 | 处理 |
|---|---|---|
| `invalid_client` | App ID/Secret 错误或客户端禁用 | 检查 Secret, 是否被重置, 客户端是否启用 |
| `invalid_request` | 参数缺失或格式错误 | 检查 redirect_uri、code、verifier、grant_type |
| `invalid_grant` | code/refresh token 无效、过期、已使用或会话撤销 | 重新授权, 不要重复用旧值 |
| `unsupported_grant_type` | grant_type 不支持 | 只使用 authorization_code 和 refresh_token |
| `access_denied` | 用户拒绝授权 | 显示失败页, 不要循环重试 |
| `login_required` | `prompt=none` 但没有登录 | 跳转普通授权流程 |
| `consent_required` | `prompt=none` 但没有既往同意 | 跳转普通授权流程或显示说明 |

## 10 安全清单

- 回调地址使用 HTTPS, 只登记必要地址
- 服务端保存 App Secret, 不放进前端代码
- 每次登录生成新的 state、nonce 和 PKCE verifier
- 回调先校验 state
- ID token 必须验签并校验 iss/aud/azp/exp/nonce
- 不要只因为 access token 能被 base64 解码就信任它
- 不要记录 access token、refresh token、App Secret 或授权码
- refresh token 轮换后只保存最新值
- 资源服务如果要本地验签 access token, 必须校验 `token_use=access`、`iss`、`exp`、`client_id`
- 本地验签无法感知用户禁用或会话撤销; 需要强一致时调用 UserInfo
- 时钟偏差建议控制在 60 秒以内, 服务器开 NTP

## 11 常见接入问题

### 回调 400 redirect_uri is not registered

后台登记值和请求值必须完全一致, 包括末尾斜杠、端口、大小写和 query.

### token 401 invalid_client

- App Secret 是否被重置
- Basic 认证里是否直接填了原始 client_id 和 secret, 不要额外做 URL 编码
- 应用是否被禁用
- 是否把 App ID 和 Secret 写反

### token 400 invalid_grant

- code 是否已经用过
- code 是否超过 2 分钟
- redirect_uri 是否和授权请求一致
- code_verifier 是否和 challenge 对应
- refresh token 是否被轮换或被撤销

### ID token 验签失败

- 是否从正确的 issuer 拉 JWKS
- 是否按 `kid` 选公钥
- 是否把 Base64URL 当成普通 Base64
- 是否在校验时用了旧的 JWKS 缓存

### 登录后一直跳回授权页

检查 state/nonce 是否每次重新生成又没保存, 或回调地址是否重复触发授权请求.

## 12 最小接入伪代码

```text
login():
  state = random()
  nonce = random()
  verifier = random()
  challenge = base64url(sha256(verifier))
  save(state, nonce, verifier)
  redirect(authorize_url + state + nonce + challenge)

callback(code, state):
  saved = load(state)
  if saved == null: reject
  tokens = post(token_url, basic(client_id, secret), {
    grant_type: authorization_code,
    code: code,
    redirect_uri: callback_url,
    code_verifier: saved.verifier,
  })
  claims = verify_jwt(tokens.id_token, jwks, issuer, client_id, saved.nonce)
  user = find_or_create_user(claims.sub)
  save_session(user, tokens.access_token, tokens.refresh_token)

refresh():
  tokens = post(token_url, basic(client_id, secret), {
    grant_type: refresh_token,
    refresh_token: current_refresh_token,
  })
  save(tokens.refresh_token)
```

## 13 当前限制

- 不支持动态客户端注册, 新应用找站长登记
- 不支持 client_credentials
- 不支持 OIDC 登出端点
- 不返回 `groups` claims
- 只有一个 active 签名密钥, 站长清空 `oidc_signing_keys` 后所有接入方需要重新拉 JWKS
- access token 默认 5 分钟, 依赖本地验签的资源服务无法立即感知撤销
