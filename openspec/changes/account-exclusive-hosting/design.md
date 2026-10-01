## Context

动机见 proposal.md。与方案相关的现状：

- 共享账号的归属在 `shared_pool_accounts(account_id, owner_user_id, enabled, assigned, admin_disabled, credential_fingerprint)`，并镜像到 `accounts.extra` 的 `shared_pool_*` 键（`shared_pool_account_state.go:102`）。
- 可调度判定 `SharedPoolSharingAllowed`（`shared_pool_accounts.go:168`）：凡有 owner 的账号必须 `shared_pool_enabled && !admin_disabled` 才可调度。
- 准入 `sharedPoolAdmissionLatest` → `sharedPoolAdmission`（`shared_pool_admission.go`）在抢到槽位后读取最新账号：要求账号属于当前分组、已授权调度、结算倍率有效且下游价格覆盖结算价，由 `GatewayProfitControlVetoLatest`（`gateway_profit_control.go:84`）与 OpenAI 对应入口（`openai_profit_control.go:341`）调用。
- `credential_fingerprint` 是对 `refresh_token`/`access_token`/`api_key` 的 SHA256（`shared_pool_credentials.go:50`），token 刷新即变化，不能作为上游身份。删除后重新导入由 `prepareSharedAccountReimport` 让出指纹占位。
- 计费：请求前 `CheckBillingEligibility` 检查余额与平台额度（`billing_cache_service.go:736`），请求后 `buildUsageBillingCommand` 生成扣费命令，`applySharedPoolBillingSnapshot` 为共享账号附加分成快照（`shared_pool_billing.go:119`）。分组倍率校验要求 > 0（`admin_group.go:399`），不能用倍率 0 表示免费。
- 并发：网关 handler 调用 `AcquireUserSlotWithWait` 与 `AcquireAccountSlot`；Redis 用户槽位键为 `concurrency:user:{id}`（`concurrency_cache.go:30`），`maxConcurrency <= 0` 表示不限（`concurrency_service.go:344,383`）。
- 兑换：`RedeemService.redeem` 按 `redeem_codes.type` 分派，已有 `balance`、`concurrency`、`subscription`；兑换码为一次性使用（`used_by`/`used_at`），已有 `expires_at`（码本身的兑换截止时间）与 `validity_days` 字段。
- 专属分组：`groups.is_exclusive` 配合 `users.allowed_groups` 控制可用范围，观察员功能已有「为用户建专属分组」的样板（`admin_observer_setup.go:100`）。

## Goals / Non-Goals

**Goals:**
- 私有调度复用「分组 → 账号」调度模型，不修改调度器选号算法、粘性会话和账号缓存结构。
- 所有放行判定都以持久化状态为准并在准入时实时校验；到期 worker 只负责收尾，不作为唯一的安全边界。
- 免计费只由分组上的 `exclusive_owner_user_id` 与请求用户一致触发，不依赖分组倍率等可被管理员修改的字段。
- 独享与共享在数据层互斥，任何写入路径都不能使一个账号同时处于两种状态。
- 兑换码自带全部规格（平台、订阅档次、次数、天数、是否含代理），是有整体有效期的名额包：一个码激活的所有账号同时到期。
- 用户并发沿用用户管理中的现有配置，不新增独享并发上限。

**Non-Goals:**
- 不支持提前续费、顺延、名额转移、换绑、迁移、退款或封号补偿。
- 不支持用户自行撤销；撤销只由管理员操作，且不退回次数。
- 不支持独享与共享同时开启。
- 不支持私有号不可用时回退到其它分组。
- 不对独享账号做账号级并发限制。
- 不做独享的在线支付，第一版只通过兑换码发放。
- 不做独立的「套餐」实体，规格直接存在兑换码上。
- 不改变已有共享账号、共享收益、普通兑换码、分组和用户并发的语义。

## Decisions

### D1：兑换码自带规格，是有整体有效期的名额包
```
redeem_codes 新增：
  exclusive_platform    VARCHAR(32) NULL   -- 独享码必填
  exclusive_tiers       JSONB NULL          -- 允许的订阅档次，[] 表示不限
  exclusive_with_proxy  BOOL NULL
  max_activations       INT NULL            -- 激活次数上限，管理员可调整
  used_activations      INT NULL DEFAULT 0
  bound_user_id         BIGINT NULL
  service_started_at    TIMESTAMPTZ NULL    -- 首次激活时间
  service_expires_at    TIMESTAMPTZ NULL    -- = service_started_at + validity_days
  （有效天数复用已有的 validity_days 列）
  CHECK (type <> 'exclusive_account' OR (
    exclusive_platform IS NOT NULL AND exclusive_with_proxy IS NOT NULL AND validity_days >= 1
    AND max_activations BETWEEN 1 AND 1000 AND used_activations BETWEEN 0 AND max_activations))
```
- 新类型 `RedeemTypeExclusiveAccount = "exclusive_account"`。
- **生成时直接设置规格**，不经过套餐：
  - 平台
  - 订阅档次：可多选，取值与共享池档位一致，不选表示不限
  - 激活次数：1–1000
  - 有效天数：≥1
  - 是否含代理
  - 可选的兑换截止时间
- **一批码规格相同**，批次信息由现有 `notes` 字段区分。
- **两个时间，各管一段**：
  - `redeem_codes.expires_at`（已有字段，可选）：**未启用**的码的兑换截止时间，过了这个时间还没首次激活，码作废。
  - `service_expires_at`：首次激活时写入，等于首次激活时间加有效天数。**之后该码激活的每个账号，独享到期时间都等于这个值**。到期后，已激活的账号全部失效，剩余次数一并作废。
- 次数语义：一次激活消耗一次，对应一个账号。剩余次数 = `max_activations - used_activations`，不单独存储。
- 码状态：
  - `unused`：未启用
  - `active`：已启用、未到期、有剩余
  - `used`：次数用完但未到期
  - `expired`：`service_expires_at` 已过。优先级最高，到期后无论剩余多少次都不可再用
  - 另外支持管理员手动 `disabled`（停用）：停用后不能再激活，已激活账号不受影响。
- 示例：码 5 次、30 天。6/1 激活账号 A，A 到期时间为 7/1；6/20 激活账号 B，B 到期时间也是 7/1。7/1 之后 A、B 同时失效，剩余 3 次作废。

### D2：独享权益绑定账号，不能提前续费
```
exclusive_account_grants(
  account_id        BIGINT PRIMARY KEY REFERENCES accounts(id),
  owner_user_id     BIGINT NOT NULL REFERENCES users(id),
  platform          VARCHAR(32) NOT NULL,
  upstream_identity VARCHAR(128) NOT NULL,
  redeem_code_id    BIGINT NOT NULL REFERENCES redeem_codes(id),  -- 当前生效的码
  with_proxy        BOOL NOT NULL,
  activated_at      TIMESTAMPTZ NOT NULL,
  expires_at        TIMESTAMPTZ NOT NULL,   -- 激活时的 redeem_codes.service_expires_at 快照
  revoked_at        TIMESTAMPTZ NULL,       -- 删除账号或管理员撤销时写入
  revoke_kind       VARCHAR(16) NULL,       -- account_deleted | admin_revoked
  finalized_at      TIMESTAMPTZ NULL,       -- 到期收尾完成时间
  created_at, updated_at)

exclusive_grant_events(
  id, account_id, owner_user_id, redeem_code_id, upstream_identity, account_tier,
  with_proxy, activated_at, expires_at, created_at,
  status VARCHAR(16) NOT NULL DEFAULT 'active',  -- active | expired | revoked | account_deleted
  revoked_at, revoked_by_admin_id, revoke_reason,
  -- 部分唯一索引：同码同上游号只能有一条未撤销的记录
  UNIQUE INDEX (redeem_code_id, upstream_identity) WHERE status <> 'revoked')
```
- `account_id` 为主键：一个账号同一时间只有一条权益。删除账号后，重新导入的账号 ID 不同，拿不到旧权益。
- **不允许提前续费**：账号存在未撤销且 `expires_at > now` 的权益时，任何兑换都返回 `EXCLUSIVE_ALREADY_ACTIVE`，兑换码不被消耗。到期或被管理员撤销后，只能用另一个未到期的码重新激活，此时覆盖权益行（`redeem_code_id`、`activated_at`、`expires_at`、`with_proxy`，并清空 `revoked_at`、`revoke_kind`、`finalized_at`），历史保留在事件表。
- **同一个码对同一个上游号只能有一次未撤销的激活**：`exclusive_grant_events` 上的部分唯一索引 `(redeem_code_id, upstream_identity) WHERE status <> 'revoked'` 保证这一点。
  - 这样可以堵住“删号重导后，用同一个码的剩余次数再激活同一个号”，满足“删号后必须重新购买”：删除产生的记录状态是 `account_deleted`，仍然占着这个位置。
  - 被管理员撤销的记录状态是 `revoked`，不占位，所以撤销后原码可以再激活同一个号。
  - 同码激活后到期的情况不需要特别处理：权益到期就等于码到期，码已经不能再用。
- 权益的 `expires_at` 是快照。管理员作废码的剩余次数不影响已激活账号；码自身的 `service_expires_at` 在第一版不开放修改。
- `accounts.extra` 只镜像 `exclusive_active` 和 `exclusive_expires_at`，供前端与调度快照使用；准入以权益表为准。

### D2a：管理员撤销激活
用途：激活搞错了（激活到错误的账号、用错了码、档次选错等）时，由管理员纠正。

- **只有管理员能撤销**，用户端没有入口。用户需要撤销时联系管理员。
- 接口：
  - `POST /admin/exclusive/activations/:event_id/revoke {reason}`：撤销某一次激活。`event_id` 是 `exclusive_grant_events` 的记录，从兑换码激活明细或账号独享详情中取。
  - 从账号侧撤销时，前端取该账号当前生效的那条激活记录，调用同一个接口。
- 条件：
  - 只能撤销**当前生效**的那次激活：事件状态为 `active`，且对应权益行的 `redeem_code_id` 相同、未撤销、未到期。
  - 已到期、已撤销、账号已删除的记录，返回 400（`EXCLUSIVE_ACTIVATION_NOT_REVOCABLE`）。
  - `reason` 必填。
- 事务（加锁顺序与激活一致：兑换码行 → 账号与归属行 → 权益行）：
  1. 权益行写入 `revoked_at=now`、`revoke_kind='admin_revoked'`。
  2. 事件行写入 `status='revoked'`、`revoked_at`、`revoked_by_admin_id`、`revoke_reason`。
  3. 把账号从私有分组解绑，清除 `accounts.extra` 的独享镜像，账号保持 `shared_pool_enabled=false`，回到闲置。
  4. 发送调度 outbox，并让准入缓存失效。
  5. 写入 `exclusive_code_adjustments`（`op='revoke_activation'`）。
- 撤销后的效果：
  - **次数不退**：`used_activations` 不变，这一次视为作废。
  - **原码可以再激活这个账号**：被撤销的记录不占用部分唯一索引，所以同一个码可以再次激活同一个上游号，**再消耗一次**。原码剩余的次数也可以激活其它账号，到期时间不变。
  - **计时不回退**：码的 `service_started_at` 和 `service_expires_at` 不变。即使撤销的是这个码的第一次激活，也不重新计时，因为码已经启用。
  - **账号可以重新激活**：账号回到闲置后，可以用任意有效兑换码重新激活（包括原码），也可以开启共享。
  - **私有分组保留**：用户已有的 API Key 不失效，重新激活后可以直接继续用。
  - **进行中的请求**：撤销时已经在执行的请求不中断；之后的新请求在准入时被拒绝。
- 用户可见：账号卡片显示「独享已被管理员撤销」，附撤销时间和原因。激活明细中该记录标为「已撤销」。
- 不提供「恢复撤销」。如果撤销错了，由管理员另发一个兑换码补偿。

### D3：上游稳定身份 `upstream_identity`
- 新增 `sharedUpstreamIdentity(platform, kind, credentials) (string, error)`，返回 `platform:kind:` 前缀加 SHA256：
  - OpenAI OAuth：`chatgpt_account_id` + `chatgpt_user_id`。只有 access_token 时，用与 `sharedOpenAIPlanClaims` 相同的 JWT 解析提取，并要求与凭证中的 `chatgpt_account_id` 一致。
  - Anthropic OAuth：`org_uuid` + `account_uuid`。
  - Gemini / Antigravity OAuth：`email`（小写）+ `project_id`。
  - API Key：`api_key` 的哈希（与现有指纹一致）。
- 保存到 `shared_pool_accounts.upstream_identity`，来源记在 `upstream_identity_source`（`auto` 表示系统自动提取，`admin` 表示管理员手动设置），并对未删除账号建部分唯一索引。删除账号时，与现有指纹让位一致，改写为 `deleted:<account_id>`。
- **无法提取身份的账号**：允许导入和共享（兼容现状），但激活独享时返回 `EXCLUSIVE_IDENTITY_REQUIRED`，提示「无法识别上游账号，请联系管理员设置」，不消耗次数。
- **管理员手动设置身份**：`PUT /admin/shared-pool/accounts/:id/upstream-identity {fields, reason}`。
  - 管理员按平台填写与自动提取**相同的字段**（OpenAI：`chatgpt_account_id` + `chatgpt_user_id`；Anthropic：`org_uuid` + `account_uuid`；Gemini / Antigravity：`email` + `project_id`），系统用同一个函数计算哈希，保证手动值与日后自动提取的值可比较。写入身份，来源记为 `admin`，并做全局唯一校验，重复则拒绝。
  - 账号有有效独享权益时，不允许修改身份。
  - 每次设置都写入 `shared_pool_identity_audits(id, account_id, admin_id, before_identity, after_identity, reason, created_at)`。
  - 管理端共享池账号列表显示身份状态（已识别 / 管理员设置 / 缺失），可按「缺失」筛选。
  - 来源为 `admin` 的账号在凭证刷新或编辑时，如果系统自动提取出身份且与手动值不同，拒绝更新（`EXCLUSIVE_IDENTITY_MISMATCH`）；提取不出时保留手动值。
- 编辑凭证：若账号存在未撤销的权益（无论是否到期），新凭证的身份必须等于权益里的 `upstream_identity`，否则返回 `EXCLUSIVE_IDENTITY_MISMATCH`。没有权益时，照常更新身份并做唯一性校验。
- 结果：同一上游号全局只能存在一个未删除的托管账号，跨用户也一样。

### D4：管理员控制兑换码
- 生成：`POST /admin/redeem-codes/generate`，`type=exclusive_account` 时必填 `exclusive_platform`、`validity_days`、`max_activations`、`exclusive_with_proxy`，选填 `exclusive_tiers` 和 `expires_at`。档次会按平台校验，使用 `canonicalSharedSubscriptionTier`，非法值返回 400。
- 修改单码：`PATCH /admin/redeem-codes/:id/exclusive {max_activations?, tiers?, validity_days?, with_proxy?, clear_bound_user?, disabled?, reason}`。
  - `max_activations`：任何未到期状态都可以改，但不能低于已用次数，也不能超过 1000。
  - `tiers`、`validity_days`、`with_proxy`：**只在未启用（`unused`）时可改**，码启用后规格即固定，防止影响已激活账号。
  - 已到期（`expired`）的码拒绝一切修改。
- 批量调整：`POST /admin/redeem-codes/batch-activations {ids, op: set|add|void_remaining|disable, value?, reason}`。每个码单独校验，逐项返回结果。
- 作废剩余次数：把上限设为已用次数。已激活账号不受影响。
- 查看明细：`GET /admin/redeem-codes/:id/activations`，列出每次激活的用户、账号、上游身份摘要、账号档次、激活时间、到期时间和是否含代理，账号已删除时仍显示。
- 审计：所有修改写入 `exclusive_code_adjustments(id, redeem_code_id, admin_id, op, before JSONB, after JSONB, reason, created_at)`。修改与激活都对兑换码行加 `FOR UPDATE`，保证并发时不超用。
- 列表：显示平台、档次、天数、是否含代理、「已用/上限」、服务到期时间与状态，支持按平台、状态、「有剩余」筛选。
- 防转卖：多次码首次使用时写入 `bound_user_id`，之后只有该用户能用。管理员可清空绑定，并记入审计。

### D5：激活流程
用户侧入口：
- `GET /shared-pool/exclusive/codes/:code/preview`：返回平台、允许档次、有效天数、是否含代理、剩余次数、状态。已启用的码额外返回 `service_expires_at` 和剩余天数，提示“激活后可用至 X”。受兑换限流保护，绑定给他人的码不返回详情。
- `POST /shared-pool/accounts/:id/exclusive/redeem {code}`：要求 `Idempotency-Key`。
- 通用兑换接口 `/redeem` 对该类型返回 `EXCLUSIVE_CODE_ACCOUNT_REQUIRED`，不消耗次数，也不计失败次数。

激活事务（单个事务内按顺序加锁：兑换码行 → 账号与归属行 → 权益行）：
1. **兑换码校验**：类型正确、未停用、`bound_user_id` 为空或等于当前用户、有剩余次数。未启用时检查 `expires_at` 兑换截止时间；已启用时检查 `service_expires_at > now`。
2. **账号校验**：归属当前用户、未删除、未被管理员停用、平台等于码的平台、档次在码的 `exclusive_tiers` 内（空表示不限，档次来自 `sharedAccountSubscriptionTier`；码限定了档次而账号识别不出档次时拒绝，由管理员用现有的档位覆盖功能设置）、`upstream_identity` 非空、`shared_pool_enabled=false`。
3. **权益校验**：不存在未撤销且未到期的权益（`EXCLUSIVE_ALREADY_ACTIVE`），且事件表中没有同一码加同一上游身份、状态不是 `revoked` 的记录（`EXCLUSIVE_CODE_USED_FOR_ACCOUNT`）。
4. **启用码**：首次使用时写入 `service_started_at = now`、`service_expires_at = now + validity_days`，以及 `bound_user_id`（仅多次码）。
5. **写权益与事件**：`expires_at = service_expires_at`，`with_proxy` 取码的 `exclusive_with_proxy`，事件记录账号当时的档次。
6. **分组**：确保私有分组存在（D6），把账号加入私有分组，并清理残留的共享分组关联。
7. **收尾**：`used_activations + 1`，更新码状态，更新 `accounts.extra` 镜像，发送调度 outbox，并让准入缓存失效。

所有失败场景下，兑换码都不被消耗。限流与分布式锁沿用 `RedeemService` 的 `checkRedeemRateLimit` 与 `acquireRedeemLock`。

### D6：私有分组自动创建与保护
```
exclusive_user_groups(user_id, platform, group_id UNIQUE, PRIMARY KEY(user_id, platform))
groups + exclusive_owner_user_id BIGINT NULL
```
- 首次激活时在同一事务内创建：`name = "独享-<user_id>-<platform>"`（`groups.name` 全局唯一，展示名由前端拼接）、`is_exclusive=true`、`is_shared_pool=false`、`subscription_type=standard`、`rate_multiplier=1`（只为通过校验，不参与计费）、`exclusive_owner_user_id=user_id`，并加入该用户的 `allowed_groups`。已存在则复用；分组被管理员停用时拒绝激活。
- 保护（管理端分组更新与删除、账号绑定分组、用户可用分组更新三个入口统一校验）：
  - 不能删除，不能修改 `is_shared_pool`、`is_exclusive`、`subscription_type`、`platform`、`exclusive_owner_user_id`，不能设置回退分组。
  - 不能授权给 owner 以外的用户。
  - 只能绑定 owner 名下的独享账号，平台自有账号也不行。
  - 管理员可以停用分组（风控用），也可以修改模型映射等不影响归属的配置。
- 到期后账号从私有分组解绑，分组保留，用户已有的 API Key 不失效；用新码重新激活后可直接继续使用。
- **分组分类**：以 `exclusive_owner_user_id` 是否为空区分：为空是「平台分组」，不为空是「私有分组」。
  - 管理端分组页：分「平台分组」「私有分组」两个标签页，分别调用 `GET /admin/groups?kind=platform|private`。私有分组页显示所属用户、平台、已绑定的独享账号数，可按用户搜索，受保护字段置灰。
  - 管理端其它选择分组的地方（账号绑定分组、用户可用分组、兑换码订阅分组、共享池分组配置、分组统计等）默认只列平台分组，`/admin/groups/all` 默认加 `kind=platform`。
  - 用户创建或编辑 API Key：分组下拉分为「平台分组」「我的私有分组」两组，私有分组显示为「独享 · 平台」。
  - 仪表盘与用量统计：按分组类型区分，私有分组的用量单独汇总，因为 `actual_cost=0`，不计入平台收入统计。

### D7：可调度判定与互斥
- `SharedPoolSharingAllowed` 扩展为 `(enabled || exclusive_active) && !admin_disabled`。
- 互斥由服务层和仓储层双重保证：
  - `setSharedEnabled(enabled=true)`：存在有效权益时返回 `EXCLUSIVE_SHARING_FORBIDDEN`。
  - 激活独享：要求 `shared_pool_enabled=false`，否则返回 `EXCLUSIVE_SHARING_ACTIVE`。
  - 仓储 `SetSharedAccountState` 加锁后再次检查权益。
  - 管理员 `AdminAssign` 不能给独享账号分配共享分组。
- `PreserveSharedPoolExtra` 与 `account_protection.go` 的共享键列表加入独享镜像键，防止管理员编辑账号时覆盖。

### D8：运行时准入
在 `sharedPoolAdmission` 开头加私有分组分支（当前分组 `exclusive_owner_user_id > 0`），不走共享授权和结算校验：
1. 请求用户等于 `group.exclusive_owner_user_id`。
2. 账号的 `shared_pool_owner_id` 等于该用户。
3. 权益存在、未撤销、`expires_at > now`、`upstream_identity` 等于账号当前身份。权益查询带 30 秒本地缓存，激活、到期、撤销、删除时主动失效，并通过现有的调度 outbox 通知其它实例一起失效；到期时间是实时比较的，不受缓存影响。
4. 账号可调度，且未被管理员停用。
5. 满足代理约束（D11）。

任一条不满足就否决该账号。所有候选都被否决时，走现有 `ErrNoAvailableAccounts` 分支，返回 503 `No available accounts`。私有分组没有回退分组，所以不会回退。

反向约束：在非私有分组中调度到带有效权益的账号时一律否决，防止误绑导致泄漏。

需要确认经过该分支的路径：Gateway、OpenAI HTTP、OpenAI WebSocket 与长连接复用、粘性会话命中路径。

### D9：免计费
- 前置：`CheckBillingEligibility` 开头判断，私有分组且用户为 owner 时，跳过余额、平台额度和订阅检查。API Key 自设的额度与限流仍然生效。
- 后置：`buildUsageBillingCommand` 在私有分组下把 `BalanceCost`、`SubscriptionCost`、`AccountQuotaCost` 置 0，不调用 `applySharedPoolBillingSnapshot`，所以不写收益。保留 `APIKeyQuotaCost` 与 `APIKeyRateLimitCost`。
- 用量日志：照常写入，`total_cost` 保留原始成本，`actual_cost=0`，`billing_mode='exclusive'`。
- 覆盖路径：Gateway、OpenAI、`openai_live`、批量图片预扣与结算、Grok 媒体。
- 备选：分组倍率设为 0。否决，理由是倍率校验要求 > 0，而且管理员一改倍率就会误扣费。

### D10：并发沿用用户管理中的用户并发
- 用户级：私有分组请求与其它请求一样调用 `AcquireUserSlotWithWait(c, userID, subject.Concurrency, ...)`，占用同一个 `concurrency:user:{id}` 计数，上限就是用户管理里设置的并发值。**不新增设置项、不新增 Redis 键、不设 100 上限**，排队、超时、队列满的行为也保持不变。
- 账号级：私有分组选中的账号不获取账号级并发槽位（传 0，现有语义即不限），避免账号上的并发配置限制号主自用。
- 实现：`gateway_helper.go` 新增 `effectiveAccountConcurrency(ctx, account)`，把账号槽位调用点（`gateway_handler.go`、`gateway_handler_chat_completions.go`、`gateway_handler_responses.go`、`gateway_web_search.go`、`gemini_v1beta_handler.go`、`openai_gateway_handler.go`、`openai_live.go`）统一改为经过它。用户槽位调用点不改。
- 上游撑不住时会返回 429，由现有限流逻辑处理，只影响用户自己的号。

### D11：代理
- 兑换码含代理：权益 `with_proxy=true`，独享期内可以使用 `proxy_mode=random`（平台随机池），沿用现有随机代理选择与空池 `reject` 策略。
- 兑换码不含代理：账号必须配置归属 owner 的固定代理（`shared_pool_proxies`）。随机模式或没有代理时，准入以 `exclusive_proxy_required` 否决，**不会直连**。
- `applyProxy` 清空代理时会自动切到随机池。对 `with_proxy=false` 的独享账号，这条路径返回 `EXCLUSIVE_PROXY_NOT_PURCHASED`。
- 激活时如果码不含代理，而账号当前是随机模式，激活仍然成功，但返回警告，账号卡片提示“请填写自有代理，否则无法使用”。
- 私有请求不收代理费，`proxy_rate_bps` 不适用。
- 需要确认同样受约束、不能绕过的路径：OpenAI 插件出站目录与 TLS 指纹路径。

### D12：到期与删除
- `ExclusiveExpiryWorker` 每分钟执行两件事：
  1. 收尾到期权益：找出 `expires_at <= now AND revoked_at IS NULL AND finalized_at IS NULL` 的权益，把账号从私有分组解绑，清除镜像，写入 `finalized_at`，对应事件状态改为 `expired`，发送 outbox。账号 `shared_pool_enabled` 保持 false，进入闲置。
  2. 更新码状态：把 `service_expires_at <= now` 的码置为 `expired`。
- 准入实时校验到期时间，worker 延迟不会造成超期使用。
- 删除账号（`RemoveSharedAccount`）：同一事务内写入 `revoked_at`、`revoke_kind='account_deleted'`，事件状态改为 `account_deleted`，解绑私有分组，让出 `upstream_identity`。重新导入后，必须用另一个码激活（D2 的唯一约束保证不能复用原码）。
- 管理员停用账号、上游封号、凭证失效：都不撤销、不暂停、不顺延权益。

## Risks / Trade-offs

- **晚激活的号可用时间短**：同一个码后激活的账号可用时间更短，这是“按码计时”的预期行为。预览接口会明确显示剩余天数，避免用户误解。
- **身份提取不完整**：部分导入凭证缺少身份字段，这类账号需要管理员手动设置身份后才能独享。手动身份依赖管理员填写正确，所以用审计记录追溯，并做全局唯一校验防止重复；上线前抽样检查存量账号能否提取身份。
- **存量重复号**：部分唯一索引上线前，存量数据可能已有同一上游号的多个账号。回填任务只输出清单，由管理员处理，不自动删除。
- **准入额外查询**：私有分组每次准入多一次权益查询，用本地短缓存加主动失效控制成本；非私有分组不受影响。
- **私有请求占用用户并发**：用户同时用平台分组和私有分组时共用同一份并发额度，这是沿用现有语义的结果；需要更多并发时由管理员在用户管理中调整。
- **撤销多实例延迟**：准入缓存在各实例本地，撤销后依靠 outbox 通知失效，最长延迟为缓存有效期 30 秒。撤销主要用于纠错，这个延迟可以接受。
- **撤销不退次数**：撤销那一次永久作废。如果撤销是平台失误造成的，由管理员用「调整次数」补回，系统不自动处理。
- **撤销后原码可再用**：撤销是管理员操作，并且每次都消耗一次，不会被用户用来绕过规则。
- **规格固化**：码启用后不能改档次、天数和代理，只能调整次数或停用，避免影响已激活账号。
- **到期断档**：不允许提前续费，到期与重新激活之间会有短暂不可用，这是业务规则的预期结果。前端在到期前 3 天提示用户准备新码。

## Migration Plan

1. 迁移 `261_exclusive_hosting.sql`：建表与加列全部使用 `IF NOT EXISTS`，新列可空或带默认值，不改写存量行语义。
2. `upstream_identity` 回填由一次性任务在应用层计算，默认关闭，管理员确认后执行，输出重复和无法提取身份的清单。
3. 功能开关 `exclusive_hosting_enabled`（默认 false）：关闭时隐藏用户入口，激活与预览接口返回 404。因为没有私有分组，准入、计费和并发分支都不会被触发。
4. 回滚：关闭开关即可。已激活权益保留，管理员可停用私有分组。不需要回滚迁移。
