# Wiki-X

用 Go 写的开源 wiki 框架. 同一套内容模型、渲染、主题和缓存管线, 支持两种内容模式:

- Git 模式: 以 Git 工作区为权威内容源, 支持静态构建、定时重载、GitHub webhook 同步和站内编辑开 PR
- PostgreSQL 模式: 以数据库为权威内容源, 支持账号登录、站内编辑、审核发布, 以及合并后的 GitHub PR 单向导入

当前版本已经具备一个社区 wiki 需要的基础闭环: 阅读、搜索、登录、账号绑定、编辑、审核、历史、diff、媒体、多皮肤、内容多语言路由和 UI 中英文文案.

## 已实现

- Markdown + YAML frontmatter, goldmark GFM 渲染, 默认关闭 raw HTML
- 按需渲染 + 内存 L1 + Redis L2 页面缓存 + singleflight 防击穿
- ETag/304, gzip, CDN 缓存头, 健康检查, 超时和优雅退出
- 静态构建, 构建清单清理, 空目录清理
- PostgreSQL 内容源, 内嵌迁移, Git -> PG 单向导入
- 本地账号, argon2id 密码, 数据库 session, CSRF, 权限组
- TOTP 两步验证 (RFC 6238): 本地密码登录和 GitHub 登录都强制二次验证, 验证码防重放, 开启或关闭会让旧会话失效
- 2FA 恢复码与二维码: 生成 10 个一次性恢复码, 丢失认证器也能登录; 待确认密钥提供二维码扫描
- GitHub OAuth 登录, 已有账号可以在 `/account` 绑定 GitHub 身份
- 站内编辑, 草稿/待审/发布, 编辑冲突检测, 历史版本, unified diff, 审核队列
- 编辑页 Markdown 工具栏和实时预览: 粗体/斜体/删除线/标题/链接/列表/任务列表/代码/引用/分隔线/表格, Ctrl+B/I/K 快捷键, 宽屏左右分栏, 实时字符数
- 组件指令 `wx-infobox`/`wx-notice`/`wx-gallery`/`wx-collapse`: 在 Markdown 中用 fenced code 声明, YAML 严格校验, 渲染失败降级为错误框
- 结构化实体: 后台定义图鉴类型和字段, 录入角色/道具等数据, 页面可绑定实体, `wx-infobox` 用 `entity:` 自动渲染字段, `/entities` 支持筛选和排序
- 审计留存: 按保留期导出 NDJSON + sha256 校验文件后清理旧日志, serve 定时执行, 也可用 `wikix audit archive` 手动跑
- Git 模式站内编辑创建 GitHub PR, 会先校验 GitHub 上的最新内容
- PG 模式接受 GitHub PR: 合并后按配置自动导入为待审或已发布修订; PR 删除的 Markdown 会同步软删除, PR 里的图片会导入媒体目录并改写正文引用
- GitHub App 或服务端 PAT 两种仓库认证方式, push webhook 触发同步重载
- 站内搜索, PG 媒体上传, Git 内容目录的图片/视频通过 `/media` 提供
- 页面保护, 监视列表和讨论页: 审核员可保护页面, 登录用户可监视页面并发表评论
- 用户管理后台和举报管理队列: 管理用户组, 禁用/启用账号, 集中处理评论举报
- 内置 default/compact 两套皮肤, 支持外部主题目录和用户切换
- UI 文案 zh-CN/en, 内容可用 `/en/...` 这类 locale 前缀组织并自动切换页面文案
- `/translations` 翻译管理: 按 translation_key 查看缺失/已同步/可能过期的语言页面
- 邮箱验证与密码找回: SMTP 发送一次性链接, 邮箱唯一, 重置后清空该账号会话
- 注册开关: 关闭 / 邀请码 / 开放注册; 邀请码只存哈希, 开放注册必须验证邮箱后才激活账号

## 快速开始: Git 模式

需要 Go 1.27 或更高版本.

```bash
go run ./cmd/wikix serve -config examples/basic/wikix.yaml
go run ./cmd/wikix build -config examples/basic/wikix.yaml -out dist
```

默认监听 `:8080`. Git 模式不依赖数据库. 如果只需要阅读和静态部署, 到这里就够了.

## 快速开始: PostgreSQL 社区模式

先准备 PostgreSQL 和 Redis. 数据库密码只通过环境变量传入:

```bash
export WIKIX_DATABASE_URL='postgres://postgres:password@127.0.0.1:5432/wikix_dev?sslmode=disable'

go run ./cmd/wikix migrate -config examples/community/wikix.yaml
go run ./cmd/wikix import  -config examples/community/wikix.yaml
printf 'change-me-please\n' | go run ./cmd/wikix user create -config examples/community/wikix.yaml -name admin -admin
go run ./cmd/wikix serve -config examples/community/wikix.yaml
```

打开 `/login` 登录. 示例配置默认使用内存缓存; 多实例部署时把 `cache.driver` 改为 `redis` 并填写地址.

创建普通用户并授予编辑权限:

```bash
printf 'another-password\n' | go run ./cmd/wikix user create -config examples/community/wikix.yaml -name editor
go run ./cmd/wikix user grant -config examples/community/wikix.yaml -name editor -group editor
```

## Docker Compose 部署

仓库自带 `Dockerfile`、`compose.yaml` 和 `deploy/wikix.yaml`:

```bash
export POSTGRES_PASSWORD='换成强口令'
export WIKIX_PORT=8080
docker compose up -d --build
docker compose exec app wikix user create -config /data/wikix.yaml -name admin -admin
```

- PostgreSQL 和 Redis 数据在命名卷里, 媒体与审计归档分别在 `wikix-media`、`wikix-audit` 卷
- 首次启动 `database.auto_migrate: true` 会自动建表
- `deploy/wikix.yaml` 的 `site.base_url` 默认是 `http://localhost:8080`, 正式部署要改成真实域名
- 生产环境把 `auth.secure_cookies` 改为 `true`, 配置 `auth.trusted_proxies`, 并在 TLS 反代后运行
- 数据库口令含 `@`/`:`/`/` 时不能直接拼进默认 URL, 请显式设置完整的 `WIKIX_DATABASE_URL` (口令部分 URL 编码)
- 版本: `wikix version`

## 配置

### 站点和内容

```yaml
site:
  title: My Wiki
  language: zh-CN        # 默认 UI 语言, 必须和空 prefix 的 locale 一致
  base_url: https://wiki.example.com
  community_ui: true     # 在内容页显示搜索/编辑入口
  locales:
    - code: zh-CN
      label: 中文
      prefix: ""
    - code: en
      label: English
      prefix: en
content:
  source: postgres       # git 或 postgres
  dir: content           # Git 模式的内容目录
  max_file_size: 1048576
  reload_interval: 30s
  git_pull: false        # Git 模式收到 push webhook 后执行 git pull --ff-only
```

### 缓存

```yaml
cache:
  driver: auto           # auto, memory, redis
  ttl: 10m
  max_entries: 2000
  max_bytes: 67108864
  redis:
    addr: 127.0.0.1:6379
    password: ""
    db: 0
    prefix: my-wiki
```

每个站点使用独立的 Redis prefix. 页面缓存键包含配置哈希、皮肤、主题版本、路由、内容版本和保护状态. 运行中 Redis 故障会触发熔断, 退化为直接渲染.

### 数据库与账号

```yaml
database:
  url: ""                # 建议通过 WIKIX_DATABASE_URL 提供
  max_conns: 10
  auto_migrate: false
auth:
  secure_cookies: true   # HTTPS 环境必须开启
  session_ttl: 336h
  trusted_proxies: []    # 只有前置反向代理时填写, 例如 - 10.0.0.0/8; 填写后才按 X-Forwarded-For 取真实客户端 IP
  github:
    client_id: ""
    client_secret: ""
    redirect_url: ""     # 留空时使用 base_url + /auth/github/callback
```

### 邮件与密码找回

```yaml
mail:
  host: smtp.example.com
  port: 587              # 465 直连 TLS 时把 implicit_tls 设为 true
  username: ""
  password: ""           # 建议通过 WIKIX_MAIL_PASSWORD 提供
  from: wiki@example.com
  implicit_tls: false
```

配置后登录页会出现"忘记密码"入口:

- `/forgot` 只对已注册且已验证的邮箱发送重置链接, 不管邮箱是否存在都返回同样的提示, 避免枚举账号
- 重置链接 30 分钟有效且只能使用一次; 重置成功后该账号的全部会话失效
- 管理员后台创建带邮箱的用户, 或账号页修改邮箱时, 会自动发送验证邮件; 验证链接 24 小时有效
- `/verify-email` 需要先打开页面再点按钮提交, 避免邮件扫描器提前消费链接
- 账号页可以修改邮箱或重发验证邮件; 邮箱在数据库里大小写不敏感唯一
- 没有配置 `mail.host` 和 `mail.from` 时, 相关路由不会注册, 账号页也不显示邮箱维护入口
- 配置邮件时 `site.base_url` 必填, 用于生成绝对链接; SMTP 服务器不支持 STARTTLS 且配置了用户名时会拒绝发送

可用环境变量: `WIKIX_MAIL_HOST`, `WIKIX_MAIL_PORT`, `WIKIX_MAIL_USERNAME`, `WIKIX_MAIL_PASSWORD`, `WIKIX_MAIL_FROM`, `WIKIX_MAIL_IMPLICIT_TLS`.

### 注册

```yaml
registration:
  mode: off              # off, invite, open
  default_group: user    # 新账号默认加入的用户组
```

- `off`: 不注册 `/register` 路由, 账号只能由管理员或 CLI 创建
- `invite`: 管理员在 `/admin/invites` 创建邀请码, 注册时必须填写有效邀请码; 邀请码只保存哈希, 明文只在创建后显示一次
- `open`: 任何人可以注册, 但必须配置邮件; 账号先以禁用状态保存, 点击验证邮件后才会启用
- 邀请码可以设置使用次数和有效天数; 次数用完或过期后拒绝注册, 删除邀请码不影响已注册账号
- 注册按 IP (5 次/小时) 和邮箱哈希 (3 次/小时) 限流; 用户名和邮箱都唯一
- 邀请码模式在插入用户失败时会回滚次数, 重复用户名不会白扣一次邀请码
- 站点没有账号时不能自助注册, 先用 `wikix user create` 创建第一个管理员
- `/register/verify` 可以重发验证邮件, 不管邮箱是否存在都返回同样的提示

可用环境变量: `WIKIX_REGISTRATION_MODE`, `WIKIX_REGISTRATION_DEFAULT_GROUP`.

内容路由约定: 默认语言使用原路由, 其它语言使用前缀路由. 例如中文首页是 `/`, 英文首页是 `/en/`. 当某个语言页面不存在但默认页面存在时, 会显示默认内容并提示尚未翻译; 多语言页面可以用 `translation_key` 把不同路由的翻译归为一组.

PG 模式提供 `/translations` 翻译管理页: 按 `translation_key` 分组显示各语言的缺失、已同步、可能过期或需要确认状态. 翻译保存时记录默认语言页面的版本, 默认页面更新后旧翻译会标记为可能过期. 缺失语言会生成带 `translation_key` 的创建链接, 编辑页会自动预填. Git 模式没有这个页面, 翻译状态直接通过仓库维护.

### 社区治理

PG 模式提供页面保护, 监视列表和讨论页:

- 审核权限用户可以在编辑页选择保护级别: 不保护 / 半保护 / 完全保护
- 半保护: 审核员直接编辑发布, 其他编辑者可以提交待审修订作为编辑请求, 审核员在 `/review` 批准后应用
- 完全保护: 只有审核员可以编辑, 其他编辑者打开编辑页和提交都会 403
- GitHub PR 导入会跳过受保护页面, PR 删除也不会删除受保护页面
- 登录用户可以在历史页监视页面, 在 `/watchlist` 查看最近修改; Git 模式显示文件更新时间, 作者列标记为 Git 仓库
- `/discussion?route=/guide/` 查看讨论; 登录用户发表评论, 审核员可以隐藏或恢复
- 评论按路由保存, 上限 4000 字符, 输出经过模板转义; 隐藏评论只对审核员可见
- 登录用户可以举报评论, 审核员可以看到未处理举报数和原因并忽略举报; 隐藏评论会自动关闭举报
- `/admin/users` 需要 `manage_users` 权限: 可以创建本地账号, 调整内置或自定义用户组, 禁用或启用账号, 重置密码
- 禁用一个账号会删除它的所有 session, 立即退出登录; 不能禁用或降级当前登录账号
- 系统始终保留至少一个启用的管理员, 最后一个管理员的降级和禁用会被拒绝
- `/admin/pages` 需要 `review` 权限: 可以按状态和关键字查找页面, 软删除页面并记录处置原因, 也可以恢复; 删除后路由立即 404, 修订和历史仍保留
- `/admin/groups` 支持新建自定义用户组和编辑 read/edit/review/upload/manage_users 权限
- 内置 admin/editor/user 组不能删除, 自定义组删除后会同时移除成员关系和权限
- 最后一个拥有 manage_users 权限的启用账号不能被降级、禁用或通过改组权限移除
- `/account` 支持修改或设置本地密码; 修改后其他设备会退出登录, 当前会话保留
- `/account` 支持导出本人数据 (JSON): 资料、身份、会话、评论、修订和监视列表, 不包含密码哈希和 2FA 密钥
- `/account` 支持删除账号: 需要密码或 2FA 验证并勾选确认; 删除后账号匿名化, 内容保留但作者显示为空
- `/admin/users` 支持管理员删除用户, 必须填写原因; 不能删除自己, 最后一个管理员不能被删除
- `/account` 支持开启 TOTP 两步验证: 先生成密钥, 在认证器 App 手动录入密钥或点 otpauth 链接, 再用 6 位验证码确认开启
- 开启后本地密码登录必须再输验证码; GitHub 登录也一样, 会跳到 `/verify` 输入验证码才签发会话, 没有绕开第二步验证的登录入口
- 关闭两步验证需要当前密码, 没有本地密码的 GitHub 账号改用一次性验证码
- 同一个 30 秒窗口内的验证码只能使用一次; 待确认的密钥 30 分钟过期; 已经开启时不能直接换密钥, 必须先关闭
- 开启或关闭两步验证都会让该账号的既有会话失效, 需要重新登录; 管理员代关同样把目标账号踢下线
- 账号页可以生成 10 个恢复码, 只在生成后显示一次; 每个恢复码只能登录一次, 重新生成会让旧恢复码全部失效
- 恢复码输入忽略大小写和连字符; 登录页和 GitHub 二段验证页都能用恢复码代替 6 位验证码
- 待确认密钥页面会显示二维码 (服务端生成 PNG, 不把密钥放进 URL), 用认证器直接扫描
- 关闭两步验证会同时清空恢复码; 纯 GitHub 账号关闭两步验证也可以用恢复码完成
- `/admin/users` 显示 2FA 徽标, 管理员可以替用户关闭两步验证 (账号丢失认证器时的找回入口), 该操作写审计且不能用于自己
- 用户列表、举报列表和审计日志都支持分页; 举报列表可以切换待处理、已处理和全部
- 审计日志支持关键字和日期范围筛选, 可以导出 CSV (最多 5000 行, 防止 Excel 公式注入)
- `/admin/reports` 需要 `review` 权限: 集中查看未处理举报, 可以直接隐藏评论或忽略举报
- `/admin/audit` 需要 `manage_users` 权限: 查看用户、页面、评论和登录相关的管理操作, 支持关键字筛选
- 评论限流默认 5 条/分钟, Redis 可用时跨实例生效, Redis 故障时降级为单实例限流; 屏蔽词使用大小写不敏感的配置列表

```yaml
comment:
  rate_limit: 5
  rate_window: 1m
  blocked_words: ["广告", "spam"]
```

屏蔽词只是简单的子串匹配, 不能替代审核和举报. 评论暂未接入图形验证码 (账号的两步验证是另一回事), 公开站点建议在反向代理层限制频率.

敏感配置优先使用环境变量:

- `WIKIX_DATABASE_URL`
- `WIKIX_GITHUB_CLIENT_ID`
- `WIKIX_GITHUB_CLIENT_SECRET`
- `WIKIX_GITHUB_WEBHOOK_SECRET`
- `WIKIX_GITHUB_TOKEN`
- `WIKIX_GITHUB_APP_ID`
- `WIKIX_GITHUB_INSTALLATION_ID`
- `WIKIX_GITHUB_PRIVATE_KEY`
- `WIKIX_GITHUB_PRIVATE_KEY_FILE`

### 主题和皮肤

内置 `default` 和 `compact`, 用户可以在页面底部切换, 选择保存在 `wikix_skin` cookie 中.

```yaml
theme:
  default: default
  disable_switch: false
  dir: ./my-theme         # 单个自定义主题, 默认皮肤会切换为 custom
  root: ./themes          # 从目录加载多个外部主题
```

外部主题至少包含 `layout.html`, `page.html`, `404.html` 和 `assets`. 静态构建只输出默认皮肤, 运行时才支持切换.

### GitHub

GitHub 登录和 GitHub 仓库写入是两条独立链路:

- OAuth 只负责登录身份, 身份存在 `identities` 表; 登录后可在 `/account` 绑定 GitHub
- 仓库操作可以使用 GitHub App, 也可以退回只授权内容仓库的 fine-grained PAT

```yaml
github:
  webhook_secret: "至少 16 个字符"
  repo: owner/repo
  app_id: 123
  installation_id: 456
  private_key_file: ./github-app.pem
  token: ""                    # 不使用 App 时可以改为 PAT
  base_branch: main
  content_path: content        # Markdown 文件在仓库中的目录
  pr_import: pending           # off, pending, publish
  api_base_url: https://api.github.com
```

Git 模式站内编辑开 PR 需要同时配置数据库、账号体系和 GitHub 凭据, 并设置 `site.community_ui: true`.

PG 模式的 `pull_request` webhook 在 PR 合并后读取变更文件, 单向导入为待审或已发布修订:

- Markdown 新增/修改会生成修订, 受保护页面跳过
- Markdown 删除会软删除对应页面并记录 PR 原因, 受保护页面跳过
- png/jpeg/gif/webp 会按内容哈希命名导入 `media.dir`, 并写入 media 表; 正文中的相对路径和唯一文件名会改写为 `/media/pr-...`
- 单次 PR 最多读取 1000 个文件, 其中媒体最多 100 个、总计 64 MiB
- renamed 旧路径和 PR 中的媒体删除暂不同步

push webhook 在 Git 模式下触发内容重载; 配置 `content.git_pull: true` 会先执行 `git pull --ff-only`.

### 媒体

```yaml
media:
  dir: media
  max_size: 8388608
```

PG 模式只允许 png/jpeg/gif/webp, 随机文件名, 文件内容通过 MIME 嗅探校验. Git 模式会把内容目录中的图片和视频通过只读 `/media` 提供, 并在静态构建时复制到 `dist/media`; 允许 png/jpg/jpeg/gif/webp/avif/mp4/webm.

### 组件指令

在 Markdown 里用 fenced code 声明组件, 语言为 `wx-` 前缀, 内容是 YAML:

````markdown
```wx-infobox
title: 蛋仔
image: /media/abc.png
rows:
  - label: 稀有度
    value: 史诗
tags: [道具, 限时]
```

```wx-notice
type: warn
title: 注意
text: 活动期间获取
```
````

内置组件目前有 `infobox` (标题/图片/属性行/标签/脚注), `notice` (info/warn/danger 提示), `gallery` (图片画廊) 和 `collapse` (原生 details 折叠). 组件内容经过严格校验: 未知字段拒绝, 单个组件上限 8 KiB, 拒绝 YAML 别名, 图片只允许站内绝对路径. 组件渲染失败时页面会显示错误框而不是 500. 内置组件列表由框架维护, 不支持站点自定义模板.

### 结构化实体

PG 模式提供图鉴数据管理:

- `/admin/entities` 由 `manage_users` 权限用户维护: 先建实体类型 (例如 `character`), 再给类型加字段 (标识/名称/类型/必填/下拉选项), 最后录入实体 (标识例如 `kiana`)
- 字段类型支持 `text`, `number`, `bool`, `date`, `url`, `image`, `select`; 保存时按字段定义校验, 未知字段、必填为空和非法格式都会被拒绝
- 编辑页的"关联实体"把页面绑定到实体; 页面修订和审核发布都会保留这个绑定
- 页面里可以用 `wx-infobox` 的 `entity:` 直接引用实体, 自动按字段顺序渲染信息框:
  ````markdown
  ```wx-infobox
  entity: kiana
  rows:
    - label: 备注
      value: 单页补充说明
  ```
  ````
- 实体字段和显式 `rows` 合并显示, 同名字段以显式行为准, 空字段自动跳过
- `/entities` 是公开图鉴页: 按类型筛选, 按字段排序, 并链接到绑定了实体的页面
- 实体数据写入会触发站点重载, 页面缓存键包含实体哈希, 修改实体后旧 HTML 不会被命中

### 审计留存

```yaml
audit:
  retention: 0s            # 0s 关闭; 开启时不得少于 180 天
  archive_dir: data/audit  # 归档目录, 相对路径按配置文件所在目录解析
  interval: 24h            # serve 定时归档间隔
```

- 开启后, serve 启动时先归档一次, 之后按 interval 定时执行
- 归档文件是 NDJSON, 一行一条审计记录, 同级 `.sha256` 文件记录校验和
- 归档成功后才删除旧记录; 删除条件同时限制最大 ID 和 cutoff, 不会误删归档期间新写入的日志
- 超过保留期但不超过 180 天的配置会被拒绝, 避免无意中违反网络日志留存要求
- 审计页使用游标分页, 翻页不依赖 OFFSET, 大数据量下不会越翻越慢
- 手动执行: `go run ./cmd/wikix audit archive -config wikix.yaml`
- 可选环境变量: `WIKIX_AUDIT_RETENTION`, `WIKIX_AUDIT_ARCHIVE_DIR`, `WIKIX_AUDIT_INTERVAL`

### 搜索

```yaml
search:
  backend: auto            # auto, memory, postgres, meilisearch
  url: http://127.0.0.1:7700
  api_key: ""              # 建议通过 WIKIX_SEARCH_API_KEY 提供
  index: wikix
```

- `auto`: PG 内容源走数据库搜索, Git 模式走进程内搜索
- `postgres`: 使用 `ILIKE` 子串 + `to_tsvector('simple', ...)` 全文条件, 标题命中优先; 需要 `content.source: postgres`
- `meilisearch`: 查询走 Meilisearch, 需要 `content.source: postgres`; 发布、审核通过、页面删除/恢复和 PR 导入会同步文档
- 数据库搜索或 Meilisearch 失败会逐级回退, 最终使用进程内搜索并记录警告
- Meilisearch 首次接入或外部改库后执行 `go run ./cmd/wikix search reindex -config wikix.yaml`
- 迁移 014 为未删除页面建立全文 GIN 索引; 中文没有分词器时 PG 后端仍靠 ILIKE
- 查询上限 200 字符, 结果上限 50 条
- 环境变量: `WIKIX_SEARCH_BACKEND`, `WIKIX_SEARCH_URL`, `WIKIX_SEARCH_API_KEY`, `WIKIX_SEARCH_INDEX`

### 监控指标

```yaml
metrics:
  enabled: false           # 默认关闭
  path: /-/metrics
  token: ""                # 建议通过 WIKIX_METRICS_TOKEN 提供
```

- 输出 Prometheus text 0.0.4 格式, 不引入额外依赖
- 指标: HTTP 请求 (method/status)、限流拒绝、页面缓存命中/未命中、渲染次数/失败/总耗时、待审修订、未处理举报
- `token` 非空时要求 `Authorization: Bearer <token>`, 否则返回 404
- 端点默认公开, 生产环境建议配 token 或只允许监控网段访问
- 指标是单进程维度, 多实例在 Prometheus 侧按 instance 聚合

### 全局限流

```yaml
ratelimit:
  enabled: false       # 默认关闭, 上线后按流量开启
  requests: 300        # 每个 IP 在 window 内的总请求数
  window: 1m
  route_requests: 0    # 每个 IP + 路径的请求数, 0 表示关闭
  route_window: 1m
```

- Redis 可用时使用 Lua 滑动窗口, 多实例共享计数; Redis 故障时退回进程内窗口
- 静态资源、`/-/healthz`、GitHub webhook 和 `/media/` 不限流
- 超限返回 429 和 `Retry-After`, 不渲染页面
- 反代部署时先配置 `auth.trusted_proxies`, 否则所有请求会按代理 IP 共用一个桶
- 环境变量: `WIKIX_RATE_LIMIT_ENABLED`, `WIKIX_RATE_LIMIT_REQUESTS`, `WIKIX_RATE_LIMIT_WINDOW`, `WIKIX_RATE_LIMIT_ROUTE_REQUESTS`, `WIKIX_RATE_LIMIT_ROUTE_WINDOW`

### 运维命令

```bash
# 预热全部页面, 默认使用 theme.default, 并发 4
go run ./cmd/wikix warm -config wikix.yaml

# 指定皮肤和并发
go run ./cmd/wikix warm -config wikix.yaml -skin compact -concurrency 8

# 按审计保留期归档并清理旧日志
go run ./cmd/wikix audit archive -config wikix.yaml
```

发布、审核通过、页面保护和删除/恢复都会清理对应路由的旧页面缓存; 缓存键本身包含内容版本, 所以即使清理失败也只会留下到 TTL 为止的旧键.

## 安全默认值

- Markdown 默认不渲染 raw HTML, 链接危险协议由 goldmark 拦截
- 页面使用 html/template 自动转义
- 主题资源从 fs.FS 提供, 目录列表关闭, 路径穿越字符和 Windows 保留名被拒绝
- 单文件默认 1 MiB, frontmatter 拒绝 YAML 别名并限制节点数和嵌套深度
- 登录使用 session + CSRF, 登录失败限流
- 两步验证使用 RFC 6238 (SHA1/6 位/30 秒, 只允许 ±1 时间窗口), 验证码常数时间比对且同一窗口只能用一次; 空密钥或过短密钥一律校验失败, 开启成功后不再回显密钥
- GitHub 登录命中两步验证时不直接签发会话, 改用 5 分钟 HMAC 票据跳转 `/verify`, 票据签名、有效期与账号状态一起校验
- 开启或关闭两步验证会清除该账号全部会话; 已开启时禁止直接重新生成密钥 (409), 防止把二次验证静默关掉
- 管理员代关两步验证要求 `manage_users` 权限和 CSRF, 且不能关闭自己的, 代关会同时踢掉目标账号会话
- 反向代理场景需要显式配置 `auth.trusted_proxies` (CIDR) 才会采信 `X-Forwarded-For`, 未配置时只认连接真实来源 IP
- 评论和举报使用 CSRF, 评论上限 4000 字符, 请求体上限 64 KiB
- 评论默认 5 条/分钟, Redis 可用时跨实例限流, Redis 故障时降级为单实例限流
- 屏蔽词大小写不敏感, 举报只能针对可见评论且不能举报自己
- 用户组变更和账号禁用写入审计日志, 禁用账号会清除 session
- 权限常量统一使用 store 白名单, 组名和权限在服务端校验, 自定义组不能写入未知权限
- 组权限变更和用户组变更共用锁, 保证至少保留一个启用且拥有 manage_users 的账号
- 修改密码需要验证当前密码并受登录失败限流保护, 成功后清除该用户全部 session
- 管理员重置密码和审计日志查询都要求 `manage_users` 权限
- 编辑预览接口要求登录和 CSRF, 请求体和正文大小受站点限制
- 组件内容 YAML 严格校验并拒绝别名, 图片仅允许站内绝对路径, 组件失败降级为错误框
- 审计 CSV 导出会转义以 =+-@ 开头的单元格, 防止 Excel 公式注入
- 不允许禁用或降级当前登录账号, 并且至少保留一个启用的管理员
- GitHub webhook 使用 HMAC-SHA256 验签
- 媒体文件 MIME 白名单和 `nosniff`
- 响应包含 CSP, Referrer-Policy, X-Frame-Options 等基础头

## 开发与测试

```bash
go test ./...
go vet ./...
```

PostgreSQL 和 Redis 集成测试需要先准备两个空数据库:

```bash
export WIKIX_TEST_POSTGRES_URL='postgres://postgres:password@127.0.0.1:5432/wikix_test?sslmode=disable'
export WIKIX_TEST_POSTGRES_URL_SERVER='postgres://postgres:password@127.0.0.1:5432/wikix_test_server?sslmode=disable'
export WIKIX_TEST_POSTGRES_URL_AUDIT='postgres://postgres:password@127.0.0.1:5432/wikix_test_audit?sslmode=disable'
export WIKIX_TEST_REDIS_ADDR=127.0.0.1:6379
go test ./...
```

集成测试只会 truncate 这三个测试库, 不会读取或修改业务库. 没设置环境变量时集成测试会 skip, 报"全绿"前先确认 store/server/audit 三行是 ok.

## 路线图

- 外置搜索引擎 (Meilisearch/Typesense) 和中文分词, 10 万页搜索基准
- 真实 GitHub OAuth/App、SMTP、TLS 反代、Prometheus/Grafana 验收
- 压测与容量结论, 按真实流量调整限流阈值
- 更多 OAuth/OIDC provider, 翻译更新通知, WYSIWYG 编辑器
- 运营准备: 隐私政策、社区公约、备案信息、处置流程

## 文档

- `docs/PROJECT_PLAN.md`: 项目规划. 定位与目标、非目标、架构决策、数据模型、容量与安全设计、合规要求、里程碑与路线图、技术债、风险
- `docs/HANDOVER.md`: 交接文档. 环境准备、命令、已知坑、一轮开发的标准流程、e2e 验收配方、代码地图、当前状态、下一步与待拍板事项
- `TASK.md`: 每轮实现记录、验证结果与修复记录, 是进度基准 (仓库内私有维护文件)
- `CHANGELOG.md`: 版本变更摘要

## 许可

本项目使用 [GNU General Public License v3.0](LICENSE).
