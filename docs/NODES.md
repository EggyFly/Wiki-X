# Wiki-X 节点与操作 ID 清单

本文把当前项目里的板块、路由、后台操作、OIDC 端点、组件、状态枚举、CLI 命令和审计动作整理成稳定 ID. 目标是后续做板块管理、父子元件绑定、互动帖和小游戏时, 有统一的引用名字.

## 1 使用规则

- ID 使用小写字母、数字、下划线, 层级用点号连接, 例如 `admin.users`
- 已发布的 ID 不重命名、不复用; 废弃时标记而不是删掉
- 路由可以变, ID 不应跟着变
- 当前真正被程序读取的只有 `revoke.json` 里的 section ID; 其他 ID 先作为文档和后续数据模型的稳定标识
- 新增功能时同时补本文、i18n 和审计动作

## 2 根节点

| ID | 类型 | 路由/入口 | 说明 |
|---|---|---|---|
| `site` | root | `/` | 站点根节点 |
| `core.home` | page | `/` | 首页 |
| `core.page` | page | `/{route}/` | Markdown 内容页 |
| `core.locale_page` | page | `/{locale}/{route}/` | 多语言内容页 |
| `core.search` | page | `/search` | 站内搜索 |
| `core.media` | asset | `/media/` | 本地或 OSS 媒体, 上传图片有 `t-*.jpg` 缩略图 |
| `core.seo.robots` | page | `/robots.txt` | 爬虫规则 |
| `core.seo.sitemap` | page | `/sitemap.xml` | 站点地图 |
| `core.seo.feed` | page | `/feed.xml` | RSS 2.0 订阅 |
| `core.assets.theme` | asset | `/assets/` | 主题资源 |
| `core.assets.ui` | asset | `/-/assets/` | UI 静态资源 |
| `core.health` | ops | `/-/healthz` | 健康检查 |
| `core.skin` | ops | `/-/skin` | 皮肤切换 |
| `core.metrics` | ops | `/-/metrics` | Prometheus 指标 |
| `core.webhook.github` | ops | `/-/webhooks/github` | GitHub webhook |

## 3 账号与认证

| ID | 类型 | 路由/入口 | 说明 |
|---|---|---|---|
| `auth.login` | page/action | `/login` | 登录 |
| `auth.logout` | action | `/logout` | 退出 |
| `auth.verify` | page/action | `/verify` | 登录二段验证 |
| `auth.register` | page/action | `/register` | 注册, 条件注册 |
| `auth.register_verify` | page/action | `/register/verify` | 重发注册验证 |
| `auth.forgot` | page/action | `/forgot` | 忘记密码 |
| `auth.reset` | page/action | `/reset` | 重置密码 |
| `auth.verify_email` | page/action | `/verify-email` | 邮箱验证 |
| `auth.account` | page | `/account` | 账号中心 |
| `auth.account.password` | action | `/account/password` | 修改密码 |
| `auth.account.totp_setup` | action | `/account/totp/setup` | 生成 TOTP 密钥 |
| `auth.account.totp_enable` | action | `/account/totp/enable` | 开启 TOTP |
| `auth.account.totp_disable` | action | `/account/totp/disable` | 关闭 TOTP |
| `auth.account.totp_qr` | asset | `/account/totp/qr` | TOTP 二维码 |
| `auth.account.recovery_generate` | action | `/account/recovery/generate` | 生成恢复码 |
| `auth.account.delete` | action | `/account/delete` | 删除自己的账号 |
| `auth.account.export` | action | `/account/export` | 导出自己的数据 |
| `auth.account.email` | action | `/account/email` | 修改邮箱 |
| `auth.account.email_resend` | action | `/account/email/resend` | 重发邮箱验证 |
| `auth.github` | page/action | `/auth/github` | GitHub 登录 |
| `auth.github.callback` | action | `/auth/github/callback` | GitHub 回调 |
| `auth.github.link` | action | `/account/link/github` | 绑定 GitHub |

## 4 内容与社区

| ID | 类型 | 路由/入口 | 说明 |
|---|---|---|---|
| `content.view` | page | `/{route}/` | 查看内容 |
| `content.editor` | page | `/edit` | 站内编辑器 |
| `content.preview` | action | `/edit/preview` | Markdown 预览 |
| `content.save` | action | `/edit/save` | 保存修订或 PR |
| `content.history` | page | `/history` | 页面历史 |
| `content.revision` | page | `/revision` | 单版本和 diff |
| `content.review` | page | `/review` | 待审队列 |
| `content.review_approve` | action | `/review/approve` | 批准修订 |
| `content.review_reject` | action | `/review/reject` | 拒绝修订 |
| `content.protect` | action | `/protect` | 修改保护级别 |
| `content.translations` | page | `/translations` | 翻译管理 |
| `content.entities` | page | `/entities` | 结构化实体图鉴, 支持类型筛选和分页 |
| `content.widget` | page | `/widgets/{key}` | 可交互元件 |
| `content.game` | page | `/games/{key}` | 内置小游戏 |
| `content.discussion` | page | `/discussion` | 页面讨论 |
| `content.discussion_add` | action | `/discussion/add` | 发表评论 |
| `content.discussion_hide` | action | `/discussion/hide` | 隐藏评论 |
| `content.discussion_resolve` | action | `/discussion/resolve` | 处理举报 |
| `content.discussion_report` | action | `/discussion/report` | 举报评论 |
| `content.watch` | action | `/watch` | 监视或取消监视 |
| `content.watchlist` | page | `/watchlist` | 监视列表 |
| `content.user_profile` | page | `/u/{name}` | 公开用户主页 |
| `content.upload` | action | `/upload` | 上传媒体 |

## 5 管理后台

| ID | 类型 | 路由/入口 | 说明 |
|---|---|---|---|
| `admin.users` | page | `/admin/users` | 用户管理 |
| `admin.users.create` | action | `/admin/users/create` | 创建用户 |
| `admin.users.groups` | action | `/admin/users/groups` | 修改用户组 |
| `admin.users.password` | action | `/admin/users/password` | 重置密码 |
| `admin.users.status` | action | `/admin/users/status` | 禁用或启用 |
| `admin.users.totp_disable` | action | `/admin/users/totp/disable` | 代关 2FA |
| `admin.users.delete` | action | `/admin/users/delete` | 撤销用户 |
| `admin.groups` | page | `/admin/groups` | 用户组管理 |
| `admin.groups.create` | action | `/admin/groups/create` | 创建用户组 |
| `admin.groups.rights` | action | `/admin/groups/rights` | 修改权限 |
| `admin.groups.delete` | action | `/admin/groups/delete` | 撤销用户组 |
| `admin.pages` | page | `/admin/pages` | 页面管理 |
| `admin.pages.delete` | action | `/admin/pages/delete` | 撤销页面 |
| `admin.pages.restore` | action | `/admin/pages/restore` | 恢复页面 |
| `admin.pages.rename` | action | `/admin/pages/rename` | 移动页面路由并迁移讨论/监视 |
| `admin.reports` | page | `/admin/reports` | 举报管理 |
| `admin.audit` | page | `/admin/audit` | 审计日志 |
| `admin.audit.export` | action | `/admin/audit/export` | 导出 CSV |
| `admin.entities` | page | `/admin/entities` | 实体管理 |
| `admin.entities.types` | action | `/admin/entities/types` | 创建实体类型 |
| `admin.entities.types.delete` | action | `/admin/entities/types/delete` | 撤销实体类型 |
| `admin.entities.fields` | action | `/admin/entities/fields` | 添加字段 |
| `admin.entities.fields.delete` | action | `/admin/entities/fields/delete` | 撤销字段 |
| `admin.entities.save` | action | `/admin/entities/save` | 保存实体 |
| `admin.entities.delete` | action | `/admin/entities/delete` | 撤销实体 |
| `admin.interactive` | page | `/admin/interactive` | Widget/Game 节点管理 |
| `admin.interactive.save` | action | `/admin/interactive/save` | 保存互动节点 |
| `admin.interactive.delete` | action | `/admin/interactive/delete` | 撤销互动节点 |
| `admin.invites` | page | `/admin/invites` | 邀请码管理 |
| `admin.invites.create` | action | `/admin/invites/create` | 创建邀请码 |
| `admin.invites.delete` | action | `/admin/invites/delete` | 撤销邀请码 |
| `admin.revoked` | page | `/admin/revoked` | 已撤销记录 |
| `admin.revoked.restore` | action | `/admin/revoked/restore` | 恢复已撤销记录 |
| `admin.oidc` | page | `/admin/oidc` | OIDC 应用管理 |
| `admin.oidc.create` | action | `/admin/oidc/create` | 创建 OIDC 应用 |
| `admin.oidc.update` | action | `/admin/oidc/update` | 修改 OIDC 应用 |
| `admin.oidc.reset_secret` | action | `/admin/oidc/reset-secret` | 重置 Secret |
| `admin.oidc.delete` | action | `/admin/oidc/delete` | 撤销 OIDC 应用 |

## 6 OIDC 节点

| ID | 类型 | 路由 | 说明 |
|---|---|---|---|
| `oidc.discovery` | endpoint | `/.well-known/openid-configuration` | Discovery |
| `oidc.jwks` | endpoint | `/.well-known/jwks.json` | JWKS |
| `oidc.authorize` | endpoint | `/oauth2/authorize` | 授权请求 |
| `oidc.consent` | endpoint | `POST /oauth2/authorize` | 同意页提交 |
| `oidc.token` | endpoint | `/oauth2/token` | 换取和刷新 token |
| `oidc.userinfo` | endpoint | `/oauth2/userinfo` | 用户信息 |
| `oidc.revoke` | endpoint | `/oauth2/revoke` | 撤销 token |
| `oidc.scope.openid` | scope | - | 身份 |
| `oidc.scope.profile` | scope | - | 用户名和资料 |
| `oidc.scope.email` | scope | - | 邮箱和验证状态 |
| `oidc.claim.sub` | claim | - | 稳定用户 ID |
| `oidc.claim.preferred_username` | claim | - | 用户名 |
| `oidc.claim.name` | claim | - | 显示名 |
| `oidc.claim.email` | claim | - | 邮箱 |
| `oidc.claim.email_verified` | claim | - | 邮箱验证状态 |

详细说明见 [OIDC Provider 运维手册](OIDC-PROVIDER.md) 和 [OIDC 接入方指南](OIDC-CLIENT.md).

## 7 Markdown 与组件节点

| ID | 类型 | 语法 | 说明 |
|---|---|---|---|
| `markdown.gfm` | format | - | GFM 表格、删除线、任务列表、自动链接 |
| `markdown.footnote` | format | `[^1]` | 脚注 |
| `markdown.alert.note` | node | `> [!NOTE]` | 普通提示 |
| `markdown.alert.tip` | node | `> [!TIP]` | 使用建议 |
| `markdown.alert.important` | node | `> [!IMPORTANT]` | 重要信息 |
| `markdown.alert.warning` | node | `> [!WARNING]` | 警告 |
| `markdown.alert.caution` | node | `> [!CAUTION]` | 危险操作 |
| `markdown.math.inline` | node | `$...$` | KaTeX 行内公式 |
| `markdown.math.display` | node | `$$...$$` | KaTeX 块级公式 |
| `markdown.emoji` | node | `:smile:` | emoji 短代码 |
| `markdown.mention` | node | `@username` | 用户主页链接 |
| `markdown.issue` | node | `#123`, `owner/repo#123` | GitHub issue/PR 链接 |
| `markdown.mermaid` | node | ` ```mermaid ` | Mermaid 图表 |
| `component.infobox` | component | `wx-infobox` | 信息框 |
| `component.notice` | component | `wx-notice` | 站内提示框 |
| `component.gallery` | component | `wx-gallery` | 图片画廊 |
| `component.collapse` | component | `wx-collapse` | 折叠块 |
| `component.audio` | component | `wx-audio` | 音频播放器 |
| `entity.kind.text` | field | - | 文本 |
| `entity.kind.number` | field | - | 数字 |
| `entity.kind.bool` | field | - | 布尔 |
| `entity.kind.date` | field | - | 日期 |
| `entity.kind.url` | field | - | HTTP(S) 链接 |
| `entity.kind.image` | field | - | 站内路径或 HTTP(S) 图片 |
| `entity.kind.select` | field | - | 下拉选项 |

Markdown 兼容说明见 [使用手册 12.1](README.md#121-markdown-兼容与安全-html).

## 8 状态与枚举节点

| ID | 取值 | 说明 |
|---|---|---|
| `page.status.revision` | `draft`, `pending`, `published`, `rejected` | 修订状态 |
| `page.protection` | `none`, `semi`, `full` | 页面保护 |
| `comment.status` | `visible`, `hidden` | 评论状态 |
| `report.status` | `open`, `resolved` | 举报状态 |
| `registration.mode` | `off`, `invite`, `open` | 注册模式 |
| `invite.status` | `active`, `used`, `expired` | 邀请码状态 |
| `oidc.client.status` | `enabled`, `disabled`, `revoked` | OIDC 应用状态 |
| `oidc.grant.status` | `active`, `revoked`, `expired` | OIDC 授权状态 |
| `account.status` | `active`, `disabled`, `deleted` | 账号状态 |
| `media.storage` | `local`, `oss` | 媒体存储 |
| `media.oss_acl` | `private`, `public-read`, `public-read-write` | OSS ACL |
| `search.backend` | `auto`, `memory`, `postgres`, `meilisearch` | 搜索后端 |
| `cache.driver` | `auto`, `memory`, `redis` | 缓存驱动 |

## 9 权限节点

| ID | 说明 |
|---|---|
| `right.read` | 读取 |
| `right.edit` | 编辑 |
| `right.review` | 审核 |
| `right.upload` | 上传 |
| `right.manage_users` | 用户和后台管理 |

内置组: `group.admin`, `group.editor`, `group.user`.

## 10 CLI 操作

| ID | 命令 | 说明 |
|---|---|---|
| `cli.build` | `wiki_x build` | 静态构建 |
| `cli.serve` | `wiki_x serve` | 启动 HTTP 服务 |
| `cli.setup` | `wiki_x setup` | 建库、迁移、可选导入和管理员 |
| `cli.migrate` | `wiki_x migrate` | 执行迁移 |
| `cli.import` | `wiki_x import` | Git 内容导入 PG |
| `cli.user_create` | `wiki_x user create` | 创建用户 |
| `cli.user_grant` | `wiki_x user grant` | 授予用户组 |
| `cli.search_reindex` | `wiki_x search reindex` | 重建 Meilisearch 索引 |
| `cli.entity_export` | `wiki_x entity export` | 导出实体类型、字段和实体 JSON |
| `cli.entity_import` | `wiki_x entity import` | 批量导入实体 JSON |
| `cli.media_thumbs` | `wiki_x media thumbs` | 为历史图片媒体回填 320 像素缩略图 |
| `cli.warm` | `wiki_x warm` | 预热页面缓存 |
| `cli.audit_archive` | `wiki_x audit archive` | 归档审计日志 |
| `cli.version` | `wiki_x version` | 版本 |

## 11 审计动作

认证与账号:

`auth.login`, `auth.logout`, `identity.link`, `user.create`, `user.register`, `user.groups`, `user.disable`, `user.enable`, `user.password_change`, `user.password_reset`, `user.totp_setup`, `user.totp_enable`, `user.totp_disable`, `user.recovery_codes_generate`, `user.recovery_code_use`, `user.email_verify`, `user.email_change`, `user.delete`

组与邀请码:

`group.create`, `group.rights`, `group.delete`, `group.restore`, `invite.create`, `invite.delete`, `invite.restore`

内容:

`page.protect`, `page.delete`, `page.restore`, `page.rename`, `revision.submit`, `revision.publish`, `revision.approve`, `revision.reject`

评论与举报:

`comment.hide`, `comment.show`, `comment.reports.resolve`

实体:

`entity_type.create`, `entity_type.delete`, `entity_type.restore`, `entity_field.create`, `entity_field.delete`, `entity_field.restore`, `entity.create`, `entity.update`, `entity.delete`, `entity.restore`

互动节点:

`interactive.create`, `interactive.update`, `interactive.delete`, `interactive.restore`

OIDC:

`oidc.client.create`, `oidc.client.update`, `oidc.client.reset_secret`, `oidc.client.delete`, `oidc.client.restore`

监视:

`watch.remove`

GitHub:

`github.pr.create`, `github.pr.import`

## 12 revoke.json section 映射

| Section ID | 覆盖范围 | 撤销表 |
|---|---|---|
| `page` | 页面删除 | `pages.deleted_at` |
| `user` | 账号删除 | `users.deleted_at` |
| `group` | 用户组、组成员、组权限 | `groups.revoked_at`, `user_groups.revoked_at`, `group_rights.revoked_at` |
| `entity` | 实体类型、字段、实体 | `entity_types.revoked_at`, `entity_fields.revoked_at`, `entities.revoked_at` |
| `invite` | 邀请码 | `invite_codes.revoked_at` |
| `oidc_client` | OIDC 应用 | `oidc_clients.revoked_at` |
| `watchlist` | 监视列表 | `watchlist.revoked_at` |
| `interactive` | Widget/Game 节点 | `interactive_nodes.revoked_at` |

`revoke.json` 中把 section 设为 `false` 表示该 section 的删除入口关闭, 不表示物理删除.

## 13 后续父子与互动节点

`widget.*` 和 `game.*` 已经有最小实现: `interactive_nodes` 表按 `node_type + node_key` 保存, 公开路由是 `/widgets/{key}` 和 `/games/{key}`, 后台入口是 `/admin/interactive`; `config.kind` 目前内置 `card` 和 `quiz`.

下面这些 ID 仍然预留给后续数据模型:

- `board.*`: 板块
- `board.*.thread.*`: 板块帖子
- `thread.*.reply.*`: 回复
- `widget.*.child.*`: 子元件
- `content_type.*`: 可配置内容类型
- `field.*`: 内容类型字段
- `plugin.*`: 编译期扩展点, 见 `internal/extension`

`plugin.*` 没有动态加载: 站点 fork 在 `internal/extension` 里调用 `Register(kind, renderer)` 增加 widget/game 渲染器, 不执行运行时下载或反射调用; 这样避免插件越过权限和模板转义边界.

新增这些节点时, 建议沿用 `parent_id` + `node_id` + `node_type` 的结构, 不要复用现有路由或 section ID.

## 14 维护检查表

- 新路由: 补 `core`/`auth`/`content`/`admin`/`oidc` 节点
- 新后台按钮: 补 `admin.*` 操作和审计动作
- 新组件: 补 `component.*` 和主题样式
- 新 Markdown 语法: 补 `markdown.*` 和清洗策略
- 新状态枚举: 补 `*.status`
- 新删除入口: 决定是否加入 `revoke.json` section
- 新 CLI: 补 `cli.*`
- 新 OIDC 端点或 claim: 同时更新两份 OIDC 文档
