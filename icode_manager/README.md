# iCode功能增强

这是放在 sub2api 仓库内的独立轻量工具，用 Python + SQLite 实现红包领取，并把返利关系绑定工具合并到同一个管理台。

- 管理员在本地可视化管理台创建活动、导入兑换码、关闭活动、生成静态领取链接。
- 管理台名称为 `iCode功能增强`，包含 `红包管理` 和 `返利管理` 两个 tab。
- 用户只访问生成后的静态活动页，输入 iCode 账号邮箱后领取兑换码。
- 后端会调用 iCode/sub2api 管理员用户接口校验邮箱是否存在。
- 返利管理复用本工具的 `security.admin_token`，不再单独维护返利工具访问令牌。
- 同一活动内同一邮箱只能领取一次；重复领取会展示原兑换码。
- 活动支持开始时间、结束时间和手动关闭；结束后页面直接显示“活动已结束”。
- 活动可配置领取后跳转地址；用户领取成功后可点击“立即使用”跳转。
- 支持多个活动 ID，例如 `spring2026`、`summer2026`，每个活动生成独立链接。

## 产品边界

V1 不改 sub2api 主后端和主前端，也不把兑换码写回 sub2api 的兑换记录。兑换码、活动、领取记录都在本目录自己的 SQLite 数据库中。

返利管理会直接连接 sub2api PostgreSQL，读写 `users` 和 `user_affiliates` 表，用于预览和执行邀请关系绑定。执行绑定属于数据库写入操作，需要在页面预览后勾选确认。

V1 不做邮箱验证码，因此知道某个已存在邮箱的人可以查看该邮箱在该活动下领到的兑换码。高价值活动建议下一版接入邮箱验证码或 iCode 登录态。

## 运行环境

- Python 3.6.8 及以上。
- 红包领取仅依赖标准库和 SQLite。
- 返利管理需要 PostgreSQL 驱动：`psycopg[binary]`。
- CentOS 7 自带的旧 SQLite 也可运行，活动保存逻辑不依赖新版 SQLite UPSERT 语法。

## 目录说明

- `app.py`：命令入口。
- `cli.py`：服务启动和少量命令兜底。
- `store.py`：SQLite 活动、码池、领取记录。
- `affiliate_admin.py`：返利关系预览和绑定，直接操作 sub2api PostgreSQL。
- `sub2api_auth.py`：iCode/sub2api 管理员鉴权，支持 `login`、`token`、`api_key`。
- `verifier.py`：调用管理员用户列表接口校验邮箱。
- `static/index.html`：用户领取页模板。
- `static/manager.html`：本地可视化管理台，不会发布到 `public/`。
- `publisher.py`：生成静态活动页。
- `config.example.json`：配置示例，复制成 `config.json` 后使用。

## 1. 准备配置

```powershell
Set-Location .\redeem_claim
Copy-Item .\config.example.json .\config.json
Copy-Item .\sub2api-config.example.json .\sub2api-config.json
Copy-Item .\lianjia-config.example.json .\lianjia-config.json
```

重点修改这些字段：

```json
{
  "publisher": {
    "output_dir": "public",
    "public_base_url": "https://example.com/redeem",
    "api_base_url": ""
  },
  "security": {
    "admin_token": "change-me-admin-token",
    "allowed_origins": []
  },
  "sub2api_database": {
    "host": "127.0.0.1",
    "port": 5432,
    "dbname": "sub2api",
    "user": "postgres",
    "password": "change-this-password",
    "sslmode": "prefer",
    "connect_timeout": 10
  },
  "sub2api": {
    "base_url": "http://127.0.0.1:8081/api/v1",
    "verify_mode": "admin_users_api",
    "auth": {
      "mode": "login",
      "email": "admin@example.com",
      "password": "admin-password"
    }
  }
}
```

说明：

- `public_base_url` 是最终给用户的领取页前缀，例如 `https://example.com/redeem`。
- 如果 Nginx 同域代理 `/api/` 到本服务，`api_base_url` 留空即可。
- 如果 API 和静态页不在同一个域名，`api_base_url` 填领取 API 地址，并把静态页域名加入 `security.allowed_origins`。
- `sub2api_database` 用于返利管理直接连接 sub2api PostgreSQL。若不配置，会兜底读取仓库 `backend/config.yaml` 的 `database` 配置。
- `config.json` 已被 `.gitignore` 忽略，不要提交真实密码、JWT、API Key。
- 本地只调试页面时，可临时把 `verify_mode` 改成 `disabled`。

## 2. 启动本地管理台和领取 API

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\app.py --config .\config.json serve
```

打开本地管理台：

```text
http://127.0.0.1:8099/manager
```

## 3. 可视化操作流程

管理台地址：

```text
http://127.0.0.1:8099/manager
```

### 红包管理

1. 在管理台填写 `管理口令`，对应 `config.json` 里的 `security.admin_token`。
2. 填写活动 ID、活动标题、活动说明、领取后跳转地址，并通过活动时间区间选择器选择开始和结束时间。
3. 普通红包在 `普通红包兑换码列表` 中一行一个粘贴兑换码。
4. 档次红包先配置档次，再在每个档次卡片里的兑换码输入框导入该档次对应的兑换码。
5. 点击 `保存并生成链接` 或 `保存、导入并生成链接` 后，页面会显示用户领取链接和生成目录。
6. 后续可继续追加普通红包码池，或在指定档次下追加档次码池；也可点击 `结束活动` 关闭活动并重新生成静态页。

### 返利管理

1. 切换到 `返利管理` tab，继续使用同一个 `管理口令`。
2. 输入邀请人邮箱、被邀请人邮箱，必要时勾选 `允许改绑已有邀请人`。
3. 点击 `预览关系`，确认用户、当前邀请关系、待执行计划和邀请人当前名单。
4. 只有预览结果可执行时，勾选 `确认执行` 后才可以点击 `执行绑定`。

### 商品上架

1. 切换到 `商品上架` tab，继续使用同一个管理密码。
2. 商品配置复用当前目录的 `sub2api-config.json` 和 `lianjia-config.json`。
3. 链动 `Cookie` 和 `Merchant-Token` 可直接在页面保存；留空保存时会保留本地已有值，页面只显示脱敏状态。
4. `添加商品` 会同时写入 sub2api 兑换码生成配置和链动上架配置；已有商品标识会按更新处理。
5. `手动生成上架` 会按所选商品调用 sub2api 生成兑换码，并可立即上架到链动商品。
6. `定时补库存` 是内置定时器，可启停、设置执行间隔、库存阈值、最小补货缺口和固定补货数量；启用后可替代外部 crontab。

### 导入设置

1. 切换到 `导入设置` tab，配置账号名前缀、备注、并发数、优先级、去重方式和输出格式。
2. 选择或拖入多个 Codex/OpenAI OAuth JSON 文件，页面会在浏览器本地转换为 sub2api 导入 JSON。
3. 可复制或下载生成结果；OAuth token 内容不会提交给后端保存，后端只保存默认导入设置。

生成后的公开静态文件在：

```text
public/<activity_id>/index.html
public/static/styles.css
```

管理台文件不会生成到 `public/`，不要在 Nginx 中暴露 `/manager`。

## 4. Nginx 示例

静态页由 Nginx 直接服务，领取 API 代理到 Python 服务：

```nginx
location /redeem/ {
    alias F:/Codes/GoCodes/sub2api/redeem_claim/public/;
    index index.html;
    try_files $uri $uri/ =404;
}

location /api/ {
    proxy_pass http://127.0.0.1:8099/api/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

用户访问示例：

```text
https://example.com/redeem/spring2026/
https://example.com/redeem/summer2026/
```

## 5. 命令兜底

推荐使用可视化管理台。下面命令仅用于自动化或排障。

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\app.py --config .\config.json publish-activity `
  --slug spring2026 `
  --admin-token "change-me-admin-token"
```

## 6. API

- `GET /api/activities/{activity_id}`：活动信息、领取后跳转地址、剩余码量、是否可领取。
- `POST /api/activities/{activity_id}/claim`：领取兑换码，请求体 `{"email":"user@example.com"}`。
- `GET /api/activities/{activity_id}/claims?email=user@example.com`：查看该邮箱在该活动下的领取记录。
- `GET /api/manager/activities`：管理台活动列表，需要 `X-Admin-Token`。
- `POST /api/manager/activities`：保存红包活动配置，需要 `X-Admin-Token`。
- `POST /api/manager/activities/{activity_id}/codes`：导入兑换码，需要 `X-Admin-Token`；普通红包不传 `tier_key`，档次红包必须传对应档次的 `tier_key`。
- `POST /api/manager/affiliate/preview`：预览返利关系绑定，需要 `X-Admin-Token`。
- `POST /api/manager/affiliate/execute`：执行返利关系绑定，需要 `X-Admin-Token` 且请求体 `confirm=true`。
- `GET /api/manager/listing/status`：读取商品上架配置状态、商品列表和定时器状态，需要 `X-Admin-Token`。
- `POST /api/manager/listing/auth`：保存链动 `Cookie` 和 `Merchant-Token`，需要 `X-Admin-Token`。
- `POST /api/manager/listing/products`：添加或更新商品配置，需要 `X-Admin-Token`。
- `POST /api/manager/listing/generate-upload`：生成兑换码并可立即上架到链动商品，需要 `X-Admin-Token`。
- `POST /api/manager/listing/scheduler`：保存内置补库存定时器配置，需要 `X-Admin-Token`。
- `POST /api/manager/listing/restock-once`：按当前页面参数立即执行一次补库存，需要 `X-Admin-Token`。
- `GET /api/manager/import-settings`：读取 Codex JSON 转 sub2api 导入的默认设置，需要 `X-Admin-Token`。
- `POST /api/manager/import-settings`：保存导入默认设置，需要 `X-Admin-Token`。

## 7. 红包类型与档次红包

管理台支持两种红包类型：

- 普通红包：保留原有逻辑，用户校验通过后从普通红包兑换码列表随机发放一个兑换码。
- 档次红包：管理员配置多个档次，每个档次包含 `档次标识`、`VIP 名称`、`门槛金额`、`动画组合` 和独立兑换码池。每个档次的兑换码在对应档次卡片里单独导入；用户领取时会根据 sub2api 充值记录计算有效充值金额，匹配最高可领取档次，并从该档次码池随机发放一个兑换码。

档次红包的门槛金额只用于服务端资格判断，不会返回给用户领取页展示。`成功提示模板` 仅在档次红包中显示和保存，普通红包不配置该模板。档次红包领取成功文案由管理台的 `成功提示模板` 配置，默认值为：

```text
欢迎您，尊贵的{vip等级}用户，下面是您本次的兑换码。
```

模板支持这些占位符：`{vip等级}`、`{vip_level}`、`{vipLevel}`、`{level}`、`{tier}`、`{code}`。

档次动画支持多选，当前内置：

- `fireworks`：烟花
- `confetti`：彩带
- `salute`：礼炮
- `sparkles`：闪光

多选动画会在领取成功时同时播放，不会串行等待。

档次红包依赖 sub2api 管理员接口：

```text
GET /api/v1/admin/users?search={email}
GET /api/v1/admin/users/{id}/balance-history?page=1&page_size=100&timezone=Asia%2FShanghai&type=balance
GET /api/v1/admin/users/{id}/balance-history?page=1&page_size=100&timezone=Asia%2FShanghai&type=admin_balance
```

资格金额计算规则：

1. 优先使用 `balance-history` 返回的 `total_recharged`。
2. 分页读取 `balance` 和 `admin_balance` 记录。
3. 如果用户在 sub2api 里兑换过的兑换码命中本地所有红包码池，则按该记录 `value` 从资格金额中核减。
4. 核减后金额小于 0 时按 0 处理。

相关配置：

```json
{
  "sub2api": {
    "timezone": "Asia/Shanghai",
    "balance_history_page_size": 100,
    "balance_history_max_pages": 50
  }
}
```
