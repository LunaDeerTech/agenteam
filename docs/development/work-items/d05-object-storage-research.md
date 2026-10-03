# D05 对象存储依赖与协议研究

- 修订：2；任务：R01 与 R02 补证；日期：2026-10-03；代码基线：`main@abf5c37`。
- 来源任务：[D05 主卡](d05-object-storage-artifact.md)。本文是依赖与隔离协议探针的实际记录，不是 D05 实现或独立验收结论；正式接口和状态机由 S01 确认。
- 仓库仅新增本文，没有修改 Go 依赖、产品源码、既有迁移或其他文档。
- 第 1–7 节保留 R01 事实；第 8 节追加无条件零字节 tombstone 和只读 bucket 配置补证。R01 修订 1 文件 SHA 为 `1f14db3777fc3da3eb620c5a944a28345765c3e14d635f6a6c84d21659ac10b0`。

## 1. 推荐组合与来源边界

建议 D05 的可复现实现/fixture 候选采用：官方 MinIO `RELEASE.2025-10-15T17-29-55Z` 的固定源码提交，由精确 Go 1.27.1/local 构建；客户端固定 `github.com/minio/minio-go/v7 v7.3.0`。本次实际运行的容器是既有固定基础镜像加只读挂载的自建 MinIO 二进制，不是已拉取的官方 MinIO 镜像。没有取得可核验的官方 MinIO image digest，不能将基础镜像 digest 写成 MinIO 官方 digest。

| 项目 | 实际固定输入/结果 |
| --- | --- |
| MinIO release 对应源码提交 | `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a` |
| Server module version | `github.com/minio/minio v0.0.0-20251015172955-9e49d5e7a648` |
| Server 源码 Go 声明 | `go 1.24.0`、`toolchain go1.24.8`；本次强制 `GOTOOLCHAIN=local` 使用 1.27.1，未切换工具链 |
| 实际 server 输出 | `minio version RELEASE.2025-10-15T17-29-55Z (commit-id=9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a)`；`Runtime: go1.27.1 linux/amd64` |
| Server binary SHA-256 | `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`；109289632 bytes |
| SDK | `github.com/minio/minio-go/v7 v7.3.0`，官方 module proxy 记录时间 `2026-08-15T19:15:42Z`、提交 `ce0e323c55c64964e6ad820ef0c6f5b286446aae` |
| SDK Go 声明 | `go 1.25.0`；已在独立临时 module 用 Go 1.27.1 编译并实际访问 server |
| 实际容器基础镜像 | `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`，已验收的 `linux/amd64` 镜像；覆盖 entrypoint，仅运行挂载的 MinIO，不启动 PostgreSQL |
| 实际环境 | Linux/amd64、GOAMD64=v1、Docker 28.4.0；server `CGO_ENABLED=0`；协议探针在宿主执行，SDK binary 的 CGO_ENABLED=1 |

来源访问的限制也属于结论：

- GitHub release API 和 Docker Hub 网页被环境代理返回 403，未读到在线 release/security 公告。
- Quay 标准匿名 Bearer 获取 token 返回 200，随后 `repository:minio/minio` manifest 请求仍返回 401。CLI 对 2025-10-15、2025-09-07、2025-04-22 三个候选 tag 显示 `no such manifest`；这不能证明 tag 不存在。Docker Hub registry 匿名访问也被拒绝，没有使用已有 Docker 凭据。
- `dl.min.io` 的对应 binary/archive 请求返回 410。没有更改代理、认证或网络限制。
- 合法替代来源是 [Go module proxy 的固定 server 信息](https://proxy.golang.org/github.com/minio/minio/@v/v0.0.0-20251015172955-9e49d5e7a648.info)与[固定源码 ZIP](https://proxy.golang.org/github.com/minio/minio/@v/v0.0.0-20251015172955-9e49d5e7a648.zip)，以及 [SDK 信息](https://proxy.golang.org/github.com/minio/minio-go/v7/@v/v7.3.0.info)、[SDK go.mod](https://proxy.golang.org/github.com/minio/minio-go/v7/@v/v7.3.0.mod)、[SDK ZIP](https://proxy.golang.org/github.com/minio/minio-go/v7/@v/v7.3.0.zip)。下载经 `sum.golang.org` 校验。
- 固定 server 源码 `README.md:28–47` 明确社区版采用 Source-Only Distribution，legacy binaries 不再维护；不是从 401/410 推测得出。源码保留 AGPLv3 许可，SDK 为 Apache-2.0。

以上推荐用于固定可复现基线，不宣称它是在线最新版本、已完成最新安全公告核验或已完成生产部署认证。D28 的自建镜像、TLS/访问控制、持久卷和升级流程仍须正式验收。

## 2. 校验和与构建复现

| 输入 | 值 |
| --- | --- |
| Server module Sum | `h1:6TdolSCLSs2nwm8i0PpWDqf9iX2Ty9WQK8wmr7dCnUM=` |
| Server GoModSum | `h1:yCWDkwWO9IWpGsT4mreDDN/B/QVmK2zC666uInRAcqE=` |
| Server 原始 ZIP SHA-256 | `b137c35bf9708b4032a6a8301495a2563cab25111c28b80fd609812a3252a2f8`；25342606 bytes |
| Server go.mod SHA-256 | `673f06144e90bc045f0a20050d2874c52e551be5bfbd70dff6e76b66da0db702` |
| Server go.sum SHA-256 | `86e062349c7abdce0465561bb409d410a95b00b0969ba05d5fb5f2e3550a5cd4` |
| SDK module Sum | `h1:HM4pFCSQq/TK+j0/zmorSh5ddh81iDgRgU0BG0Vz/YU=` |
| SDK GoModSum | `h1:KUPWdecEO1LWyUz+sTGXAuf2jZHrPh5fCsRH86QbPfk=` |
| SDK 原始 ZIP SHA-256 | `3eae423e698f12a98febb5464525e882e1a4bf05da2a9b40f24ef2003a34c3f6`；594178 bytes |

以下为实际构建参数的可复现写法；目录是新任务临时目录，不使用仓库 module，不读取环境 DSN 或部署凭据。version flags 注入的是已经从固定源码解析确认的 release/commit，不是官方二进制签名的替代品。

```sh
research_dir=$(mktemp -d /tmp/agenteam-d05-build.XXXXXX)
research_go=/workspace/toolchains/go1.27.1/bin/go
export GOTOOLCHAIN=local GOENV=off GOWORK=off
export GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org
export GOMODCACHE="$research_dir/modcache"
export GOOS=linux GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0
mkdir "$research_dir/bin"
cd "$research_dir"
test "$("$research_go" env GOVERSION)" = go1.27.1
"$research_go" mod download -json \
  github.com/minio/minio@v0.0.0-20251015172955-9e49d5e7a648 \
  > server-source.json
cd "$GOMODCACHE/github.com/minio/minio@v0.0.0-20251015172955-9e49d5e7a648"
"$research_go" build -mod=readonly -trimpath -buildvcs=false \
  -ldflags '-s -w -X github.com/minio/minio/cmd.Version=2025-10-15T17:29:55Z -X github.com/minio/minio/cmd.ReleaseTag=RELEASE.2025-10-15T17-29-55Z -X github.com/minio/minio/cmd.CommitID=9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a -X github.com/minio/minio/cmd.ShortCommitID=9e49d5e7a648 -X github.com/minio/minio/cmd.CopyrightYear=2025' \
  -o "$research_dir/bin/minio" .
"$research_dir/bin/minio" --version
sha256sum "$research_dir/bin/minio" go.mod go.sum
```

SDK 使用独立 `module example.invalid/agenteam-d05-r01`，`go 1.27.1`，唯一直接依赖为 `github.com/minio/minio-go/v7 v7.3.0`。实际执行 `go mod tidy`、`go build -trimpath`，使用同一 local/proxy/sumdb 设置；没有执行仓库 `go get/tidy`。固定的临时 SDK `go.mod` SHA 为 `03ec44378627e6d988a57b4c5de01b1fade40ff56870dc51ab569924123f43de`，`go.sum` SHA 为 `cb280c54a5e0efc9becfbd50617e71c574be85dfc58212221fc457d51f6c9bfd`。

## 3. SDK 运行依赖与选项边界

实际 `go list -deps -json` 得到 19 个外部 module（包含 SDK）、42 个外部 package。选中 module 的 ZIP 合计 55274978 bytes，展开源码含测试/测试数据合计 104407555 bytes；其中 compress/text 占主要部分。这是缓存体积，不是常驻内存或产品 binary 增量。完整协议探针 binary 为 12301734 bytes；没有测量生产 RSS 或性能。SDK go.mod 中的工具/linter 依赖不进入这个编译闭包。

| Module | 固定版本 |
| --- | --- |
| github.com/minio/minio-go/v7 | v7.3.0 |
| github.com/cespare/xxhash/v2 | v2.3.0 |
| github.com/dustin/go-humanize | v1.0.1 |
| github.com/google/uuid | v1.6.0 |
| github.com/klauspost/compress | v1.19.2 |
| github.com/klauspost/cpuid/v2 | v2.4.0 |
| github.com/klauspost/crc32 | v1.3.0 |
| github.com/minio/crc64nvme | v1.1.1 |
| github.com/minio/md5-simd | v1.1.2 |
| github.com/philhofer/fwd | v1.2.0 |
| github.com/rs/xid | v1.6.0 |
| github.com/tinylib/msgp | v1.6.4 |
| github.com/zeebo/xxh3 | v1.1.0 |
| go.yaml.in/yaml/v3 | v3.0.5 |
| golang.org/x/crypto | v0.55.0 |
| golang.org/x/net | v0.58.0 |
| golang.org/x/sys | v0.47.0 |
| golang.org/x/text | v0.41.0 |
| gopkg.in/ini.v1 | v1.67.3 |

Server 自建所需依赖只用于独立 server 编译，不应作为 Central 的 module 依赖引入。SDK 的 RDMA 可选路径未启用，本次不需要 libminiocpp 或其他新 native SDK。

本次 SDK client 显式设置固定 `Region`、静态随机临时凭据、`TrailingHeaders:true`、`MaxRetries:1`，Transport 不读环境代理。存储 endpoint 是已核验的 owned 私网容器 IP，属于可信部署通道；没有复用或修改 D04 运行时 loopback/private 分类。

## 4. 真实协议观察

全部操作只访问同一个任务 nonce 的隔离 MinIO，使用随机临时 root 凭据和新 bucket/key。没有使用已有基础设施。下表为最后完整协议轮的结果；不是仅凭 SDK 文档推断。

| 场景 | 观察与约束 |
| --- | --- |
| 已知长度非 seek 单 PUT | `DisableMultipart:true`、SDK 自动 SHA256，生成式 Reader 上传 8388611 bytes；最大传入 Reader 缓冲 65536 bytes；HEAD 长度正确且 checksum 等于独立累计的全文 SHA256 |
| 预计算 SHA 单 PUT | 9437191 bytes；`UserMetadata["x-amz-checksum-sha256"]` 为可信预计算 base64 SHA，`DisableMultipart:true`、`SetMatchETagExcept("*")`；最大 Reader 缓冲 65536 bytes，HEAD 全文 SHA 相等，第二次同 key 返回 `PreconditionFailed` |
| 流式 GET / Range | GET 流式读回 8388611 bytes、独立 SHA 相等；Range 5–12 得到精确 8 bytes |
| HEAD / ETag | 单 PUT 的 ETag 为 32 位 hex，不是 SHA256。HEAD 只有请求 checksum 且对象保存了相应 checksum 时才返回该事实，不能把任意自定义 metadata 当服务端校验结果 |
| 多余/不足输入 | 非 seek Reader 提供 8 bytes、声明 3，返回 client error 且 HEAD `NoSuchKey`；仅提供 3、声明 8 同样失败。但 seekable Reader 提供 8、声明 3 时 SDK 成功存前 3 bytes，因 SDK 构造了 SectionReader；业务 adapter 必须独立落实精确长度/尾随检查 |
| 普通 PresignedPutObject | 只签 `host`；同 URL 首次与重复均 200，第二份内容覆盖前一份；没有一次性、长度或 checksum 绑定保证 |
| PresignHeader PUT | 签 `content-length;host;if-none-match;x-amz-checksum-sha256`：首次 200，同 key 重复 412 `PreconditionFailed` |
| 校验失败 | 保持签名 SHA 而更改同长度 body：400 `XAmzContentChecksumMismatch`，随后 HEAD `NoSuchKey`；第一轮曾仅收到 client error，不能要求所有网络路径都返回确定的 400 |
| 签名头篡改/遗漏 | 更改 SHA 或 Content-Length 返回 403 `SignatureDoesNotMatch`；遗漏 SHA 返回 400 `AccessDenied` |
| 删除后的 URL 重放 | 删除条件 PUT 对象后，原尚未过期 URL 再次 PUT 返回 200。`If-None-Match:*` 只保护当前对象存在性，不是消耗型 token |
| Presigned GET / 过期 | 有效 GET 返回正确内容；1s URL 在超过有效期后新发 GET 返回 403 `AccessDenied`；SDK 拒绝小于 1s 或大于 7d 的 expiry |
| CopyObject 源条件 | 错误 `CopySrcOptions.MatchETag` 返回 `PreconditionFailed`；正确值可复制内容；调用设置了 ReplaceMetadata/UserMetadata，但本次未另读回 metadata 验证 |
| CopyObject 目标 | 目标已有不同内容时仍被覆盖。公开 `CopyDestOptions` 没有 destination `If-None-Match` 参数；不能用它宣称发布目标天然不可变 |
| Multipart 完整性 | 6MiB、PartSize=5MiB、NumThreads=1 得到 2 parts。ETag `39196c287c83de246893d27411b64dba-2`；HEAD SHA `Xbsl+NXf9kWp2vPEIu18/TBiyi5XKdQ00dXnMTBO3xI=-2`，不等于全文 SHA；最大 Reader 缓冲为 5MiB |
| Multipart 取消 | 在 part 1 已真实返回 200 后取消原 ctx；PUT 返回 canceled，HEAD `NoSuchKey`，仍有 1 个 incomplete upload。fresh、5s 有界 `RemoveIncompleteUpload` 后残留为 0 |
| 删除 | 重复 RemoveObject 均成功，随后 HEAD `NoSuchKey`；删除的成功响应和后续真实 absence 分开记录 |

单 PUT 的流式依据包括实际非 seek Reader 路径与固定源码 `api-put-object-streaming.go:658–731`：`SendContentMd5:false` 时不走按对象 size 分配缓冲的分支。开启 `SendContentMd5` 对非 seek 输入会分配整个 size 的缓冲；因此不能仅因 API 参数为 Reader 就声称流式。64KiB Reader 观察本身也不是全进程内存上限证明。S01 拟定的 ≤1GiB、已知 length、DisableMultipart 路线与上述证据相容，但平台上限仍由 S01 正式确认。

预计算 SHA 通过 SDK 的受信 options 写入真正的 `x-amz-checksum-sha256` 头（`api-put-object.go:249`），不是 `x-amz-meta-*` 自述。HTTP caller 使用预签 URL 时也须设置真实 request ContentLength 和相同校验头。业务调用者不应直接获得这些 SDK options。

## 5. 慢条件 PUT 与 fence

额外的有界探针使用两个独立 owned key，分别预备“保留 fence”和“若 fence 先成功则删除 fence”路径。旧 PUT 签名绑定总长 280000 bytes、全文 SHA 与 `If-None-Match:*`，在真实 TCP socket 写出前 140000 body bytes 后暂停；并行发出同 key、零字节、相同条件的 fence PUT。

两次观察一致：

1. 在 500ms 观察窗口内，fence 未完成，旧 body 仍暂停。
2. 释放余下 140000 bytes 后，旧 PUT 返回 200；fence 最终返回 `PreconditionFailed`。
3. 最终 HEAD 为 280000 bytes，全文 SHA 与旧 payload 相等。
4. 因 fence 从未先成功，“成功后删除”分支没有执行。此记录不能证明成功 fence 后删除时不存在已启动旧 PUT 的迟到提交，也不能将观察窗口内等待推广为所有入站请求已经静默。

这支持该固定 server 在所观测的 single PUT 竞争中执行互斥/条件判定，但不把 `HEAD 404`、URL 过期、客户端取消或一次 fence 成功升级为“所有旧 PUT 已结束”的证明。未测试成功 marker 的 If-Match 替换、headers/body 其他迟到排序或已开始请求跨 URL 过期后的完成行为；这些如果成为 S01 正式收敛算法，需要单独验证。

## 6. Retry、Copy 与恢复建议

以下为固定 v7.3.0 源码观察，未增加丢响应代理实验：

- `retry.go:34` 默认 MaxRetry=10；`api.go:680–745` 对可 seek body 可在网络错误后重试，非 seek body 仅一次；`retry.go:138` 对多数网络错误允许重试。
- 已提交的条件 PUT 若丢失响应，后续重试可能得到 412。412 不能反证第一次未提交，也不能直接作为业务失败事实。建议显式 `Options.MaxRetries:1`，把未知结果交领域恢复，以固定 object identity、预期 length 和受服务端验证的全文 SHA 核实；HEAD 404 仍不足以排除未结束旧请求。
- `api-put-object-streaming.go:124` 等失败 defer 用原 ctx 调 Abort；取消后不保证清理，本次真实残留已证明。若以后允许 multipart，需要持久化 attempt/upload 身份、独立有界清理和恢复；当前 ≤1GiB single PUT 可以避免主动引入该复杂度。
- `api-compose-object.go:199` 的 CopySrcOptions 支持 `VersionID`、MatchETag/NoMatchETag 和时间条件；本次只实际验证 MatchETag。VersionID 的版本化 bucket 行为未验证。
- Copy 的源 ETag 条件只解决源条件匹配，不能代替全文 SHA，也不保护目标不被覆盖。staging 到独立 canonical key 的发布仍需领域唯一身份、当前引用/lease/发布锁序和复制后完整性核实，不能把 Copy 成功当 DB 提交成功。
- 标准预签 URL 是 bearer capability；它不会查询 Central 的 grant/权限/撤销表，也不能识别持有人是否为指定 Runner。Runner/operation 绑定来自正式受信通道和领域状态；GET 多次使用、已经开始的请求、删除后条件 PUT 重放等边界不能被“单次授权”措辞掩盖。

## 7. 复用证据与资源清理

非敏感构建缓存/源码保留在 `/tmp/agenteam-d05-r01-hxwb6ogm/`，供后续 fixture 复用；它不是运行中的服务。目录包含：

- `bin/minio`：上述固定源码构建的 server；`server-source.json`、`server-build.log` 与 `modcache/` 保存精确下载/构建输入。
- `module/go.mod`、`module/go.sum`、`module/main.go`：完整协议探针。main.go SHA `617a58b39b810fd49d1c510ca83eb63e2ee413c5da5a7fe845161b6f2e502a89`。
- `module/fence/main.go`：慢 PUT/fence 探针，SHA `9e9676771a50341d94a173a826b37571da908067026767cfabec69906fe52614`。
- `run_fixture.py`：owned fixture 创建/校验/清理器，SHA `3744f7dbc878d0b5cf547140fdfdb938877da06648488708c67cdbd065ca61b3`。生成随机凭据到 0600 文件，以宿主 UID/GID 运行；检查 exact container/network ID、nonce label、image、挂载来源、内部网络与实际 IP。
- `protocol-third.log`：最后完整协议结果，SHA `3432d37597bf6167de5a91b8953f5819d0f7394d8106dde7c245441642c4e549`；含该轮创建/清理记录的 `fixture-run-third.log` SHA `a6668270d19801e07b1f22af7efea5df72d8b098da17d03322417002e8f808f5`。
- `protocol.log`：慢 PUT/fence 结果，SHA `3b6d45bedc36496c734799580e1ce71b3ffb8a2aa861892397720b551b57b769`；`fixture-run.log` SHA `e24a69a4ba757415392f713ff82ed88b9956b9ab58ddaa876fe4d92dec372688`。

复用时先核对上述文件 SHA；源码构建/SDK build 命令均须继续精确工具链。fixture 调用形式为 `python3 <owned-cache>/run_fixture.py sdk-probe` 或 `fence-probe`；helper 内每轮新生成临时凭据，不在命令或报告中写出凭据/完整签名 URL。网络为 owned `--internal`，不发布宿主端口，health 与 SDK 只连接已核验的容器私网 IP。

本任务唯一资源归属 nonce 为 `3b818b801c22b067572d551d18ae5255`（标签标识，不是访问凭据）；四轮串行复用该任务归属，每轮 container/network exact ID 单独核验。首轮 protocol 完成后，容器 root 写出的 data 子目录导致宿主 unlink 权限失败；仅为该 owned 挂载创建 `--network none` 的精确 cleanup helper 清空，然后删除 helper。后续三轮改用宿主 UID/GID，完整 wrapper 均 exit0。

最终核验该 nonce 下 Docker 容器和网络均为 0，`data/`、`server.env`、`probe-config.json` 均不存在。没有 prune、没有接触其他资源或现有凭据；保留的日志不含访问密钥、secret、token 或完整签名 URL。全部命令停止，Docker 资源可顺序交回。

未验证范围明确保留：官方 MinIO image digest、最新在线安全公告、生产 TLS/证书与 Runner 可达性、持久卷崩溃/多节点/版本化对象、SDK 丢响应实测、成功 fence 删除后的迟到写入排除、D05 DB/对象跨系统领域恢复和产品授权。本文不把这些标为支持或已验收。

## 8. R02：保留零字节 tombstone 的单项补证

root 为 S01 采纳的候选是：cleanup gate 关闭后，以无条件零字节 PUT 替换需要永久拒绝旧写入的随机对象 key；所有业务 payload PUT 仍须携带 `If-None-Match:*`，有迟到写/旧 grant 的 key 首版不自动删除 marker。marker key 不含 Project、Owner、名称或正文，marker body 为空。本次只验证 MinIO 协议，不模拟或证明尚未实现的 cleanup gate、领域事务和授权。

### 8.1 实际并发顺序与完整读取

使用新的 owned bucket 和 20 个随机字节生成的 opaque hex key，沿用第 1 节精确 server binary、SDK、Go 与基础镜像。全探针 context 20s，旧 socket deadline 7s，marker PUT deadline 5s，观察窗口 500ms；SDK `MaxRetries:1`。没有压力测试或追加 If-Match 枚举。

1. 旧条件 payload PUT 的签名绑定 280000 bytes、全文 SHA 和 `If-None-Match:*`。实际 socket 写出前 140000 body bytes，然后暂停余下 body。
2. 并发发起同 key 的无条件零字节 PUT：`DisableMultipart:true`，携带预计算的空 SHA 头，不设置 If-Match/If-None-Match，没有业务 metadata。
3. marker 在 500ms 观察窗口内没有完成。以 probe 起点计，旧 body 于 `504906µs` 释放，旧请求的 200 响应于 `507127µs` 被读到；marker 于 `533380µs` 返回成功且 Size=0。时间是客户端实际观测，不是服务端内部提交 trace。
4. 两请求结束后，HEAD 长度为 0、SHA 为 `47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=`；完整 GET 读到 0 bytes，独立计算 SHA256 为 `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`。
5. 使用仍有效的原条件签名 URL 重放原正文，返回 412 `PreconditionFailed`；通过 SDK `SetMatchETagExcept("*")` 重放也返回 `PreconditionFailed`。随后再次 HEAD 和完整 GET，长度与空 SHA 仍相同，正文没有恢复。
6. 协议流程没有删除 marker。仅在探针进程结束、socket 已关闭后，通过整套 owned fixture teardown 清除临时数据卷。

这一有界时序实际支持“旧写完成后，无条件 marker 覆盖正文；marker 存在时旧条件 grant 不能恢复正文”的候选。它不证明删除 marker 后安全，也不证明所有 HTTP 到达排序、已接受但尚未执行的服务端任务、多节点或崩溃恢复；第 5 节的未验证分支保持未验证。要沿用该结论，正式实现必须保持 payload 的条件写规则和 marker 存在性，不能把 marker 后的零字节读取再解释为可以物理删除 key。

成功加完整空读只支持 payload 已被替换为空这一事实，不证明 active/unknown lease 已结束。正式 lease 仍须由其事实所有者确认可信 terminal，不能据此宣称整体清理完成。

### 8.2 专用 bucket 的只读配置结果

bucket 仅通过普通 `MakeBucket` 创建，没有启用 versioning、ObjectLock、retention 或 lifecycle；后续只读查询没有修改其配置。记录 SDK 返回和实际 HTTP/XML code，避免把 API error 与空配置混为一谈。

| 查询方法 | 实际结果 | 解释边界 |
| --- | --- | --- |
| `GetBucketVersioning(ctx,bucket)` | HTTP 200；SDK error=nil；Status=`""`、Enabled=false、Suspended=false | 当前是从未启用版本的专用 bucket；不能仅用 `!Enabled()` 接受 Suspended 状态，因为历史版本是不同事实 |
| `GetObjectLockConfig(ctx,bucket)` | HTTP 404 / `ObjectLockConfigurationNotFoundError`；SDK 同错误码；objectLock 为空，默认 mode/validity/unit 均 nil | 此 bucket 没有 ObjectLock 配置；公开 API 同时返回 ObjectLockEnabled 与默认 retention，比只读 mode 的兼容 wrapper 信息完整 |
| `GetBucketLifecycle(ctx,bucket)` | HTTP 404 / `NoSuchLifecycleConfiguration`；SDK 同错误码、configuration=nil | 实际是明确不存在配置，不是一个无错误的空 Rules 列表；没有自动过期 marker 的规则 |
| `GetObjectRetention(ctx,bucket,existingMarker,"")` | HTTP 400 / `InvalidRequest`；SDK 同错误码、mode/until=nil | 对已经存在的 marker 查询；bucket 未启用 ObjectLock，不能把任何 400/InvalidRequest 都归并为“没有 retention” |

对应固定 SDK 实现为 `api-bucket-versioning.go:115`、`api-object-lock.go:187`、`api-bucket-lifecycle.go:105`、`api-object-retention.go:146`。本次没有额外制造权限失败流量。S01 的配置检查必须只接受已定义的成功值/明确缺失码；AccessDenied、认证错误、超时、网络错误及其他未知返回均是配置不可核验，不能当作 disabled。实际 bucket 版本/锁/lifecycle 状态是本轮原始观察，不代表配置以后不能被部署管理员改变。

### 8.3 输入、指纹与清理

R02 非敏感缓存为 `/tmp/agenteam-d05-r02-ctxv0uvu/`，Go module cache 复用 R01 已校验内容；独立 module 的 go.mod/go.sum 与第 2 节指纹相同，server binary SHA 也未变。实际命令在该独立 module 执行，未触碰仓库依赖：

```sh
# 本轮已核验的非敏感缓存；重新执行仍须先取得 Docker 资源所有权。
r02_cache=/tmp/agenteam-d05-r02-ctxv0uvu
cd "$r02_cache/module"
GOTOOLCHAIN=local GOENV=off GOWORK=off \
GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org \
GOMODCACHE=/tmp/agenteam-d05-r01-hxwb6ogm/modcache \
/workspace/toolchains/go1.27.1/bin/go build -trimpath \
  -o "$r02_cache/tombstone-probe" .
python3 "$r02_cache/run_fixture.py" tombstone-probe
```

完整 fixture wrapper **exit0**。稳定证据：

| 文件（相对 R02 cache） | SHA-256 |
| --- | --- |
| `module/main.go` | `2cbd1b12050a0a601f6d782b005ed4c4713be5361f064f3ca68a06e1d0a987db` |
| `run_fixture.py` | `bf84e1489695e228ad7d59dfa5b6fcaca753e5cc185b4d880530d9dd70e874f6` |
| `tombstone-probe` | `234819734f907928f9945408fbe557cbb256e5f89e7db7c6094b230ceea7ba78` |
| `protocol.log` | `00792a0bbb386d90511409e2cf45d32d083eb952f066ed91be9d63b922b33cf7` |
| `fixture-run.log` | `7d6d3f6de659c7d21a3caf69e4ab2bd4004b4a5d660e4829cc495d20dc1da1fa` |
| `fixture-metadata.json` | `2517528c73adab5367404e3dc687e3291d88d6dcdea478f3ef581bbd12ea2c6a` |

新归属 nonce 为 `1e67a8f347f9b7a26a4d9c14f6db2ad4`；容器 `e7055291c3976e116af23ebbe898fe657f1310c405cc0c2dfd3ecd2c6a84ee79`、网络 `496530e3a2eb4e924667822ccb64d66373580c24905f953c0c7126ceb5ea1baf` 均按 nonce/名称/image/挂载/exact ID 校验后删除。最终精确 label 查询返回容器与网络各 0，`data/`、`server.env`、`probe-config.json` 均不存在。没有临时凭据或完整签名 URL 写入报告；非敏感源码、binary 与日志缓存按授权保留。

R02 完成后停止全部命令与写入，Docker 资源交回。除这一单项补证及上述只读配置查询外，没有追加其他实验。
