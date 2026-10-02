# sub2api 项目 Agent 规则

本文件记录本仓库的项目级约束。若与用户当前会话明确要求冲突，以用户当前要求为准。

## 默认工作方式

- 默认使用简体中文沟通，代码标识符保持英文。
- 修改前先看工作树状态，不能覆盖或回退用户已有改动。
- 当前仓库可能存在本地 UI 定制；合并上游代码时必须保留用户原先的 UI 改动，不能因为选择上游版本而直接丢弃本地前端交互、样式、路由或构建配置。
- 对数据库、schema、线上样例数据、SQL 查询做排查时，优先通过 `mcp_router` 暴露的数据库工具执行只读检查；写入、删除、迁移、DDL、批量更新或数据修复必须先确认。

## 本地主版本、Ranxi 版本与打包规则

- 触发信号：完成一批新的本地修改、合并上游增量、准备打包，或用户明确指定版本。
- develop 统一门禁：正式交付源码和打包统一在 `develop`。打包前记录当前分支、HEAD、线上最近有效交付版本及其源码 SHA、所有待交付功能/上游整合分支的固定 SHA；逐一执行 `git merge-base --is-ancestor <来源 SHA> develop`，并核对实际功能链和取舍记录。存在待交付分支未整合、未决冲突或关键回归失败时禁止正式出包，不能从旧功能分支沿用旧版本构建。
- 版本基线核对：以已确认的最近有效交付版本和实际整合官方版本核定本批次版本，不能只读取当前分支的 `VERSION`。例如线上已交付 `0.2.11.2` 时，应先把对应整合源码和本批次功能统一到 `develop`，新批次沿用官方 `0.2.11` 并递增为 `0.2.11.3`；禁止仅改版本文件或重命名旧包冒充源码整合。
- develop 出包验收：源码验证通过后只提交本次源码和门禁文档，再以最终 `develop` 提交构建前端及 Linux amd64 embed 后端，产物名称、二进制提交信息、版本和构建记录必须一致。按已授权的合并交付流程推送 `origin/develop` 并核验远端 SHA；源码未提交或推送失败须明确报告，不得声称已同步。已有无关工作树改动单独记录，不纳入源码提交。
- 适用范围：本仓库应用版本、更新接口、版本弹层和部署产物。本地主版本唯一来源为 `backend/cmd/server/VERSION`，采用 `<已整合的官方三段版本>.<本地构建序号>` 四段格式；默认界面徽标、公开设置、CLI 和产物名称均以本地主版本为准。
- 根因与约束：本地主版本前三位必须等于实际整合的官方 `Wei-Shaw/sub2api` 发布版本，不能沿用旧官方系列，也不能仅根据远程发现新版本就提前升级。官方版本变化后首次出包，第四位重置为 `1`，例如整合官方 `0.2.9` 后从 `0.2.8.29` 升为 `0.2.9.1`。本地和 Ranxi 属于不同版本序列，禁止把 Ranxi 的版本写入本地 `VERSION`，也禁止跨来源比较版本；仅合并 Ranxi 不改变本地主版本前三位。
- 本地改动递增门禁：官方发布版本未变化时，前三位保持不动；每完成一批新的本地改动（包括修复、UI/配置调整、合并 Ranxi 或同一官方版本后的上游增量），必须将 `VERSION` 第四位在上一有效本地版本基础上加 `1`，例如 `0.2.9.1 → 0.2.9.2`。仅查询或拉取、没有实际整合改动时不递增；误用 Ranxi 系列的包不作为递增基线。用户明确指定本次版本时优先。
- 批次边界：同一批改动只递增一次，该批次交付前的修正、验证和构建重试沿用已确定的版本。已有产物交付后再加入新的本地改动或 Ranxi 增量，属于新批次，必须再加 `1`，不能因为仍在同一会话或官方版本没变而复用旧版本。例如已交付 `0.2.9.1` 后再合并 Ranxi，应出 `0.2.9.2`；同一批次连续整合两个上游只递增一次。
- `backend/cmd/server/RANXI_VERSION` 独立记录实际合并的 Ranxi 版本（例如 `2.8.14`），只能在核实合并来源后更新；通过 embed、BuildInfo 和 update service 成套传递，不覆盖本地版本链路。
- 点击原版本徽标后展示“本地版本”和“Ranxi 版本”两块，各自显示当前版本、最新版本、更新状态和发布说明；任一来源有更新时沿用原徽标更新提示。缓存和错误降级必须保留独立来源及当前构建版本，旧接口响应仍需兼容。
- 自动更新和回滚继续使用原本来源与语义；Ranxi 更新通过源代码合并处理，不得因 Ranxi 单独有更新就触发本地自动替换二进制。
- 打包前更新版本文件，先构建前端，再构建带 `embed` tag 的 Linux amd64 后端。产物命名为 `sub2api-linux-amd64-v<四段版本>-<当前提交短 SHA>`；含未提交源码时须记录 `vcs.modified=true` 及其来源。
- 验证方式：先记录实际整合的官方发布版本、上一有效本地版本、本批次改动和目标版本，确认前三位不被本地或 Ranxi 改动影响、第四位按上述批次规则递增；再核对版本文件、二进制版本信息、产物名称和构建记录一致，执行相关测试及构建，并记录大小、SHA256、目标平台和 `go version -m`。发现新增改动仍复用已交付版本时，不得通过出包门禁；禁止仅重命名旧二进制冒充新版本。以上是本仓库的四段版本约定，不套用通用三段 SemVer 自动递增规则。
- 双来源回归：验证无更新、本地单独更新、Ranxi 单独更新、双方更新、缓存命中、单来源失败和旧响应；确认默认徽标仍显示本地主版本，Ranxi 单独更新不会开放本地自动更新。

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

## 上游变更择优吸收（强制）

- 触发信号：拉取新代码、更新上游、合并官方或 Ranxi、解决上游冲突、升级后打包；适用于两个上游的全部增量，包括 Git 自动合并成功且没有冲突的文件。
- 核心原则：以本地业务需求、已承诺的功能和 UI 为兼容底线，逐项比较本地、官方、Ranxi 的实现后择优吸收。禁止仅凭版本更新、提交更晚、来源是上游或改动更多认定实现更优；也不能以“保护本地”为由保留已证实的缺陷或忽略适用的安全修复。
- 判断顺序：先核对正确性、安全边界、数据兼容和完整调用链，再比较功能覆盖、错误恢复、可维护性及有测量证据的性能/资源成本。依据必须来自实际代码、测试或运行证据，不能只看 Release Notes、PR 标题或编译结果。

### 合并前的取舍记录

1. 固定本地合并前 HEAD、各上游上次整合基线和本次获取的 SHA；分别核对上游增量与最终本地增量。用户已有未提交改动单独记录，不得算作上游新增；同版本发版后继续进入目标分支的提交也要单独说明。
2. 按业务功能或重要修复建立取舍清单，复用 `remoteTemp/` 下本次日期化合并记录，至少记录：来源/PR/SHA、解决的问题、本地现状、采用结论、取舍理由、依赖与兼容处理、验证证据。两个上游修改同一功能时必须交叉比较，不能因 Ranxi 后合并就覆盖先前较优的整合结果。
3. 每项必须归入以下四类；未完成评估或缺少关键验证的变更不能默认为已吸收：

| 结论 | 采用条件与处理方式 |
| --- | --- |
| 直接吸收 | 上游解决本地实际问题或补充所需能力，完整依赖与当前架构兼容，且没有破坏本地行为；连同必要测试一起吸收。 |
| 适配吸收 | 上游思路或修复有效，但依赖、接口、权限、代理或 UI 与本地不同；提取有效部分接入本地完整调用链，并补兼容回归。 |
| 保留本地 | 本地已提供等效能力，或有证据表明本地在所需行为、兼容性或可靠性上更优；记录比较依据，并检查上游修复是否仍有独立可吸收的部分。 |
| 暂缓吸收 | 依赖未就绪、收益不明、破坏兼容或关键验证不足；记录具体阻塞和重新评估条件，保留现有可用链路，不留下可触发的半链路，也不得宣称该项已实现。 |

### 实施与验收约束

- 以功能链路和代码块为取舍单位；禁止整仓库 `ours`/`theirs`、整文件覆盖本地共享模块，或为了消除冲突删掉本地能力。保留本地实现也必须复核相关上游修复，不能只处理 Git 标出的冲突。
- 不得为迁就上游实现而直接恢复本地已淘汰的旧架构、重复服务或配置入口。例如 Ranxi 的 Mihomo/旧 harvest 代理方案，应先比较本地固定代理、随机代理、IP 池和票据运行链路，优先适配有效修复；确需替换架构时，明确影响、迁移和验证方案，超出当前授权范围才确认。
- 不得静默扩大权限、改变计费/余额语义、默认开启高风险功能或执行数据迁移。新增配置保持明确的启用条件，旧配置与旧数据语义必须兼容；保留源码迁移文件不等于获得执行数据库写入的授权。
- 代理、票据、BPS 重试等修复必须保留实际出站、TLS、并发、失败回退及计费归属的一致性；观察员等角色功能必须检查数据归属与管理员接口边界，不能为了页面可用放宽权限。相关验证沿用本文件的保护清单与接口门禁。
- 对直接吸收和适配吸收项验证上游原问题已解决、本地关键行为未退化、成功/边界/失败路径可用；对保留本地项验证替代实现及仍适用的修复。构建通过、页面存在或路由名称相同均不能替代行为验证。
- 必须区分本次新增失败与合并前既有失败；没有可复现的基线证据不得把失败归为既有问题。关键回归未解决时不得宣称整合通过或可部署，也不得通过删除测试、跳过断言、放宽权限或关闭保护掩盖失败。
- 两个上游 SHA 的祖先核验只证明提交历史已整合，不证明所有功能原样采用。禁止用 `git merge -s ours` 或伪造父提交绕过逐项评估；历史已包含的来源仍须复核取舍记录中的暂缓项，不能将其视为已采用。
- 最终报告必须分别列出直接吸收、适配吸收、保留本地和暂缓项及理由，附验证结果与剩余限制。`RANXI_VERSION` 记录核实过的整合来源版本，不代表照单全收该版本全部功能；双来源版本、构建、源码提交和推送仍按下文原规则执行。

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

用户后续说“拉取新代码”“更新上游”或“拉取最新代码并合并”时，默认同时拉取以下两个上游，并按“上游变更择优吸收（强制）”逐项评估、整合提交历史和有效改进，不能只处理其中一个，也不代表照单全收全部功能；用户明确指定单一来源时，以当次要求为准：

- 官方上游：`Wei-Shaw/sub2api`，远程 `old-origin`，分支 `main`（`old-origin/main`）。
- Ranxi 上游：`ranxi2001/sub2api`，远程 `ranxi`，分支 `production`（`ranxi/production`）。
- `origin`（`ccx1/sub2api`）是本仓库源码推送目标，不计入上述两个上游。

双上游更新必须按以下顺序执行：

1. 合并前记录当前分支、最近提交、`git status --short`，区分用户已有改动和本次要做的改动。
2. 分别 fetch 两个上游的目标分支，记录实际来源、分支和本次获取的提交 SHA；若远程 URL 指向失效的本地目录或旧镜像，应核实并从对应 GitHub 仓库拉取，不能将本地缓存视为远程最新代码。任一来源拉取失败时，明确报告阻塞，不能省略后声称双上游更新完成。
3. 按 `old-origin/main` → `ranxi/production` 的顺序合并本次获取的提交，并落实逐项取舍清单；已经包含的来源无需重复合并，但必须复核曾暂缓吸收的项目。优先保留本地 UI 和功能定制，遇到前端冲突时不能简单使用 `theirs` 覆盖本地 UI；后合并的实现不因顺序自动优先。
4. 合并后检查前端 API 调用和后端路由是否仍然成对存在；对新增或本地保留的功能，必须确认 `frontend/src/api`、页面组件、后端 route、handler、service、repository/DB 查询链路完整，不能只保留 UI 而漏掉依赖的后端接口。
5. 对两个来源本次获取的 SHA 分别执行 `git merge-base --is-ancestor <SHA> HEAD`，确认均已进入最终提交历史；另外逐项核对取舍清单与实际代码、定向验证结果一致，不能以祖先关系代替功能验收。存在暂缓项时明确说明范围和原因；没有验证证据时，不要声称“合并完成”“可部署”。
6. 两个来源合并完成后，按“本地主版本、Ranxi 版本与打包规则”核定版本和批次边界，再针对最终整合结果统一执行前端生产构建，并用最新前端产物构建后端 Linux amd64 可执行二进制。最终回复必须给出两个上游的合并 SHA，以及二进制路径、版本、最终提交号、大小和 SHA256。
7. 拉取 / 合并上游新代码并产出构建物后，必须提交源码合并结果并将当前分支推送至 `origin`；构建物、二进制和临时产物不得暂存、提交或推送。

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

## 账号编辑、导入默认与批量编辑一致性门禁（强制）

- 触发信号：新增、修改或删除账号创建/单账号编辑中的配置项、开关、子选项、默认值、校验及显示条件，或修改对应后端字段、归一化、序列化和运行时读取逻辑；包含这些改动的上游合并、修复和打包同样触发。本门禁适用于各平台的账号配置，不仅限于 Excel / BPS。
- 根因与约束：单账号编辑、导入默认、批量编辑和共享池入口可能各自维护字段清单，只改一个表单会造成“单独可编辑，导入或批量不可配置”；字段也可能在保存、归一化、导入应用或回显过程中丢失。仅检查模板里有开关、类型编译通过或某个 API 接收字段，不能证明功能完整。
- 默认要求：凡能作为导入通用默认值的配置，必须同步提供导入默认设置；凡能对多个符合条件的账号安全应用同一配置的项，必须同步提供批量编辑。两种适用性分别判断，不能因其中一种不适用而跳过另一种。主开关、全部子选项、作用范围、关闭/清空行为和说明文案应成套处理，禁止只补主开关。
- 例外必须有依据：确实依赖单账号唯一凭据、交互式授权、只读计算结果或不允许批量写入的业务约束时，记录不适用的入口、具体原因和替代操作；“现有表单没有字段”“本次只改编辑弹窗”或“以后再补”不属于不适用理由。不得为了覆盖入口而放宽平台、账号类型、角色权限或默认开启风险功能。

### 入口与数据链路核对

每次按本次变更的配置项核对下列入口，记录“已同步并验证”或“有依据的不适用”；可复用任务说明和测试记录，不要求额外新建文档：

| 入口 | 必须核对的行为 |
| --- | --- |
| 单账号创建/编辑 | 可见条件、默认值、校验、提交、保存后重新打开回显，以及实际运行时读取。 |
| 导入默认设置 | 前端默认值与归一化、后端设置结构与持久化、保存往返、重新打开、后续实际导入采用；旧配置缺字段保持原有语义。 |
| 管理员导入/共享池导入 | 对各自适用入口检查默认继承、单次覆盖和文件显式字段优先级；共享池的创建、编辑及导入表单涉及同一配置时同步核对，不得混用权限或归属规则。 |
| 批量编辑已选账号 | 本次修改是否启用、字段独立选择、开启/关闭/清空的实际提交与后端应用；未参与修改的配置及无关字段保持原值。 |
| 批量编辑筛选结果 | 覆盖完整筛选目标，保持与编辑已选账号一致的资格、校验、保存及错误处理语义。 |

- 从字段控件追踪到共用类型/默认值/归一化、请求 payload、后端处理与设置存储、账号写入、运行时读取和响应回显；优先复用已有共用选项组件和转换函数，新增字段不能在任一环节被白名单或默认值覆盖丢弃。
- 明确区分缺字段、显式 `false`、`0`、`null` 和空列表的既有含义。文件显式配置、单次覆盖和全局默认的优先级遵循原契约；关闭主功能时按原语义清理子配置，各子开关不能相互误启用。批量“不修改”不能当作“关闭”，也不能用完整默认对象覆盖未勾选的设置。
- 批量目标的资格判断必须基于完整目标集合。跨页已选账号不能只从当前页推导平台/类型；筛选批量不能用首屏或前 100 条采样代表全部账号。补查失败或结果不完整时不得用空/局部元数据继续打开或提交；选择、筛选改变或页面卸载后应丢弃过期异步结果，并防止重复点击误应用。
- 在实际页面调用的组件上核对显示条件。若子选项需要先勾选“参与本次修改”再打开主开关，应提供可见提示，并验证正确的目标下确实可展开；不能因默认隐藏而被误认为不存在，也不能通过对不兼容账号强行显示修复遗漏。

### 验证与交付门禁

- 为本次字段补充成功、边界和失败路径：保存后重新打开；旧配置缺字段；显式开/关或设值/清空；默认继承与显式覆盖；主功能关闭清理；多个子项独立组合；未勾选批量修改不写入；不兼容账号或权限不通过。导入必须验证实际账号创建/写入后的配置与运行时读取，不能只测试设置接口返回字段。
- 修改批量目标或显示判断时，回归跨页选择后当前页无已选账号、跨页混合类型、筛选后页出现不兼容账号、补查失败/不完整，以及选择/筛选改变后的过期请求。导入后继续编辑的入口也应保持正确目标信息。
- 前端至少运行受影响的单账号编辑、导入默认/导入执行、批量组件和页面测试，并执行类型检查、文案完整性检查和生产构建；后端运行该字段的默认设置往返、导入应用、批量更新及运行时行为定向测试。测试范围随字段扩展，不能只跑既有 BPS 用例就声称其他配置通过。
- 涉及共享池导入时，同时执行本文件“共享账号池导入回归门禁”。浏览器可用时验证真实入口的显示、展开、保存和回显；无法执行的浏览器、数据库或运行时验证必须明确记录限制，不能用编译通过替代。
- 验收记录列明变更配置项、各入口覆盖/不适用原因、实际测试命令与结果。任何适用入口未同步、配置未实际生效或关键回归失败时，不得宣称完成或通过交付打包门禁；用户明确缩小范围时如实列出未覆盖部分。

可复用的前端基础回归（仍须补充本次字段及受影响单账号/共享池入口的测试）：

```powershell
pnpm --dir frontend test:run src/components/admin/account/__tests__/AccountImportSettingsModal.spec.ts src/components/admin/account/__tests__/ImportDataModal.spec.ts src/components/account/__tests__/BulkEditAccountModal.spec.ts src/views/admin/__tests__/AccountsView.bulkEdit.spec.ts
pnpm --dir frontend typecheck
pnpm --dir frontend build
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

## 共享账号池导入回归门禁

- 触发信号：修改共享池 JSON 导入、套餐分组配置、调度授权、首次启用或对应仓储校验，以及包含这些改动的合并和打包。
- 根因与约束：服务层允许套餐专属分组，而创建/首次启用仓储仍执行默认非专属组规则，会出现“配置和选组成功、实际入库失败”。分组配置、服务选组与事务内校验必须保持一致，不能用放宽全部默认组或伪装管理员手工分配修复。
- 必测原始场景：使用脱敏的 `sub2api-data` v1 JSON，`platform=openai`、`type=oauth`、`plan_type=prolite`、无 `group_ids`；已授权且套餐配置指向同平台已启用的标准计费专属组时，必须成功导入。
- 创建回归必须执行真实 `CreateSharedAccount` 仓储方法，核验账号、共享归属、分组关联、outbox 与事务提交；只模拟仓储成功，或在账号 INSERT 人为报错之前通过，均不能替代成功路径回归。写入/关联/outbox 失败必须验证事务回滚。
- 同步覆盖用户首次授权启用、管理员首次启用、同次覆盖/清空套餐档位；默认组仍不可专属。未授权、API Key、未知档位、跨平台、停用/删除分组、未配置目标及订阅计费分组不得借套餐例外绕过限制；数据库异常不能伪装为分组不兼容。
- 管理员账号导入 `/admin/accounts/data` 与用户共享池导入必须分别回归；普通账号创建不得读取共享池套餐分组规则或获得共享池归属。测试必须包含 `TestSharedImport`，不能仅匹配 `TestSharedPool` 而遗漏导入入口。
- 删除后重新导入以 `accounts.deleted_at` 为准：旧账号已删除时，不限制原所有者、删除者或旧管理员停用标记，允许同一凭证登记为当前导入用户的新账号；仅停止共享或停用但未删除的记录仍须拒绝重复。旧账号、归属和收益历史必须保留，凭证占位释放与新账号创建必须在同一事务内；不通过删除历史、改名或清空全部唯一约束实现。
- 删除重导回归必须覆盖共享池删除、普通后台软删除、跨用户重新登记、旧管理员停用但已删除、连续两轮重导、活跃/仅停用重复拒绝，以及释放占位后写入/关联/outbox/提交失败回滚；隔离 PostgreSQL 可用时验证并发重导仅一成功。新旧账号 ID、所有者与收益历史不能混用，查询异常不能误报为重复账号。
- 验证方式：在 `backend` 目录执行下列定向门禁；现有 CI 的 `make test-unit` 同时收录新增单元回归。记录实际结果，明确 SQL mock、真实 PostgreSQL、并发及浏览器验证各自的覆盖范围；没有隔离测试库时不连接业务库，也不宣称完整端到端覆盖。

```powershell
go test -tags unit -p 1 -count=1 ./internal/handler ./internal/handler/admin ./internal/service ./internal/repository -run 'Test(Shared|ImportData|AccountImport|AdminCreateAccountImport)'
```

## 参考项目目录

- 从 GitHub 或其它远程来源下载、克隆、抽取的参考项目统一放在仓库根目录 `remoteTemp/`。
- `remoteTemp/` 已被 `.gitignore` 忽略；参考工程保留为独立 checkout 或压缩包，不得当作本项目源码提交。
- 具体审计记录、来源 URL、抓取日期、commit/tag、源码快照说明和研究结论统一放入 `remoteTemp/` 的日期化 Markdown 文件。
