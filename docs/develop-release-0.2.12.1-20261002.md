# develop 双上游整合记录：0.2.12.1（2026-10-02）

本记录对应本轮源码整合；[此前 develop 统一记录](develop-release-20261002.md) 的 `0.2.11.3` 是旧阶段目标，不能作为本轮版本或验证依据。最终源码提交、远端 SHA 和产物信息由 `remoteTemp/upstream-20261002/release-manifest.json` 记录。

## 固定基线与版本

| 项目 | 固定值 |
| --- | --- |
| 工作分支 | `develop` |
| 本轮请求开始时 HEAD | `51ca062aa5cdc978a5ea75395d70a0986ed8f385` |
| 实际合并前 HEAD | `9207f559b`；用户外部提交的站点/脚本删除已保留 |
| 最近有产物证据的有效交付 | `da6d61126880ecc28f811889c593dd992346a480`，`0.2.11.2` |
| 官方上次整合基线 | `42bc7f6cffe24bcb471608e48e66b4a0afa1f882` |
| 本轮官方来源 | `Wei-Shaw/sub2api`，`old-origin/main`，`458b92abd4b0b09d123d4c6727dd8d59060bd883`，`0.2.12` |
| 官方实际合并提交 | `7774c6a15c31ea956ec0dac9ebc5ee0850cc90e3` |
| Ranxi 上次整合基线 | `9f7d96be367e795bd76e1f45c82bd5d4c563e0fa` |
| 本轮 Ranxi 来源 | `ranxi2001/sub2api`，`ranxi/production`，`bf9405e4ab58c1be4fc8ec2101371753a016908e`，`2.9.6` |
| 本地主版本 | `backend/cmd/server/VERSION`：`0.2.12.1` |
| Ranxi 独立版本 | `backend/cmd/server/RANXI_VERSION`：`2.9.6`；同版本发布后的增量以上述 SHA 为准 |
| 推送目标 | `origin/develop`，`ccx1/sub2api` |

官方从 `0.2.11` 升至已实际整合的 `0.2.12`，本地序号重置为 `1`。本轮合并后的修复和构建重试属于同一批次，不再次递增。原功能来源 `153c5b8d84edf170a15f041ba7b13f4b63adc8fb`、原 develop `f3b716843d571f3f7a768da866089370f56b1461`、有效交付 `da6d61126880ecc28f811889c593dd992346a480` 已核验进入 develop；提交前及最终提交后核验双上游祖先关系。

已有 stash（含 `codex-pre-upstream-merge-20261002`）保持原样，未盲目应用。数字后缀备份、临时缓存、运行日志、dist 和二进制不进入本次提交。暂存区遗留的 165 个 `AD` 已按实际源码树清理，两项本地 ignore-images 回归恢复为无净删除。最终暂存 201 个源码/资源/文档文件，无未解决冲突、未暂存跟踪改动或禁入产物；`git diff --cached --check` 和冲突标记扫描退出 0。

## 直接吸收

| 来源 | 问题与采用结果 | 兼容与验证依据 |
| --- | --- | --- |
| 官方 `b1ce3c6d5` / #7676 | 邮箱验证码尝试原子化，重置 token 哈希及单次使用 | 保留公开认证和本地注册/返利链路，handler/service 单元回归 |
| 官方 `d359196ad` / #7674 | Antigravity 上游错误脱敏 | 保留本地错误分类，securityaudit/handler 回归 |
| 官方 `cf19f30d2` / #7673 | 匿名订单核验限流 | routes/payment 回归，不扩大匿名权限 |
| 官方 `53158587a`、`00940743d` | Axios 1.20.0 安全更新 | package/lock 来自实际官方整合；前端 typecheck、测试、生产构建 |
| 官方 `1aa34d478`、`1b1039f4f` / #7802 | 充值促销阶梯与价签 | 采用官方完整设置及计费链路，handler/service/API 契约回归 |
| 官方 `0da18e4a5`、`58d605083` / #7630 | 账号优先级快捷调整 | 保留现有账号权限和本地编辑入口，账号页面与批量回归 |
| 官方 `0cecf3c36` / #7803、`2e3c2cf59` / #7773 | Key 按分组排序及错误策略警告文案 | Key 页面/API/i18n 测试 |
| 官方 `fdce5b8d0`、`ad05eda15` / #7780 | Grok CLI 版本与身份头 | Ranxi 的 `67266c9cb` 为旧版本更新；采用已整合官方较完整修复 |
| Ranxi `c885caa4f` / #262 | HTTP 非安全上下文的 Pelican 预览 | 本地预览组件及 URL 工具测试、实际浏览器验证 |
| Ranxi `0dfbd70cf` / #245 | 裸 API 别名不回退 SPA | `internal/web` 测试覆盖别名拒绝，不新增公开业务权限 |
| Ranxi `1fd3c958f` | WS 下游写状态跨轮隔离 | WS service/handler 回归，控制帧与推理轮分别计数 |

## 适配吸收

| 来源 | 本地接入与取舍 | 验证依据 |
| --- | --- | --- |
| 官方 `d1ba57977` / #7425 | System One/TypeSafe 原生支持；恢复兼容入口协议隔离，接入本地 Key/user 准入 | typesafe、API 契约、handler/routes 回归 |
| Ranxi `cbb8ef3ed`、`467f54973`、`a7263faa2` / #263 | Key 并发字段、Redis 准入与队列统计；补配置、缓存版本、请求复验、精确释放、Live 移交、WS 每轮准入、wire shutdown | config/repository/middleware/service/handler 测试；正数增减、grant 后 0、鉴权拒绝、失败释放；前端 Key 创建/编辑/批量测试 |
| Ranxi `2ecd5b7d5`、`04e76d18d` / #250 | 权限感知功能搜索；仅索引实际存在的本地功能、设置节与深链接 | FeatureSearch、settingsSearch、useSettingsNavigation、SettingsView 测试 |
| Ranxi `1c018400b`、`6d0ff27b5`、`c6ee0f51c` | Codex catalog/套餐显示兼容本地配置；保留 API Key 客户端生成格式和本地模型语义 | catalog 兼容、模型服务与前端测试 |
| Ranxi `0e1cad458`、`8519ded90` / #242 | BPS 图片默认启用且优先 native；保留本地图片摘要、ignore-images、ignore-encrypted、容量和出站代理/TLS | basispoints、BPS runtime、设置往返、导入与批量入口测试；契约预期同步新默认 |
| Ranxi `27b20d536`、`f98a37082` / #259 | 提取逐轮准入和无 Key 账号测试兼容；不引入 Prism 运行时 | keyless/allowlist/queue/WS 回归；业务 hold 续期失败两条真实 30 秒路径 |
| 本地合并恢复 | 两个兼容入口保留 ClaudeCodeOnly fallback 分组映射；计费仍属于原 Key 分组 | 恢复 protocol guard、余额 reservation 及计费引用移交；完整 handler 回归 |
| 本地票据恢复 | usage policy 接回选票；消费账本排除已用票并参与库存状态、复验候选与迟到发布；本轮领取最后票仍能发送 | 保留原失败断言，票据成功/消费/复验回归与完整 service 门禁 |

新增队列采用已有状态机及分层测试风格，部分上游函数/文件超过全局建议行数；为保持行为与审查范围，不在合并任务中进行无关拆分。

## 保留本地

| 功能 | 取舍理由与检查范围 |
| --- | --- |
| 认证页、首页、后台壳层 | 保留 showcase、日夜切换、QQ/Codex 入口及移动端行为；前端构建和 16 个浏览器状态检查 |
| 安全策略、SpendGuard、账号保护、随机代理、IP 池、TLS、插件注入 | 共享模块按代码块整合，沿用本地完整权限、失败恢复、代理与持久化链路；repository/service/handler 定向及完整单元回归 |
| Codex CookieReceipt、票据保管库和消费策略 | Ranxi 旧 harvest identity 不适用于本地票据方案；移除其未使用钩子，不改成本地请求身份跟随探针 |
| 图片摘要与 ignore-images | 保留本地运行时和原端到端测试/脚本；新增默认图片支持与忽略图片是独立配置，不能以默认启用覆盖显式忽略 |
| 现有 Claude/panel RPM 与本地调度 | 不恢复尚未整合的 OpenAI RPM/priority 旧架构；队列准入不绕过原用户、分组、模型或余额检查 |

## 暂缓吸收

| 来源/功能 | 具体原因与重评条件 |
| --- | --- |
| Ranxi #260/#264/#267 Prism browser bridge/session cache | 新浏览器账号、会话身份及适配器依赖没有接入本地出站、权限、票据和部署链路；需完整独立设计与实测 |
| Ranxi #251 Serverless region Pods、#247 k3s helper | runtime role、跨 Pod 生命周期和基础设施升级未建立本地运维契约；不得保留可配置的半链路 |
| Guard v2、reauth runtime/worker、凭据运行控制 | 依赖此前暂缓的 repository、持久化、worker 和 UI；需明确迁移及账号保护兼容，不仅恢复页面 |
| Mihomo/旧 harvest、Quality BPS coexistence、AutoConfig/aliases、自动恢复 | 本地随机代理/IP 池/票据链路已提供所需基础行为；上游新增依赖与 schema 未验证，需单独完整评估 |
| OpenAI RPM、priority capacity rebalance | 依赖新的共享调度及配置，不能覆盖现有本地计费与调度语义 |
| Ranxi #268 公共 Pelican results API | 新公开读取权限与数据归属尚未完整验证；保留现有认证用户面板，不注册公共 API |

祖先核验只证明历史已整合，`RANXI_VERSION=2.9.6` 不代表以上功能全部采用。暂缓项的独立源码、路由、注入、页面和配置入口均不应进入最终源码树。

## 验证记录

使用原生 Go 1.27 `windows/amd64` 工具链：`C:\Users\ZJGC\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.27.0.windows-amd64\bin\go.exe`。设置 `GOOS=windows`、`GOARCH=amd64`、`GOTOOLCHAIN=local`、`GOMAXPROCS=2`，避免默认 386 工具链常量溢出及 32 位编译器内存上限。下列命令在 `backend` 执行；不省略断言或降低阈值以通过测试。

| 门禁 | 实际结果与证据 |
| --- | --- |
| `go test -json -tags unit -p 1 ./internal/handler ./internal/handler/admin -count=1` | 退出 0，handler 48.591s、admin 5.782s；`.codex-run/merge-handler-final-json.log` |
| `go test -tags unit -p 1 -count=1 ./internal/server/routes ./internal/server` | 退出 0，14.694s/6.987s；`.codex-run/merge-routes-contracts-recheck.log`；补 imageAdmission 链路预期及 Key/BPS 契约新字段 |
| queue/proxy/BPS 定向 | service/middleware/repository 退出 0；`.codex-run/queue-proxy-bps-regression.log` |
| `go test -p 2 -tags unit ./internal/service -run APIKeyQueue -count=1 -v` | 退出 0，26 个顶层测试、7 个子例；`remoteTemp/upstream-20260930/backend-queue-final-revalidation-native-amd64.log` |
| WS 票据/准入定向 | 436 个顶层测试、含子例 857 项通过；`.codex-run/ws-ticket-admission-regression.log` |
| WS business hold 续期失败 | 两个真实 30 秒路径通过；`.codex-run/ws-business-hold-regression.log` |
| `go test ./internal/service -run 'Test.*CodexTicket' -count=1 -p=1 -v` | 退出 0，33.313s，533 个顶层测试、含子例 1257 项 PASS、0 FAIL；`.codex-run/ticket-final-regression.log`；消费与复验原断言未弱化 |
| securityaudit/typesafe/xai/openai/TLS/migrations/cmd/server/basispoints | 全部通过；`.codex-run/merge-final-contracts-unit.log` 中旧 server 契约失败已由上述独立复测解决 |
| `go test -json -tags unit -p 1 -count=1 ./internal/service ./internal/repository ./internal/server/middleware ./internal/server/routes ./internal/server ./internal/setup ./internal/config ./internal/web ./cmd/server` | 退出 0，9 包通过，含子例 22891 项 PASS、0 FAIL、36 项按原环境条件 SKIP；service 260.165s、repository 19.212s；`.codex-run/final-release-unit.jsonl`，stderr 空 |
| 前端 typecheck/build/i18n 与受影响回归 | typecheck/build 成功，13 文件 514 测试通过；`remoteTemp/upstream-20260930/frontend-*-final.log` |
| 桌面/移动浏览器 | 原 16 状态加补充 9 状态通过：`home/login/register` 移动暗色、dashboard 桌面/移动日夜、移动侧栏展开、dashboard/keys 双向导航与主题持久化；`pageerror=[]`、`unknownApi=[]`、`externalRequests=[]`、无横向溢出；`frontend-browser/report.json`、`remoteTemp/upstream-20260930/frontend-browser-supplement/report.json`；API 使用 mock，新增 dashboard snapshot/trend/ranking mock |

service 早前在途缓存内存测试曾以 `8400736 > 8388608` 失败；不修改阈值，单独连续 5 次复测通过，随后完整 service 最终复测通过。不无依据声称早前失败是合并前既有失败。36 项跳过主要涉及隔离 PostgreSQL、外部 live 凭据和插件包，未视为执行通过。

账号入口回归包括创建/编辑、导入默认、导入执行、批量已选与筛选，以及共享池 `TestSharedImport`。新增 Key 限额属于用户 Key 配置，非账号导入默认；需从前端 payload 到 DTO/service/repository/Ent/migration/cache/runtime 全链路核对。BPS 图片默认设置保持文件显式配置优先于单次及全局默认。

## 验证限制与交付顺序

本轮不部署，不连接业务数据库，不执行数据库迁移。数据库创建/回滚以单元桩与 SQL mock 验证，未执行隔离 PostgreSQL 并发验收。浏览器 API 为 mock，不能视为真实账号入库、扣费或上游模型 E2E；完整 handler 使用本机 HTTP/WS 上游桩。交付前执行本机空数据目录的 embed HTTP GET smoke，仅验启动、静态资源和只读 setup 状态。使用全新工作目录与 `DATA_DIR`、不存在的 `CONFIG_FILE`、`SKIP_SETUP=false`、`AUTO_SETUP=false`、`SERVER_HOST=127.0.0.1`；实际自动安装开关为 `AUTO_SETUP`，`AUTO_SETUP_ENABLED` 单独无效。不得调用 setup 的数据库/Redis 测试或安装接口。

完成源码门禁后，只提交本轮源码与本记录；最终合并提交必须包含两个固定上游 SHA。推送 `origin/develop` 后核验远端 SHA，再从同一最终提交构建前端，随后以 `CGO_ENABLED=0`、`GOOS=linux`、`GOARCH=amd64`、`-tags embed -trimpath` 构建后端。产物名为 `sub2api-linux-amd64-v0.2.12.1-<最终提交短SHA>`，`ldflags` 注入同一提交和构建时间。

产物路径、字节大小、SHA256、`go version -m`、版本文件与构建时间写入本轮 manifest。若 `vcs.modified=true`，必须记录实际残留来源；源码提交后再修改交付源码或版本则须重跑相关门禁并重构产物，不重命名旧二进制充当新版本。
