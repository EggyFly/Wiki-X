---
title: 指南
description: 内容格式, 社区模式, 权限审核, 缓存, 主题和 GitHub 集成的入口
updated: 2026-10-20
---

这里放 Wiki-X 的使用说明. 当前版本已经具备 Git 与 PostgreSQL 双模式, 账号体系, 编辑审核, GitHub PR 双向流程和多皮肤.

## 阅读顺序

- [快速开始](/guide/getting-started/) 跑起本地站点, 了解 frontmatter, 路由和社区模式
- 账号, 权限组, 编辑审核和版本历史
- Git 模式 PR 与 PG 模式 PR 导入
- 缓存, 主题, i18n 和生产部署

## 能力边界

| 能力 | 当前版本 | 后续计划 |
| --- | --- | --- |
| 内容源 | Git, PostgreSQL | 更多导入工具 |
| 编辑入口 | Markdown 站内编辑, GitHub PR | 可视化编辑器 |
| 用户体系 | 本地账号, GitHub OAuth, 身份绑定, 权限组 | 用户管理 UI |
| 审核 | 待审, 发布, 拒绝, 历史, diff | 页面保护, 滥用过滤 |
| 缓存 | 内存 + Redis, ETag, gzip | 标签失效, 预热, 指标 |
| 主题 | default, compact, 外部主题, 切换 | 更多内置皮肤 |
| i18n | UI 中英文, locale 前缀内容 | 翻译关联和工作流 |
| 游戏数据 | 普通页面和媒体 | 结构化实体与数据组件 |

如果你要参与开发, 先看 TASK.md 里的需求和测试清单, 确保改动和现有逻辑一致.
