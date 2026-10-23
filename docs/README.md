# Wiki-X 使用与运维手册

本文档是 Wiki-X 的详细说明, 覆盖配置、命令行、社区功能、主题、搜索、审计、安全默认值、前端构建和测试. 项目简介和快速开始见根目录 `README.md`.

## 目录

- [1 内容模式](#1-内容模式)
- [2 配置](#2-配置)
- [3 站点与内容](#3-站点与内容)
- [4 缓存](#4-缓存)
- [5 数据库与账号](#5-数据库与账号)
- [6 邮件与密码找回](#6-邮件与密码找回)
- [7 注册](#7-注册)
- [8 社区治理](#8-社区治理)
- [9 主题和皮肤](#9-主题和皮肤)
- [10 GitHub](#10-github)
- [11 媒体](#11-媒体)
- [12 组件指令](#12-组件指令)
- [13 结构化实体](#13-结构化实体)
- [14 审计留存](#14-审计留存)
- [15 搜索](#15-搜索)
- [16 监控指标](#16-监控指标)
- [17 全局限流](#17-全局限流)
- [18 运维命令](#18-运维命令)
- [19 安全默认值](#19-安全默认值)
- [20 前端构建](#20-前端构建)
- [21 开发与测试](#21-开发与测试)
- [22 路线图](#22-路线图)

## 1 内容模式

Wiki-X 有两种权威内容源, 同一个站点只能选一种:

- Git 模式: 以 Git 工作区为权威内容源, 支持静态构建、定时重载、GitHub webhook 同步和站内编辑开 PR
- PostgreSQL 模式: 以数据库为权威内容源, 支持账号登录、站内编辑、审核发布, 以及合并后的 GitHub PR 单向导入

两种模式共用 Markdown + YAML frontmatter、goldmark 渲染、主题和缓存管线.

## 2 配置

### 2.1 配置文件

```yaml
site:
  title: My Wiki
  description: 站点描述
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
  nav:
    - title: 首页
      href: /
```

站点导航和多语言列表属于结构性配置, 只写在 YAML 里. 服务参数和密钥可以写到 `.env`.
仓库根目录提供了 `wiki_x.yaml`, 默认是 Git 模式并指向示例内容; 直接 `go run ./cmd/wiki_x serve` 即可启动, 不需要再传 `-config`.

### 2.2 .env

推荐把服务参数、数据库口令、SMTP/GitHub 凭据放进 `.env`:

```bash
cp .env.example /path/to/config-dir/.env
# 或 export ENV_FILE=/path/to/.env
# 修改 .env 里的数据库口令、SMTP、GitHub 等配置
```

- `wiki_x` 读取配置文件同目录的 `.env` 
- 优先级: 进程环境变量 > `.env` > YAML
- 用 `ENV_FILE=/path/to/file` 可以指定其他 env 文件
- `.env` 支持 `KEY=VALUE`, `export KEY=VALUE`, 单引号/双引号值, 空行, 整行 `#` 注释, 以及值后面的行内注释
- 行内注释只在 `#` 前面有空格或制表符, 或 `#` 位于值开头时生效; 值本身要带 `#` 时请用引号包住整个值, 引号后面仍可写行内注释
- `.env.example` 顶部的 `POSTGRES_PASSWORD` 和 `PORT` 只给 Docker Compose 用, Wiki-X 本身会忽略这两个键
- `.env` 不支持多行值和变量展开, 复杂结构继续写在 YAML 里
- `.env` 已加入 `.gitignore`, 不要提交真实口令

`ENV_FILE` 本身不会从 `.env` 读取, 需要用进程环境变量传入. `wiki_x version` 这类不读配置的命令不会加载 `.env`.

### 2.3 Git 模式仓库

Git 模式可以直接使用已有工作区, 也可以由 Wiki-X 克隆仓库:

```yaml
content:
  source: git
  dir: content
  git_url: https://github.com/owner/wiki-content.git
  git_branch: main       # 克隆时使用的分支, 留空使用远端默认分支
  git_depth: 0           # 0 为完整克隆, 大于 0 为浅克隆深度, 上限 1000
  git_pull: false        # 启动和收到 push webhook 时执行 git pull --ff-only
  max_file_size: 1048576
  reload_interval: 30s
```

行为约定:

- `git_url` 留空时只使用 `dir` 现有内容, 不会因为 URL 自动克隆; 已有的 Git 仓库仍可由 `git_pull` 在 push webhook 时拉取
- `dir` 不存在或为空目录时执行 `git clone`; `git_depth > 0` 加 `--depth`, `git_branch` 非空加 `--branch`, 留空使用远端默认分支
- `dir` 已经是 Git 仓库时, 只有 `git_pull: true` 才执行 `git pull --ff-only`; 已有仓库不会改 origin 或切换分支, 换仓库/分支前请换目录或手动处理
- `dir` 存在、非空、但不是 Git 仓库时直接报错, 不会覆盖已有文件
- 克隆失败会清理本次新建的目录 (原本就存在的目录不动)
- 需要系统 PATH 里有 `git` 命令; 容器镜像已包含 git
- 拉取以非交互方式运行 (`GIT_TERMINAL_PROMPT=0`), 私库请提前配置 SSH key 或 credential helper
- `git_url` 支持 `http://`, `https://`, `ssh://`, `git://` 和 `git@host:owner/repo.git`; 生产环境优先 https 或 ssh, `http`/`git` 是明文传输
- 仓库地址里不要写明文 token; 用 SSH key、credential helper, 或通过 `CONTENT_GIT_URL` 在运行时注入
- 站内编辑开 PR 仍使用 `github.*` 配置, 和这里的拉取仓库是两条链路

`build`, `serve`, `warm` 在 Git 模式加载前都会执行同样的检查; 目录缺失时会先克隆. `import` 读取 Git 内容目录前也会检查, 所以 PG 模式可以用它导入远程仓库. PostgreSQL 模式的 `build`/`serve`/`warm` 忽略这些字段.

## 3 站点与内容

```yaml
content:
  source: postgres       # git 或 postgres
  dir: content           # Git 内容目录, import 也读这里
  max_file_size: 1048576
  reload_interval: 30s
  git_pull: false
```

内容路由约定: 默认语言使用原路由, 其它语言使用前缀路由. 例如中文首页是 `/`, 英文首页是 `/en/`. 当某个语言页面不存在但默认页面存在时, 会显示默认内容并提示尚未翻译; 多语言页面可以用 `translation_key` 把不同路由的翻译归为一组.

PG 模式提供 `/translations` 翻译管理页: 按 `translation_key` 分组显示各语言的缺失、已同步、可能过期或需要确认状态. 翻译保存时记录默认语言页面的版本, 默认页面更新后旧翻译会标记为可能过期. 缺失语言会生成带 `translation_key` 的创建链接, 编辑页会自动预填. Git 模式没有这个页面, 翻译状态直接通过仓库维护.

## 4 缓存

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

可用环境变量: `CACHE_DRIVER`, `CACHE_TTL`, `CACHE_MAX_ENTRIES`, `CACHE_MAX_BYTES`, `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB`, `REDIS_PREFIX`.

## 5 数据库与账号

```yaml
database:
  url: ""                # 建议通过 DATABASE_URL 提供
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

Git 模式不依赖数据库; 配置 `database.url` 后才会启用账号、审核、后台和站内编辑 PR. PG 模式必须配置数据库.

## 6 邮件与密码找回

```yaml
mail:
  host: smtp.example.com
  port: 587              # 465 直连 TLS 时把 implicit_tls 设为 true
  username: ""
  password: ""           # 建议通过 MAIL_PASSWORD 提供
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

可用环境变量: `MAIL_HOST`, `MAIL_PORT`, `MAIL_USERNAME`, `MAIL_PASSWORD`, `MAIL_FROM`, `MAIL_IMPLICIT_TLS`.

## 7 注册

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
- 站点没有账号时不能自助注册, 先用 `wiki_x user create` 创建第一个管理员
- `/register/verify` 可以重发验证邮件, 不管邮箱是否存在都返回同样的提示

可用环境变量: `REGISTRATION_MODE`, `REGISTRATION_DEFAULT_GROUP`.

## 8 社区治理

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

- `DATABASE_URL`
- `GITHUB_CLIENT_ID`
- `GITHUB_CLIENT_SECRET`
- `GITHUB_WEBHOOK_SECRET`
- `GITHUB_TOKEN`
- `GITHUB_APP_ID`
- `GITHUB_INSTALLATION_ID`
- `GITHUB_PRIVATE_KEY`
- `GITHUB_PRIVATE_KEY_FILE`

仓库地址如果包含凭据, 也建议通过 `CONTENT_GIT_URL` 在运行时注入.

## 9 主题和皮肤

内置 `default` 和 `compact`, 用户可以在页面底部切换, 选择保存在 `wiki_x_skin` cookie 中.

```yaml
theme:
  default: default
  disable_switch: false
  dir: ./my-theme         # 单个自定义主题, 默认皮肤会切换为 custom
  root: ./themes          # 从目录加载多个外部主题
```

外部主题至少包含 `layout.html`, `page.html`, `404.html` 和 `assets`. 静态构建只输出默认皮肤, 运行时才支持切换.

## 10 GitHub

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

push webhook 在 Git 模式下触发内容重载; 配置 `content.git_pull: true` 会先执行 `git pull --ff-only`. 如果启动时 `content.git_url` 非空且目录已经是仓库, `git_pull: true` 也会在启动阶段先拉取一次.

## 11 媒体

```yaml
media:
  storage: local
  dir: media
  max_size: 8388608
  oss:
    endpoint: https://oss-cn-hangzhou.aliyuncs.com
    bucket: my-bucket
    public_base_url: https://cdn.example.com
    path_prefix: media
    acl: private
    signed_url_ttl: 10m
```

PG 模式允许 png/jpeg/gif/webp 和 wav/mp3/ogg/flac, 随机文件名, 文件内容通过 MIME 嗅探校验. Git 模式会把内容目录中的图片、视频和音频通过只读 `/media` 提供, 并在静态构建时复制到 `dist/media`; 允许 png/jpg/jpeg/gif/webp/avif/mp4/webm/wav/mp3/ogg/flac.
Wwise/FMOD 的 `.bnk`/`.wem`/`.bank`/`.fsb` 不会被解析, 上传时会直接拒绝; 请先在游戏工具或导出流程中转成 wav/mp3/ogg/flac. `media.max_size` 同时限制图片和音频, 音频文件较大时需要调高这个值.

`media.storage: oss` 会把 PG 上传和 PR 导入的媒体写入阿里云 OSS, 内容里的 `/media/<name>` 地址保持不变, 访问时由服务端跳转. OSS endpoint 必须使用 https, 避免 AccessKey 和媒体走明文.
`acl: private` 使用带时效的签名 URL, `public-read` 使用公开 URL, `public-read-write` 也使用公开 URL 但允许匿名写入, 不建议在生产环境使用.
引擎只给上传对象设置对象 ACL, 不会自动修改 bucket ACL; 需要匿名写入时必须由站长自行在 OSS 控制台配置 bucket ACL.
`public_base_url` 填自定义域名或 CDN 地址; 留空时按 `bucket.endpoint` 推导, 使用内网 endpoint 时必须显式填写公开地址.
AccessKey 通过 `OSS_ACCESS_KEY_ID` 和 `OSS_ACCESS_KEY_SECRET` 环境变量提供, 不要写进仓库.
切换 `media.storage` 或修改 `OSS_ACL` 不会迁移或重设已有对象; ACL 只对新上传生效, 切换存储前需要手动迁移旧媒体.
Git 模式的内容媒体仍然来自内容目录, OSS 只负责 PG 模式的上传媒体.

## 12 组件指令

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

```wx-audio
src: /media/theme.mp3
title: 主题曲
caption: 活动版本
```
````

内置组件目前有 `infobox` (标题/图片/属性行/标签/脚注), `notice` (info/warn/danger 提示), `gallery` (图片画廊), `collapse` (原生 details 折叠) 和 `audio` (wav/mp3/ogg/flac 播放器). 组件内容经过严格校验: 未知字段拒绝, 单个组件上限 8 KiB, 拒绝 YAML 别名, 图片和音频只允许站内绝对路径. 组件渲染失败时页面会显示错误框而不是 500. 内置组件列表由框架维护, 不支持站点自定义模板.

## 13 结构化实体

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

## 14 审计留存

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
- 手动执行: `go run ./cmd/wiki_x audit archive -config wiki_x.yaml`
- 可选环境变量: `AUDIT_RETENTION`, `AUDIT_ARCHIVE_DIR`, `AUDIT_INTERVAL`

## 15 搜索

```yaml
search:
  backend: auto            # auto, memory, postgres, meilisearch
  url: http://127.0.0.1:7700
  api_key: ""              # 建议通过 SEARCH_API_KEY 提供
  index: wiki_x
```

- `auto`: PG 内容源走数据库搜索, Git 模式走进程内搜索
- `postgres`: 使用 `ILIKE` 子串 + `to_tsvector('simple', ...)` 全文条件, 标题命中优先; 需要 `content.source: postgres`
- `meilisearch`: 查询走 Meilisearch, 需要 `content.source: postgres`; 发布、审核通过、页面删除/恢复和 PR 导入会同步文档
- 数据库搜索或 Meilisearch 失败会逐级回退, 最终使用进程内搜索并记录警告
- Meilisearch 首次接入或外部改库后执行 `go run ./cmd/wiki_x search reindex -config wiki_x.yaml`
- 迁移 014 为未删除页面建立全文 GIN 索引; 中文没有分词器时 PG 后端仍靠 ILIKE
- 查询上限 200 字符, 结果上限 50 条
- 环境变量: `SEARCH_BACKEND`, `SEARCH_URL`, `SEARCH_API_KEY`, `SEARCH_INDEX`

## 16 监控指标

```yaml
metrics:
  enabled: false           # 默认关闭
  path: /-/metrics
  token: ""                # 建议通过 METRICS_TOKEN 提供
```

- 输出 Prometheus text 0.0.4 格式, 不引入额外依赖
- 指标: HTTP 请求 (method/status)、限流拒绝、页面缓存命中/未命中、渲染次数/失败/总耗时、待审修订、未处理举报
- `token` 非空时要求 `Authorization: Bearer <token>`, 否则返回 404
- 端点默认公开, 生产环境建议配 token 或只允许监控网段访问
- 指标是单进程维度, 多实例在 Prometheus 侧按 instance 聚合

## 17 全局限流

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
- 环境变量: `RATE_LIMIT_ENABLED`, `RATE_LIMIT_REQUESTS`, `RATE_LIMIT_WINDOW`, `RATE_LIMIT_ROUTE_REQUESTS`, `RATE_LIMIT_ROUTE_WINDOW`

## 18 运维命令

```bash
# 预热全部页面, 默认使用 theme.default, 并发 4
go run ./cmd/wiki_x warm -config wiki_x.yaml

# 指定皮肤和并发
go run ./cmd/wiki_x warm -config wiki_x.yaml -skin compact -concurrency 8

# 按审计保留期归档并清理旧日志
go run ./cmd/wiki_x audit archive -config wiki_x.yaml
```

发布、审核通过、页面保护和删除/恢复都会清理对应路由的旧页面缓存; 缓存键本身包含内容版本, 所以即使清理失败也只会留下到 TTL 为止的旧键.

## 19 安全默认值

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

## 20 前端构建

前端没有 npm 和运行时框架, 源文件放在 `web/`:

- `web/ui/app.css`, `web/ui/app.js`: 站内 UI 资源
- `web/themes/{default,compact}/style.css`, `favicon.svg`: 主题资源

修改前端源文件后执行:

```bash
go generate ./...
```

生成器会做三件事:

1. 用 `tdewolff/minify` 压缩 CSS/JS/SVG
2. 按内容 sha256 前 8 位命名, 例如 `app~3cf97567.css`, `style~1a5a2d3c.css`
3. 清理目标目录里过期的 `~hash` 旧文件

生成结果统一在 `frontend/dist/` (`ui/` 与 `themes/`), 该目录已加入 `.gitignore`, 只保留 `.gitkeep`. 这些文件会被 embed 进二进制. 模板只引用带 hash 的文件名, 这些文件响应 `immutable` 缓存
CI 和 Docker 构建都会先运行生成器, 也可以用 `go run ./cmd/frontend -check` 检查生成文件是否过期.

## 21 开发与测试

```bash
go generate ./...
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

PostgreSQL 和 Redis 集成测试需要先准备三个空数据库:

```bash
export TEST_POSTGRES_URL='postgres://postgres:password@127.0.0.1:5432/wiki_x_test?sslmode=disable'
export TEST_POSTGRES_URL_SERVER='postgres://postgres:password@127.0.0.1:5432/wiki_x_test_server?sslmode=disable'
export TEST_POSTGRES_URL_AUDIT='postgres://postgres:password@127.0.0.1:5432/wiki_x_test_audit?sslmode=disable'
export TEST_REDIS_ADDR=127.0.0.1:6379
go test ./...
```

集成测试只会 truncate 这三个测试库, 不会读取或修改业务库. 没设置环境变量时集成测试会 skip, 报"全绿"前先确认 store/server/audit 三行是 ok.

## 22 路线图

- 真实 Meilisearch 实例验收、中文分词和 10 万页搜索基准
- 真实 GitHub OAuth/App、SMTP、TLS 反代、Prometheus/Grafana 验收
- 压测与容量结论, 按真实流量调整限流阈值
- 更多 OAuth/OIDC provider, 翻译更新通知, WYSIWYG 编辑器
- 运营准备: 隐私政策、社区公约、备案信息、处置流程

## 相关文档

- `docs/HANDOVER.md`: 交接文档. 环境准备、命令、已知坑、一轮开发的标准流程、e2e 验收配方、代码地图、当前状态、下一步与待拍板事项
- `docs/PROJECT_PLAN.md`: 项目规划. 定位与目标、非目标、架构决策、数据模型、容量与安全设计、合规要求、里程碑与路线图、技术债、风险
- `TASK.md`: 每轮实现记录、验证结果与修复记录, 是进度基准 (仓库内私有维护文件)
- `CHANGELOG.md`: 版本变更摘要
