# 2026-10-05 固定测试依赖恢复

> 历史记录：以下“当前”“已恢复”、实例名、临时路径、授权窗口和资源状态均指记录当时，不代表本次环境。继续开发先读[当前台账](tasks.md)与[团队流程](README.md)，只在追溯具体失败或依赖来源时读取本页；不重建旧哈希清单、归档或逐轮审批。

本轮在 `main@8872110099c84cf0600bb5b62cdcd6c0c6c843e3` 的固定输入上恢复依赖。两个 PostgreSQL 镜像、仓库既有 Go 依赖和精确 MinIO 二进制均已就绪；没有启动容器、网络、数据库或对象服务，本记录不构成真实集成验收。

依据为 [D05 研究第 2 节](../work-items/d05-object-storage-research.md#2-校验和与构建复现)、[PostgreSQL fixture](../../../tests/testsupport/postgres/fixture.go) 和 [对象 fixture](../../../tests/testsupport/objectstore/fixture.go)。实际命令、环境与工作目录见 [commands.json](evidence/dependency-recovery/commands.json)，结果和输入指纹见 [results.json](evidence/dependency-recovery/results.json)，关键原始输出见 [checks.log](evidence/dependency-recovery/checks.log)。

## 已恢复的固定输入

| 依赖 | 实际结果 |
| --- | --- |
| Go | `/workspace/toolchains/go1.27.1/bin/go`；`go1.27.1 linux/amd64`；SHA-256 `30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624` |
| PostgreSQL 17 / pgvector | `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`；匿名 pull exit 0，inspect 确认精确 RepoDigest、`linux/amd64` |
| PostgreSQL 16 不支持版本测试 | `pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782`；匿名 pull exit 0，inspect 确认精确 RepoDigest、`linux/amd64` |
| MinIO | `RELEASE.2025-10-15T17-29-55Z`；源码提交 `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`；Go 1.27.1/local 构建 exit 0 |
| MinIO 二进制 | 109289632 bytes；SHA-256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，与 fixture 完全一致；`--version` 通过 |
| 仓库 Go 依赖 | 官方 proxy/sumdb 下完整依赖图下载与 `go mod verify` 通过；显式下载原锁文件的 57 个唯一 module@version，97 行校验和值全部匹配；恢复原锁副本后离线下载、verify 和锁文件 SHA 检查均 exit 0 |

Docker 操作显式指定本机 `unix:///var/run/docker.sock` 和任务自有空 `DOCKER_CONFIG`，未读取既有 Docker 凭据；[docker-images.json](evidence/dependency-recovery/docker-images.json) 保留实际检查命令和结果。没有使用 onboarding 的旧 MinIO、既有数据库或运行中服务，也没有变更产品版本、fixture SHA 或仓库 `go.mod/go.sum`。

MinIO 的 module Sum、GoModSum、原始 ZIP、源码 `go.mod/go.sum` 和 SDK ZIP 均匹配 D05 研究中的固定值。构建使用 `GOOS=linux GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0`，以及原 `-mod=readonly -trimpath -buildvcs=false` 和完整版本 ldflags；未切换工具链或降低版本。

## 可复用缓存与重建入口

```sh
export AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go
export AGENTEAM_MINIO_BINARY=/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio
export GOMODCACHE=/workspace/agenteam-dependency-cache/modcache
export GOTOOLCHAIN=local GOENV=off GOWORK=off
```

后续执行者使用自己的 `GOCACHE` 与测试临时目录，并沿正式 fixture 获取本轮资源所有权。恢复执行者未获得或使用真实 fixture 执行权。

`/workspace/agenteam-dependency-recovery-5drj88a2/` 保存固定 MinIO 源码、独立 module/build cache、二进制、空 Docker 配置、本轮实际脚本和原始日志；`/workspace/agenteam-dependency-cache/` 保存仓库依赖缓存及锁文件副本。它们均为非敏感缓存，不纳入 Git，也不是运行服务。

缓存丢失时，先按 D05 研究第 2 节在新的任务目录执行固定源码下载和构建，再核对上述二进制 SHA；不要恢复已失效的历史 `/tmp` 路径。两个镜像使用表中精确 digest 匿名 pull。仓库依赖在新目录复制既有 `go.mod/go.sum` 后执行 `go mod download -json all` 与 `go mod verify`；具体环境和后续离线检查命令已保存在 `commands.json`，不需要修改仓库锁文件或执行 `go get/tidy`。

## 保留的失败事实与边界

首次仓库依赖 wrapper 的 `go mod download -json all` 和 `go mod verify` 均 exit 0，下载了依赖图中的 303 个 module；`all` 为工具等传递依赖在**任务自有副本**中增加了校验行，wrapper 最后的原锁 SHA 检查因此 exit 1：`go.mod: OK`、`go.sum: FAILED`。这不是仓库锁文件被改写，也不是下载或模块完整性校验失败。

扩展后的副本保留在本地 `logs/root-download-all-generated.go.sum`；随后仅恢复任务副本为仓库原字节，执行 `GOPROXY=off go mod download -json`、`go mod verify` 和 SHA 检查，全部通过。再按原锁显式补齐未被当前依赖图选中的历史版本，57 个唯一版本全部下载成功，97 行原校验和值匹配。大下载 JSON、完整构建日志和扩展副本保留本地；仓库只保存精简证据。

恢复结束时所有下载、pull、构建命令已退出，未创建服务资源。无剩余依赖恢复阻塞；真实 PostgreSQL/MinIO 行为、协议、迁移及产品测试仍由后续获授权验收执行者验证。
