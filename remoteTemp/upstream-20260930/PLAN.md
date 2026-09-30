# 双上游择优整合记录（2026-09-30）

## 来源与目标

- 本地合并前 HEAD：`dcda8558019ce7e8a855d61f6251e357472c5059`
- 官方来源：`old-origin/main`，最终获取 SHA：`a0f41f95a07ee6ca0b1300d3c96ce4b62e24724b`
- Ranxi 来源：`ranxi/production`，最终获取 SHA：`a53a7ff163d9337a094e7537df3aac2b31f308b7`
- 目标本地版本：`0.2.10.1`；Ranxi 来源版本：`2.9.5`

## 当前阶段

1. 第一轮官方与 Ranxi 增量已完成隔离合并；最终官方增量已整合，Ranxi 最终 SHA 的冲突已解决并完成定向回归。
2. BPS 协议、附件、工具目录、失败分类、调度器边界和账号入口按 `bps-decisions.md` 逐项择优。
3. 非 BPS 的质量、运维、凭据、自动配置、Mihomo、Pelican、Guard v2、Candy 与计费扩展按 `non-bps-decisions.md` 记录结论。
4. 最终 Ranxi 增量的 priority 跨 BPS/native 均衡、OAuth aliases/自动配置和 reauth 引擎依赖此前暂缓的完整链路，继续暂缓。已执行 GOARCH=amd64 后端定向测试、前端 typecheck/Vitest/build；待构建 Linux amd64 embed 产物。

## 交付门禁

- [x] 无未决冲突、无真实冲突标记，`git diff --check` 通过。
- [x] BPS 成功、边界、失败、Free 账号、附件与共享池权限路径通过定向测试。
- [x] 前端账号创建/编辑、导入默认、批量编辑和共享池入口契约完成定向回归。
- [x] 前端 typecheck 与生产构建通过；`/home`、`/login`、`/register` 完成桌面/移动端 smoke。
- [ ] 官方与 Ranxi 获取 SHA 均为最终 HEAD 祖先，版本文件与构建记录一致。
- [ ] 只提交源码和取舍记录，构建物、dist、临时输出不进入 Git。

## 验证范围与限制

- Go 1.27 windows/amd64、`GOMAXPROCS=2`、`-p 1 -vet=off`：`basispoints` 包全测、BPS service 定向测试、handler fallback 和共享池导入、routes BPS、`TestAPIContracts` 通过。共享池导入四包门禁 `Test(Shared|ImportData|AccountImport|AdminCreateAccountImport)` 通过。
- 前端账号创建/编辑、导入默认/执行、批量操作与 i18n 共 328 项通过；`pnpm --dir frontend typecheck` 和 `build` 通过。真实浏览器在 1280x720 与 390x844 验证三处公开页面，未登录后台壳层无法验收。
- 共享池文件显式 BPS 值的创建回归使用内存仓储桩；本次未连接隔离 PostgreSQL，不将其等同于真实事务验证。fallback 渠道映射测试覆盖两个 OpenAI 兼容入口的出站模型和 API key 分组，上游 400 响应后结束，未覆盖成功后的真实扣费落库。
