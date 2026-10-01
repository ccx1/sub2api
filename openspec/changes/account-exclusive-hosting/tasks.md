## 1. 数据模型与迁移

- [ ] 1.1 新增迁移 `backend/migrations/261_exclusive_hosting.sql`：
  - 新表（权益表含 `revoke_kind`，事件表含 `status`、`revoked_at`、`revoked_by_admin_id`、`revoke_reason`）：`exclusive_account_grants`、`exclusive_grant_events`（含部分唯一索引 `(redeem_code_id, upstream_identity) WHERE status <> 'revoked'`）、`exclusive_code_adjustments`、`exclusive_user_groups`、`shared_pool_identity_audits`
  - `groups` 新增 `exclusive_owner_user_id`
  - `redeem_codes` 新增 `exclusive_platform`、`exclusive_tiers`、`exclusive_with_proxy`、`max_activations`、`used_activations`、`bound_user_id`、`service_started_at`、`service_expires_at`，以及独享码规格 CHECK 约束；有效天数复用 `validity_days`
  - `shared_pool_accounts` 新增 `upstream_identity`、`upstream_identity_source`，并为未删除账号建部分唯一索引
  - `usage_logs` 新增 `billing_mode`
  - 全部使用 `IF NOT EXISTS`，列可空或有默认值
- [ ] 1.2 ent schema：`group.go` 增加 `exclusive_owner_user_id`，`redeem_code.go` 增加八列，`usage_log.go` 增加 `billing_mode`，然后执行 `go generate ./ent`
- [ ] 1.3 `service.Group` 增加 `ExclusiveOwnerUserID`，并贯通以下映射：`group_repo` 创建与更新、`api_key_repo` 分组投影、`api_key_auth_cache` 两处快照、调度快照 Lite 读取；复制分组时置空
- [ ] 1.4 `domain` 新增 `RedeemTypeExclusiveAccount = "exclusive_account"` 与兑换码状态 `active`、`expired`、`disabled`，并在 `service/domain_constants.go` 中导出
- [ ] 1.5 迁移测试：
  - 新列默认值
  - 删除让位后，部分唯一索引允许重新导入
  - 同码同身份唯一约束对 `active`、`expired`、`account_deleted` 生效，对 `revoked` 不生效

## 2. 上游身份

- [ ] 2.1 新增 `backend/internal/service/shared_pool_upstream_identity.go`，实现 `sharedUpstreamIdentity(platform, kind, credentials)`：
  - 覆盖 OpenAI、Anthropic、Gemini、Antigravity 的 OAuth 与 API Key
  - OpenAI 只有 token 时复用 `sharedOpenAIPlanClaims` 解析，并校验 `chatgpt_account_id` 一致
- [ ] 2.2 `SharedPoolService.Create`、导入、OAuth finish 写入 `upstream_identity`；冲突时返回现有的共享冲突错误
- [ ] 2.3 `SharedPoolService.Update` 替换凭证时：
  - 有权益（含已到期未删除）：要求身份一致，否则返回 `EXCLUSIVE_IDENTITY_MISMATCH`
  - 无权益：更新身份并校验唯一
- [ ] 2.4 `prepareSharedAccountReimport` 与 `RemoveSharedAccount` 同步让出 `upstream_identity`
- [ ] 2.5 管理员手动设置身份：
  - 接口：`PUT /admin/shared-pool/accounts/:id/upstream-identity {fields, reason}`，按平台填写与自动提取相同的字段
  - 身份计算：复用 `sharedUpstreamIdentity`，来源记为 `admin`，并做全局唯一校验
  - 限制：有效独享期内拒绝修改
  - 审计：写入 `shared_pool_identity_audits`
  - 管理端共享池账号列表：返回身份状态，并支持按「缺失」筛选
- [ ] 2.6 来源为 `admin` 的账号在凭证更新时：自动提取出不同身份则拒绝；提取不出则保留手动值
- [ ] 2.7 单元测试：
  - 身份提取：各平台身份提取；token 刷新后身份不变；缺字段返回空
  - 导入去重：跨用户重复导入被拒；删除后可重新导入
  - 手动设置：设置成功后可激活；重复被拒；有效期内修改被拒；审计完整；手动身份账号的凭证刷新行为

## 3. 独享兑换码

- [ ] 3.1 `GenerateRedeemCodesInput` 与 `admin/redeem_handler.go` 支持生成 `exclusive_account` 类型：
  - 必填 `exclusive_platform`、`validity_days`（≥1）、`max_activations`（1–1000）、`exclusive_with_proxy`
  - 选填 `exclusive_tiers`（按平台用 `canonicalSharedSubscriptionTier` 校验）、`expires_at`
- [ ] 3.2 兑换码状态计算 `exclusiveCodeStatus`，按以下优先级判定，列表、预览、激活与调整统一使用：
  1. disabled
  2. expired（`service_expires_at` 已过）
  3. used（剩余为 0）
  4. active（已启用）
  5. unused
- [ ] 3.3 管理接口：
  - `PATCH /admin/redeem-codes/:id/exclusive`：
    - 可修改次数上限，不能低于已用次数、不能超过 1000
    - 档次、天数、是否含代理只在 unused 状态下可改
    - 可以停用或启用码，可以清空 `bound_user_id`
    - expired 的码拒绝修改
  - `POST /admin/redeem-codes/batch-activations`：支持设置、增量、作废剩余、停用，逐项返回结果
  - `GET /admin/redeem-codes/:id/activations`：激活明细
  - 修改与激活对同一兑换码行加锁，并写入审计表（包含修改前后值）
- [ ] 3.4 用户端 `GET /shared-pool/exclusive/codes/:code/preview`：
  - 返回平台、档次、天数、是否含代理、剩余次数和状态；已启用的码额外返回 `service_expires_at` 和剩余天数
  - 受兑换限流保护；绑定给他人的码不返回详情
- [ ] 3.5 `RedeemService.redeem` 对该类型返回 `EXCLUSIVE_CODE_ACCOUNT_REQUIRED`，不消耗次数、不启动计时、不计失败次数
- [ ] 3.6 测试：
  - 生成：按规格生成、不限档次、次数或天数越界、档次与平台不符
  - 通用兑换接口拒绝独享码
  - 修改规格：unused 可改，active 不可改
  - 停用：停用后不能激活，已激活账号不受影响
  - 次数：调高后恢复为 active；调低不能低于已用；作废剩余后已激活账号不受影响；expired 的码不可调整；批量调整逐项返回结果
  - 并发：调整与激活同时进行时不超用
  - 审计记录完整
  - 清空绑定后他人可用，且到期时间不变

## 4. 激活

- [ ] 4.1 新增 `backend/internal/service/exclusive_activation.go`：`ActivateExclusive(ctx, userID, accountID, code)`，复用 `checkRedeemRateLimit` 与 `acquireRedeemLock`
- [ ] 4.2 新增 `backend/internal/repository/exclusive_activation.go`，单事务按「兑换码 → 账号与归属 → 权益」顺序加锁，依次执行：
  1. 校验兑换码：类型、未停用、绑定用户、剩余次数；未启用码检查 `expires_at`，已启用码检查 `service_expires_at > now`
  2. 校验账号：归属、未删除、未被停用、平台与码一致、档次在码的档次范围内（码限定档次而账号档次未知时拒绝）、`upstream_identity` 非空（为空时提示联系管理员）、未开启共享
  3. 校验权益：没有有效权益（`EXCLUSIVE_ALREADY_ACTIVE`），且事件表中没有同码同身份、未撤销的记录（`EXCLUSIVE_CODE_USED_FOR_ACCOUNT`）
  4. 首次使用时写入 `service_started_at` 与 `service_expires_at`，多次码同时写入 `bound_user_id`
  5. 插入或覆盖权益：`expires_at = service_expires_at`，`with_proxy` 取码的 `exclusive_with_proxy`，并清空 `finalized_at`；同时写入事件，事件中记录账号档次
  6. 扣减次数，确保私有分组存在并绑定账号，清理残留的共享分组关联，更新镜像，发送 outbox
- [ ] 4.3 私有分组创建 `ensureExclusiveUserGroup`：创建分组、写入 `exclusive_user_groups`、追加到 `allowed_groups`；分组已停用时拒绝激活
- [ ] 4.4 用户端路由 `POST /shared-pool/accounts/:id/exclusive/redeem`（要求 `Idempotency-Key`）与 `GET /shared-pool/exclusive/groups`
- [ ] 4.5 `SharedPoolAccountView` 增加 `exclusive` 字段：`active`、`expires_at`、`with_proxy`、`group_id`、`code_expires_at`
- [ ] 4.6 测试：
  - 首次激活启动计时
  - 同码两个账号在不同时间激活，`expires_at` 相同
  - 码到期后剩余次数不可用；码未启用时超过兑换截止时间被拒
  - 有效期内再次兑换返回 `EXCLUSIVE_ALREADY_ACTIVE`
  - 到期后用新码重新激活，覆盖权益行并保留事件
  - 同码同身份二次使用被拒，包括删号重导后的情况
  - 同码不同身份都能激活
  - 平台或档次不符、档次未知、身份缺失、共享中均被拒
  - 多次码绑定用户
  - 并发使用同一码时，成功次数不超过剩余次数
  - 已删除账号被拒
  - 上述所有失败场景下，兑换码都不被消耗、不启动计时

## 4a. 管理员撤销激活

- [ ] 4a.1 新增 `ExclusiveActivationService.AdminRevoke(ctx, adminID, eventID, reason)`，仓储层单事务处理：
  - 加锁顺序：兑换码行 → 账号与归属行 → 权益行
  - 校验：事件状态为 `active`、对应权益生效且未到期；`reason` 非空
  - 权益写入 `revoked_at`、`revoke_kind='admin_revoked'`
  - 事件写入 `status='revoked'` 以及撤销人、时间、原因
  - 解绑私有分组，清除镜像，账号保持闲置
  - 写入审计，发送 outbox，让准入缓存失效
  - 不修改 `used_activations`、`service_started_at`、`service_expires_at`
- [ ] 4a.2 管理端路由 `POST /admin/exclusive/activations/:event_id/revoke`，只挂在 admin 路由组；用户端不提供任何撤销入口
- [ ] 4a.3 激活流程允许对 `revoked_at IS NOT NULL` 的权益行覆盖重新激活，并清空 `revoked_at`、`revoke_kind`、`finalized_at`
- [ ] 4a.4 `SharedPoolAccountView.exclusive` 增加 `revoked_at` 与 `revoke_reason`，用于账号卡片展示
- [ ] 4a.5 测试：
  - 撤销后立即失去独享、账号回到闲置、私有分组保留
  - 次数不退、到期时间不变、撤销首次激活也不重新计时
  - 撤销后用原码再激活同一上游号成功，并再消耗一次（3 次码：激活 → 撤销 → 再激活，已用 2、剩余 1）
  - 原码剩余次数仍可激活其它账号
  - 多次撤销后，事件表中允许存在多条 revoked 记录
  - 撤销后用新码激活成功
  - 单次码撤销后变为 used
  - 普通用户调用返回 403；缺少原因返回 400
  - 已到期、已撤销、已删除的记录不可撤销
  - 撤销与激活、撤销与到期 worker 并发时状态一致
  - 撤销后新请求被准入否决，多实例缓存经 outbox 失效

## 5. 互斥与分组保护

- [ ] 5.1 `SharedPoolSharingAllowed` 支持 `exclusive_active`；`PreserveSharedPoolExtra` 与 `account_protection.go` 的共享键列表加入独享镜像键，防止管理员编辑时覆盖
- [ ] 5.2 `setSharedEnabled` 与仓储 `SetSharedAccountState` 在加锁后检查有效权益，返回 `EXCLUSIVE_SHARING_FORBIDDEN`；`AdminAssign` 拒绝为独享账号分配共享分组
- [ ] 5.3 `admin_group.go`：更新与删除时保护私有分组的受保护字段；另在两个入口校验私有分组归属：`admin_account` 的分组绑定，以及用户 `allowed_groups` 更新
- [ ] 5.4 分组分类：
  - `ListGroups` 与 `/admin/groups/all` 支持 `kind=platform|private`；`/all` 默认只返回 platform
  - 管理端其它分组选择处统一改为只列平台分组：账号绑定分组、用户可用分组、兑换码订阅分组、共享池分组配置
  - 用户端可用分组接口返回分组类型，供 API Key 下拉分组显示
  - 私有分组的用量在统计中单独汇总
- [ ] 5.5 测试：双向互斥、开共享与激活并发、各分组保护入口、`kind` 筛选、`/all` 默认不含私有分组

## 6. 运行时准入

- [ ] 6.1 仓储 `ExclusiveGrantForAccount` 加 30 秒本地缓存，在激活、到期、撤销、删除时主动失效，并经调度 outbox 通知其它实例；到期时间按实时时间比较
- [ ] 6.2 `sharedPoolAdmission` 增加私有分组分支，校验 owner、权益、身份、可调度状态与代理；在非私有分组中否决带有效权益的账号
- [ ] 6.3 确认以下路径都经过该分支：Gateway 与 OpenAI 两条准入入口（`gateway_profit_control.go:84`、`openai_profit_control.go:341`）、WebSocket、长连接复用、粘性会话
- [ ] 6.4 测试：
  - 到期未收尾的账号被否决（包括同码的晚激活账号）
  - 非 owner 被否决
  - 身份不一致被否决
  - 全部否决时返回 503 `No available accounts`，且不回退
  - 独享账号出现在公共分组时被否决

## 7. 免计费

- [ ] 7.1 `CheckBillingEligibility`：私有分组且用户为 owner 时，跳过余额、平台额度与订阅检查，保留 API Key 限额检查
- [ ] 7.2 `buildUsageBillingCommand`：私有分组下把 `BalanceCost`、`SubscriptionCost`、`AccountQuotaCost` 置 0，并跳过 `applySharedPoolBillingSnapshot`
- [ ] 7.3 统一接入以下计费路径：Gateway、OpenAI、`openai_live`、批量图片预扣与结算、Grok 媒体；用量日志写入 `billing_mode='exclusive'`
- [ ] 7.4 测试：余额为 0 仍可用；结算后余额不变；不产生收益记录；日志 `actual_cost=0`；修改分组倍率后仍不扣费；API Key 限额仍生效

## 8. 并发

- [ ] 8.1 用户级并发不改：私有分组请求照常调用 `AcquireUserSlotWithWait(c, userID, subject.Concurrency, ...)`，与其它分组共用用户管理中的并发额度
- [ ] 8.2 `gateway_helper.go` 新增 `effectiveAccountConcurrency(ctx, account)`，私有分组下返回 0，即不获取账号槽位
- [ ] 8.3 替换以下文件中的账号槽位调用点：`gateway_handler.go`、`gateway_handler_chat_completions.go`、`gateway_handler_responses.go`、`gateway_web_search.go`、`gemini_v1beta_handler.go`、`openai_gateway_handler.go`、`openai_live.go`
- [ ] 8.4 测试：
  - 私有分组受用户原有并发限制，排队与拒绝行为与现有一致
  - 私有分组与平台分组共用同一额度
  - 私有分组不受账号并发限制
  - 排队超时被拒时不扣费

## 9. 代理

- [ ] 9.1 `applyProxy`：`with_proxy=false` 的独享账号不允许切到随机池，返回 `EXCLUSIVE_PROXY_NOT_PURCHASED`
- [ ] 9.2 准入层代理校验：随机模式要求权益 `with_proxy=true`；固定代理要求代理归属 owner
- [ ] 9.3 激活时，如果码不含代理而账号是随机模式，激活成功但返回警告
- [ ] 9.4 确认 OpenAI 插件出站目录（`openai_plugin_account_directory.go`）与 TLS 指纹路径同样受约束，不会直连
- [ ] 9.5 测试：
  - 不含代理且未配置代理时被否决
  - 含代理的码到期后，改用不含代理的码重新激活，随机模式被否决
  - 清空代理被拒
  - 含代理时可以使用随机池
  - 空池 `reject` 语义不变

## 10. 到期与删除

- [ ] 10.1 新增 `exclusive_expiry_worker.go`，每分钟执行，并在 wire 中注入：
  - 收尾到期权益：解绑分组、清除镜像、写入 `finalized_at`、发送 outbox
  - 把 `service_expires_at` 已过的码置为 expired
- [ ] 10.2 `RemoveSharedAccount`：同一事务内撤销权益（`revoke_kind='account_deleted'`，事件状态 `account_deleted`）、解绑私有分组；到期 worker 同时把事件状态改为 `expired`
- [ ] 10.3 测试：
  - 同码的多个账号同时收尾
  - 收尾后私有分组保留
  - 用新码重新激活后，原 API Key 可用
  - 删除后权益被撤销
  - 管理员停用不影响计时

## 11. 前端

- [ ] 11.1 `frontend/src/api/sharedPool.ts`：增加预览与激活接口，以及 `exclusive` 字段类型
- [ ] 11.2 `views/user/SharedPoolView.vue` 账号卡片：
  - 显示独享状态、到期时间、是否含平台代理，到期前 3 天提醒
  - 未独享或已到期时显示「激活独享」按钮，有效期内不显示
  - 激活弹窗：先预览（平台、档次、天数、代理、剩余次数；未启用码提示“激活后开始计时，X 天后全部到期”，已启用码提示“可用至 X，剩余 N 天”），再确认
  - 身份缺失或档次未知的账号显示「请联系管理员设置」
  - 被管理员撤销的账号显示「独享已被管理员撤销」、撤销时间与原因，并显示「激活独享」按钮
  - 独享期间禁用共享开关并提示原因；码不含代理且未填写代理时提示
- [ ] 11.3 `KeysView.vue`：分组下拉分「平台分组」「我的私有分组」两组，私有分组显示为「独享 · 平台」
- [ ] 11.4 管理端：
  - 兑换码页面：
    - 生成 `exclusive_account` 类型时设置平台、订阅档次（多选，随平台变化）、激活次数、有效天数、是否含代理、兑换截止时间
    - 列表显示规格、「已用/上限」、服务到期时间和状态，支持筛选
    - 行内与批量操作：调整次数、作废剩余、停用、清空绑定、查看激活明细；未启用码可修改规格
    - 激活明细：每条显示状态；生效中的记录提供「撤销」按钮，二次确认，必须填写原因，并提示「这次次数不退；账号可以重新激活，用原码会再消耗一次」
  - 分组页面：分「平台分组」「私有分组」两个标签页；私有分组显示所属用户，受保护字段置灰
  - 其它分组选择器只列平台分组
  - 共享池账号管理：显示身份状态，支持按缺失筛选，提供「设置上游身份」弹窗（需填写原因）；独享账号行提供「撤销独享」入口，调用同一个撤销接口
- [ ] 11.5 中英文 i18n
- [ ] 11.6 组件测试：
  - 开关互斥
  - 预览文案
  - 兑换成功与各类失败提示（含联系管理员）
  - 代理提示与到期提醒
  - 分组两个标签页
  - API Key 分组分组显示
  - 兑换码规格表单联动
  - 撤销弹窗必须填写原因，用户端不出现撤销入口

## 12. 开关与验证

- [ ] 12.1 设置项 `exclusive_hosting_enabled`（默认 false）：关闭时隐藏入口，预览与激活接口返回 404
- [ ] 12.2 `upstream_identity` 存量回填任务：默认关闭，需管理员确认后执行；输出重复账号与无法提取身份的清单
- [ ] 12.3 执行 `go test -tags=unit ./...`、相关 postgres 集成测试、`golangci-lint`、前端 `vitest` 与 `vue-tsc`、`openspec validate account-exclusive-hosting --strict`，结果写入 `verification.md`
- [ ] 12.4 回归：共享池现有的导入、共享、收益、自动转入、订阅分组派发，普通兑换码、用户并发，以及管理端现有分组选择均不受影响
