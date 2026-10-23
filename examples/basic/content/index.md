---
title: Wiki-X
description: 一个用 Go 写的开源 wiki 框架, 支持 Git 与 PostgreSQL 两种内容模式
updated: 2026-10-20
translation_key: home
tags:
  - 框架
  - Wiki
---

Wiki-X 的目标不是复刻 MediaWiki, 而是把社区 wiki 最常需要的能力做成一套清晰、便宜、能长期维护的底层系统.

## 现在已经能做什么

- Git 工作区和 PostgreSQL 两种内容源, 共用同一套 Markdown 渲染和主题
- 账号登录, 权限组, 站内编辑, 待审修订, 审核发布和版本历史
- 编辑冲突检测, unified diff, 审核发布和审计日志
- Git 模式站内编辑创建 GitHub PR, PG 模式接受合并后的 GitHub PR 单向导入
- GitHub App 或 PAT 认证, push webhook 触发同步重载, 可选自动 git pull
- 已有账号可以在账号页绑定 GitHub 身份
- 内存加 Redis 两级缓存, singleflight, ETag/304, gzip, 健康检查
- 站内搜索, PG 图片上传, Git 内容图片通过 `/media` 提供
- 内置 default/compact 两套皮肤, 支持外部主题和用户切换
- UI 文案中英双语, 内容可用 `/en/` 前缀做多语言页面

## 两种内容模式

| 模式 | 权威内容源 | 编辑方式 | 适合场景 |
| --- | --- | --- | --- |
| Git | GitHub 仓库 | PR 或站内编辑开 PR | 个人博客, 文档站, 团队知识库 |
| PostgreSQL | 数据库 | 站内编辑加审核 | 社区 wiki, 大型 UGC 站点 |

每个站点只允许一个权威内容源, 不做双向同步. 从 Git 迁移到 PostgreSQL 时使用单向导入.

## 快速开始

Git 模式:

```bash
go run ./cmd/wiki_x serve -config examples/basic/wiki_x.yaml
go run ./cmd/wiki_x build -config examples/basic/wiki_x.yaml -out dist
```

社区模式需要 PostgreSQL 和账号:

```bash
export DATABASE_URL='postgres://postgres:password@127.0.0.1:5432/wiki_x_dev?sslmode=disable'
go run ./cmd/wiki_x migrate -config examples/community/wiki_x.yaml
go run ./cmd/wiki_x import  -config examples/community/wiki_x.yaml
printf 'change-me-please\n' | go run ./cmd/wiki_x user create -config examples/community/wiki_x.yaml -name admin -admin
go run ./cmd/wiki_x serve -config examples/community/wiki_x.yaml
```

> 先用小规模核心玩家把编辑、审核和搜索跑顺, 再扩展玩法页面和数据模块. 没有内容生态的 wiki, 功能再多也只是空壳.

## 下一步

- [阅读指南](/guide/) 了解内容格式、账号、审核、缓存和 GitHub 集成
- 可视化编辑器和游戏结构化数据组件
- 评论, watchlist, 页面保护和更完整的社区治理
- 内容多语言, 外置搜索和标签化缓存失效
