## Why

部分供号用户希望把账号托管在平台上只给自己用，不参与共享池调度，也不赚取共享收益。当前共享池只有「闲置」和「授权共享」两种状态：`SharedPoolSharingAllowed` 要求共享开关打开才可调度，未授权调度的账号还会被 `sharedPoolAdmission` 拒绝，用户无法通过自己的 API Key 使用自己的账号。平台希望以包月方式收取托管费，通过兑换码售卖，并可选附带平台代理。

## What Changes

- 新增「独享」模式：账号导入共享池后，用户在该账号上输入独享兑换码即可激活独享；独享期间只有号主本人能通过自己的私有分组使用该账号。
- **兑换码自带全部规格**：管理员生成独享兑换码时直接设置平台、订阅档次（可多选或不限）、激活次数（1–1000）、有效天数、是否含代理，以及可选的兑换截止时间。不再单独维护套餐。
- **兑换码是一份有整体有效期的名额包**：首次激活时开始计时，服务到期时间 = 首次激活时间 + 有效天数；之后用该码激活的账号都在同一时间到期。到期后所有账号的独享与未用次数一并作废。
- **不允许提前续费**：账号独享有效期内不能再次兑换；到期后只能用另一个未到期的兑换码重新激活。同一兑换码对同一上游账号只能有一次未被撤销的激活（删号重导也不能复用），被管理员撤销的那次不算。
- 管理员可控制兑换码：生成后可单码或批量调整次数上限、作废剩余次数、清空绑定用户，并查看激活明细；未启用的码还可修改档次、天数与是否含代理。所有调整记审计。
- 独享与共享**互斥**：独享有效期内共享开关不可开启，账号不进入任何共享分组、不产生共享收益；已开启共享的账号必须先关闭共享才能激活。独享到期后账号回到闲置。
- **管理员撤销激活**：激活搞错时，只有管理员可以撤销某一次激活。撤销后账号立即失去独享、回到闲置；这一次数作废不退回；账号可以用任意有效兑换码重新激活，包括原来那个码（再消耗一次）。撤销需要填写原因，并记审计。
- 独享权益**绑定账号**：用户不能解绑、转移或迁移；账号删除即作废，剩余时间不退不转；删除后可以重新导入，但必须用另一个兑换码激活。上游封号不做补偿或顺延。
- **同一上游账号不能同时存在**：导入与编辑凭证时按上游稳定身份（而非 token）全局去重；独享账号编辑凭证时，新凭证的上游身份必须与激活时一致。系统识别不出上游身份的账号，由用户联系管理员，管理员在后台手动设置身份后才能激活。
- 按「用户 × 平台」自动创建私有分组，并自动加入号主的可用分组；分组受保护，不能被删除、改为共享池、设置回退分组、授权给他人或绑定他人账号。
- **分组区分「平台分组」与「私有分组」**：管理端分组页面分两个标签页，其它选择分组的地方默认只列平台分组；用户创建 API Key 时分两组显示。
- 私有分组请求**不扣费**：跳过余额与平台额度前置检查，扣费金额为 0，只记录用量日志，不产生共享收益。
- **并发沿用用户管理里的用户并发**：私有分组请求与其它请求一样占用该用户原有的并发额度，不新增上限、不另开计数；私有分组选中的账号不获取账号级并发槽位。
- 私有号全部不可用时返回标准「No available accounts」，不回退到任何其它分组。
- 代理：兑换码含代理时，用它激活的账号在独享期内可使用平台随机代理池；不含代理时必须配置用户自带的固定代理，否则拒绝调度，不直连。

## Capabilities

### New Capabilities
- `exclusive-account-activation`：独享兑换码（规格、次数与整体有效期）、账号激活、上游身份去重与管理员手动设置、删除作废、与共享互斥。
- `exclusive-private-dispatch`：私有分组自动创建、保护与分类展示、运行时准入、免计费、并发、代理约束、到期处理。

### Modified Capabilities
<!-- openspec/specs 目前为空，没有既有能力需要修改。 -->

## Impact

- **数据库**：新增迁移 `261_exclusive_hosting.sql`，内容如下。迁移只提交源码，是否执行需另行确认。
  - 新表：`exclusive_account_grants`、`exclusive_grant_events`、`exclusive_code_adjustments`、`exclusive_user_groups`、`shared_pool_identity_audits`
  - `groups` 新增 `exclusive_owner_user_id`
  - `redeem_codes` 新增 `exclusive_platform`、`exclusive_tiers`、`exclusive_with_proxy`、`max_activations`、`used_activations`、`bound_user_id`、`service_started_at`、`service_expires_at`，有效天数复用已有的 `validity_days`
  - `shared_pool_accounts` 新增 `upstream_identity`、`upstream_identity_source`
  - `usage_logs` 新增 `billing_mode`
- **后端**：
  - 共享池：服务（创建、更新、开关、删除）与管理员身份设置
  - 兑换服务
  - 调度：可调度判定（`SharedPoolSharingAllowed`）与准入（`sharedPoolAdmissionLatest`）
  - 计费：计费资格（`CheckBillingEligibility`）与计费命令（`buildUsageBillingCommand`）
  - 网关各 handler 的账号并发调用点
  - 代理选择
  - 分组：管理保护与分类查询
  - 新增到期 worker
- **接口**：
  - 用户端新增：`POST /shared-pool/accounts/:id/exclusive/redeem`、`GET /shared-pool/exclusive/codes/:code/preview`、`GET /shared-pool/exclusive/groups`
  - 管理端兑换码：生成接口支持 `exclusive_account` 类型；新增 `PATCH /admin/redeem-codes/:id/exclusive`、`POST /admin/redeem-codes/batch-activations`、`GET /admin/redeem-codes/:id/activations`
  - 管理端身份设置：新增 `PUT /admin/shared-pool/accounts/:id/upstream-identity`
  - 管理端撤销：新增 `POST /admin/exclusive/activations/:event_id/revoke`
  - 管理端分组：列表支持 `kind=platform|private` 筛选
- **前端**：
  - 用户共享池页面：账号卡片独享状态、兑换预览与激活弹窗、互斥开关、身份缺失提示
  - 管理端兑换码页面：独享规格设置、次数管理、激活明细与撤销
  - 管理端共享池账号：手动设置上游身份
  - 管理端分组页面：平台分组 / 私有分组两个标签页
  - 创建 API Key 时分组分类显示
  - 中英文 i18n
- **兼容**：已有共享账号、普通兑换码、分组、用户并发的语义不变；新字段均可空或有默认值。
