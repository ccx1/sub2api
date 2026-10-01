## Purpose

规定私有分组的自动创建、保护与分类展示，以及私有分组请求的准入、免计费、并发、代理约束和到期处理，保证独享账号只服务号主本人。

## ADDED Requirements

### Requirement: 按用户与平台自动创建私有分组
用户在某平台首次激活独享时，系统 MUST 在激活事务内创建私有分组（`is_exclusive=true`、`is_shared_pool=false`、`subscription_type=standard`、`exclusive_owner_user_id=用户ID`），并加入该用户的可用分组。同一用户同一平台 MUST 只有一个私有分组，后续激活 MUST 复用。

#### Scenario: 首次激活
- **WHEN** 用户首次在 openai 平台激活独享
- **THEN** 系统 MUST 创建该用户的 openai 私有分组，将账号绑定到该分组，并把分组加入用户可用分组

#### Scenario: 第二个账号激活
- **WHEN** 同一用户在同一平台激活第二个独享账号
- **THEN** 系统 MUST 复用已有私有分组，MUST NOT 新建分组

### Requirement: 私有分组受保护
私有分组 MUST NOT 被删除，MUST NOT 修改 `is_shared_pool`、`is_exclusive`、`subscription_type`、`platform`、`exclusive_owner_user_id`，MUST NOT 设置 fallback 分组，MUST NOT 授权给 owner 以外的用户，MUST NOT 绑定 owner 名下独享账号以外的账号。管理员 MAY 停用分组或修改模型映射等不影响归属的配置。

#### Scenario: 管理员绑定他人账号
- **WHEN** 管理员尝试把其它用户或平台自有的账号绑定到私有分组
- **THEN** 系统 MUST 拒绝

#### Scenario: 管理员把私有分组授权给他人
- **WHEN** 管理员把私有分组加入另一个用户的可用分组
- **THEN** 系统 MUST 拒绝

#### Scenario: 删除私有分组
- **WHEN** 管理员删除私有分组
- **THEN** 系统 MUST 拒绝

### Requirement: 分组区分平台分组与私有分组
系统 MUST 以 `exclusive_owner_user_id` 是否为空区分分组：为空是平台分组，不为空是私有分组。
- **管理端分组页面**：MUST 分为「平台分组」「私有分组」两个标签页，分组列表接口 MUST 支持 `kind=platform|private` 筛选。私有分组页面 MUST 显示所属用户与平台，并 MAY 按用户搜索。
- **管理端其它分组选择处**：MUST 默认只列平台分组，包括账号绑定分组、用户可用分组、兑换码订阅分组与共享池分组配置。
- **用户 API Key 分组下拉**：MUST 分为「平台分组」「我的私有分组」两组显示。

#### Scenario: 管理员查看分组
- **WHEN** 管理员打开分组管理页面
- **THEN** 平台分组与私有分组 MUST 分别在两个标签页中显示，平台分组页 MUST NOT 出现私有分组

#### Scenario: 管理员为账号选择分组
- **WHEN** 管理员在账号编辑中选择绑定分组
- **THEN** 可选项 MUST 只包含平台分组

#### Scenario: 用户创建 API Key
- **WHEN** 已激活独享的用户创建 API Key
- **THEN** 分组下拉 MUST 分组显示平台分组与「独享 · 平台」私有分组，其他用户的私有分组 MUST NOT 出现

### Requirement: 私有分组准入只放行号主本人的有效独享账号
请求落在私有分组时，系统 MUST 在抢到槽位后的准入阶段实时校验：请求用户等于分组 owner；账号 owner 等于该用户；账号存在未撤销、`expires_at > now` 的独享权益；账号当前上游身份等于权益记录的身份；账号可调度且未被管理员停用；代理约束满足。任一不满足 MUST 否决该账号。

#### Scenario: 撤销后缓存尚未失效
- **WHEN** 管理员已撤销激活，但某个实例的本地缓存仍认为权益有效
- **THEN** 该实例 MUST 在收到撤销通知后立即失效缓存，且最长延迟 MUST NOT 超过缓存有效期

#### Scenario: 到期后 worker 尚未执行
- **WHEN** 账号独享已到期但到期 worker 尚未解绑
- **THEN** 准入 MUST 否决该账号

#### Scenario: 其它用户拿到分组权限
- **WHEN** 非 owner 用户的 API Key 请求落在该私有分组（例如数据异常）
- **THEN** 准入 MUST 否决全部账号

#### Scenario: 独享账号出现在非私有分组
- **WHEN** 调度器在非私有分组选中一个带有效独享权益的账号
- **THEN** 准入 MUST 否决该账号

### Requirement: 私有号不可用时返回无可用账号
私有分组内所有账号都被否决或不可调度时，系统 MUST 返回 503 与标准 `No available accounts` 错误，MUST NOT 回退到任何其它分组或共享池。

#### Scenario: 唯一私有号被上游限流
- **WHEN** 用户私有分组内唯一的账号处于限流窗口
- **THEN** 请求 MUST 返回 503 `No available accounts`，用户余额 MUST 不变

### Requirement: 私有分组请求不扣费
请求用户等于私有分组 owner 时，系统 MUST 跳过余额、平台额度与订阅的前置检查；扣费命令中的余额、订阅与账号额度扣减 MUST 为 0；MUST NOT 产生共享收益。用量日志 MUST 照常写入，`actual_cost` MUST 为 0，并标记 `billing_mode='exclusive'`。API Key 自身设置的额度与限流 MUST 继续生效。

#### Scenario: 余额为 0 的用户使用私有分组
- **WHEN** 余额为 0 的用户通过私有分组 API Key 发起请求
- **THEN** 请求 MUST 不因余额不足被拒绝

#### Scenario: 请求完成后结算
- **WHEN** 私有分组请求成功完成
- **THEN** 用户余额 MUST 不变，`shared_pool_earnings` MUST 无新增记录，用量日志 MUST 存在且 `actual_cost=0`

#### Scenario: 管理员修改私有分组倍率
- **WHEN** 管理员把私有分组倍率改为 2
- **THEN** 私有请求 MUST 仍不扣费

### Requirement: 私有分组请求沿用用户原有并发
请求用户等于私有分组 owner 时，系统 MUST 按用户管理中该用户已配置的并发值获取用户级并发槽位，与该用户其它分组的请求共用同一计数，排队、超时与队列满的行为 MUST 与现有一致。系统 MUST NOT 为私有分组新增并发上限或独立计数。私有分组选中的账号 MUST NOT 获取账号级并发槽位。

#### Scenario: 用户并发为 5
- **WHEN** 用户在用户管理中并发为 5，同时通过私有分组发起 8 个请求
- **THEN** 最多 5 个请求 MUST 同时执行，其余 MUST 按现有规则排队或被拒绝，且 MUST NOT 扣费

#### Scenario: 用户并发为 300
- **WHEN** 用户并发为 300，同时通过私有分组发起 200 个请求
- **THEN** 200 个请求 MUST 都不因用户并发限制而排队

#### Scenario: 私有与平台分组共用
- **WHEN** 用户同时通过平台分组与私有分组发起请求
- **THEN** 两类请求 MUST 共用该用户的同一个并发额度

#### Scenario: 账号设置了并发
- **WHEN** 独享账号自身配置了并发 1，用户通过私有分组同时发起 3 个请求且用户并发足够
- **THEN** 3 个请求 MUST NOT 因账号并发限制而排队

### Requirement: 平台代理按兑换码授权
独享账号权益 `with_proxy=true`（来自激活它的兑换码）时，账号在独享有效期内 MAY 使用平台随机代理池。`with_proxy=false` 时，账号 MUST 使用归属 owner 的自有固定代理；使用随机代理或无代理时，准入 MUST 否决该账号，MUST NOT 直连。

#### Scenario: 未购买代理且未填写代理
- **WHEN** 账号独享有效但权益不含代理，且未配置自有代理
- **THEN** 准入 MUST 否决该账号，账号卡片 MUST 提示填写自有代理

#### Scenario: 用不含代理的新码重新激活
- **WHEN** 账号上一期用含代理的码激活，到期后用不含代理的码重新激活，且账号仍为随机代理模式
- **THEN** 激活 MUST 成功并返回警告，准入 MUST 否决该账号直到用户填写自有代理

#### Scenario: 未购买代理时清空代理地址
- **WHEN** 用户对 `with_proxy=false` 的独享账号清空代理地址
- **THEN** 系统 MUST 返回 `EXCLUSIVE_PROXY_NOT_PURCHASED`，MUST NOT 自动切换为平台随机代理

### Requirement: 到期收尾
到期 worker MUST 定期把已到期且未收尾的独享账号从私有分组解绑、清除调度镜像并通知调度器，并把 `service_expires_at` 已过的兑换码状态置为 expired。私有分组 MUST 保留，用户已有 API Key MUST 不失效。worker 延迟 MUST NOT 导致超期使用（由准入实时校验保证）。

#### Scenario: 同一兑换码的账号同时收尾
- **WHEN** 一个兑换码的 `service_expires_at` 到达，它激活的账号 A、B 都未删除
- **THEN** worker MUST 把 A、B 都从私有分组解绑，兑换码状态 MUST 变为 expired

#### Scenario: 撤销后私有分组保留
- **WHEN** 管理员撤销用户在某平台唯一的独享账号
- **THEN** 私有分组 MUST 保留，用户已有 API Key MUST 不失效，用新码重新激活后 MUST 可直接使用

#### Scenario: 到期后用新码重新激活
- **WHEN** 账号到期被解绑后，用户用另一个有效兑换码重新激活
- **THEN** 账号 MUST 重新绑定到原私有分组，已有 API Key MUST 无需修改即可继续使用

#### Scenario: 到期提醒
- **WHEN** 账号独享距离到期不足 3 天
- **THEN** 用户账号卡片 MUST 显示到期提醒，提示到期后需使用新兑换码重新激活
