# payHtml

这是一个不依赖构建工具的静态充值跳转页，适合直接被 iframe 嵌入，或独立打开后作为储值卡充值入口使用。

## 你需要改的地方

只改 [config.js](/F:/Codes/GoCodes/sub2api/payHtml/config.js)：

```js
window.PAY_PAGE_CONFIG = {
  pageTitle: '储值卡充值',
  openMode: 'top',
  sections: [
    {
      title: '推荐套餐',
      desc: '适合稳定使用的常规充值档位。',
      tag: 'Hot',
      theme: 'orange',
      items: [
        {
          name: '100 元储值卡',
          desc: '说明',
          tag: '推荐',
          price: '¥100',
          meta: '购买后按页面提示完成兑换',
          url: 'https://example.com/pay/card-100',
        },
        {
          name: '10 元储值卡',
          desc: '同一金额提供多条支付线路。',
          tag: '常用',
          price: '¥10',
          channels: [
            { name: '支付线路 A', note: '推荐', url: 'https://example.com/pay/card-10-a' },
            { name: '支付线路 B', note: '备用', url: 'https://example.com/pay/card-10-b' },
          ],
        },
      ],
    },
  ],
}
```

## 板块配置

- `sections`：页面板块列表，页面顶部会渲染为可切换的板块 Tab
- `sections[].title`：板块标题
- `sections[].desc`：板块说明
- `sections[].tag`：板块角标
- `sections[].theme`：板块配色，可选 `orange`、`blue`、`emerald`、`rose`
- `sections[].actionText`：当前板块内卡片默认按钮文案
- `sections[].emptyText`：当前板块没有 `items` 时显示的提示
- `sections[].items`：当前板块下的金额选项，切换金额后显示对应支付线路

## 卡片配置

- `name`：卡片标题
- `desc`：卡片说明
- `tag`：卡片角标
- `price`：右上角价格展示，可不填
- `meta`：补充说明，可不填
- `url`：点击后跳转的支付地址
- `openMode`：单张卡片的打开方式，可覆盖全局或板块的 `openMode`

## 多线路支付配置

当同一个金额有多个支付渠道时，不要复制多张相同金额的卡片，在该卡片下使用 `channels`：

```js
{
  name: '10 元储值卡',
  price: '¥10',
  channels: [
    {
      name: '支付宝线路',
      note: '推荐',
      url: 'https://example.com/pay/alipay-10',
    },
    {
      name: '微信线路',
      note: '备用',
      url: 'https://example.com/pay/wechat-10',
    },
  ],
}
```

- `channels`：同一金额下的支付线路列表，页面会在金额卡片内分别显示按钮
- `channels[].name`：线路名称，例如支付宝、微信或线路 A
- `channels[].note`：线路补充说明，可不填
- `channels[].url`：该线路的实际支付地址；为空时按钮会禁用，适合暂时下线线路
- `channels[].openMode`：可选，覆盖卡片、板块和全局的 `openMode`

如果卡片不配置 `channels`，仍可直接使用原来的 `url` 单线路写法。

旧版 `items` 配置仍然兼容；如果没有配置 `sections`，页面会自动把 `items` 渲染成一个默认板块。

## openMode 说明

- `top`：优先跳出 iframe，在顶层窗口打开，适合充值跳转
- `parent`：在父级窗口打开
- `self`：仅在当前 iframe 内打开
- `new-tab`：新标签页打开

## iframe 嵌入示例

```html
<iframe
  src="/payHtml/index.html"
  style="width: 100%; height: 100vh; border: 0;"
  referrerpolicy="no-referrer"
></iframe>
```

## 文件

- [index.html](/F:/Codes/GoCodes/sub2api/payHtml/index.html)：页面主体
- [config.js](/F:/Codes/GoCodes/sub2api/payHtml/config.js)：卡片名称、跳转地址、打开方式配置
