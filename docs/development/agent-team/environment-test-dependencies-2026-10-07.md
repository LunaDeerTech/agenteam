# 2026-10-07 测试依赖恢复

固定测试依赖已恢复，主线程已核对[原始结果](environment-test-dependencies-2026-10-07-evidence/evidence/recovery-result.json)。本次仅完成依赖下载、构建及指纹/版本检查，未启动任何容器、服务、listener、数据库连接或产品测试，不构成产品验收，也不解除既有三项停止。恢复输入基线为 `6fa2ee721a75ea34a1ccd6523b8b25d68328c5b9`。

## 可用依赖与边界

- MinIO 注入路径：`AGENTEAM_MINIO_BINARY=/workspace/scratch/fixture-recovery/bin/minio`。二进制 109289632 bytes，SHA-256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`；实际 `--version` 为 `RELEASE.2025-10-15T17-29-55Z`，commit `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`，Go `1.27.1 linux/amd64`。
- 源自 Go module proxy 的 `github.com/minio/minio@v0.0.0-20251015172955-9e49d5e7a648`；[源码核对](environment-test-dependencies-2026-10-07-evidence/evidence/source-verification.json)保留 ZIP SHA、module/go.mod sum 及源码 `go.mod/go.sum` 指纹。ZIP SHA-256 为 `b137c35bf9708b4032a6a8301495a2563cab25111c28b80fd609812a3252a2f8`。
- 两个镜像均完成固定 digest pull 和 image inspect，记录为 `linux/amd64`。下表 `PG_VERSION` 是镜像配置，**不是运行中的 PostgreSQL 版本观测**；实际数据库和 fixture 尚未运行。

| 用途 | 固定镜像引用 | inspect 配置 |
| --- | --- | --- |
| PG17 | `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc` | `PG_VERSION=17.8-1.pgdg12+1` |
| PG16 | `pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782` | `PG_VERSION=16.12-1.pgdg12+1` |

原 module cache 只读使用；缺少的 `github.com/philhofer/fwd@v1.2.0`、`github.com/tinylib/msgp@v1.4.0` 仅下载到任务自有 `missing-modcache`，其 checksum、来源和实际自有路径见[缺件核对](environment-test-dependencies-2026-10-07-evidence/evidence/missing-module-verification.json)。未更改 fixture helper 或既定 SHA 契约。

## 十六条实际命令与原失败

每条 meta 原件保存实际 argv、cwd、env、PID、起止时间、退出码及 raw SHA；proxy 环境值按原记录仅标注存在。以下按启动时间排序，12 条退出 0、4 条退出 1，不将最终恢复写成首轮全绿。

| 命令记录（完整 meta） | 原始输出 | 退出码 |
| --- | --- | --- |
| [bwrap-go-version](environment-test-dependencies-2026-10-07-evidence/evidence/bwrap-go-version.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/bwrap-go-version.raw) | 0 |
| [pg17-pull](environment-test-dependencies-2026-10-07-evidence/evidence/pg17-pull.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/pg17-pull.raw) | 0 |
| [minio-source-zip](environment-test-dependencies-2026-10-07-evidence/evidence/minio-source-zip.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/minio-source-zip.raw) | 0 |
| [pg16-pull](environment-test-dependencies-2026-10-07-evidence/evidence/pg16-pull.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/pg16-pull.raw) | 0 |
| [minio-source-verify](environment-test-dependencies-2026-10-07-evidence/evidence/minio-source-verify.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/minio-source-verify.raw) | 0 |
| [minio-build-01](environment-test-dependencies-2026-10-07-evidence/evidence/minio-build-01.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/minio-build-01.raw) | 1 |
| [pg17-inspect](environment-test-dependencies-2026-10-07-evidence/evidence/pg17-inspect.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/pg17-inspect.raw) | 0 |
| [pg16-inspect](environment-test-dependencies-2026-10-07-evidence/evidence/pg16-inspect.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/pg16-inspect.raw) | 0 |
| [minio-build-02](environment-test-dependencies-2026-10-07-evidence/evidence/minio-build-02.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/minio-build-02.raw) | 1 |
| [missing-module-download](environment-test-dependencies-2026-10-07-evidence/evidence/missing-module-download.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/missing-module-download.raw) | 1 |
| [missing-module-download-02](environment-test-dependencies-2026-10-07-evidence/evidence/missing-module-download-02.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/missing-module-download-02.raw) | 0 |
| [minio-build-03](environment-test-dependencies-2026-10-07-evidence/evidence/minio-build-03.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/minio-build-03.raw) | 1 |
| [minio-build-04](environment-test-dependencies-2026-10-07-evidence/evidence/minio-build-04.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/minio-build-04.raw) | 0 |
| [minio-binary-sha](environment-test-dependencies-2026-10-07-evidence/evidence/minio-binary-sha.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/minio-binary-sha.raw) | 0 |
| [minio-version](environment-test-dependencies-2026-10-07-evidence/evidence/minio-version.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/minio-version.raw) | 0 |
| [minio-buildinfo](environment-test-dependencies-2026-10-07-evidence/evidence/minio-buildinfo.meta.json) | [raw](environment-test-dependencies-2026-10-07-evidence/evidence/minio-buildinfo.raw) | 0 |

原 build01 因缺件尝试在只读 cache 建 lock 失败；build02 的 overlay mount 返回 `Invalid argument`。首次缺件下载因无 GOPATH/HOME 无法定位 sumdb，补任务自有 GOPATH 后第二次成功；build03 因 `/dev/null: permission denied` 失败，build04 增加独立 `/dev` 后实际退出 0（77.59030146 秒）。四次失败均原样保存，未降低校验或改动依赖版本。

## 来源映射与归档检查

原始根目录 `/workspace/scratch/fixture-recovery/` 映射到[本档证据目录](environment-test-dependencies-2026-10-07-evidence/)，保留相同相对路径。[原 output-manifest.json](environment-test-dependencies-2026-10-07-evidence/evidence/output-manifest.json)保持 42 项原文，SHA-256 为 `6fa27b93e03b74e5f90ab19c94a426b4cafff1261375227606b109f303eb1604`；其中仅 40 项归档原字节，`bin/minio` 和 `download/minio.zip` 仅保留指纹，未复制二进制、ZIP、cache 或 module 源码。加上 manifest 自身，共保存 41 个原件、83116 bytes。

原件包含最终/旧版 `run.py`、`verify-source.py`、`finalize.py`、16 对 meta/raw、[输入清单](environment-test-dependencies-2026-10-07-evidence/evidence/input-manifest.json)、源码/缺件核对与最终结果，不新建另一套归档框架。归档时核对原 42 项 SHA、40 项保存字节及 manifest 原件、11 项仓库输入与两个 MinIO 源码文件未变，16 对 meta/raw 与最终结果一致；检查 JSON、相对链接和限定范围 `git diff --check`。本次文档归档不执行恢复脚本或产品测试，后续真实 fixture/业务验收仍须各自冻结输入并获对应运行授权。

空白检查例外：`minio-buildinfo.raw` 第 3 行保留 `go version -m` 原输出末尾 tab，`git diff --no-index --check` 对此报告 trailing whitespace；为保持原件 SHA 未做规范化。文档及其余 40 个原件无空白错误。
