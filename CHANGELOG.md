# Changelog

本项目从 0.2.0-dev 起逐步补齐社区 wiki 的完整闭环, 0.3.0 是第一个可部署的功能版本.

## 未发布

### 破坏性变更

- 配置键去掉项目前缀: 使用 `SITE_TITLE`、`REDIS_ADDR` 等裸键, 旧 `WIKIX_*` 不再读取
- 数据库配置改为 `same` + `primary`/`replica`: 地址、用户名、密码、库名和 URL query 形式附加参数
- 主库负责写、迁移、会话和强一致读; 页面、搜索、审计和多数内容只读查询走从库; 从库连接错误回退主库
- `wiki_x setup` 可配置维护库, 支持创建数据库、迁移、可选导入和创建管理员
- CLI、配置文件和目录统一为 `wiki_x`: `cmd/wiki_x`、`wiki_x.yaml`
- Compose 数据库/用户/密码默认值/卷名改为 `wiki_x`, 测试库改为 `wiki_x_test*`
- 内部命名统一为 `wiki_x`: Cookie、Redis 前缀、搜索索引、Prometheus 指标、构建清单、管理员锁
- 升级影响: 旧 Cookie 会话失效, Redis 缓存冷启动, Meilisearch 需重新 `wiki_x search reindex`, Docker 数据需要显式迁移
- OIDC 开启时新增必填 `OIDC_KEY_ENCRYPTION_KEY` (32 字节 base64); 未配置会拒绝启动, 旧明文签名密钥会在首次启动自动加密回写

### 新增

- `wiki_x setup`: 自动创建不存在的数据库、执行迁移, 可选导入内容和创建管理员
- 站点自动输出 `/robots.txt`、`/sitemap.xml` 和 `/feed.xml`, 静态构建同步生成; layout 增加 RSS alternate 链接
- PG 上传图片自动生成 320 像素缩略图, 图鉴图片字段优先使用缩略图, `/media/` 元数据增加进程内缓存和独立限流
- `/admin/pages` 增加页面移动, 事务内迁移讨论和监视列表, 写 `page.rename` 审计
- `wiki_x entity export/import`: 批量导出和导入实体类型、字段和实体 JSON
- `wiki_x media thumbs`: 为升级前上传的历史图片批量回填 320 像素缩略图, `-force` 重新生成
- `/admin/interactive` 和 `/widgets/{key}`、`/games/{key}`: 最小 widget/game 节点实现, 内置 `card` 和 `quiz`
- `internal/extension`: 编译期渲染器注册表, 站点 fork 可以增加 widget/game kind
- 根目录 `wiki_x.yaml` 和 `.env` 一键启动: 默认 Git 模式, 不需要 `-config` 和数据库
- 音频媒体: 上传和 Git 媒体支持 wav/mp3/ogg/flac, Range 播放, `wx-audio` 组件和编辑器工具栏
- Wwise/FMOD bank 不做解析, 上传时拒绝 `.bnk`/`.wem`/`.bank`/`.fsb`, 要求先导出为网页音频格式
- 阿里云 OSS 媒体存储: `media.storage: oss`, 私有签名 URL、public-read、public-read-write, 自定义公开域名和对象前缀
- 内置 OIDC Provider: 后台登记应用, App Secret 只显示一次并可重置, 授权码 + PKCE S256, RS256 ID/access token, UserInfo、Refresh 轮换和 Revoke
- OIDC 发现与 JWKS: `/.well-known/openid-configuration`、`/.well-known/jwks.json`, 签名私钥用 `OIDC_KEY_ENCRYPTION_KEY` 加密后存数据库并由多实例安全共享
- OIDC 应用管理页 `/admin/oidc`: 多回调地址、图标、启停、编辑、重置 Secret 和删除, 操作写审计; 不做动态客户端注册
- Markdown 支持 GitHub alerts (`[!NOTE]`/`[!TIP]`/`[!IMPORTANT]`/`[!WARNING]`/`[!CAUTION]`) 和安全 HTML 白名单
- Markdown 支持 KaTeX 数学公式、Mermaid 图、emoji 短代码、@mention 和 `#123`/`owner/repo#123` issue/PR 引用
- 安全 HTML 允许 details/summary、kbd、mark、表格、图片、音视频等, 渲染后经 bluemonday 清洗
- 新增公开用户主页 `/u/{name}` 和静态构建的 Mermaid/KaTeX/diagrams 资源复制
- 新增 OIDC Provider 运维手册、OIDC 接入方指南和节点/操作 ID 清单

### 修复

- 修复 `.gitignore` 的 `build/` 误忽略 `internal/build`, 远端仓库重新可编译; `.gitattributes` 固定 Go 文件 LF
- 修复审计 CSV 导出被存储层 1000 上限重置成 100 行, 现在最多 5000 行并返回截断响应头
- 修复搜索把 `%`、`_` 当通配符, 增加生成列 `search_vector` 和 GIN 索引, 搜索默认按 IP 限流
- 修复图鉴先 LIMIT 2000 再按类型筛选, 改为 SQL 类型过滤、排序、分页和总数
- 修复内容路由可以占用 `/admin/`、`/login/`、`/oauth2/`、`/search/`、`/u/` 等系统命名空间
- 修复大小写不同路由冲突被提示成"页面已被其他人修改", 现在返回路由占用; 大小写变体 301 到规范路由
- 修复组件图片路径把 `1..2.png` 误判为路径穿越, 改为只拒绝完整的 `..` 段
- PG 模式页面正文改为按需读取, 发布/保护/删除/实体变更改为单页或实体增量刷新, 不再每次写操作全表重载
- PG 模式启动加载改读主库, 避免主从延迟时首页读到旧内容; 实体导入改成整体事务, 中途失败全部回滚
- 互动节点后台列表增加分页
- OIDC 签名私钥不再明文落库, 旧明文密钥首次启动自动加密回写
- OIDC 过期授权码、refresh token、session 和空 grant 每小时清理
- refresh token 并发重试不再误撤销整个授权会话, 只拒绝重复请求
- 禁用 OIDC 应用时撤销已有会话和未使用授权码
- OIDC 后台校验错误不再显示成 App Secret, 并接入中英文 i18n
- 业务数据删除改为撤销标记 (revoked_at/revoked_by/revoke_reason), 保留记录并写审计; 页面和用户沿用 deleted_at
- 用户组、组权限、用户组成员、实体、实体字段、实体类型、邀请码、OIDC 应用和监视列表支持撤销后重建/恢复
- OIDC 所有签名密钥 (含 inactive) 启动时统一加密迁移; 禁用应用和撤销会话改为同一事务

## 0.3.0 (2026-10-21)

### 内容与数据

- 结构化实体 (R1): 实体类型、字段定义、JSONB 数据, 7 种字段校验, 后台录入, 页面绑定, `wx-infobox entity:` 引用, `/entities` 图鉴筛选排序
- 页面软删除与恢复 (R6): 删除原因、操作人、审计记录, 删除后 404, 修订和 PR 导入不会复活已删除页面
- PR 删除同步与媒体导入 (R11): removed 文件软删除, 图片通过 Git Blobs 导入并按内容哈希命名, 正文路径自动改写
- 保护级别 (R12): none/semi/full, semi 允许普通编辑者提交待审编辑请求
- PG 全文搜索 (R10): `ILIKE` + `to_tsvector` + GIN 索引, SQL 摘要, 失败回退内存搜索
- 外置搜索 (R10b): Meilisearch 后端, 发布/审核/删除/PR 导入同步文档, `wiki_x search reindex`, 查询失败逐级回退

### 账号与安全

- 邮箱验证与密码找回 (R2): SMTP、一次性令牌、邮箱唯一、防枚举与双桶限流
- 注册开关与邀请码 (R3): off/invite/open, 邀请码哈希存储与次数/有效期, 开放注册邮箱验证后激活
- 2FA 恢复码与二维码 (R4): 10 个一次性恢复码, 关闭 2FA 清空, 待确认密钥服务端生成二维码
- 账号删除与数据导出 (R7): 匿名化软删除, 关联数据清理, `/account/export` JSON 导出

### 运维与性能

- 审计留存与归档 (R5): `audit.retention` 至少 180 天, NDJSON + sha256 归档, serve 定时执行, `wiki_x audit archive`
- 应用层全局限流 (R8): IP 全局桶 + IP/路径桶, Redis Lua 跨实例, 429 + Retry-After
- 缓存失效与预热 (R9): 按路由清理旧键, `wiki_x warm` 并发预热
- 审计游标分页 (R13): `(created_at, id)` 键集分页, 深分页不退化
- Prometheus 指标 (R14): `/-/metrics`, HTTP/限流/缓存/渲染/队列指标, 可选 Bearer token
- 编辑器增强 (R15): 删除线/任务列表/分隔线, Ctrl+B/I/K, 宽屏分栏预览, 字符数
- 加载反馈: 预览骨架屏扫光, 上传/表单按钮 spinner, 请求超时提示, 减少动态效果偏好支持
- 前端构建: `web/` 源文件经 `cmd/frontend` 压缩, 输出 `filename~hash8.ext`, 模板引用 hash 文件并 immutable 缓存
- 服务配置: 支持配置文件同目录 `.env`, 补全常用环境变量覆盖和 `.env.example`
- `.env` 支持 `KEY=VALUE # 注释` 形式的行内注释, 引号值里的 `#` 保持原样
- Git 模式新增 `content.git_url`/`git_branch`/`git_depth`: 目录缺失时自动克隆, `git_pull` 控制已有仓库的启动拉取
- 文档拆分: 根 `README.md` 只保留项目特点和快速开始, 详细使用与运维说明移到 `docs/README.md`

### 发布

- module 路径改为 `github.com/EggyFly/Wiki-X`
- 增加 GitHub Actions CI (gofmt/build/vet/race test)
- 增加 Dockerfile、compose.yaml 和 `deploy/wiki_x.yaml`
- 版本号 0.3.0
- 安全: 工具链固定 Go 1.27.3, 修复 CVE-2026-111943 (crypto/x509 中间证书吊销检查)

### 已知限制

- 中文搜索仍依赖子串匹配, 大规模内容需要 zhparser 或 Meilisearch/Typesense
- race detector 只在 CI 中运行, 本机 Windows 缺 gcc 未运行
- GitHub OAuth/App 真实凭据流程未在本机验证, 使用 fake API 测试
- renamed PR 文件、媒体删除同步、其他对象存储尚未实现

## 0.2.0-dev (2026-10-20 至 2026-10-21)

前 13 轮的基础版本: Git/PG 双内容源、渲染与主题、两级缓存、账号与权限、审核发布、历史 diff、讨论与举报、用户/组/审计后台、翻译管理、组件指令、TOTP 两步验证与加固.
