# auto_redeem

用 Python 调 `sub2api` 的管理员兑换码生成接口，把兑换码落到本地 `txt`，再把同一份 `txt` 循环上架到多个链动商品。

## 文件说明

- `sub2api-config.example.json`：`sub2api` 侧示例配置，复制成 `sub2api-config.json`
- `lianjia-config.example.json`：链动侧示例配置，复制成 `lianjia-config.json`
- `generate_codes.py`：读取 `sub2api-config.json`，生成兑换码并输出到本地 txt
- `upload_goods_cards.py`：同时读取 `sub2api-config.json` 和 `lianjia-config.json`，把卡密按 `\n` 拼接后循环上架
- `auto_restock.py`：查询链动商品库存，低于阈值时自动生成并上架，适合服务器定时跑
- `outputs/`：默认输出目录，生成的 txt 会放这里

## 配置拆分

### 1. sub2api-config.json

只负责“怎么领用兑换码”。

核心字段：

```json
{
  "sub2api": {
    "api_base_url": "http://127.0.0.1:8081/api/v1",
    "auth": {
      "mode": "login",
      "login": {
        "email": "admin@example.com",
        "password": "真实密码"
      }
    }
  }
}
```

认证支持 3 种模式：

- `token`：直接使用现成管理员 JWT
- `admin_api_key`：使用 `x-api-key`
- `login`：如果前两种拿不到，就配置账号密码登录后再操作

如果后台登录启用了额外校验：

- Turnstile：可配置 `turnstile_token`，也可运行时传 `--turnstile-token`
- 2FA：可配置 `totp_code`，也可运行时传 `--totp-code`

产品配置放在 `products.<product_key>.redeem`：

```json
"subscription_30d": {
  "label": "30天订阅卡",
  "redeem": {
    "count": 5,
    "type": "subscription",
    "value": 1,
    "group_id": 1,
    "validity_days": 30,
    "expires_in_days": 30
  }
}
```

### 2. lianjia-config.json

只负责“怎么上架到链动”。

链动鉴权当前按你的要求走 `cookie`，并且这个值可配置：

```json
{
  "lianjia": {
    "endpoint": "https://pay.ldxp.cn/merchantApi/GoodsCardStorage/add",
    "auth": {
      "mode": "cookie",
      "cookie": "浏览器里抓到的完整 Cookie"
    }
  }
}
```

商品配置放在 `products.<product_key>.listing`：

```json
"subscription_30d": {
  "label": "30天订阅卡",
  "listing": {
    "group_id": 1,
    "goods_id": 278374
  }
}
```

说明：
- `goods_id` 是最常用配置，一条产品对应一个链动商品时就用它
- `goods_ids` 是兼容扩展配置，只有一条产品要循环上到多个链动商品时才用它
- `listing.group_id` 可选，但建议填上
- 如果填了，脚本会校验它和 `sub2api-config.json` 中同产品的 `redeem.group_id` 一致

### 3. 两份配置之间的关系

推荐让同一个 `product_key` 同时出现在两份配置里：

- `sub2api-config.json` 决定生成什么兑换码
- `lianjia-config.json` 决定把这批兑换码上到哪些商品

也就是：

- 你传 `--product subscription_30d`
- 先按 `sub2api-config.json` 里的 `redeem.group_id=1` 生成
- 再按 `lianjia-config.json` 里的 `goods_id` 上架

## 使用方式

先复制示例配置：

```powershell
Copy-Item .\auto_redeem\sub2api-config.example.json .\auto_redeem\sub2api-config.json
Copy-Item .\auto_redeem\lianjia-config.example.json .\auto_redeem\lianjia-config.json
```

### 查看可用产品档位

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\auto_redeem\generate_codes.py --sub2api-config .\auto_redeem\sub2api-config.json --list-products
```

### 生成兑换码

不传 `--product`：默认按 `sub2api-config.json` 里的全部产品逐个生成。

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\auto_redeem\generate_codes.py --sub2api-config .\auto_redeem\sub2api-config.json --product subscription_30d
```

全部生成：

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\auto_redeem\generate_codes.py --sub2api-config .\auto_redeem\sub2api-config.json
```

### 循环上架卡密

不传 `--product`：默认按 `lianjia-config.json` 里的全部产品逐个上架，并为每个产品自动寻找各自最新生成的 txt。

默认读取该产品最新生成的 txt：

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\auto_redeem\upload_goods_cards.py --sub2api-config .\auto_redeem\sub2api-config.json --lianjia-config .\auto_redeem\lianjia-config.json --product subscription_30d
```

全部上架：

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\auto_redeem\upload_goods_cards.py --sub2api-config .\auto_redeem\sub2api-config.json --lianjia-config .\auto_redeem\lianjia-config.json
```

也可以显式指定某个 txt：

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\auto_redeem\upload_goods_cards.py --sub2api-config .\auto_redeem\sub2api-config.json --lianjia-config .\auto_redeem\lianjia-config.json --product subscription_30d --input .\auto_redeem\outputs\subscription_30d_20260522_120000.txt
```

脚本会打印每个 `goods_id` 的结果：

- `[OK] goods_id=...`
- `[FAIL] goods_id=...`

如果其中任意一个调用失败，脚本最后会以非 0 退出码结束。

上传成功后的文件处理：

- 某个产品的全部 `goods_id` 都上架成功：自动删除该产品对应的 txt
- 只要有任意一个 `goods_id` 失败：保留 txt，方便你重试

注意：

- `--output` 只能配合单个 `--product` 使用
- `--input` 也只能配合单个 `--product` 使用

### 自动补库存

脚本会先请求链动商品列表接口，读取 `extend.stock_count`。
当某个产品对应商品的库存低于阈值时，自动：

1. 调 `sub2api` 生成兑换码
2. 调链动上架
3. 全部成功后删除本地输出 txt

补货数量规则：

- 默认按 `stock_threshold - 当前最低库存` 计算补货数量
- 只有显式传了 `--restock-count`，才使用固定补货数量
- `sub2api-config.json` 里的 `redeem.count` 不再作为自动补库存的默认数量来源

默认阈值是 `99`：

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\auto_redeem\auto_restock.py --sub2api-config .\auto_redeem\sub2api-config.json --lianjia-config .\auto_redeem\lianjia-config.json
```

只处理单个产品：

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\auto_redeem\auto_restock.py --sub2api-config .\auto_redeem\sub2api-config.json --lianjia-config .\auto_redeem\lianjia-config.json --product 50
```

自定义阈值或补货数量：

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\auto_redeem\auto_restock.py --sub2api-config .\auto_redeem\sub2api-config.json --lianjia-config .\auto_redeem\lianjia-config.json --stock-threshold 20 --restock-count 50
```

只看计划，不真正执行：

```powershell
& "D:\Program Files\Python\Python314\python.exe" .\auto_redeem\auto_restock.py --sub2api-config .\auto_redeem\sub2api-config.json --lianjia-config .\auto_redeem\lianjia-config.json --dry-run
```

服务器定时任务建议直接调用这个脚本。

## 建议

- 如果鉴权不能自动获取，直接把 `sub2api.auth.mode` 配成 `login`，填账号密码
- 链动侧按 `lianjia.auth.mode=cookie` 配置浏览器抓到的 Cookie
- `sub2api-config.json` 和 `lianjia-config.json` 都不要提交，`.gitignore` 已忽略
