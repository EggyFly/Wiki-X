# Changelog

本项目从 0.2.0-dev 起逐步补齐社区 wiki 的完整闭环, 0.3.0 是第一个可部署的功能版本.

## 0.3.0 (2026-10-21)

### 内容与数据

- 结构化实体 (R1): 实体类型、字段定义、JSONB 数据, 7 种字段校验, 后台录入, 页面绑定, `wx-infobox entity:` 引用, `/entities` 图鉴筛选排序
- 页面软删除与恢复 (R6): 删除原因、操作人、审计记录, 删除后 404, 修订和 PR 导入不会复活已删除页面
- PR 删除同步与媒体导入 (R11): removed 文件软删除, 图片通过 Git Blobs 导入并按内容哈希命名, 正文路径自动改写
- 保护级别 (R12): none/semi/full, semi 允许普通编辑者提交待审编辑请求
- PG 全文搜索 (R10): `ILIKE` + `to_tsvector` + GIN 索引, SQL 摘要, 失败回退内存搜索
- 外置搜索 (R10b): Meilisearch 后端, 发布/审核/删除/PR 导入同步文档, `wikix search reindex`, 查询失败逐级回退

### 账号与安全

- 邮箱验证与密码找回 (R2): SMTP、一次性令牌、邮箱唯一、防枚举与双桶限流
- 注册开关与邀请码 (R3): off/invite/open, 邀请码哈希存储与次数/有效期, 开放注册邮箱验证后激活
- 2FA 恢复码与二维码 (R4): 10 个一次性恢复码, 关闭 2FA 清空, 待确认密钥服务端生成二维码
- 账号删除与数据导出 (R7): 匿名化软删除, 关联数据清理, `/account/export` JSON 导出

### 运维与性能

- 审计留存与归档 (R5): `audit.retention` 至少 180 天, NDJSON + sha256 归档, serve 定时执行, `wikix audit archive`
- 应用层全局限流 (R8): IP 全局桶 + IP/路径桶, Redis Lua 跨实例, 429 + Retry-After
- 缓存失效与预热 (R9): 按路由清理旧键, `wikix warm` 并发预热
- 审计游标分页 (R13): `(created_at, id)` 键集分页, 深分页不退化
- Prometheus 指标 (R14): `/-/metrics`, HTTP/限流/缓存/渲染/队列指标, 可选 Bearer token
- 编辑器增强 (R15): 删除线/任务列表/分隔线, Ctrl+B/I/K, 宽屏分栏预览, 字符数
- 加载反馈: 预览骨架屏扫光, 上传/表单按钮 spinner, 请求超时提示, 减少动态效果偏好支持
- 前端构建: `web/` 源文件经 `cmd/frontend` 压缩, 输出 `filename~hash8.ext`, 模板引用 hash 文件并 immutable 缓存

### 发布

- module 路径改为 `github.com/EggyFly/Wiki-X`
- 增加 GitHub Actions CI (gofmt/build/vet/race test)
- 增加 Dockerfile、compose.yaml 和 `deploy/wikix.yaml`
- 版本号 0.3.0
- 安全: 工具链固定 Go 1.27.3, 修复 CVE-2026-111943 (crypto/x509 中间证书吊销检查)

### 已知限制

- 中文搜索仍依赖子串匹配, 大规模内容需要 zhparser 或 Meilisearch/Typesense
- race detector 只在 CI 中运行, 本机 Windows 缺 gcc 未运行
- GitHub OAuth/App 真实凭据流程未在本机验证, 使用 fake API 测试
- renamed PR 文件、媒体删除同步、对象存储媒体尚未实现

## 0.2.0-dev (2026-10-20 至 2026-10-21)

前 13 轮的基础版本: Git/PG 双内容源、渲染与主题、两级缓存、账号与权限、审核发布、历史 diff、讨论与举报、用户/组/审计后台、翻译管理、组件指令、TOTP 两步验证与加固.
