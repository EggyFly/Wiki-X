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
- TOTP 两步验证 (RFC 6238): 账号页自助开启/关闭, 管理员可代关, 登录失败计入限流
- GitHub OAuth 登录, 已有账号可以在 `/account` 绑定 GitHub 身份
- 站内编辑, 草稿/待审/发布, 编辑冲突检测, 历史版本, unified diff, 审核队列
- 编辑页 Markdown 工具栏和实时预览, raw HTML 默认转义, 预览要求登录和 CSRF
- 组件指令 `wx-infobox`/`wx-notice`/`wx-gallery`/`wx-collapse`: 在 Markdown 中用 fenced code 声明, YAML 严格校验, 渲染失败降级为错误框
- Git 模式站内编辑创建 GitHub PR, 会先校验 GitHub 上的最新内容
- PG 模式接受 GitHub PR: 合并后按配置自动导入为待审或已发布修订
- GitHub App 或服务端 PAT 两种仓库认证方式, push webhook 触发同步重载
- 站内搜索, PG 媒体上传, Git 内容目录的图片/视频通过 `/media` 提供
- 页面保护, 监视列表和讨论页: 审核员可保护页面, 登录用户可监视页面并发表评论
- 用户管理后台和举报管理队列: 管理用户组, 禁用/启用账号, 集中处理评论举报
- 内置 default/compact 两套皮肤, 支持外部主题目录和用户切换
- UI 文案 zh-CN/en, 内容可用 `/en/...` 这类 locale 前缀组织并自动切换页面文案
- `/translations` 翻译管理: 按 translation_key 查看缺失/已同步/可能过期的语言页面

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
  github:
    client_id: ""
    client_secret: ""
    redirect_url: ""     # 留空时使用 base_url + /auth/github/callback
```

内容路由约定: 默认语言使用原路由, 其它语言使用前缀路由. 例如中文首页是 `/`, 英文首页是 `/en/`. 当某个语言页面不存在但默认页面存在时, 会显示默认内容并提示尚未翻译; 多语言页面可以用 `translation_key` 把不同路由的翻译归为一组.

PG 模式提供 `/translations` 翻译管理页: 按 `translation_key` 分组显示各语言的缺失、已同步、可能过期或需要确认状态. 翻译保存时记录默认语言页面的版本, 默认页面更新后旧翻译会标记为可能过期. 缺失语言会生成带 `translation_key` 的创建链接, 编辑页会自动预填. Git 模式没有这个页面, 翻译状态直接通过仓库维护.

### 社区治理

PG 模式提供页面保护, 监视列表和讨论页:

- 审核权限用户可以在编辑页保护或取消保护页面; 保护后只有审核权限用户可以编辑, GitHub PR 导入也会跳过保护页面
- 登录用户可以在历史页监视页面, 在 `/watchlist` 查看最近修改; Git 模式显示文件更新时间, 作者列标记为 Git 仓库
- `/discussion?route=/guide/` 查看讨论; 登录用户发表评论, 审核员可以隐藏或恢复
- 评论按路由保存, 上限 4000 字符, 输出经过模板转义; 隐藏评论只对审核员可见
- 登录用户可以举报评论, 审核员可以看到未处理举报数和原因并忽略举报; 隐藏评论会自动关闭举报
- `/admin/users` 需要 `manage_users` 权限: 可以创建本地账号, 调整内置或自定义用户组, 禁用或启用账号, 重置密码
- 禁用一个账号会删除它的所有 session, 立即退出登录; 不能禁用或降级当前登录账号
- 系统始终保留至少一个启用的管理员, 最后一个管理员的降级和禁用会被拒绝
- `/admin/groups` 支持新建自定义用户组和编辑 read/edit/review/upload/manage_users 权限
- 内置 admin/editor/user 组不能删除, 自定义组删除后会同时移除成员关系和权限
- 最后一个拥有 manage_users 权限的启用账号不能被降级、禁用或通过改组权限移除
- `/account` 支持修改或设置本地密码; 修改后其他设备会退出登录, 当前会话保留
- `/account` 支持开启 TOTP 两步验证: 先生成密钥, 在认证器 App 手动录入密钥或点 otpauth 链接, 再用 6 位验证码确认开启
- 开启后本地密码登录必须再输验证码; 关闭两步验证需要当前密码, 没有本地密码的 GitHub 账号改用一次性验证码
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

PG 模式的 `pull_request` webhook 在 PR 合并后读取变更的 Markdown 文件, 单向导入为待审或已发布修订. 会自动分页读取 PR 文件; 删除文件、媒体文件和超过 1000 个文件的 PR 暂不处理.

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

## 安全默认值

- Markdown 默认不渲染 raw HTML, 链接危险协议由 goldmark 拦截
- 页面使用 html/template 自动转义
- 主题资源从 fs.FS 提供, 目录列表关闭, 路径穿越字符和 Windows 保留名被拒绝
- 单文件默认 1 MiB, frontmatter 拒绝 YAML 别名并限制节点数和嵌套深度
- 登录使用 session + CSRF, 登录失败限流
- 两步验证使用 RFC 6238 (SHA1/6 位/30 秒, 只允许 ±1 时间窗口), 验证码常数时间比对, 开启成功后不再回显密钥
- 管理员代关两步验证要求 `manage_users` 权限和 CSRF, 且不能关闭自己的
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
export WIKIX_TEST_REDIS_ADDR=127.0.0.1:6379
go test ./...
```

集成测试只会 truncate 这两个测试库, 不会读取或修改业务库.

## 路线图

- 翻译更新通知, 翻译工作流和 Git 模式翻译状态
- 可视化编辑器, 更多组件指令, 游戏结构化实体与数据录入
- 用户组重命名、描述和排序, 邮箱验证, 验证码, 邮件通知, 两步验证二维码和恢复码
- 外置搜索, 标签化缓存失效, 预热, Prometheus 指标
- PR 删除同步和媒体 PR 导入, 更多 OAuth/OIDC provider

## 文档

- `docs/PROJECT_PLAN.md`: 项目规划. 定位与目标、非目标、架构决策、数据模型、容量与安全设计、合规要求、里程碑与路线图、技术债、风险
- `docs/HANDOVER.md`: 交接文档. 环境准备、命令、已知坑、一轮开发的标准流程、e2e 验收配方、代码地图、当前状态、下一步与待拍板事项
- `TASK.md`: 每轮实现记录、验证结果与修复记录, 是进度基准 (仓库内私有维护文件)

## 许可

本项目使用 [GNU General Public License v3.0](LICENSE).
