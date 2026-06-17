# Sub2API 用户使用手册

这是一份面向普通用户的单页版说明，适合放到站内“使用说明”页面。更细的部署、后台初始化、支付和运维内容见同目录下的分章节文档。

## 使用前准备

开始接入前，请确认三件事：

1. 已经注册并登录 Sub2API。
2. 账户有可用余额、订阅、兑换额度，或管理员已经给你分配了可用分组。
3. 管理员已经配置好上游账号、分组、渠道和模型。

如果“可用渠道”页面没有任何内容，通常说明当前账户没有可访问分组，或管理员尚未启用对应渠道。

## 1. 创建 API Key

进入“API 密钥”页面，点击“创建密钥”。

建议填写一个能区分用途的名称，例如：

- `codex-local`
- `claude-code`
- `gemini-cli`
- `server-job`

创建后请保存生成的 `sk-...` 密钥。密钥用于客户端调用，请不要公开到仓库、截图或聊天记录中。

如果密钥还没有分配分组，需要先在密钥列表中设置分组，再查看“使用密钥”弹窗里的客户端配置。

## 2. 查看可用渠道和模型

进入“可用渠道”页面，可以查看当前账户能使用的渠道、模型和价格。

常见判断方式：

- Claude Code 通常使用 Anthropic/Claude 兼容入口。
- Codex CLI 通常使用 OpenAI Responses 兼容入口。
- Gemini CLI 通常使用 Gemini v1beta 兼容入口。
- 具体模型名以页面显示和管理员配置为准。

如果请求报模型不存在，先确认模型名称是否在“可用渠道”里出现，再检查客户端配置里的 Base URL 和 API Key。

## 3. 配置客户端

客户端接入只需要两类核心信息：

- Base URL：你的 Sub2API 站点地址，例如 `https://api.example.com`
- API Key：你在“API 密钥”页面创建的 `sk-...`

### Claude Code

Linux/macOS：

```bash
export ANTHROPIC_BASE_URL="https://api.example.com"
export ANTHROPIC_AUTH_TOKEN="sk-xxxx"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
```

PowerShell：

```powershell
$env:ANTHROPIC_BASE_URL="https://api.example.com"
$env:ANTHROPIC_AUTH_TOKEN="sk-xxxx"
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"
```

### Codex CLI

“API 密钥”页面的“使用密钥”弹窗会生成 Codex CLI 所需的 `config.toml` 和 `auth.json`。以页面生成内容为准。

核心配置形态如下：

```toml
[model_providers.sub2api]
name = "sub2api"
base_url = "https://api.example.com"
wire_api = "responses"
requires_openai_auth = true
```

`auth.json` 中配置：

```json
{
  "OPENAI_API_KEY": "sk-xxxx"
}
```

### Gemini CLI

Linux/macOS：

```bash
export GOOGLE_GEMINI_BASE_URL="https://api.example.com"
export GEMINI_API_KEY="sk-xxxx"
export GEMINI_MODEL="gemini-2.0-flash"
```

PowerShell：

```powershell
$env:GOOGLE_GEMINI_BASE_URL="https://api.example.com"
$env:GEMINI_API_KEY="sk-xxxx"
$env:GEMINI_MODEL="gemini-2.0-flash"
```

如客户端要求填写 Gemini API 根路径，可使用：

```text
https://api.example.com/v1beta
```

### curl 调用

OpenAI Chat Completions：

```bash
curl "https://api.example.com/v1/chat/completions" \
  -H "Authorization: Bearer sk-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.4",
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'
```

OpenAI Responses：

```bash
curl "https://api.example.com/v1/responses" \
  -H "Authorization: Bearer sk-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.4",
    "input": "hello"
  }'
```

Anthropic Messages：

```bash
curl "https://api.example.com/v1/messages" \
  -H "Authorization: Bearer sk-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'
```

Gemini：

```bash
curl "https://api.example.com/v1beta/models/gemini-2.0-flash:generateContent" \
  -H "x-goog-api-key: sk-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          {"text": "hello"}
        ]
      }
    ]
  }'
```

## 4. 查看用量、充值和订单

- “仪表盘”展示近期请求、费用和用量趋势。
- “使用记录”展示请求明细、模型、Token、费用和端点。
- “充值/订阅”用于购买可用套餐或充值，前提是管理员已启用支付。
- “我的订单”用于查看支付订单状态，前提是管理员已启用支付。
- “兑换”用于输入兑换码，前提是管理员已发放兑换码。

## 5. 常见问题

### Key 无法使用

先检查密钥是否过期、停用、额度耗尽，或没有分配可访问分组。

### 页面看不到可用渠道

通常是当前账户没有可访问分组，或管理员关闭了“可用渠道”入口。请联系管理员确认分组和渠道配置。

### 客户端提示 401

通常是 API Key 不正确、复制时多了空格，或客户端没有按要求放到请求头里。

### 客户端提示模型不存在

以“可用渠道”页面展示的模型名为准，确认客户端请求里的 `model` 字段是否一致。

### Codex CLI 代理后异常

如果前面有 Nginx，且 Codex CLI 请求包含带下划线的请求头，需要在 Nginx `http` 块启用：

```nginx
underscores_in_headers on;
```

