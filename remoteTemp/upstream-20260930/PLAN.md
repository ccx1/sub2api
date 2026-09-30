# 双上游择优整合记录（2026-09-30）

## 来源与目标

- 本地合并前 HEAD：`dcda8558019ce7e8a855d61f6251e357472c5059`
- 官方来源：`old-origin/main`，获取 SHA：`a60a29549f488a854966aaec9541abbe006cac22`
- Ranxi 来源：`ranxi/production`，获取 SHA：`7124114c22c7cb786a62d5e3ee64713ca87ebdfc`
- 目标本地版本：`0.2.10.1`；Ranxi 来源版本：`2.9.4`

## 当前阶段

1. 已完成官方增量与 Ranxi 增量的隔离合并，正在解决剩余文件级冲突。
2. BPS 协议、附件、工具目录、失败分类、调度器边界和账号入口按 `bps-decisions.md` 逐项择优。
3. 非 BPS 的质量、运维、凭据、自动配置、Mihomo、Pelican、Guard v2、Candy 与计费扩展按 `non-bps-decisions.md` 记录结论。
4. 冲突收口后执行 GOARCH=amd64 后端定向测试、前端 typecheck/Vitest/build，再构建 Linux amd64 embed 产物。

## 交付门禁

- [ ] 无未决冲突、无真实冲突标记，`git diff --check` 通过。
- [ ] BPS 成功、边界、失败、Free 账号、附件与共享池权限路径通过定向测试。
- [ ] 前端账号创建/编辑、导入默认、批量编辑和共享池入口契约完整。
- [ ] 前端 typecheck 与生产构建通过；可行时完成桌面/移动端 smoke。
- [ ] 官方与 Ranxi 获取 SHA 均为最终 HEAD 祖先，版本文件与构建记录一致。
- [ ] 只提交源码和取舍记录，构建物、dist、临时输出不进入 Git。
