# Wiki-X

用 Go 写的开源 wiki 框架. 同一套内容模型、渲染、主题和缓存管线, 支持两种内容模式:

- Git 模式: 以 Git 工作区为权威内容源, 支持仓库克隆/拉取、静态构建、定时重载和 GitHub webhook 同步
- PostgreSQL 模式: 以数据库为权威内容源, 支持账号登录、站内编辑、审核发布, 以及合并后的 GitHub PR 单向导入

目标是让一个小团队用单二进制部署一个能长期运营的社区 wiki.

## 特点

- Markdown + YAML frontmatter, goldmark GFM 渲染, 默认关闭 raw HTML
- 按需渲染 + 内存 L1 + Redis L2 缓存 + singleflight 防击穿, ETag/304, gzip, CDN 缓存头
- 本地账号, argon2id 密码, 数据库 session, CSRF, 权限组
- TOTP 两步验证、恢复码、二维码, 邮箱验证与密码找回, 邀请码/开放注册
- 站内编辑, 草稿/待审/发布, 编辑冲突检测, 历史版本, diff, 审核队列
- 页面保护、监视列表、讨论页、评论限流、举报队列、页面软删除/恢复
- 组件指令 `wx-infobox`/`wx-notice`/`wx-gallery`/`wx-collapse`/`wx-audio` 和结构化实体图鉴
- PostgreSQL 全文搜索、进程内搜索和可选 Meilisearch
- 本地或阿里云 OSS 媒体存储, 支持私有签名 URL、公共读和公共读写
- 审计日志、NDJSON 归档、全局限流、Prometheus 指标
- 内置 default/compact 两套皮肤, UI 中英文, 内容多语言路由
- 前端压缩 + 内容哈希文件名, 生成物 embed 进二进制
- Dockerfile、Compose 示例和 GitHub Actions CI

更完整的特性、配置和运维说明见 [docs/README.md](docs/README.md).

## 快速开始

需要 Go 1.27.3 或更高版本. 首次运行先生成前端资源:

```bash
go generate ./...
```

### Git 模式

```bash
go run ./cmd/wiki_x serve -config examples/basic/wiki_x.yaml
go run ./cmd/wiki_x build -config examples/basic/wiki_x.yaml -out dist
```

默认监听 `:8080`. Git 模式不依赖数据库. 如果只需要阅读和静态部署, 到这里就够了.

要用远程仓库时, 在配置里填写 `content.git_url`, Wiki-X 会在目录缺失时自动克隆:

```yaml
content:
  source: git
  dir: content
  git_url: https://github.com/owner/wiki-content.git
  git_branch: main
  git_depth: 0
  git_pull: false
```

### PostgreSQL 社区模式

先准备 PostgreSQL 和 Redis. 数据库口令只通过环境变量传入:

```bash
export DATABASE_URL='postgres://postgres:password@127.0.0.1:5432/wiki_x_dev?sslmode=disable'

go run ./cmd/wiki_x migrate -config examples/community/wiki_x.yaml
go run ./cmd/wiki_x import  -config examples/community/wiki_x.yaml
printf 'change-me-please\n' | go run ./cmd/wiki_x user create -config examples/community/wiki_x.yaml -name admin -admin
go run ./cmd/wiki_x serve -config examples/community/wiki_x.yaml
```

打开 `/login` 登录. 示例配置默认使用内存缓存; 多实例部署时把 `cache.driver` 改为 `redis` 并填写地址.

### Docker Compose

```bash
cp .env.example .env
# 修改 .env: POSTGRES_PASSWORD 要和 DATABASE_URL 里的口令一致
# DATABASE_URL 留空时使用 compose.yaml 里的默认连接串
docker compose up -d --build
docker compose exec app wiki_x user create -config /data/wiki_x.yaml -name admin -admin
```

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
