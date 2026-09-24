# sub2api 项目 Agent 规则

本文件记录本仓库的项目级约束。若与用户当前会话明确要求冲突，以用户当前要求为准。

## 默认工作方式

- 默认使用简体中文沟通，代码标识符保持英文。
- 修改前先看工作树状态，不能覆盖或回退用户已有改动。
- 当前仓库可能存在本地 UI 定制；合并上游代码时必须保留用户原先的 UI 改动，不能因为选择上游版本而直接丢弃本地前端交互、样式、路由或构建配置。
- 对数据库、schema、线上样例数据、SQL 查询做排查时，优先通过 `mcp_router` 暴露的数据库工具执行只读检查；写入、删除、迁移、DDL、批量更新或数据修复必须先确认。

## 四段版本号与打包规则

- 触发信号：本地修改后打包、拉取远程代码合并后打包，或用户明确指定版本。
- 适用范围：本仓库应用版本和部署二进制；版本来源统一为 `backend/cmd/server/VERSION`，格式为 `主版本.次版本.补丁版本.本地修订号`，例如 `0.2.7.2`。
- 前三位跟随实际合并的远程上游版本；仅本地功能改动、修复或定制时，保持前三位不变，第四位在上一已出包版本基础上递增 `1`，例如 `0.2.7.1 → 0.2.7.2`。
- 远程合并使前三位升级时，第四位重置为 `1`，例如 `0.2.7.2 → 0.2.8.1`；若远程合并未提升前三位，则保持前三位并递增第四位，不重置为 `1`。
- 用户明确指定版本时优先采用指定值；同一次出包的验证或构建重试沿用该版本，不重复递增。本次本地修改出包版本为 `0.2.7.2`。
- 打包前更新版本文件，先构建前端，再构建带 `embed` tag 的 Linux amd64 后端。产物命名为 `sub2api-linux-amd64-v<四段版本>-<当前提交短 SHA>`；含未提交源码时须记录 `vcs.modified=true` 及其来源。
- 验证方式：核对版本文件、二进制版本信息、产物名称和构建记录中的版本一致，执行相关测试及构建，并记录大小、SHA256、目标平台和 `go version -m`。这是本仓库的四段版本约定，不套用通用三段 SemVer 自动递增规则。

## 本地定制保护清单

本仓库当前包含一组必须和前端页面、后端接口、持久化结构一起保留的本地功能。后续合并 `old-origin` 或其它上游代码时，不能只保留页面文件，也不能用上游版本整体覆盖下列共享文件。发生冲突时先按功能链路核对，再选择性吸收上游修复。

### 安全策略与消费冻结

- 分组安全策略链路：`backend/ent/schema/group.go`、生成的 `backend/ent/*` 分组文件、`backend/internal/repository/group_repo.go`、`backend/internal/repository/security_policy_repo.go`、`backend/internal/service/security_policy*.go`、`backend/internal/handler/admin/security_policy_handler.go`、`backend/internal/server/routes/admin.go`，以及 `frontend/src/api/admin/securityPolicy.ts`、后台安全策略页面和对应中英文 i18n。关键词、会话解封、网关审计快照和策略版本必须保持前后端字段一致。
- SpendGuard 链路：`backend/internal/service/spend_guard*.go`、`backend/internal/service/api_key_spend_guard.go`、`backend/internal/repository/spend_guard*.go`、`backend/internal/handler/admin/spend_guard_handler.go`、`backend/internal/server/routes/admin.go`、`frontend/src/api/admin/spendGuard.ts`、`frontend/src/views/admin/SpendGuardView.vue`，以及迁移 `backend/migrations/248_spend_guard_persistence.sql`。冻结、解冻、消费拦截、事务持久化、缓存失效和管理员页面必须成套存在。
- 与上述功能相关的迁移 `backend/migrations/238_content_moderation_overturned.sql`、`239_account_random_proxy_policy.sql`、`247_group_security_policy.sql`、`248_spend_guard_persistence.sql` 属于本地数据契约；合并时先检查同名迁移和执行顺序，不得删除、重命名或覆盖已有字段。

### 账号保护、代理与 TLS

- 账号保护链路：`backend/internal/service/account_protection*.go`、`account_anti_degrade.go`、`account_mode1_protection.go`、`account_request_integrity.go`、`account_traffic_*.go`、`backend/internal/repository/account_protection.go`、`account_traffic_cache.go`、`backend/internal/handler/admin/anti_degrade_handler.go` 和账号路由。账号状态转换、失败释放、停止调度、流量事件和 WebSocket 兼容逻辑必须一起保留。
- 随机代理链路：`backend/internal/service/account_proxy_selection.go`、`account_random_proxy_*.go`、`backend/internal/repository/account_random_proxy.go`、`frontend/src/components/account/RandomProxySettings.vue`、`ProtectionToggle.vue`、`frontend/src/utils/randomProxy.ts`、`proxyParser.ts` 及账号创建/编辑/批量编辑页面。随机代理必须是请求时选择的运行时关联，不能被错误持久化为固定 `proxy_id`；空池策略 `reject`、`disable`、`direct` 的语义不能互换。
- OpenAI 插件出站账号目录 `backend/internal/service/openai_plugin_account_directory.go` 必须复用随机代理选择和空池停用逻辑，不能让插件路径绕过代理池后直连。固定代理、随机代理、空池和非 OAuth/影子账号都要保留定向测试。
- TLS 指纹链路：`backend/internal/pkg/tlsfingerprint/*`、`backend/internal/service/tls_fingerprint_profile_*.go`、`backend/internal/pkg/tlsfingerprint/proxy_context_test.go` 及账号转发/测试路径。代理选择、TLS profile、并发和失败降级必须保持同一请求上下文，不能只合并 UI 配置而遗漏运行时使用。

### Codex、插件与分组能力

- Codex 票据和流量管理：`backend/internal/handler/admin/account_codex_ticket_handler.go`、`account_traffic_handler.go`、`backend/internal/service/openai_codex_ticket*.go`、`openai_ws_*`、账号路由、`frontend/src/api/admin/accounts.ts`、账号页面和对应测试。票据开关、采集/注入、WebSocket、调度器写入隔离和账号编辑路径必须成套验证。
- 插件运行时链路：`backend/internal/repository/plugin_kv_store.go`、`backend/internal/service/plugin_manager.go`、`plugin_runtime.go`、`plugin_host_services.go`、`openai_plugin_transport.go`、`backend/cmd/server/wire*.go`。新增依赖必须同时出现在 wire、repository provider、handler/service 构造函数和运行时调用中，不能出现编译通过但运行时未注入的半链路。
- 分组统计：`backend/internal/repository/group_detail_stats.go`、`backend/internal/service/group_detail_stats.go`、`backend/internal/handler/admin/group_handler.go`、`backend/internal/server/routes/admin.go`、`frontend/src/api/admin/groups.ts` 和分组页面/弹窗。统计 API、分页/筛选参数和页面展示必须保持一致。

## 本地定制的最优合并策略

1. 合并前记录当前分支、HEAD、`git status --short`、上游 ref 和本地备份；对重叠的已跟踪路径做路径级保护，不能使用整仓库 `theirs` 或 `ours`。未跟踪迁移、生成代码、资源和构建产物先逐项判断归属。
2. 先合并基础契约：迁移、Ent schema/生成代码、repository 接口和 wire provider；再合并 service；然后合并 handler/routes；最后合并 API wrapper、页面、i18n 和导航。每一步都检查调用方和被调用方是否同时存在。
3. 前端冲突优先保留本地认证页、首页、内部壳层和后台功能入口；上游的空值/有限数值保护、兼容 payload、错误处理和新增字段可以按块吸收。禁止用上游文件整体覆盖 `AuthLayout.vue`、`HomeView.vue`、`AppLayout.vue`、`AppHeader.vue`、`AppSidebar.vue`、`style.css` 或账号管理组件。
4. 对每个新增前端 API 执行契约核对：`frontend/src/api` 路径 → `backend/internal/server/routes` → handler → service → repository/setting/schema/migration。发现 404、未注入依赖或只存在 UI 的接口时，先修完整链路再继续。
5. 对随机代理、消费冻结、账号保护和安全策略执行行为优先验证：成功路径、边界值、空池/冻结/策略拒绝、错误回滚、重复请求和旧数据库迁移。不要只做 TypeScript 编译或静态 grep。
6. 合并后先跑定向测试，再跑前端 `typecheck`、生产构建和后端 handler/service/routes 测试；前端构建成功后再构建 Linux amd64 embed 二进制。浏览器可用时检查 `/home`、`/login`、`/register`、后台壳层和移动端溢出；浏览器不可用时至少做静态契约和 HTTP smoke，并明确记录限制。
7. 提交前执行 `git diff --check`、冲突标记扫描和 staged-file 审计。只提交源码/文档；`sub2api-linux-amd64-*`、`frontend/dist`、`backend/internal/web/dist`、`.codex-run`、`.pnpm-store`、日志和临时测试输出不得进入 Git。
8. 生成物命名使用版本和最终提交短 SHA，报告路径、版本、提交号、目标平台、大小、SHA256、build tags、`CGO_ENABLED` 和 `go version -m`。若工作区仍有本地未提交改动，`vcs.modified=true` 属于预期状态，必须说明其来源。

## 合并上游代码要求

合并 `Wei-Shaw/sub2api` 上游最新代码时，必须按以下顺序执行：

1. 合并前记录当前分支、最近提交、`git status --short`，区分用户已有改动和本次要做的改动。
2. 拉取上游后优先保留本地 UI 改动；遇到前端冲突时不能简单使用 `theirs` 覆盖本地 UI。
3. 合并后检查前端 API 调用和后端路由是否仍然成对存在，不能只保留 UI 而漏掉 UI 依赖的后端接口。
4. 对新增或本地保留的功能，必须确认 `frontend/src/api`、页面组件、后端 route、handler、service、repository/DB 查询链路完整。
5. 合并完成前必须跑定向验证；没有验证证据时，不要声称“合并完成”“可部署”。
6. 每次合并完上游新代码后，必须先执行前端生产构建，再用最新前端产物构建后端 Linux amd64 可执行二进制；最终回复必须给出二进制路径、版本、提交号、大小和 SHA256。
7. 每次拉取 / 合并上游新代码并产出构建物后，必须提交源码合并结果并推送至远端；构建物、二进制和临时产物不得暂存、提交或推送。

## 登录 / 注册页 UI 定制门禁

本仓库存在本地认证页视觉定制，合并上游或处理前端冲突时必须保留，不能用上游版本直接覆盖。

必须保留的文件与链路：

- `frontend/src/components/layout/AuthLayout.vue`
  - `variant="showcase"` 的左右展示布局必须保留，登录页和注册页共用这套布局。
  - 保留开灯 / 关灯完整主题切换，使用 `document.documentElement.classList.toggle('dark')` 与 `localStorage.theme`。
  - logo 容器使用白底弱边框风格，不要恢复成绿色或渐变底。
  - 左侧大标题为两行：`为热爱赋能` / `为创造而生`，标题中不要重复出现 `iCode`。
  - 日间副文案为 `你的光芒会照亮每一个人。`，夜间副文案为 `你的努力会被你想要的人看见`。
  - 不显示 `Day Mode / 开灯后`、`Night Mode / 关灯` 这类模式标签行。
  - QQ 群入口保持低调、左侧对齐、紧凑宽度；只显示 `QQ群 1009439039` 和网格图标，不显示“悬停显示”文字。
  - QQ 二维码弹层允许覆盖指标卡片区域，不能撑开布局或造成左侧内容拥挤。
  - QQ 群入口旁保留 `Codex 下载` 入口，使用新标签页打开 `https://codex.download.icodett.xyz/`，并保持低调紧凑、左侧对齐。
  - 移动端 / 单列布局下隐藏左侧展示区，只保留登录或注册表单卡片区域，避免 QQ、Codex、指标卡和品牌文案挤占首屏。
- `frontend/src/views/auth/LoginView.vue`
  - 必须继续使用 `<AuthLayout variant="showcase" :form-label="t('auth.loginPanelTitle')">`。
  - 登录提交、2FA、OAuth、Turnstile、登录协议、忘记密码和注册链接逻辑不能被 UI 合并覆盖破坏。
- `frontend/src/views/auth/RegisterView.vue`
  - 必须继续使用 `<AuthLayout variant="showcase" :form-label="t('auth.createAccount')">`。
  - 注册、邮箱验证、返利邀请码、优惠码、OAuth、Turnstile 和登录协议逻辑不能被 UI 合并覆盖破坏。
- `frontend/src/i18n/locales/zh.ts` 与 `frontend/src/i18n/locales/en.ts`
  - 必须保留 `auth.loginPanelTitle`、`auth.loginPanelSubtitle`、`auth.loginHero` 相关文案键。
- `frontend/public/qq-group-1009439039.png`
  - 必须保留，作为 QQ 群 `1009439039` 的扫码加群二维码资源。

合并后至少验证：

```powershell
pnpm --dir frontend typecheck
pnpm --dir frontend build
```

并在浏览器检查：

- `/login` 与 `/register` 均为同一套左右布局。
- 开灯后进入日间主题，关灯后进入夜间主题。
- logo 为白底，标题两行显示且没有重复品牌名。
- QQ 群入口与 Codex 下载入口左对齐、宽度紧凑，二维码悬停 / 聚焦可显示且不遮挡表单。
- 移动端 `/login` 与 `/register` 只展示表单区域，不展示左侧展示区。

## 首页 UI 定制门禁

本仓库 `/home` 默认首页已按认证页同源的黑科技 / 日间透明控制台风格定制，合并上游或处理前端冲突时必须保留。

必须保留的文件与链路：

- `frontend/src/views/HomeView.vue`
  - 必须保留 `homeContent` 自定义首页分支，管理员配置 URL 时继续 iframe 展示，配置 HTML 时继续 `v-html` 展示。
  - 默认首页使用 `home-showcase` 日夜主题变量、科技网格、白底弱边框 logo、顶部导航、Hero、Command Center、能力卡、模型支持区和页脚。
  - Hero 大标题必须与认证页一致，复用 `auth.loginHero.*.title`，展示两行：`为热爱赋能` / `为创造而生`，不要恢复成站点名加 `API 中枢`。
  - Hero 副文案必须复用认证页日夜文案：日间 `你的光芒会照亮每一个人。`，夜间 `你的努力会被你想要的人看见`，不要显示日间模式解释性长句。
  - 保留开灯 / 关灯完整主题切换，继续使用 `document.documentElement.classList.toggle('dark')` 与 `localStorage.theme`。
  - 顶部导航必须保留语言切换、文档链接、登录 / 控制台跳转，认证用户继续根据管理员状态进入 `/admin/dashboard` 或 `/dashboard`。
  - Command Center 标题统一使用 `您的AI中枢平台`，文案通过 `home.redesign.panelTitle` 维护，不要恢复成日夜两套标题或直接写死在模板里。
  - 移动端必须自然单列堆叠，不允许出现横向滚动、按钮文字挤压或右侧 Command Center 遮挡主要内容。
- `frontend/src/i18n/locales/zh.ts` 与 `frontend/src/i18n/locales/en.ts`
  - 必须保留 `home.redesign` 相关文案键，避免首页核心文案散落硬编码。

合并后至少验证：

```powershell
pnpm --dir frontend typecheck
pnpm --dir frontend build
```

并在浏览器检查：

- `/home` 默认首页和登录 / 注册页属于同一套黑科技 / 日间透明控制台视觉系统。
- 开灯后进入浅色日间主题，关灯后回到深色夜间主题。
- 自定义 `home_content` 分支、文档链接、语言切换、登录 / 控制台跳转仍可用。
- 桌面端 Command Center、能力卡、模型卡不互相遮挡；移动端无横向溢出。

## 内部后台页面 UI 定制门禁

本仓库登录后的内部页面已按首页 / 认证页同源的黑科技 / 日间透明控制台风格改造，合并上游或处理前端冲突时必须保留共享壳层样式。

必须保留的文件与链路：

- `frontend/src/style.css`
  - `:root` 与 `.dark` 的全局 token 必须保留浅色透明控制台与深色科技网格两套变量，不要恢复成旧米色 / 绿色主视觉。
  - `app-backdrop`、`app-shell`、`app-content`、`app-main` 必须保留科技网格背景和内部页面外框线。
  - `card`、`glass`、`table-container`、`dropdown`、`modal-content`、`sidebar`、`sidebar-link-active` 等共享类必须保持透明面板、8px 圆角、细边框和日夜可读层级。
- `frontend/src/components/layout/AppLayout.vue`
  - 必须保留 `app-shell`、`app-content`、`app-main` 类名，登录后的所有标准页面继续通过 `AppLayout` 继承内部页面视觉系统。
- `frontend/src/components/layout/AppHeader.vue`
  - 顶部栏必须保留 `app-header`、`app-header__inner`、`app-balance-pill`、`app-user-button`、`app-user-avatar` 等类名。
  - 顶部栏保留语言切换、公告、文档 / 使用说明、余额、用户菜单与退出逻辑，不允许因视觉合并覆盖认证和导航逻辑。
- `frontend/src/components/layout/AppSidebar.vue`
  - 侧栏继续使用白底弱边框 logo，菜单激活态保持 cyan / emerald 科技感，不要恢复成绿色块或旧灰白纯卡片。
  - 侧栏背景保持干净的透明渐变面板，不要叠加网格纹理，避免干扰导航阅读。

合并后至少验证：

```powershell
pnpm --dir frontend typecheck
pnpm --dir frontend build
```

并在浏览器检查：

- `/admin/dashboard` 或 `/dashboard` 使用内部页面新壳层，侧栏、顶部栏、卡片和表格视觉与 `/home` 风格一致。
- 深色模式为黑科技网格，浅色模式为透明日间控制台。
- 移动端侧栏、顶部按钮和主要内容无横向溢出，卡片文字不挤压。

## 前后端接口契约门禁

合并后至少检查这些契约：

- 前端 `apiClient.get/post/put/delete(...)` 使用的路径，在后端 `backend/internal/server/routes` 中有对应路由。
- 路由引用的 handler 方法存在，handler 调用的 service 方法存在。
- service 若依赖 repository、setting、feature flag 或数据库表，相关代码和迁移不能在合并中被遗漏。
- 如果接口返回 `404 page not found`，优先判断为后端路由缺失或运行二进制未更新，不要直接判断为业务数据无效。

常用检查命令：

```powershell
rg -n "apiClient\.(get|post|put|delete)\(" frontend/src
rg -n "validate-affiliate-code|ValidateAffiliateCode|validate-invitation-code" backend/internal frontend/src
```

## 邀请返利专项门禁

邀请返利功能合并后必须保持以下链路完整：

- 前端校验入口：`frontend/src/api/auth.ts` 中 `validateAffiliateCode()` 请求 `/auth/validate-affiliate-code`。
- 后端公开路由：`backend/internal/server/routes/auth.go` 注册 `POST /validate-affiliate-code`。
- Handler：`backend/internal/handler/auth_handler.go` 存在 `ValidateAffiliateCodeRequest`、`ValidateAffiliateCodeResponse` 和 `AuthHandler.ValidateAffiliateCode`。
- Service：`backend/internal/service/auth_service.go` 存在 `AuthService.ValidateAffiliateCode`。
- 业务依赖：`backend/internal/service/affiliate_service.go` 中 `IsEnabled`、`isValidAffiliateCodeFormat`、`BindInviterByCode` 逻辑不能被破坏。
- 数据依赖：`user_affiliates.aff_code` 存储返利邀请码，`settings.affiliate_enabled` 控制邀请返利总开关。

如果用户反馈返利邀请码“无效”，先执行：

1. 用 curl 或 `Invoke-WebRequest` 检查 `/api/v1/auth/validate-affiliate-code` 是否返回 404。
2. 若是 404，先补路由/handler/service 或确认线上二进制是否已更新。
3. 若不是 404，再查数据库中 `user_affiliates.aff_code`、对应 `users.email`、`settings.affiliate_enabled`。

只读数据库核验建议 SQL：

```sql
SELECT ua.user_id, u.email, ua.aff_code, ua.aff_code_custom, ua.inviter_id, s.value AS affiliate_enabled
FROM user_affiliates ua
JOIN users u ON u.id = ua.user_id
LEFT JOIN settings s ON s.key = 'affiliate_enabled'
WHERE ua.aff_code = $1 OR u.email = $2
ORDER BY CASE WHEN ua.aff_code = $1 THEN 0 ELSE 1 END, ua.user_id;
```

## 合并后验证

后端相关合并至少执行：

```powershell
$env:GOARCH='amd64'; $env:GOOS='windows'
& "D:\Program Files (x86)\Go\bin\go.exe" test ./internal/handler ./internal/service ./internal/server/routes
& "D:\Program Files (x86)\Go\bin\go.exe" build -tags embed ./cmd/server
```

每次合并完上游新代码后，必须在前端构建成功后继续构建 Linux 部署二进制；不得只完成合并或只完成前端构建就结束：

```powershell
pnpm --dir frontend build
```

随后使用：

```powershell
$env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'; $env:GOPROXY='https://goproxy.cn,direct'
$version = (Get-Content -LiteralPath "cmd/server/VERSION").Trim()
$short = (& git rev-parse --short HEAD).Trim()
$out = "sub2api-linux-amd64-v$version-$short"
& "D:\Program Files (x86)\Go\bin\go.exe" build -tags embed -ldflags="-s -w" -trimpath -o $out ./cmd/server
& "D:\Program Files (x86)\Go\bin\go.exe" version -m ".\$out"
Get-Item -LiteralPath ".\$out" | Select-Object FullName,Length,LastWriteTime
Get-FileHash -Algorithm SHA256 -LiteralPath ".\$out"
```

构建物产出后，固定收尾顺序为：`git status --short` 确认仅源码改动进入提交范围、构建物保持未跟踪或已忽略 → 暂存并提交源码改动 → push 当前分支。不得把 `sub2api-linux-amd64-*`、前端构建输出、临时目录或其它产出物加入提交。

如果工作区存在未提交改动，`go version -m` 可能显示 `vcs.modified=true`；最终回复必须说明该状态是否来自合并前已有改动、构建生成物，或本次任务修改。

部署更新时默认保留运行时 `DATA_DIR`，不要覆盖 `config.yaml` 和 `.installed`，只替换二进制并重启服务。

## 参考项目目录

- 从 GitHub 或其它远程来源下载、克隆、抽取的参考项目统一放在仓库根目录 `remoteTemp/`。
- `remoteTemp/` 已被 `.gitignore` 忽略；参考工程保留为独立 checkout 或压缩包，不得当作本项目源码提交。
- 具体审计记录、来源 URL、抓取日期、commit/tag、源码快照说明和研究结论统一放入 `remoteTemp/` 的日期化 Markdown 文件。
