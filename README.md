# Wiki-X

用 Go 写的开源 wiki 框架. 同一套内容模型、渲染、主题和缓存管线, 支持两种内容模式:

- Git 模式: 以 Git 工作区为权威内容源, 支持仓库克隆/拉取、静态构建、定时重载和 GitHub webhook 同步
- PostgreSQL 模式: 以数据库为权威内容源, 支持账号登录、站内编辑、审核发布, 以及合并后的 GitHub PR 单向导入

目标是让一个小团队用单二进制部署一个能长期运营的社区 wiki.

## 特点

- Markdown + YAML frontmatter, goldmark GFM 渲染, 默认关闭 raw HTML
- 按需渲染 + 内存 L1 + Redis L2 缓存 + singleflight 防击穿, ETag/304, gzip, CDN 缓存头
- 本地账号, argon2id 密码, 数据库 session, CSRF, 权限组
- 内置 OIDC Provider, 后台登记应用, App Secret 只显示一次并可重置, 支持授权码 + PKCE
- TOTP 两步验证、恢复码、二维码, 邮箱验证与密码找回, 邀请码/开放注册
- 站内编辑, 草稿/待审/发布, 编辑冲突检测, 历史版本, diff, 审核队列
- 页面保护、监视列表、讨论页、评论限流、举报队列、页面软删除/恢复
- 业务数据删除改为撤销标记, 保留操作人和原因, 写审计日志
- 组件指令 `wx-infobox`/`wx-notice`/`wx-gallery`/`wx-collapse`/`wx-audio` 和结构化实体图鉴
- PostgreSQL 全文搜索、进程内搜索和可选 Meilisearch
- 本地或阿里云 OSS 媒体存储, 支持私有签名 URL、公共读和公共读写
- 审计日志、NDJSON 归档、全局限流、Prometheus 指标
- 内置 default/compact 两套皮肤, UI 中英文, 内容多语言路由
- 前端压缩 + 内容哈希文件名, 生成物 embed 进二进制
- Dockerfile、Compose 示例和 GitHub Actions CI

更完整的特性、配置和运维说明见 [docs/README.md](docs/README.md).

## 快速开始

需要 Go 1.27.3 或更高版本.

```bash
cp .env.example .env
# 编辑 .env, 至少确认 SITE_BASE_URL
go generate ./...
go run ./cmd/wiki_x serve
```

默认读取根目录的 `wiki_x.yaml`, 使用 Git 模式, 内容在 `examples/basic/content`, 监听 `:8080`.
不需要数据库, 也不需要额外传 `-config`.
静态构建直接运行 `go run ./cmd/wiki_x build`, 产物在 `dist/`.

**要开社区模式时**, 先准备 PostgreSQL 服务, 把 `.env` 改成下面这样, 然后执行一次 setup 和启动.
setup 会尝试创建不存在的数据库、执行迁移，按需导入示例内容并创建管理员.

```dotenv
CONTENT_SOURCE=postgres
CONTENT_DIR=examples/basic/content
DATABASE_SAME=true
DATABASE_MAINTENANCE_DATABASE=postgres
DATABASE_PRIMARY_ADDRESS=127.0.0.1:5432
DATABASE_PRIMARY_USERNAME=postgres
DATABASE_PRIMARY_PASSWORD=你的密码
DATABASE_PRIMARY_DATABASE=wiki_x_dev
DATABASE_PRIMARY_PARAMS=sslmode=disable
SITE_COMMUNITY_UI=true
```

```bash
go run ./cmd/wiki_x setup -import -admin admin
go run ./cmd/wiki_x serve
```

`setup -admin` 会从标准输入读取管理员密码，按提示输入即可.
数据库已经存在时 setup 仍然是幂等的，不会删除已有表或数据.

Redis 是可选的, `CACHE_DRIVER=auto` 在没有 Redis 时会自动使用内存缓存.

要让其他站点用 Wiki-X 账号登录, 配置 `OIDC_ENABLED=true`、公开的 `OIDC_ISSUER` 和 32 字节 base64 的 `OIDC_KEY_ENCRYPTION_KEY`, 启动后在 `/admin/oidc` 登记应用并保存只显示一次的 App Secret. 完整流程见 [OIDC Provider](docs/README.md#51-oidc-provider).

### Docker Compose

Docker 部署参数见 `compose.yaml` 和 `deploy/wiki_x.yaml`.

细节见 [docs/README.md](docs/README.md#2-配置) 和 [docs/HANDOVER.md](docs/HANDOVER.md).

## 配置

站点导航、多语言列表等结构性配置写在 YAML; 服务参数和密钥推荐放进 `.env`.

- `.env` 默认从配置文件同目录读取, 也可以用 `ENV_FILE` 指定
- 优先级: 进程环境变量 > `.env` > YAML
- 完整变量和注释见 [.env.example](.env.example)
- `.env` 已加入 `.gitignore`, 不要提交真实口令

## 文档

- [docs/README.md](docs/README.md): 使用与运维手册, 配置、社区功能、搜索、审计、安全、前端构建、测试
- [docs/HANDOVER.md](docs/HANDOVER.md): 交接文档, 环境准备、命令、已知坑、开发流程、代码地图
- [docs/PROJECT_PLAN.md](docs/PROJECT_PLAN.md): 项目规划, 架构、数据模型、里程碑、技术债
- [TASK.md](TASK.md): 每轮实现记录和验证结果 (仓库内私有维护文件)
- [CHANGELOG.md](CHANGELOG.md): 版本变更摘要

## 许可

本项目使用 [GNU General Public License v3.0](LICENSE).
