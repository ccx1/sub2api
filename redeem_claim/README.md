# iCode 兑换码可视化领取工具

这是放在 sub2api 仓库内的独立轻量工具，用 Python + SQLite 实现兑换码活动领取。

- 管理员在本地可视化管理台创建活动、导入兑换码、关闭活动、生成静态领取链接。
- 用户只访问生成后的静态活动页，输入 iCode 账号邮箱后领取兑换码。
- 后端会调用 iCode/sub2api 管理员用户接口校验邮箱是否存在。
- 同一活动内同一邮箱只能领取一次；重复领取会展示原兑换码。
- 活动支持开始时间、结束时间和手动关闭；结束后页面直接显示“活动已结束”。
- 活动可配置领取后跳转地址；用户领取成功后可点击“立即使用”跳转。
- 支持多个活动 ID，例如 `spring2026`、`summer2026`，每个活动生成独立链接。

## 产品边界

V1 不改 sub2api 主后端和主前端，也不把兑换码写回 sub2api 的兑换记录。兑换码、活动、领取记录都在本目录自己的 SQLite 数据库中。

V1 不做邮箱验证码，因此知道某个已存在邮箱的人可以查看该邮箱在该活动下领到的兑换码。高价值活动建议下一版接入邮箱验证码或 iCode 登录态。

## 运行环境

- Python 3.6.8 及以上。
- 不需要安装第三方 Python 包，使用标准库和 SQLite。
- CentOS 7 自带的旧 SQLite 也可运行，活动保存逻辑不依赖新版 SQLite UPSERT 语法。

## 目录说明

- `app.py`：命令入口。
- `cli.py`：服务启动和少量命令兜底。
- `store.py`：SQLite 活动、码池、领取记录。
- `sub2api_auth.py`：iCode/sub2api 管理员鉴权，支持 `login`、`token`、`api_key`。
- `verifier.py`：调用管理员用户列表接口校验邮箱。
- `static/index.html`：用户领取页模板。
- `static/manager.html`：本地可视化管理台，不会发布到 `public/`。
- `publisher.py`：生成静态活动页。
- `config.example.json`：配置示例，复制成 `config.json` 后使用。

## 1. 准备配置

```powershell
Copy-Item .\redeem_claim\config.example.json .\redeem_claim\config.json
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
- `config.json` 已被 `.gitignore` 忽略，不要提交真实密码、JWT、API Key。
- 本地只调试页面时，可临时把 `verify_mode` 改成 `disabled`。

## 2. 启动本地管理台和领取 API

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\redeem_claim\app.py --config .\redeem_claim\config.json serve
```

打开本地管理台：

```text
http://127.0.0.1:8099/manager
```

## 3. 可视化操作流程

1. 在管理台填写 `管理口令`，对应 `config.json` 里的 `security.admin_token`。
2. 填写活动 ID、活动标题、活动说明、领取后跳转地址，并通过活动时间区间选择器选择开始和结束时间。
3. 在兑换码列表中一行一个粘贴兑换码。
4. 点击 `保存、导入并生成链接`，页面会显示用户领取链接和生成目录。
5. 后续可点击 `导入兑换码` 追加码池，或点击 `结束活动` 关闭活动并重新生成静态页。

生成后的公开静态文件在：

```text
redeem_claim/public/<activity_id>/index.html
redeem_claim/public/static/styles.css
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
& "D:\Program Files\Python\Python314\python.exe" .\redeem_claim\app.py --config .\redeem_claim\config.json publish-activity `
  --slug spring2026 `
  --admin-token "change-me-admin-token"
```

## 6. API

- `GET /api/activities/{activity_id}`：活动信息、领取后跳转地址、剩余码量、是否可领取。
- `POST /api/activities/{activity_id}/claim`：领取兑换码，请求体 `{"email":"user@example.com"}`。
- `GET /api/activities/{activity_id}/claims?email=user@example.com`：查看该邮箱在该活动下的领取记录。
