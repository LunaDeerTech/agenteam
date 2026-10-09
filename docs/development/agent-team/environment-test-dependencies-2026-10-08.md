# 2026-10-08 测试依赖恢复

> 历史记录：以下“当前”“已恢复”、实例名、临时路径、授权窗口和资源状态均指记录当时，不代表本次环境。继续开发先读[当前台账](tasks.md)与[团队流程](README.md)，只在追溯具体失败或依赖来源时读取本页；不重建旧哈希清单、归档或逐轮审批。

固定依赖已再次恢复。MinIO 二进制 SHA 与现有 fixture 契约精确一致，两个 PostgreSQL 固定 digest 可供本机 Docker 使用，锁定 Node 依赖已补齐。首阶段完整数值见[恢复结果](environment-test-dependencies-2026-10-08-evidence/evidence/recovery-result.json)。本次只执行依赖下载、构建、安装及版本/指纹检查，未启动容器、数据库连接、listener、浏览器会话或产品测试；不构成 Owner 工作区 UI 或其他业务验收，不解除既有三项停止。

本轮从 root 恢复记录所列 `ff396a4e` 基线继续；恢复执行者未运行 Git，实际相关输入以[首次指纹](environment-test-dependencies-2026-10-08-evidence/evidence/source-inputs.raw)、[前端锁指纹](environment-test-dependencies-2026-10-08-evidence/evidence/node-inputs.raw)绑定，并与[结束指纹](environment-test-dependencies-2026-10-08-evidence/evidence/preserved-inputs.raw)、[安装后指纹](environment-test-dependencies-2026-10-08-evidence/evidence/node-inputs-after.raw)逐字节核对一致。[2026-10-07 原恢复档](environment-test-dependencies-2026-10-07.md)及其原始失败保持不变。

## 恢复前的实际缺失

- Docker 初查只有 PG17 的 `3e8b3adf…` 引用和旧 `RELEASE.2025-09-07T16-13-09Z-01ce918d8279` MinIO 镜像。对当前契约 PG17、PG16 两个 digest 的初次 inspect 均退出 1。恢复 PG17 固定引用复用了相同 image ID，不能据此声称重新下载了全部层。
- 原 `/workspace/scratch/fixture-recovery/bin/minio`、fixture 默认缓存路径和精确 MinIO module 源码均缺失。现有 module cache 中 fwd/msgp 是较旧版本；精确的两个缺件后来只下载到自有 cache。
- `tests/account-captcha-web/node_modules` 尚未安装；全局 Playwright 为 1.62.1，默认 browser cache 缺失，不能替代 harness 锁定的 1.56.1。`web/node_modules` 已有其他依赖，但 `npm ls` 因缺少 `go-captcha-vue@2.0.7` 退出 1。
- 首次 cache 枚举遇到 `/root/.cache/ms-playwright` 访问权限错误并退出 1；第二次只读枚举保留错误为观测项后继续。未把无法读取该目录写成确认无缓存。

## 精确恢复结果

MinIO 注入值为 `AGENTEAM_MINIO_BINARY=/workspace/scratch/fixture-recovery-new/bin/minio`。二进制 109289632 bytes，SHA-256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`；实际 `--version` 为 `RELEASE.2025-10-15T17-29-55Z`、commit `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`、Go `1.27.1 linux/amd64`。

源码仍为 `github.com/minio/minio@v0.0.0-20251015172955-9e49d5e7a648`，ZIP SHA-256 `b137c35bf9708b4032a6a8301495a2563cab25111c28b80fd609812a3252a2f8`；ZIP bytes、module/go.mod sum、源码 `go.mod/go.sum` 指纹逐项等于[原固定来源](environment-test-dependencies-2026-10-07-evidence/evidence/source-verification.json)。[本轮源码核对](environment-test-dependencies-2026-10-08-evidence/evidence/source-verification.json)与[缺件核对](environment-test-dependencies-2026-10-08-evidence/evidence/missing-module-verification.json)保存实际自有路径。

构建复用原 build04 的 Go/ldflags/架构参数和只读共享 module cache，仅将自有根路径换为 `fixture-recovery-new`；两个固定缺件只读叠加到构建 namespace。构建实际 wait 退出 0，耗时 73.372607706 秒，未更改 fixture helper 或 SHA 契约。此轮 MinIO 构建首轮成功，不覆盖前一日的四次原失败。

| 用途 | 固定镜像引用 | image inspect 的配置 |
| --- | --- | --- |
| PG17 | `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc` | `linux/amd64`、`PG_VERSION=17.8-1.pgdg12+1` |
| PG16 | `pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782` | `linux/amd64`、`PG_VERSION=16.12-1.pgdg12+1` |

两个引用均完成 pull exit0，最终 inspect 的 `RepoDigests` 含所需完整 digest。表内版本来自镜像配置，未观测运行中的数据库。

## Node 依赖与浏览器边界

实际 Node 为 `v24.19.0`。先按原锁执行 `npm ci --prefix tests/account-captcha-web --no-audit --no-fund --prefer-offline`，实际安装 85 个包并退出 0；本地 Playwright CLI 输出 `Version 1.56.1`。

root 将 web 依赖安装窗授予本执行者后，frontend_worker 明确 ACK 当前没有 Node 读者或后台检查，并在窗口释放前暂停 Node。核对两份锁中 `go-captcha-vue@2.0.7` 的版本、resolved URL、SHA-512 完全相同后，仅把成功安装的该包复制到缺失的 `web/node_modules/go-captcha-vue`，未重建 web 依赖树。两次安装后 `npm ls --depth=0` 均退出 0；web 146 个、harness 85 个已安装包版本逐项匹配锁，无非 optional 缺项，分别保留 25/24 个未安装 optional 条目。两目录 package/lock 指纹均未变，随后向 frontend_worker 释放窗口；[窗口交接记录](environment-test-dependencies-2026-10-08-evidence/evidence/window-handover.json)明确记录来源为本轮协调消息的事后整理。

既有 Go Web harness 使用 `/usr/bin/chromium`。该路径实际 `--version` 输出 `Chromium 151.0.7922.173 built on Debian GNU/Linux 13 (trixie)`，`ldd /usr/lib/chromium/chromium` 无 `not found`，入口及实际 ELF 的 SHA 已记录。依 root 后续指示复用系统浏览器，未另下载 Playwright revision1194。此次没有启动浏览器，因此 Playwright 1.56.1 与系统 Chromium 的实际兼容性、视觉和交互仍待对应授权窗口验证。

## 后续发现的根 Go 依赖缺口与恢复

首阶段完成后，backend_worker 的真实离线 `graph-account01` 因根模块版本缺失失败；[原 meta](environment-test-dependencies-2026-10-08-evidence/upstream/graph-account01/meta.json)与[原 raw](environment-test-dependencies-2026-10-08-evidence/upstream/graph-account01/raw)保留该执行者的 argv、环境、退出 1 及实际 wait/双空记录。该失败不属于上面的 MinIO 构建，也不以恢复结果回填为通过。

root 追加授权后，[实际前检](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-cache-preflight.json)确认 `go.mod` 的 31 个明确要求中有 25 个源码目录缺失，含 pgx/v5 `v5.11.0`、minio-go/v7 `v7.3.0`、goose/v3 `v3.28.0`、go-captcha/v2 `v2.0.5`、x/crypto `v0.55.0`、x/image `v0.45.0`。共享 cache `/workspace/go/pkg/mod` 属于当前 uid1000 且可写；backend_worker 与 verification_worker 均先明确 ACK 已停止 Go/cache 读者，恢复者才取得唯一 cache 写窗。

首次 `go mod download -json all` 扩展到了依赖工具图，输出 303 个模块记录，其中 272 个不在根 `go.mod` 的显式 31 要求中。末尾尝试补写 `go.sum`，被 bwrap 中的仓库只读挂载拒绝，实际退出 1；未降低锁校验或让仓库可写。原输出包含缓存复用，未统计更广图中实际新增下载数量，也未删除无法据此前检证明为本次新增的共享 cache。该失败和原字节保留。

随后限定 `go.mod` 明列的 31 个 `module@exact-version`，每项预先要求原 `go.sum` 有 module 与 go.mod 两个 checksum，下载命令实际退出 0。31 组 Sum/GoModSum 与原锁逐项匹配，源码目录现全在；`GOPROXY=off GOSUMDB=off`、仓库/cache 只读的 `go mod verify` 实际退出 0，输出 `all modules verified`。[根 Go 恢复结果](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-result.json)记录各项版本/校验及窗口交还；[固定 go.mod](environment-test-dependencies-2026-10-08-evidence/inputs/go.mod) SHA 为 `294a95594aa10b67474d8c82b01a6def9d64ca132fca53a1afff081736fe8758`，[固定 go.sum](environment-test-dependencies-2026-10-08-evidence/inputs/go.sum) SHA 为 `6bc1fd93b203eefb8360ed556980575429b0a35f999f5c8c984f611203633245`，恢复前后不变。

共享 cache 已向 root、backend_worker、verification_worker 交还，后继可继续使用 `GOMODCACHE=/workspace/go/pkg/mod`。恢复者未运行业务依赖图、编译或测试；实际产品图由对应执行者另行绑定。原 MinIO 阶段的源码、build 与 binary SHA 结果保持独立，未因为后续 cache 补件而冒称重新构建或复验。

本补充增加六条本执行者命令，五条退出 0、一条退出 1：

| 命令 meta | 原始输出 | 退出码 |
| --- | --- | --- |
| [root-go-inputs](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-inputs.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-inputs.raw) | 0 |
| [root-go-download](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-download.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-download.raw) | 1 |
| [root-go-download02](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-download02.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-download02.raw) | 0 |
| [root-go-verify](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-verify.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-verify.raw) | 0 |
| [root-go-sum-check](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-sum-check.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-sum-check.raw) | 0 |
| [root-go-inputs-after](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-inputs-after.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/root-go-inputs-after.raw) | 0 |

## 首阶段实际命令与原失败

首阶段共 33 条依赖命令，29 条退出 0、4 条退出 1；后续根 Go 阶段另有上列六条，两阶段合计 39 条、34 条退出 0、5 条退出 1，另外引用 backend 的原 graph 失败而不纳入本执行者命令数。每组 meta 保留实际 argv、cwd、安全环境项、PID、起止时间、实际 wait 退出码及 raw SHA；proxy 值仅记录存在。表格按实际启动时间排序，并行初查的完成顺序可能不同。

| 命令 meta | 原始输出 | 退出码 |
| --- | --- | --- |
| [docker-inventory](environment-test-dependencies-2026-10-08-evidence/evidence/docker-inventory.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/docker-inventory.raw) | 0 |
| [cache-inventory](environment-test-dependencies-2026-10-08-evidence/evidence/cache-inventory.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/cache-inventory.raw) | 1 |
| [pg17-inspect](environment-test-dependencies-2026-10-08-evidence/evidence/pg17-inspect.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/pg17-inspect.raw) | 1 |
| [pg16-inspect](environment-test-dependencies-2026-10-08-evidence/evidence/pg16-inspect.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/pg16-inspect.raw) | 1 |
| [pg17-pull](environment-test-dependencies-2026-10-08-evidence/evidence/pg17-pull.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/pg17-pull.raw) | 0 |
| [pg16-pull](environment-test-dependencies-2026-10-08-evidence/evidence/pg16-pull.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/pg16-pull.raw) | 0 |
| [cache-inventory02](environment-test-dependencies-2026-10-08-evidence/evidence/cache-inventory02.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/cache-inventory02.raw) | 0 |
| [pg17-inspect02](environment-test-dependencies-2026-10-08-evidence/evidence/pg17-inspect02.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/pg17-inspect02.raw) | 0 |
| [source-inputs](environment-test-dependencies-2026-10-08-evidence/evidence/source-inputs.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/source-inputs.raw) | 0 |
| [playwright-inventory](environment-test-dependencies-2026-10-08-evidence/evidence/playwright-inventory.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/playwright-inventory.raw) | 0 |
| [npm-installed](environment-test-dependencies-2026-10-08-evidence/evidence/npm-installed.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/npm-installed.raw) | 1 |
| [pg16-inspect02](environment-test-dependencies-2026-10-08-evidence/evidence/pg16-inspect02.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/pg16-inspect02.raw) | 0 |
| [minio-source-zip](environment-test-dependencies-2026-10-08-evidence/evidence/minio-source-zip.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/minio-source-zip.raw) | 0 |
| [node-inputs](environment-test-dependencies-2026-10-08-evidence/evidence/node-inputs.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/node-inputs.raw) | 0 |
| [harness-npm-ci](environment-test-dependencies-2026-10-08-evidence/evidence/harness-npm-ci.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/harness-npm-ci.raw) | 0 |
| [minio-source-verify](environment-test-dependencies-2026-10-08-evidence/evidence/minio-source-verify.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/minio-source-verify.raw) | 0 |
| [go-version](environment-test-dependencies-2026-10-08-evidence/evidence/go-version.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/go-version.raw) | 0 |
| [missing-module-download](environment-test-dependencies-2026-10-08-evidence/evidence/missing-module-download.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/missing-module-download.raw) | 0 |
| [web-missing-package-restore](environment-test-dependencies-2026-10-08-evidence/evidence/web-missing-package-restore.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/web-missing-package-restore.raw) | 0 |
| [npm-installed02](environment-test-dependencies-2026-10-08-evidence/evidence/npm-installed02.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/npm-installed02.raw) | 0 |
| [harness-installed](environment-test-dependencies-2026-10-08-evidence/evidence/harness-installed.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/harness-installed.raw) | 0 |
| [node-inputs-after](environment-test-dependencies-2026-10-08-evidence/evidence/node-inputs-after.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/node-inputs-after.raw) | 0 |
| [missing-module-verify](environment-test-dependencies-2026-10-08-evidence/evidence/missing-module-verify.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/missing-module-verify.raw) | 0 |
| [minio-build-01](environment-test-dependencies-2026-10-08-evidence/evidence/minio-build-01.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/minio-build-01.raw) | 0 |
| [lock-installed-check](environment-test-dependencies-2026-10-08-evidence/evidence/lock-installed-check.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/lock-installed-check.raw) | 0 |
| [chromium-version](environment-test-dependencies-2026-10-08-evidence/evidence/chromium-version.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/chromium-version.raw) | 0 |
| [chromium-libs](environment-test-dependencies-2026-10-08-evidence/evidence/chromium-libs.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/chromium-libs.raw) | 0 |
| [chromium-sha](environment-test-dependencies-2026-10-08-evidence/evidence/chromium-sha.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/chromium-sha.raw) | 0 |
| [locked-playwright-version](environment-test-dependencies-2026-10-08-evidence/evidence/locked-playwright-version.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/locked-playwright-version.raw) | 0 |
| [minio-buildinfo](environment-test-dependencies-2026-10-08-evidence/evidence/minio-buildinfo.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/minio-buildinfo.raw) | 0 |
| [preserved-inputs](environment-test-dependencies-2026-10-08-evidence/evidence/preserved-inputs.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/preserved-inputs.raw) | 0 |
| [minio-binary-sha](environment-test-dependencies-2026-10-08-evidence/evidence/minio-binary-sha.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/minio-binary-sha.raw) | 0 |
| [minio-version](environment-test-dependencies-2026-10-08-evidence/evidence/minio-version.meta.json) | [raw](environment-test-dependencies-2026-10-08-evidence/evidence/minio-version.raw) | 0 |

初次缺镜像、cache 权限错误和 web 缺包四条失败均保留。包装器没有在启动时独立捕获 stdin；六条 stdin 脚本的[提交源码补记](environment-test-dependencies-2026-10-08-evidence/evidence/submitted-stdin.json)来自本轮实际工具入参，并明确标注这一证据限制，不冒充启动时写盘原件。

## 归档及停止状态

`/workspace/scratch/fixture-recovery-new/` 映射到本档 `environment-test-dependencies-2026-10-08-evidence/`，保留对应相对路径。复制 76 个小原件、143470 bytes，逐文件核字节一致；另有一份[归档检查](environment-test-dependencies-2026-10-08-evidence/evidence/archive-check.json)。归档包括 33 组 meta/raw、来源与恢复结果、窗口/命令源码补记，以及五个小型执行/核对辅助脚本；未复制 binary、源码 ZIP/tree、cache 或依赖树，也未遍历生成全仓索引。

首阶段 33 个、后续 Go 阶段六个记录命令分别完成真实 `wait`，各阶段终结核对时其直接子 PID 均不在 `/proc`；这只覆盖本任务记录的命令，不声称全机进程清零或对其他任务做过 wait。下载保留的镜像、MinIO binary 和锁定 Node 安装供后继验收使用。本次没有需要清理的测试容器、网络、数据库或 listener。

后续根 Go 阶段追加 21 个小原件/辅助脚本、245088 bytes，另存[补充归档检查](environment-test-dependencies-2026-10-08-evidence/evidence/archive-check-root-go.json)。包括六组 meta/raw、前检/结果、两份锁输入、backend 原 graph 失败及三个小辅助脚本；首阶段 76 个原件及其检查未改写。两次归档均未复制 module cache，首次 all 的较长 raw 是实际命令输出，不是新建的全 cache 索引。

归档自查覆盖 JSON 可解析、全部 meta/raw SHA 对应、源/归档字节相等、相对链接及限定文本格式；执行者按 root 的禁 Git 授权未运行 `git diff --check`，由 root 整合时检查。原 raw 保持字节，包括 `minio-buildinfo.raw` 的 Go 输出末尾 tab、curl 原始回车进度，以及 ldd 的 tab 缩进；不为格式检查改写原证据。

root 实际 Git 格式核对：仅三个原 raw 命中——`minio-buildinfo.raw:3` trailing tab，以及 `harness-installed.raw:8`、`npm-installed02.raw:15` 的末尾空行；均已与实际原件逐字节核同，保留不规范化。curl 原 CR 不被此次 Git 报为错误。文档、其他原件与入口差量检查通过。
