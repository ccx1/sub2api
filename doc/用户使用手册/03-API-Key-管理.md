# API Key 管理

API Key 是用户调用网关的凭证。普通用户在“API 密钥”页面创建和管理自己的 Key，管理员也可以在后台查看和调整用户 Key。

## 创建 API Key

用户登录后进入“API 密钥”页面，点击创建，常见字段包括：

- 名称：用于区分用途，例如 `claude-code-work`。
- 分组：决定这个 Key 走哪个平台和账号池。
- 自定义密钥：可选，至少 16 个字符，只允许字母、数字、下划线和连字符。
- IP 白名单：设置后仅允许这些 IP 或 CIDR 调用。
- IP 黑名单：禁止指定 IP 或 CIDR 调用。
- 额度限制：设置该 Key 最多可消费的金额，`0` 表示不限制。
- 有效期：设置 Key 过期天数。
- 5 小时、1 天、7 天消费限额：用于限制指定时间窗口内的消费额。

创建后请及时复制完整 Key。对外使用时统一通过请求头传递：

```http
Authorization: Bearer sk-xxxx
```

## 分组必须匹配用途

同一个 Key 的能力取决于绑定的分组平台：

| 分组平台 | 常用接入 |
| --- | --- |
| Anthropic | Claude Messages、Claude Code、Anthropic 兼容客户端 |
| OpenAI | OpenAI Responses、Chat Completions、Codex CLI、OpenAI 兼容客户端 |
| Gemini | Gemini v1beta、Gemini CLI、Google 兼容客户端 |
| Antigravity | Antigravity 专用 Claude/Gemini 端点 |

如果 Key 没有分组，用户侧“使用密钥”弹窗会提示先分配分组。

## 查看和复制配置

在 API Key 列表中点击“使用密钥”，前端会根据 Key 所属平台生成对应配置，包括：

- Claude Code 环境变量
- Gemini CLI 环境变量
- Codex CLI `config.toml` 和 `auth.json`
- Codex CLI WebSocket 配置
- OpenCode 示例配置

配置内容由前端当前实现生成，通常会自动带上站点 API 基础地址和当前 Key。

## 查询用量

用户可在前端用量页面查看请求记录和统计，也可以用 API Key 直接查询网关用量端点：

```bash
curl "$SUB2API_BASE_URL/v1/usage" \
  -H "Authorization: Bearer sk-xxxx"
```

这里的 `$SUB2API_BASE_URL` 是站点根地址，例如：

```text
https://api.example.com
```

## 常见管理动作

- 禁用 Key：临时阻止继续调用。
- 删除 Key：永久移除凭证。
- 修改分组：切换 Key 的可用平台和账号池。
- 重置额度用量：清空已用额度统计。
- 重置速率限制用量：清空窗口限额统计。

涉及生产 Key 时，建议先新建替代 Key 并完成客户端切换，再删除旧 Key。

