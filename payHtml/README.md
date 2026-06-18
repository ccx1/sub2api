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
      ],
    },
  ],
}
```

## 板块配置

- `sections`：页面板块列表，每个板块会独立显示标题、说明和卡片网格
- `sections[].title`：板块标题
- `sections[].desc`：板块说明
- `sections[].tag`：板块角标
- `sections[].theme`：板块配色，可选 `orange`、`blue`、`emerald`、`rose`
- `sections[].actionText`：当前板块内卡片默认按钮文案
- `sections[].emptyText`：当前板块没有 `items` 时显示的提示
- `sections[].items`：当前板块下的商品卡片

## 卡片配置

- `name`：卡片标题
- `desc`：卡片说明
- `tag`：卡片角标
- `price`：右上角价格展示，可不填
- `meta`：补充说明，可不填
- `url`：点击后跳转的支付地址
- `openMode`：单张卡片的打开方式，可覆盖全局或板块的 `openMode`

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
