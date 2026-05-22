# payHtml

这是一个不依赖构建工具的静态充值跳转页，适合直接被 iframe 嵌入，或独立打开后作为储值卡充值入口使用。

## 你需要改的地方

只改 [config.js](/F:/Codes/GoCodes/sub2api/payHtml/config.js)：

```js
window.PAY_PAGE_CONFIG = {
  openMode: 'top',
  items: [
    { name: '100 元储值卡', desc: '说明', tag: '推荐', url: 'https://example.com/pay/card-100' },
  ],
}
```

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
