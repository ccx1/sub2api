## Purpose

规定独享兑换码（规格、次数与整体有效期）、账号激活、上游身份去重、删除作废，以及独享与共享互斥的行为，使独享权益始终只绑定在激活时的那个账号上，且同一码激活的所有账号同时到期。

## ADDED Requirements

### Requirement: 管理员生成独享兑换码时设置规格
系统 SHALL 支持生成类型为 `exclusive_account` 的兑换码。生成请求 MUST 指定平台、激活次数 `max_activations`（1–1000）、有效天数 `validity_days`（≥1）、是否含代理 `with_proxy`；MAY 指定订阅档次 `tiers`（可多选，不填表示不限）与兑换截止时间 `expires_at`。档次 MUST 为该平台支持的共享池档位，否则 MUST 返回 400。同一批生成的码 MUST 规格相同。系统 MUST NOT 依赖独立的套餐实体。

#### Scenario: 按规格生成
- **WHEN** 管理员生成 openai 平台、档次 [plus, pro]、5 次、30 天、含代理的兑换码
- **THEN** 每个码 MUST 记录上述规格，`used_activations` MUST 为 0，`service_started_at` 与 `service_expires_at` MUST 为空

#### Scenario: 不限档次
- **WHEN** 管理员生成时不选订阅档次
- **THEN** 该码 MUST 可激活该平台任意档次的账号

#### Scenario: 次数或天数越界
- **WHEN** 生成请求的 `max_activations` 为 0 或超过 1000，或 `validity_days` 小于 1
- **THEN** 系统 MUST 返回 400

#### Scenario: 档次与平台不符
- **WHEN** 管理员为 anthropic 平台选择 openai 才有的档次
- **THEN** 系统 MUST 返回 400

### Requirement: 兑换码按首次激活统一计时
兑换码首次成功激活时，系统 MUST 写入 `service_started_at = now` 与 `service_expires_at = now + validity_days`。此后用该码激活的每个账号，独享到期时间 MUST 等于该码的 `service_expires_at`，与账号激活时间无关。`service_expires_at` 到达后，该码 MUST 作废：剩余次数 MUST NOT 再被使用，用该码激活的所有账号的独享 MUST 同时失效。未启用的码 MUST 受 `expires_at`（兑换截止时间，可空）约束，已启用的码 MUST 只受 `service_expires_at` 约束。

#### Scenario: 同码不同时间激活的账号同时到期
- **GIVEN** 一个 5 次、30 天的兑换码
- **WHEN** 用户 6/1 用它激活账号 A，6/20 激活账号 B
- **THEN** 该码 `service_expires_at` MUST 为 7/1，A 和 B 的 `expires_at` MUST 均为 7/1

#### Scenario: 整体到期后全部作废
- **WHEN** 当前时间超过上例的 7/1
- **THEN** A、B 的独享 MUST 同时失效，剩余 3 次 MUST NOT 可用，兑换码状态 MUST 为 expired

#### Scenario: 到期后继续使用剩余次数
- **WHEN** 用户在 `service_expires_at` 之后提交该码激活账号 C
- **THEN** 系统 MUST 返回 `EXCLUSIVE_CODE_EXPIRED`，`used_activations` MUST 不变

#### Scenario: 未启用码超过兑换截止时间
- **WHEN** 一个从未激活过的码 `expires_at` 已过，用户提交该码
- **THEN** 系统 MUST 拒绝，`service_started_at` MUST 保持为空

#### Scenario: 首次激活失败不启动计时
- **WHEN** 首次提交因账号校验失败而被拒绝
- **THEN** `service_started_at` 与 `service_expires_at` MUST 保持为空

#### Scenario: 预览显示剩余天数
- **WHEN** 用户预览一个已启用、离到期还剩 11 天的码
- **THEN** 系统 MUST 返回 `service_expires_at` 与剩余天数 11

### Requirement: 管理员可控制兑换码次数与规格
每次激活 MUST 消耗兑换码一次，并对应一个账号。剩余次数 MUST 等于 `max_activations - used_activations`。
- **次数**：管理员 MUST 能对单个或多个兑换码设置上限、增减上限或作废剩余次数。新上限 MUST NOT 低于 `used_activations`，也 MUST NOT 超过 1000。
- **规格**：档次、有效天数、是否含代理 MUST 只在码未启用（unused）时可修改，码启用后 MUST NOT 可修改。
- **停用**：管理员 MAY 停用兑换码。停用后 MUST NOT 可再激活，已激活账号 MUST 不受影响。
- **已到期**：已 expired 的码 MUST NOT 可调整。
- **不影响已激活账号**：任何调整 MUST NOT 影响已激活账号的到期时间，也 MUST NOT 修改 `service_expires_at`。
- **审计**：每次调整 MUST 记录操作人、调整前后值与原因。
- **状态**：兑换码状态 MUST 按以下优先级联动：
  1. 已停用：disabled
  2. `service_expires_at` 已过：expired
  3. 剩余为 0：used
  4. 已启用：active
  5. 未启用：unused

#### Scenario: 未启用时修改规格
- **WHEN** 管理员把一个 unused 码的有效天数从 30 改为 60
- **THEN** 修改 MUST 成功，该码首次激活后 MUST 按 60 天计算

#### Scenario: 启用后修改规格
- **WHEN** 管理员修改一个 active 码的档次、有效天数或是否含代理
- **THEN** 系统 MUST 返回 400，规格 MUST 不变

#### Scenario: 停用兑换码
- **WHEN** 管理员停用一个 active 码
- **THEN** 该码 MUST 无法再激活账号，已用它激活的账号到期时间 MUST 不变

#### Scenario: 调高上限
- **WHEN** 一个未到期、`max_activations=2, used_activations=2` 的码被调整为 5
- **THEN** 该码状态 MUST 恢复为 active，剩余次数 MUST 为 3，`service_expires_at` MUST 不变

#### Scenario: 调低到已用次数以下
- **WHEN** 管理员把 `used_activations=3` 的码上限设为 2
- **THEN** 系统 MUST 返回 400，上限 MUST 不变

#### Scenario: 作废剩余次数
- **WHEN** 管理员对 `max_activations=5, used_activations=2` 的码执行作废剩余
- **THEN** `max_activations` MUST 变为 2，状态 MUST 为 used，已用该码激活的账号 MUST 继续有效至原到期时间

#### Scenario: 调整已到期的码
- **WHEN** 管理员调整一个 expired 的码
- **THEN** 系统 MUST 返回 400，码 MUST 不被修改

#### Scenario: 批量调整部分失败
- **WHEN** 管理员批量把 3 个码的上限设为 2，其中一个码已用 3 次
- **THEN** 另外两个码 MUST 调整成功，失败的码 MUST 在逐项结果中返回原因且不被修改

#### Scenario: 调整与激活并发
- **WHEN** 管理员作废剩余次数的同时，用户正在使用该码激活
- **THEN** 最终 `used_activations` MUST NOT 超过 `max_activations`

#### Scenario: 查看激活明细
- **WHEN** 管理员查看某个码的激活明细
- **THEN** 系统 MUST 列出每次激活的用户、账号、上游身份摘要、账号档次、激活时间、到期时间、是否含代理与状态（生效、已到期、已撤销、账号已删除）
- **THEN** 已撤销的记录 MUST 显示撤销人、时间与原因，账号已删除的记录 MUST 仍然显示

### Requirement: 多次兑换码首次使用后绑定用户
`max_activations > 1` 的兑换码首次使用时 MUST 绑定使用者，之后 MUST 只能由该用户使用。管理员 MAY 清空绑定，操作 MUST 记入审计；清空绑定 MUST NOT 改变 `service_expires_at`。

#### Scenario: 他人使用已绑定的码
- **WHEN** 一个码已被用户 A 使用过一次，用户 B 提交该码
- **THEN** 系统 MUST 拒绝，`used_activations` MUST 不变

#### Scenario: 管理员清空绑定
- **WHEN** 管理员清空某个未到期码的绑定后，用户 B 提交该码
- **THEN** 若有剩余次数，激活 MUST 成功，该码 MUST 改为绑定用户 B，B 账号的到期时间 MUST 等于该码原有的 `service_expires_at`

### Requirement: 用户激活前可预览兑换码
用户 MUST 能在激活前预览兑换码的平台、允许档次、有效天数、是否含代理、剩余次数与状态；已启用的码 MUST 额外返回 `service_expires_at` 与剩余天数。预览 MUST 受兑换限流保护；绑定给其他用户的码 MUST NOT 返回详情。

#### Scenario: 预览未启用码
- **WHEN** 用户预览一个未绑定、剩余 2 次的未启用码
- **THEN** 系统 MUST 返回规格、剩余次数 2 与“激活后开始计时”，且 MUST NOT 消耗次数或启动计时

### Requirement: 独享兑换码只能在账号上使用
`exclusive_account` 类型的兑换码 MUST 只能通过 `POST /shared-pool/accounts/:id/exclusive/redeem` 使用。通用兑换接口收到该类型时 MUST 返回 400，错误码为 `EXCLUSIVE_CODE_ACCOUNT_REQUIRED`，且 MUST NOT 消耗兑换码、启动计时或计入兑换失败次数。

#### Scenario: 在通用兑换页输入独享码
- **WHEN** 用户在通用兑换接口提交独享兑换码
- **THEN** 系统 MUST 返回 `EXCLUSIVE_CODE_ACCOUNT_REQUIRED`，兑换码 `used_activations` MUST 不变

### Requirement: 激活把独享权益绑定到指定账号
用户在自己的共享池账号上提交有效独享兑换码时，系统 MUST 在单个事务内完成：校验兑换码、校验账号、校验权益、按需启动兑换码计时、写入该账号的独享权益与事件、确保私有分组存在并绑定账号、扣减兑换码次数。权益 MUST 以账号 ID 为键，MUST NOT 可解绑、转移或迁移到其它账号。权益的 `with_proxy` MUST 取自兑换码的是否含代理。任一校验失败时 MUST NOT 消耗次数或启动计时。

#### Scenario: 首次激活
- **WHEN** 用户对未开启共享、身份可识别、平台与档次符合兑换码的账号提交有效兑换码
- **THEN** 账号 MUST 获得 `expires_at = 兑换码 service_expires_at` 的独享权益
- **THEN** 兑换码 `used_activations` MUST 加 1，达到 `max_activations` 时状态 MUST 变为 used

#### Scenario: 平台或档次不符
- **WHEN** 账号平台与兑换码不同，或账号档次不在兑换码的档次范围内
- **THEN** 系统 MUST 返回 400，兑换码 MUST 不被消耗

#### Scenario: 码限定档次但账号档次未知
- **WHEN** 兑换码限定了档次，而系统识别不出账号档次
- **THEN** 系统 MUST 拒绝并提示联系管理员设置档次，兑换码 MUST 不被消耗

#### Scenario: 并发提交
- **WHEN** 同一用户用剩余 1 次的兑换码同时激活两个账号
- **THEN** 最多一个请求 MUST 成功，另一个 MUST 返回兑换码已用完

### Requirement: 不允许提前续费
账号存在未撤销且 `expires_at > now` 的独享权益时，系统 MUST 拒绝任何兑换码，返回 `EXCLUSIVE_ALREADY_ACTIVE`，且 MUST NOT 消耗次数或启动计时。独享到期后，用户 MUST 使用另一个未到期的兑换码重新激活；重新激活 MUST 覆盖该账号的权益行，`expires_at` MUST 等于新码的 `service_expires_at`，历史 MUST 保留在事件表中。同一兑换码对同一 `upstream_identity` MUST 最多只有一次未被撤销的激活，删除账号后重新导入也受此限制；被管理员撤销的激活 MUST NOT 计入此限制。

#### Scenario: 有效期内再次兑换
- **WHEN** 用户对仍在独享有效期内的账号提交任意兑换码
- **THEN** 系统 MUST 返回 `EXCLUSIVE_ALREADY_ACTIVE`，账号到期时间 MUST 不变，兑换码 MUST 不被消耗

#### Scenario: 到期后用新码重新激活
- **WHEN** 账号独享已到期，用户提交另一个未到期、从未用于该上游号的兑换码
- **THEN** 激活 MUST 成功，`expires_at` MUST 等于新码的 `service_expires_at`，账号 MUST 重新绑定私有分组

#### Scenario: 同一码对同一上游号重复使用
- **WHEN** 用户用一个仍有剩余次数、未到期的码，再次激活它曾激活过且那次激活未被撤销的同一上游号（包括删除后重新导入的新账号）
- **THEN** 系统 MUST 返回 `EXCLUSIVE_CODE_USED_FOR_ACCOUNT`，兑换码 MUST 不被消耗

#### Scenario: 同一码激活不同上游号
- **WHEN** 用户用剩余 2 次的码分别激活两个不同上游号的账号
- **THEN** 两次激活 MUST 都成功，两个账号的 `expires_at` MUST 相同

### Requirement: 管理员可撤销激活
系统 MUST 只允许管理员撤销某一次独享激活，用户 MUST NOT 能自行撤销。撤销 MUST 填写原因，并 MUST 只针对账号当前生效且未到期的那次激活。撤销 MUST 在单个事务内完成以下操作：
- 使该账号的独享权益立即失效
- 把账号从私有分组解绑，账号回到闲置
- 把该次激活记录标为已撤销
- 写入审计

撤销后：
- **次数**：`used_activations` MUST NOT 退回，这一次视为作废。
- **原码**：同一兑换码 MUST 可以再次激活同一上游账号，并 MUST 再消耗一次。
- **计时**：兑换码的 `service_started_at` 与 `service_expires_at` MUST 不变。
- **重新激活**：账号 MUST 能用任意有效兑换码重新激活，包括原码。
- **私有分组**：私有分组 MUST 保留。

#### Scenario: 管理员撤销激活错的账号
- **GIVEN** 用户用剩余 3 次的码 X 误激活了账号 A
- **WHEN** 管理员撤销这次激活
- **THEN** A MUST 立即失去独享并回到闲置
- **THEN** 码 X 的 `used_activations` MUST 仍为 1，剩余 2 次 MUST 可用于其它账号，到期时间 MUST 不变

#### Scenario: 撤销后用原码重新激活同一账号
- **WHEN** 撤销后用户再用码 X 激活账号 A
- **THEN** 激活 MUST 成功，码 X 的 `used_activations` MUST 变为 2，剩余 MUST 为 1
- **THEN** A 的 `expires_at` MUST 等于码 X 的 `service_expires_at`

#### Scenario: 再次撤销后再用原码
- **WHEN** 上例中管理员再次撤销 A 的激活，用户第三次用码 X 激活 A
- **THEN** 激活 MUST 成功，码 X MUST 变为 used（3/3）

#### Scenario: 撤销后用新码激活
- **WHEN** 撤销后用户用另一个有效兑换码 Y 激活账号 A
- **THEN** 激活 MUST 成功，A 的 `expires_at` MUST 等于码 Y 的 `service_expires_at`

#### Scenario: 撤销单次码的激活
- **WHEN** 管理员撤销一个只有 1 次的码的激活
- **THEN** 该码 MUST 变为 used 且不可再用；账号 MUST 只能用其它兑换码重新激活，除非管理员先调高该码的次数

#### Scenario: 撤销码的首次激活
- **WHEN** 被撤销的是某个码的第一次激活
- **THEN** 该码 MUST 仍保持已启用状态，`service_expires_at` MUST 不变，MUST NOT 重新计时

#### Scenario: 用户尝试撤销
- **WHEN** 普通用户调用撤销接口
- **THEN** 系统 MUST 返回 403

#### Scenario: 撤销不可撤销的记录
- **WHEN** 管理员撤销一条已到期、已撤销或账号已删除的激活记录
- **THEN** 系统 MUST 返回 `EXCLUSIVE_ACTIVATION_NOT_REVOCABLE`

#### Scenario: 撤销缺少原因
- **WHEN** 管理员撤销时未填写原因
- **THEN** 系统 MUST 返回 400

#### Scenario: 撤销后的请求
- **WHEN** 撤销完成后，用户通过私有分组发起新请求
- **THEN** 准入 MUST 否决该账号；私有分组没有其它可用账号时 MUST 返回 503 `No available accounts`

#### Scenario: 用户查看被撤销的账号
- **WHEN** 用户查看被撤销的账号
- **THEN** 账号卡片 MUST 显示已被管理员撤销、撤销时间与原因

### Requirement: 独享与共享互斥
存在未撤销且未过期独享权益的账号 MUST NOT 开启共享，也 MUST NOT 被分配到任何共享分组。已开启共享的账号 MUST NOT 激活独享。互斥 MUST 在加锁后的仓储层再次校验。

#### Scenario: 独享期间开启共享
- **WHEN** 用户对独享有效期内的账号开启共享
- **THEN** 系统 MUST 返回 `EXCLUSIVE_SHARING_FORBIDDEN`

#### Scenario: 共享中的账号激活独享
- **WHEN** 用户对 `shared_pool_enabled=true` 的账号提交独享兑换码
- **THEN** 系统 MUST 返回 `EXCLUSIVE_SHARING_ACTIVE`，兑换码 MUST 不被消耗

#### Scenario: 管理员给独享账号分配共享分组
- **WHEN** 管理员通过共享池管理接口为独享账号分配共享分组
- **THEN** 系统 MUST 拒绝

#### Scenario: 独享到期后
- **WHEN** 账号独享已到期
- **THEN** 账号 MUST 处于闲置状态，用户 MAY 开启共享或用新兑换码重新激活独享

#### Scenario: 独享被撤销后
- **WHEN** 账号独享被管理员撤销
- **THEN** 账号 MUST 处于闲置状态，用户 MAY 开启共享或用新兑换码重新激活独享

### Requirement: 同一上游账号不能同时存在
系统 MUST 为托管账号计算上游稳定身份 `upstream_identity`，该身份 MUST 不随 token 刷新而变化。未删除的托管账号之间 `upstream_identity` MUST 全局唯一（跨用户）。无法提取身份且管理员未手动设置身份的账号 MUST NOT 激活独享。

#### Scenario: 重复导入同一上游号
- **WHEN** 用户导入的凭证与某个未删除托管账号的上游身份相同（无论是否同一用户、token 是否相同）
- **THEN** 系统 MUST 拒绝导入

#### Scenario: 凭证缺少身份
- **WHEN** 用户对无法提取上游身份的账号提交独享兑换码
- **THEN** 系统 MUST 返回 `EXCLUSIVE_IDENTITY_REQUIRED`，提示用户联系管理员设置，兑换码 MUST 不被消耗

#### Scenario: 独享账号换成其它号的凭证
- **WHEN** 用户编辑有独享权益（含已到期未删除）的账号，提交的新凭证上游身份与权益记录不同
- **THEN** 系统 MUST 返回 `EXCLUSIVE_IDENTITY_MISMATCH`，凭证 MUST 不被更新

#### Scenario: token 刷新或重新授权同一号
- **WHEN** 独享账号的 token 被刷新，或用户用同一上游号重新授权
- **THEN** 独享权益 MUST 保持有效

### Requirement: 管理员可手动设置上游身份
管理员 MUST 能为系统识别不出上游身份的托管账号手动设置身份。
- **计算方式**：管理员 MUST 按平台填写与自动提取相同的字段，手动身份 MUST 用与自动身份相同的函数计算，来源 MUST 记为 admin。
- **唯一性**：手动身份 MUST 满足全局唯一。
- **有效期内锁定**：账号存在有效独享权益时，MUST NOT 修改身份。
- **审计**：每次设置 MUST 记录操作人、修改前后值与原因。
- **用户不可自设**：用户 MUST NOT 能自行设置身份。

#### Scenario: 管理员为缺失身份的账号设置身份
- **WHEN** 管理员为一个身份缺失的账号设置上游标识
- **THEN** 账号 MUST 获得身份，来源 MUST 为 admin，用户随后 MUST 能激活独享

#### Scenario: 手动身份与已有账号重复
- **WHEN** 管理员设置的身份与另一个未删除托管账号相同
- **THEN** 系统 MUST 拒绝，身份 MUST 不变

#### Scenario: 独享期内修改身份
- **WHEN** 管理员修改一个独享有效期内账号的身份
- **THEN** 系统 MUST 拒绝

#### Scenario: 手动身份账号的凭证刷新
- **WHEN** 来源为 admin 的账号刷新凭证后，系统自动提取出的身份与手动值不同
- **THEN** 系统 MUST 拒绝更新凭证并返回 `EXCLUSIVE_IDENTITY_MISMATCH`；提取不出身份时 MUST 保留手动值

#### Scenario: 管理端筛选身份缺失账号
- **WHEN** 管理员在共享池账号列表按身份缺失筛选
- **THEN** 系统 MUST 列出所有 `upstream_identity` 为空的未删除账号

### Requirement: 删除账号使独享作废
删除托管账号时，系统 MUST 在同一事务内撤销其独享权益、解绑私有分组并让出上游身份占位。剩余时长 MUST NOT 退还或转移。重新导入同一上游号 MUST 生成新账号且不带独享权益，并 MUST 用另一个兑换码激活。

#### Scenario: 删除后重新导入
- **WHEN** 用户删除独享账号后重新导入同一上游号
- **THEN** 导入 MUST 成功，新账号 MUST 没有独享权益
- **THEN** 用原兑换码激活 MUST 返回 `EXCLUSIVE_CODE_USED_FOR_ACCOUNT`，用另一个有效兑换码 MUST 能激活

#### Scenario: 对已删除账号兑换
- **WHEN** 请求对已删除账号提交兑换码
- **THEN** 系统 MUST 返回账号不存在，兑换码 MUST 不被消耗

### Requirement: 封号与停用不影响计时
上游封号、凭证失效或管理员停用账号时，系统 MUST NOT 撤销、暂停、顺延或迁移独享权益，兑换码的 `service_expires_at` MUST 不变。

#### Scenario: 上游封号
- **WHEN** 独享账号因上游封号持续不可用
- **THEN** `expires_at` MUST 保持不变，系统 MUST NOT 提供迁移入口
