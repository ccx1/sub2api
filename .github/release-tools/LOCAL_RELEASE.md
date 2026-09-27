# 本地平台发行版

本地版本唯一来源为 `backend/cmd/server/VERSION`，继续使用 `0.2.8.x`。Ranxi 来源版本保持独立。目标为 `ccx1/sub2api` 的 GitHub Releases，当前发布 Linux amd64 二进制，不发布 Docker 镜像。正式发布由推送 `v0.2.8.*` 标签触发；普通分支提交、`pnpm build` 和 `go build` 不会上传。

源码和发布工作流保留在当前开发分支，不需要加入或合并到 `main`。标签指向包含工作流与发布工具的已审核提交，Actions 从该提交构建。

## 首次准备

仓库需启用 GitHub Actions，本机需要向目标仓库推送源码分支和标签的权限。本次源码提交必须包含 `.github/workflows/local-release.yml`、`.github/release-tools/` 以及更新后的版本文件。Actions 自动准备编译环境。

Actions 使用任务自带的 token 上传 Release，工作流已声明 `contents: write`；采用标签发布时，本机不需要 `GH_TOKEN`、GitHub CLI 或额外的 Releases API 凭证。

如需本地试包，需要 Python 3.11+、64 位 Go（匹配 `backend/go.mod`）、pnpm、Node.js、Git 和 Bash。Windows 可用 Git Bash。先执行 `pnpm --dir frontend install --frozen-lockfile`。Python / Go 不在 PATH 时使用本机绝对路径，不修改依赖与锁文件。

## 每次正式发布

1. 开始一个新版本时执行 `prepare`，第四段递增一次；未提交的版本已变化时不会再次递增。同次出包或重试沿用原版本。
2. 审核源码，将版本文件与对应改动提交在当前开发分支，再推送该分支到 `origin`。只纳入源码和必要文档，构建产物不提交。
3. 对已审核提交创建并推送同版本标签。Actions 自动验证、构建前端、构建后端、打包、创建草稿、上传并校验全部附件，最后正式发布为 Latest。无需手动上传文件或合并 `main`。

```powershell
python .github/release-tools/local_release.py prepare
# 审核并提交源码后，在当前开发分支执行：
$version = (Get-Content -LiteralPath 'backend/cmd/server/VERSION').Trim()
$tag = "v$version"
git push origin HEAD
git tag -a $tag -m "Release $tag"
git push origin "refs/tags/$tag"
```

同一版失败后，在该次 Actions 运行中重跑失败任务，不要再次 `prepare`、移动标签或同时从本机执行 `publish`。重跑仍使用原标签的提交，不会读取业务分支后续修改；源码修正后应另起版本和标签。发布脚本由 Actions 调用，远端草稿只补缺少的附件，已存在附件必须哈希相同。正式版本和不同源码/附件不会被覆盖。网络失败保留草稿，不暴露不完整的 Latest。

为了让 CI 新 runner 重试仍可复用草稿附件，二进制中的构建日期和压缩包时间固定为源码提交时间，文本附件使用 LF；不会用每次执行的当前时间生成不同附件。`build-info.json` 的 `build_date_utc` 因此表示可复现构建使用的来源提交时间。发布器拒绝把 Latest 指回更旧的本地版本。

## 本地试包，不上传

```powershell
python .github/release-tools/local_release.py build --go 'C:/path/to/go.exe'
```

`build` 允许已有未提交源码，并在 `build-info.json` 记录 `source_modified`、源码指纹和 Go 的 `vcs_modified`；`publish` 拒绝未提交源码。默认产物在已忽略的 `release/v<版本>/`。不同源码已有同版本输出时拒绝覆盖，可给本地试包指定另一个 `--output`。

附件包括 `sub2api_<版本>_linux_amd64.tar.gz`（根目录二进制名为 `sub2api`）、`checksums.txt`、`install.sh`、`build-info.json`。本地保留带版本和提交号的裸二进制及 `go-version.txt`。不要将这些产物放进 Git。

出包时验证 ELF 架构、Go 目标平台、CGO、embed、trimpath、源码提交元数据及二进制中注入的版本/提交/日期。Go 在 `-trimpath` 下不记录 `-ldflags`，因此实际传入的链接参数另存于 `build-info.json` 的 `linker_flags`；`build_type=release` 来自受控构建命令，不把二进制中出现普通 `release` 文本视为运行时构建类型的证明。Windows 交叉构建不等于完成 Linux 安装或服务启动验证。

## GitHub Actions

工作流 **Local Platform Release** 只监听 `push.tags: v0.2.8.*`，不使用依赖默认分支入口的 `workflow_dispatch`。只要推送的标签所指提交包含工作流，便可触发发布；本方案不修改 `main`。

checkout 固定使用事件的 `github.sha`；发布脚本接收 `github.ref_name` 作为 `--expected-tag`，要求标签与提交内 `VERSION` 完全一致。发布前还会核对远端标签所指提交，防止把错误版本或错误提交发布为对应标签。仅推送业务分支、推送其它版本系列标签或在本地构建，都不会触发此工作流。

本仓库的旧 GoReleaser 工作流不再执行，避免 Tag 创建后重复发布及四段版本与 GoReleaser SemVer 不兼容。其它 fork 保留旧工作流。新发布器直接使用四段版本，不依赖 GoReleaser。

## 用户安装与升级

```bash
# 首次安装；已安装时按安装脚本的升级逻辑处理
curl -fsSL https://github.com/ccx1/sub2api/releases/latest/download/install.sh | sudo bash
# 明确升级
curl -fsSL https://github.com/ccx1/sub2api/releases/latest/download/install.sh | sudo bash -s -- upgrade
```

旧部署仍查询官方源，首次必须手动安装本次修改后的发行版。之后左上角徽标与弹层分别展示平台发行版和 Ranxi 源码更新，平台升级仍需管理员操作；发布 Release 不会自动重启服务器。升级保留 `DATA_DIR`、`config.yaml` 和 `.installed`，二进制回退不等于数据库回退。
