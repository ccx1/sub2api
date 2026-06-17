# Sub2API monitor 使用说明

`monitor/monitor.py` 是一个独立的 API Key 日用量巡检脚本，用来查询 Sub2API 某天的 `usage` 数据，并按需发送钉钉通知。

当前这份脚本是 Python 2 风格实现，依赖 `urllib2`，请按 `Python 2.7` 运行。用 Python 3 直接执行会报 `ModuleNotFoundError: No module named 'urllib2'`。

## 目录说明

当前目录建议保留这些文件：

- `monitor.py`
- `apikey.txt`
- `apikey.txt.example`
- `config.example.toml`

其中真正执行 `monitor.py` 时，脚本默认读取同目录的 `apikey.txt`。如果你历史上保留的是 `apikeys.txt`，脚本也会自动兼容，但后续建议统一成 `apikey.txt`。

## apikey.txt 格式

先参考 [apikey.txt.example](./apikey.txt.example) 新建 `apikey.txt`：

```text
alice,sk-xxxx
bob sk-yyyy
sk-zzzz
```

支持三种写法：

- `名称,sk-xxx`
- `名称 sk-xxx`
- 只写 `sk-xxx`，脚本会自动脱敏后作为展示名称

额外规则：

- 空行会忽略
- `#` 开头的注释行会忽略
- 建议一个 key 一行

## 命令参数

`monitor.py` 支持这些参数：

- `--base-url`：Sub2API 根地址，默认 `http://192.168.30.96:10888`
- `--apikey-file`：API Key 清单文件，默认 `apikey.txt`
- `--timezone`：统计日期时区，默认 `Asia/Shanghai`
- `--timeout`：请求超时秒数，默认 `30`
- `--insecure`：跳过 HTTPS 证书校验
- `--dry-run`：只打印结果，不发送钉钉
- `--dingtalk-webhook`：钉钉机器人 webhook
- `--dingtalk-secret`：钉钉加签 secret
- `--keyword`：钉钉关键词，默认 `Token额度`

## 本地执行

### Windows

如果机器上装了 Python 2.7，可以在 `monitor` 目录执行：

```powershell
cd F:\Codes\GoCodes\sub2api\monitor
py -2 monitor.py --dry-run
```

如果 `py -2` 不可用，就改成你本机的 Python 2.7 绝对路径，例如：

```powershell
cd F:\Codes\GoCodes\sub2api\monitor
& "D:\Python27\python.exe" monitor.py --dry-run
```

发送钉钉通知示例：

```powershell
cd F:\Codes\GoCodes\sub2api\monitor
$env:SUB2API_BASE_URL = "http://127.0.0.1:10888"
$env:DINGTALK_WEBHOOK = "https://oapi.dingtalk.com/robot/send?access_token=replace_me"
$env:DINGTALK_SECRET = ""
$env:DINGTALK_KEYWORD = "Token额度"
py -2 monitor.py
```

### Linux

如果脚本会上传到服务器独立运行，建议把 `monitor.py` 和 `apikey.txt` 放在同一目录：

```bash
cd /opt/sub2api-monitor
python2 monitor.py --dry-run --base-url "http://127.0.0.1:10888"
```

发送钉钉通知：

```bash
cd /opt/sub2api-monitor
SUB2API_BASE_URL="http://127.0.0.1:10888" \
DINGTALK_WEBHOOK="https://oapi.dingtalk.com/robot/send?access_token=replace_me" \
DINGTALK_SECRET="" \
DINGTALK_KEYWORD="Token额度" \
python2 monitor.py
```

## 环境变量

脚本也支持通过环境变量传参：

```text
SUB2API_BASE_URL
APIKEY_FILE
TZ_NAME
TIMEOUT_SECONDS
DINGTALK_WEBHOOK
DINGTALK_SECRET
DINGTALK_KEYWORD
```

Windows 示例：

```powershell
$env:SUB2API_BASE_URL = "http://127.0.0.1:10888"
$env:APIKEY_FILE = "apikey.txt"
$env:TZ_NAME = "Asia/Shanghai"
$env:TIMEOUT_SECONDS = "30"
$env:DINGTALK_WEBHOOK = "https://oapi.dingtalk.com/robot/send?access_token=replace_me"
$env:DINGTALK_SECRET = ""
$env:DINGTALK_KEYWORD = "Token额度"
py -2 monitor.py
```

Linux `crontab` 示例：

```cron
55 23 * * * cd /opt/sub2api-monitor && SUB2API_BASE_URL='http://127.0.0.1:10888' DINGTALK_WEBHOOK='https://oapi.dingtalk.com/robot/send?access_token=replace_me' DINGTALK_SECRET='' DINGTALK_KEYWORD='Token额度' python2 monitor.py >> monitor.log 2>&1
```

## 输出与接口

每个 API Key 会请求：

```text
GET /v1/usage?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD
Authorization: Bearer sk-xxx
```

脚本会按配置时区计算“今天”的日期，并输出：

- key 名称
- 今日花费 `actual_cost`
- 今日请求数 `requests`
- 今日总 token `total_tokens`
- 当前状态或错误信息

`--dry-run` 只在终端打印结果，不发钉钉；未配置 `DINGTALK_WEBHOOK` 时，脚本也会跳过通知。

## 当前已知限制

- 这份 `monitor.py` 不能直接用 Python 3 运行
- 当前仓库内没有可用的 Python 2 解释器验证记录，若要在这台机器上执行，需要你本机另装或指定 Python 2.7
- `config.example.toml` 和 `sub2api_monitor/` 目录属于另一套较新的实现思路，不是 `monitor.py` 当前执行所必需的入口
