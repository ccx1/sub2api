# sub2api 项目 Agent 规则

本文件记录本仓库的项目级约束。若与用户当前会话明确要求冲突，以用户当前要求为准。

## 默认工作方式

- 默认使用简体中文沟通，代码标识符保持英文。
- 修改前先看工作树状态，不能覆盖或回退用户已有改动。
- 当前仓库可能存在本地 UI 定制；合并上游代码时必须保留用户原先的 UI 改动，不能因为选择上游版本而直接丢弃本地前端交互、样式、路由或构建配置。
- 对数据库、schema、线上样例数据、SQL 查询做排查时，优先通过 `mcp_router` 暴露的数据库工具执行只读检查；写入、删除、迁移、DDL、批量更新或数据修复必须先确认。

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
