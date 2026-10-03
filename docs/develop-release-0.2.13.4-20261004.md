# develop 0.2.13.4 交付范围（2026-10-04）

## 来源与批次

| 项目 | 固定值 |
| --- | --- |
| 分支与合并前 HEAD | `develop`，`fec4c0bcec1fb7885046b8e948c1ff9688fd09d2` |
| 最近有远端交付记录的版本 | `0.2.12.1`，`32cb4781fbae1ac6089e397f8618266f81fa8adc` |
| 官方来源 | 已整合 `Wei-Shaw/sub2api` 0.2.13，`b8dece9000c68815a5b867ca5a1e6f236e173905` |
| Ranxi 已整合基线 | `2.9.6`，`bf9405e4ab58c1be4fc8ec2101371753a016908e` |
| 本次部分采用来源 | `502db27d20f7c597e7e97edfcc9ad9121950d1b4` 中用户批准的 R4、R5；不纳入该提交的其他祖先变更 |
| 本地主版本 | `backend/cmd/server/VERSION`：`0.2.13.4`；工作区原定目标版本，`.2/.3/.4` 旧二进制无对应正式源码交付记录，不作为本次产物 |
| Ranxi 版本 | 保持 `backend/cmd/server/RANXI_VERSION=2.9.6`，部分采用不宣称整版合并 |

旧 Ranxi 合并现场的 16 文件暂存 patch 指纹为 `af161fde66d8fef7c1c2bcc67193d2f1bc2ff183`。用户本次明确批准 R4、R5 正式出包。因 `MERGE_HEAD` 的祖先链还包含未批准能力，使用 `git merge --quit` 保留原暂存和工作树，再仅将获准代码作为单父适配提交；不伪造 Ranxi 整版祖先关系。其余本地未提交定制、临时文件、旧二进制不纳入源码提交。

## 逐项取舍

| 结论 | 项目与理由 | 验证 |
| --- | --- | --- |
| 直接吸收 | 无新增官方提交；官方 0.2.13 已在本批次前整合 | `git merge-base --is-ancestor b8dece900 develop` |
| 适配吸收 | Ranxi R4 Claude/Pelican/IQ 测试：提高测试输出上限、按模型传递 effort，保留本地普通连接探测与账号测试边界 | Claude/Pelican Go 定向测试、IQ 弹窗 10 例 |
| 适配吸收 | Ranxi R5 WebSocket Key 撤销后重新读库复验，保留本地逐轮准入、槽和 hold 的释放语义 | handler/service/middleware 定向测试 |
| 保留本地 | 历史票只用达到沉淀时间且处于独立有效期的票；后台继续采新票；Cookie 默认每张新票从空 jar 开始；每账号总容量跨模型分配 | 票据 config/repository/service 测试、设置页/票库/用量组件测试 |
| 暂缓吸收 | Ranxi R1/R2 Prism、R3 公共 Pelican API、其他未批准候选；不引入其运行时、路由、默认值或迁移 | 精确暂存路径审计及单父提交 |

历史票有效期默认 8 天、最长 30 天，支持 7 天沉淀后仍有使用窗口。旧模型存票在账号下一次成功发布新票时通过并发保护的 CAS 收敛；刚保存容量配置时不会批量写数据库。1000 张票池的本机基准为 11.726 ms/op、3.52 MB/op、10055 allocs/op，生产高并发和数据库体积仍需实测。

## 源码验收

- 从暂存树导出隔离源码，并使用锁文件安装前端依赖；5 个票据、IQ 与设置页测试文件共 283 例通过。
- 隔离源码前端 `pnpm build` 通过，含 i18n 完整性 3 例和 `vue-tsc`，Vite 转换 1339 模块。
- 隔离源码后端 config/repository/service/handler/middleware 的票据、R4、R5 定向测试通过。
- `git diff --cached --check` 通过；`RANXI_VERSION` 未更新，未执行生产数据库迁移或业务数据写入。

最终源码提交、远端 SHA、Linux amd64 embed 产物大小、SHA256 和 `go version -m` 记录于本次 `.codex-run/release-20261004-v0.2.13.4/` 构建记录。正式部署与真实账号/上游 E2E 不属于本次打包。
