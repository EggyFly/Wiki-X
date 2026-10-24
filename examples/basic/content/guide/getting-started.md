---
title: 快速开始
description: 从 Markdown 文件到可访问页面, 只要几分钟
updated: 2026-10-20
tags:
  - 入门
---

## 运行示例站

在仓库根目录执行:

```bash
go run ./cmd/wiki_x serve -config examples/basic/wiki_x.yaml
```

打开 `http://localhost:8080` 就能看到当前页面. 需要自动同步内容时配置 `content.reload_interval`, 或者用 GitHub webhook 触发重载.

生成静态文件:

```bash
go run ./cmd/wiki_x build -config examples/basic/wiki_x.yaml -out dist
```

输出目录会包含每个页面的 `index.html`, `404.html`, 主题资源, 以及一份 `.wiki_x-build.json`. 这份清单只用于清理上一次构建产生, 这次不再需要的文件, 不会删除目录里的其他内容.

## 文件与路由

| 文件 | 路由 |
| --- | --- |
| content/index.md | / |
| content/about.md | /about/ |
| content/guide/index.md | /guide/ |
| content/guide/start.md | /guide/start/ |

路径里的每一段都会做安全校验. 带 `..`, 反斜杠, 冒号, 通配符等字符的 slug 会被拒绝, 避免在 Windows 上构建时出现路径穿越或非法文件名.

## frontmatter

```yaml
---
title: 页面标题
description: 页面摘要, 会出现在 meta description 里
slug: custom/path
draft: false
updated: 2026-10-20
translation_key: guide-start
tags:
  - 示例
---
```

- `title` 为空时用文件名兜底
- `slug` 可以覆盖默认路由, 但必须通过安全字符校验
- `draft: true` 的页面不会进入构建和路由表
- `updated` 为空时用文件修改日期
- `translation_key` 用于把不同语言的不同路由归为一组翻译

## 主题

默认主题在 `themes/default`, 通过 embed 打进二进制. 要改样式, 复制一份主题目录, 然后在配置里指向它:

```yaml
theme:
  dir: ./my-theme
```

主题目录至少需要 `layout.html`, `page.html`, `404.html` 和 `assets` 目录. 模板使用 Go 标准库的 html/template, 站点内容会经过上下文自动转义. 主题文件变化后, 资源 URL 的版本号会跟着变.

> 主题属于受信的站点代码, 模板本身可以访问渲染数据, 不要给普通用户上传或编辑模板的权限.

## 缓存与 Redis

页面渲染采用按需方式, 不在启动时把全站 HTML 塞进内存. 读取顺序是:

1. 进程内 TTL 缓存
2. Redis 页面缓存
3. 渲染页面, 并通过 singleflight 合并同一页面的并发回源

本地开发可以保持默认配置, 不依赖 Redis:

```yaml
cache:
  driver: auto
  ttl: 10m
  max_entries: 2000
  max_bytes: 67108864
```

多实例或大流量部署时配置 Redis:

```yaml
cache:
  driver: redis
  ttl: 10m
  redis:
    addr: 127.0.0.1:6379
    password: ""
    db: 0
    prefix: my-wiki
```

每个站点要使用独立的 `prefix`. 敏感配置可以用 `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB` 环境变量覆盖.

页面响应带 `ETag`, `Cache-Control` 和 gzip. 配置 CDN 时可以用 `s-maxage` 和 `stale-while-revalidate` 降低回源压力.

## 内容热重载

默认只在启动时读取内容. 需要定时同步 Git 工作区时开启:

```yaml
content:
  reload_interval: 30s
```

每次重载会重新索引内容并加载主题. 页面缓存键包含内容版本, 主题版本和渲染器版本, 旧缓存会在 TTL 后自然过期. 间隔不要设得太短, 页面很多时重复扫描会消耗 CPU.

## 社区模式

社区模式以 PostgreSQL 为权威内容源, 需要先设置数据库连接:

```bash
export DATABASE_SAME=true
export DATABASE_PRIMARY_ADDRESS=127.0.0.1:5432
export DATABASE_PRIMARY_USERNAME=postgres
export DATABASE_PRIMARY_PASSWORD=password
export DATABASE_PRIMARY_DATABASE=wiki_x_dev
export DATABASE_PRIMARY_PARAMS=sslmode=disable
go run ./cmd/wiki_x migrate -config examples/community/wiki_x.yaml
go run ./cmd/wiki_x import  -config examples/community/wiki_x.yaml
go run ./cmd/wiki_x serve   -config examples/community/wiki_x.yaml
```

`import` 把 Git 内容单向导入数据库, 之后不再和 Git 双向同步. `migrate` 是幂等的, 也可以在配置里打开 `database.auto_migrate`.

## 账号与权限

站内账号通过 CLI 创建, 密码从 stdin 读取:

```bash
printf 'change-me-please\n' | go run ./cmd/wiki_x user create -config examples/community/wiki_x.yaml -name admin -admin
printf 'editor-password\n'  | go run ./cmd/wiki_x user create -config examples/community/wiki_x.yaml -name editor
go run ./cmd/wiki_x user grant -config examples/community/wiki_x.yaml -name editor -group editor
```

- 权限组: `admin`, `editor`, `user`
- admin 拥有编辑, 审核, 上传和用户管理权限
- editor 可以编辑和上传, 修改进入审核队列
- user 默认只读
- GitHub OAuth 只负责登录身份, 和仓库写入凭据分开配置
- 本地账号登录后可以访问 `/account`, 把 GitHub 身份绑定到当前账号
- 生产环境设置 `auth.secure_cookies: true`, 并在 HTTPS 后运行

## 编辑与审核

登录后访问 `/edit` 新建或编辑页面:

- 管理员保存后直接发布
- editor 保存后进入 `/review` 待审队列
- 审核人可以查看 unified diff 后批准或拒绝
- 页面历史在 `/history?route=/...`
- 过期基础版本会返回冲突, 不会静默覆盖别人刚发布的内容

## GitHub 集成

GitHub 登录和 GitHub 仓库写入是两条独立链路. 仓库写入建议使用只授权内容仓库的 fine-grained PAT:

可以使用 GitHub App, 也可以使用只授权内容仓库的 fine-grained PAT:

```bash
export GITHUB_APP_ID='123'
export GITHUB_INSTALLATION_ID='456'
export GITHUB_PRIVATE_KEY_FILE='/secure/github-app.pem'
```

```yaml
github:
  repo: owner/repo
  app_id: 123
  installation_id: 456
  private_key_file: /secure/github-app.pem
  token: ""              # 不使用 App 时可改为 PAT
  base_branch: main
  content_path: content
  pr_import: pending
  webhook_secret: "至少 16 个字符"
```

- Git 模式: 站内编辑会先校验 GitHub 上的最新内容, 再创建分支和 PR
- Git 模式: push webhook 触发内容重载, `content.git_pull: true` 时先执行 `git pull --ff-only`
- PG 模式: 合并后的 PR 会被读取并单向导入, `pr_import: pending` 进入审核, `publish` 直接发布, `off` 关闭
- PR 文件会自动分页, 当前不处理删除文件, 媒体文件和超过 1000 个文件的 PR

## 媒体

PG 模式在编辑页上传图片, 文件保存在 `media.dir`, 通过 `/media/<随机文件名>` 访问.

Git 模式把内容目录里的图片和视频直接映射到 `/media/<相对路径>`, 支持 png/jpg/jpeg/gif/webp/avif/mp4/webm. 静态构建会把它们复制到 `dist/media`. Markdown 里使用 `/media/...` 引用即可.

## 多皮肤与 i18n

内置 `default` 和 `compact` 两套皮肤, 页面底部可以切换, 选择保存在 cookie 中. 站点也可以用 `theme.dir` 指定单个自定义主题, 或用 `theme.root` 加载多个外部主题. 静态构建只输出默认皮肤.

```yaml
site:
  language: zh-CN
  locales:
    - code: zh-CN
      label: 中文
      prefix: ""
    - code: en
      label: English
      prefix: en
```

默认语言使用原路由, 其它语言使用前缀路由. 例如中文首页是 `/`, 英文首页是 `/en/`. 页面 UI 文案会跟随当前内容语言.

翻译页面可以用 `translation_key` 关联不同路由:

```yaml
translation_key: guide-start
```

当某个语言的页面不存在但默认页面存在时, 会显示默认语言内容并给出创建翻译的提示. 翻译管理 UI 和更新提醒仍在路线图中.

## 生产部署

- 前面放 CDN 和反向代理, 只暴露必要的端口
- 多实例共享 Redis 页面缓存
- `server.access_log` 默认关闭, 只记录 5xx; 访问日志交给网关或按需打开
- 用 `GET /-/healthz` 做存活检查
- 用环境变量传入 Redis 密码, 不要把密码提交进仓库
- Redis 启动失败会阻止服务启动; 运行中 Redis 故障会退化成直接渲染, 并限制错误日志频率

## 安全默认值

- Markdown 默认不渲染 raw HTML, 危险链接会被 goldmark 处理
- 页面模板使用 html/template 自动转义标题, 描述和链接
- 静态资源从 fs.FS 提供, 目录列表被关闭
- 响应包含 CSP, nosniff, frame 限制等基础安全头
- 内容源拒绝符号链接, 避免读取仓库以外的文件
- 单文件大小默认 1 MiB, frontmatter 拒绝 YAML 别名并限制节点数量和嵌套深度
- 路由拒绝大小写不敏感冲突和系统保留路径

## 接下来做什么

下一批重点是可视化编辑器, 游戏结构化数据组件, 内容 i18n, 评论和 watchlist. 内容规模继续变大后, 再补外置搜索, 标签化缓存失效, 预热和监控指标.
