# D05 对象存储与 Artifact 实施规格

- 修订：2；业务输入 `main@abf5c37`、[D05 主卡修订 1](d05-object-storage-artifact.md)，复审时 `main@08accd3` 仅增加研究/进展记录；本文是 S01 设计，不是实现或验收通过声明。
- 依据：[对象存储](../../architecture/platform-infrastructure/object-storage.md)、[Artifact](../../architecture/tool-system/artifact-tools.md)、[D01 资源](d01-contracts/resources-skills.md#对象与业务引用)、[生命周期](d01-contracts/domain-lifecycle.md)、[权限/幂等/Tx](d01-contracts/foundation.md)、[部署运行](../../architecture/platform-infrastructure/deployment-runtime.md)。
- 已核对实际 [D03 Tx](../../../internal/central/postgres/transaction.go)、[identity](../../../internal/central/identity/contract/identity.go)、[Audit contract](../../../internal/central/audit/contract/types.go)/[授权](../../../internal/central/audit/service.go)、[cursor](../../../internal/central/cursor/cursor.go)、[Central](../../../internal/central/app/app.go)。复用 Go **1.27.1 / GOTOOLCHAIN=local**、pgx **5.11.0**、Goose **3.28.0**、既定 PG17.8/vector0.8.1 fixture。

## 1. 分块与所有权

按以下完整结果串行实施并独立验收。root 拥有主卡/计划/台账；以下是实现阶段所有权建议，S01 仅写本文。

| 块 | 结果与独占新增范围 | 全局迁移/公共改动 |
| --- | --- | --- |
| B01 对象与可靠存储 | `internal/central/object/`（含 `contract/`、MinIO adapter、spool、引用/lease、上传/清理恢复）；`tests/objects/` 的 object 场景；`tests/testsupport/objectstore/`；`scripts/test-objects.sh` | `00005_object_storage.sql`；本节列明的最小 Audit/identity 扩展；`go.mod/go.sum` 仅本块锁定已核验 SDK/依赖 |
| B02 Artifact 与浏览器访问 | `internal/central/artifact/`（含 `contract/`）；`internal/central/object/download*.go` 及测试；`tests/objects/` 的 Artifact/download 场景 | `00006_artifact_download.sql`；D04 cursor 直接复用，不改变原编码；B01 object 仅增加本文已固定的组合接口 |
| B03 Runner transfer 与进程 | `internal/central/object/transfer*.go` 及测试；`tests/objects/` 的真实直传/恢复场景；Central MinIO 初始化/健康/关闭 | `00007_object_transfer.sql`；配置/进程/fixture 文档与本节共享范围串行移交 |

共享文件按 B01→B02→B03 单作者移交：`internal/central/config/`、`internal/central/app/`、`internal/platform/logging/` 的中立阶段/错误枚举及测试、`tests/process/`、`tests/testsupport/postgres/cmd/fixture/main.go`、`tests/testsupport/outbound/cmd/fixture/main.go`、`docs/development/backend/README.md`、`AGENTS.md`。fixture 调度改动仅为给真实 Central 进程提供全部必需的 owned PG/MinIO；不回退 D04 入口验证。`00001–00004` 冻结，不改原 SQL/校验历史；新迁移均为 D03 的事务 migration。

必要公共扩展由 B01 同一作者完成，随后冻结，不能借已有 Secret/Outbound 身份替代对象职责：

| 现有文件 | 必要增量及限制 |
| --- | --- |
| `identity/contract/identity.go` 及测试 | 闭集增加 `object`、`object-maintenance` ServiceName；只在组合根注册固定 cause，未授予 Owner/admin |
| `audit/contract/types.go` 及测试 | 增加 object upload/delete/transfer 与 Artifact create/list/read/download 动作、`stored_object/object_transfer/artifact/artifact_collection` resource、`object/artifact` producer；验证 action/resource/scope 对应，集合 resource ID 为 ProjectID |
| `audit/contract/metadata.go` 及测试 | typed Object/Artifact metadata 构造与 DB Decode 白名单：安全 ID、source kind/ID/revision、media_type、byte_size、固定 phase/reason/count、非负 sent_bytes；无 name/description/正文/key/URL/token |
| `audit/service.go` 及测试 | producer/cause 精确映射；Agent 仅增已授权 Artifact 动作；Human 的新增 Artifact list/read/download 使用 Read intent，其余 D04 action 语义保持；仍逐次 Session/Owner 和 `CheckAppendInTx`，不以新 action 绕过原业务权限 |
| `internal/central/foundation/fault.go`、`internal/central/httpapi/problem.go`、`api/openapi/common.json`；`foundation/scalar_test.go`、`httpapi/httpapi_test.go`、`httpapi/schema_test.go` | 增加 `OBJECT_PAYLOAD_MISSING`→503、`OBJECT_INTEGRITY_MISMATCH`→502、`RANGE_NOT_SATISFIABLE`→416；错误枚举、HTTP安全文案与schema同步，不新增业务route，不输出SDK原错误 |
| `00005_object_storage.sql` | 追加更新 Audit action/resource/producer/service/相关 resource_id CHECK，核实旧约束名，保留全部旧合法数据；不重写 `00002` |

固定动作闭集：`object.upload.complete/failed`、`object.delete`、`object.transfer.issue/complete/revoke`；`artifact.create/list/read/download`。object 技术事件由 object/object-maintenance Service 以本域已持久 operation/cause 产生、metadata 保留安全 initiator ID，业务当前授权仍在同一 Tx 执行；普通 Avatar 的 System 分区不能误要求管理员。Service 只使用 object producer，不能生成 Artifact 业务行为或浏览 Audit；Artifact 事件保留实际 Human/Agent。Artifact read/list/download 在 archived 项目仍是合法只读及读取审计；同 Actor 新建/改写仍拒绝，管理员不代 Owner。Audit 失败/unknown 时不交付尚未返回的列表/预览/下载字节；已开始传输的故障按真实阶段记录，不把已发送说成未开始。

## 2. 依赖与部署通道

SDK 固定 `github.com/minio/minio-go/v7` **v7.3.0**；R01 已用 Go1.27.1 编译，运行依赖共 19 个模块，具体校验和/间接版本按[研究报告](d05-object-storage-research.md#2-校验和与构建复现)锁定，MVS 对旧依赖的升级须记录并验证 D03/D04。MinIO 固定官方 `RELEASE.2025-10-15T17-29-55Z`、commit `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`，module `v0.0.0-20251015172955-9e49d5e7a648`；源码 ZIP SHA256 `b137c35bf9708b4032a6a8301495a2563cab25111c28b80fd609812a3252a2f8`、Go1.27.1/local 构建 binary SHA256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`。

fixture 使用 R01 的固定 `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc` linux/amd64 基础镜像，仅执行只读挂载的 MinIO binary、不启动 PG；它**不是官方 MinIO image digest**。构建 flags、模块校验和和缓存核对沿研究报告；缓存不存在须按固定输入重建，不能浮动下载或跳过测试。D28 负责正式部署镜像/持久卷/升级；本次不宣称最新安全公告已核验。

R01 已观察：普通 presigned PUT 只签 host，可重放覆盖；`PresignHeader` 签 length/SHA/`If-None-Match:*` 首写成功、重放 412、改签头 403。multipart ETag/checksum 可能是 composite，不能当全文 SHA256；取消 multipart 可能留下 incomplete，原取消 ctx 的 abort 不足以清理。故首版统一 **≤1 GiB、明确 DisableMultipart 的单 PUT**，不提供 multipart/resumable UI/Tool，不生成 multipart grant。

R01 的 single PUT/非 seek 流已验证长度、range、全文 SHA；SDK 对 seekable Reader 可能只存声明长度的前缀而成功。平台必须自己检查原始流的短读及额外尾字节，不能让 SDK 的 SectionReader 替代完整性验证。下面所有 payload PUT 均使用条件写，不依赖 CopyObject 的目标条件（该 SDK CopyDest 不提供同等保障）。

MinIO 是可信 Deployment 基础连接，独立于 D04 DB 出站业务策略。只有 object adapter 可 import SDK/接触 endpoint、bucket、key、access credential；业务/Runner contract 不 import SDK。凭据只由固定环境加载，不走 AWS 默认凭据链/业务 Secret 解析，不把初始化再套依赖 DB 策略的出站 Client。独立 Transport 强制 TLS 验证、无环境代理、固定 endpoint、禁止跟随任意重定向；不会接受用户传入存储 endpoint。SDK 显式 `MaxRetries:1`、`SendContentMd5:false`；不让可 seek spool 触发默认10次网络重试或非 seek body 被整份缓冲。

| `AGENTEAM_CENTRAL_` 配置 | 默认与验证 |
| --- | --- |
| `OBJECT_ENDPOINT`、`OBJECT_BUCKET`、`OBJECT_ACCESS_KEY`、`OBJECT_SECRET_KEY` | B03 起必填；endpoint 为无 userinfo/query/fragment/path 前缀的 http/https origin；bucket 为合法 DNS bucket 名；credential 非空且有界，私有闭包保存、任何 fmt/JSON/error 不泄露 |
| `OBJECT_TLS_MODE`、`OBJECT_CA_FILE` | TLS mode 默认 `verify-full`，要求 HTTPS；显式 `disable` 才允许可信内网 HTTP，不自动降级；可选 PEM CA 追加系统 roots，配置错误失败；不复用 outbound/DB CA |
| `OBJECT_TRANSFER_ENDPOINT` | 默认与 OBJECT_ENDPOINT 相同，部署声明 Runner 可达地址；单独签名时仍固定 bucket/region/path-style，须确指同一后端；不在调用参数覆盖 |
| `OBJECT_SPOOL_DIR` | 默认 `/var/lib/agenteam/object-spool`；绝对路径、实际拥有者、0700、无 symlink/路径逃逸；本实例持排他目录锁，第二实例拒绝，不清他人的目录 |
| `OBJECT_DOWNLOAD_KEYRING` | B02 配置类型、B03 起必填；独立随机 32-byte HMAC keys，编码沿 D04 cursor keyring 的 format/current_kid/keys 约定，1–32 keys、≤16KiB；拒绝与 AES/cursor 环重复的原始 key 材料 |

region 固定 `us-east-1`，path-style；bucket 由部署/fixture 预建，缺失不静默创建。bucket 与 credential 专用且由平台独占管理：无匿名公开策略、versioning 从未启用（不能接受 Suspended）、Object Lock/default retention 禁用、**无 bucket lifecycle 规则**。启动主动查询全部前提，查询权限缺失/响应未知即失败，不修改既有配置、不以默认值推断安全；否则旧版本 payload 或自动删除技术 marker 都破坏永久清理。部署管理员在平台外篡改存储不属于正常业务契约。对象命名仅随机安全 ID：canonical candidate、staging、control 各独立前缀，不含 Project 名称/用户 filename。

[R02 只读配置证据](d05-object-storage-research.md#82-专用-bucket-的只读配置结果)给出可接受结果：GetBucketVersioning HTTP200且Status为空；GetObjectLockConfig明确 `ObjectLockConfigurationNotFoundError`；GetBucketLifecycle明确 `NoSuchLifecycleConfiguration`。其他码不能折算禁用；尤其 GetObjectRetention 的一般400/InvalidRequest不构成无retention证明，须以已确认无ObjectLock为前提。非空bucket policy首版拒绝，避免把无法完整判定的匿名授权策略当私有；部署凭据访问权来自明确的服务身份策略。

## 3. 身份、权限与公共端口

复用 D01 `ObjectOwner` 闭集和 identity Actor/Scope。Project 对象始终绑定 ProjectID；Avatar 无 ProjectID，但安全分区绑定真实 user_id，不能把 System scope 误作管理员可读他人头像原始上传的授权。对象 ID、reference、lease、upload receipt、签名都不替代业务授权。

`object/contract` 仅依赖 foundation/identity；私有闭包保存 `PreparedPayload`、`UploadReceipt`、`AuthorizedObject`、`DownloadMaterial/TransferMaterial`。显式安全投影只有 ObjectMeta、业务 ref、状态/稳定错误；MinIO key、storage locator、临时文件路径、token/签名 headers 不进入普通 DTO/日志/ToolResult/Transcript。

```text
PutObject(ctx, actor, owner, meta, media_type, length, expected_sha256?, io.ReadCloser) -> ObjectMeta
LookupPut(ctx, actor, owner, command_key) -> not_observed | pending/unknown | failed | committed{ObjectMeta, UploadReceipt?} | revoked{object_id,cleanup_state}
CancelUpload(ctx, actor_or_trusted_cleanup, owner, command_key) -> upload_result/cleanup_checkpoint
PreparePayload(ctx, actor, owner, media_type, length, expected_sha256?, io.ReadCloser) -> PreparedPayload
ReserveUploadInTx(ctx, tx, actor, owner, command, PreparedPayload) -> UploadAttempt
PublishVerifiedInTx(ctx, tx, actor, owner, verified_attempt) -> ObjectMeta/UploadReceipt
AttachObjectInTx(ctx, tx, actor, owner, object_id) -> ObjectReference
ConsumeUploadInTx(ctx, tx, actor, owner, UploadReceipt) -> ObjectReference
OpenUploadSource(ctx, actor, owner, UploadReceipt) -> ObjectReader // 仅内部复制准备，不是普通Read
ReleaseObjectInTx(ctx, tx, actor, owner, object_id) -> error
ReadObject(ctx, actor, owner, object_id, ByteRange?) -> ObjectReader
StatObject(ctx, actor, owner, object_id) -> ObjectMeta
AcquireLeaseInTx/ReleaseLeaseInTx(ctx, tx, trusted_actor, object_id, LeaseOwner) -> lease/result
InspectReferences(ctx, object_id) -> {references[], active_leases[]} // 内部清理端口
DeleteUnreferenced(ctx, ObjectCleanupCause, object_id) -> completed | pending | failed
```

`PutObject` 是事务外 wrapper，复用准备/预留/上传/发布流程；InTx 端口仅 DB metadata/ref，不隐藏外部 I/O、nested Tx 或 commit。低层组合端口供 Artifact 与未来领域把真实业务发布、object reference、成功 command receipt/Audit 放入同一 Tx；`verified_attempt` 只能由存储适配器生成，客户端不能提交布尔 verified。

正式必需出口：`ResourceAuthority.AuthorizeOwner[InTx]` 验证真实 owner/调用 actor、prospective creation cause、可见性与 intent，并从持久实体/创建cause区分 existing/prospective，不能接受调用方boolean；`ProjectGate.CheckInTx` 校验同 Tx 生命周期；`SourceResolver.Resolve/ValidateInTx` 解析固定业务引用与版本；`RunnerTransferAuthority` 校验真实 Runner/Operation/当前 Project/取消及后续完成/停止证据。未绑定即 `DEPENDENCY_UNBOUND`，不能返回空引用、匿名 grant 或成功；只在测试构造可拒绝的替身。

D07 绑定 Session/Avatar 当前用户，D08 绑定 Owner/gate/生命周期，D12/D18/D20 绑定 Knowledge/Execution/Runner-result/MCP source，D15/D17 绑定 Runner 身份与传输确认，D21 注册 Artifact Tools，D27 绑定 UI 下载入口。D17 将可信 material 映射到独立 Runner 协议，Runner 不 import Central；D05 不提前定义操作执行协议。浏览器 download URL 的服务/验证在 D05 完整实现，正式 HTTP 装配由 D07/D08 授权成熟后接入；本次不注册匿名业务路由。Agent 只拿业务 file_ref/image_ref，无 object 枚举/签名 Tool。

## 4. 表、状态与锁

`agenteam_object` 最小事实如下；S3 locator 为内部列，所有关联与历史安全 ID 都有严格类型/非零约束，不以数据库 cascade 执行外部删除。

| 表 | 必须保存/约束的事实 |
| --- | --- |
| `objects` | UUIDv7、安全 scope/分区、规范 media_type、byte_size、全文 sha256、`pending/available/failed/deleted`、created_at/deleted_at、version、已发布唯一 candidate locator；available 必须有完整 verified metadata |
| `uploads`、`upload_attempts` | 本域 command identity/semantic_digest、稳定目标 ObjectID/owner、状态与结果；receipt ID、原稳定actor/创建cause、reserved/attached/revoked disposition及消费关联；每次外部尝试独立 key/attempt ID、声明长度/实际 digest、已发/verified/unknown/failed、错误 code/恢复 checkpoint；唯一 command identity，不能并发重新写同一 published key |
| `object_references` | `(object_id, owner_kind, owner_id)` 唯一，分区一致；reservation与canonical reference显式区分；reserved可存在于pending或available，保护删除但不授权普通Read/Stat/下载 |
| `object_leases` | lease ID、object/attempt、owner kind/id、持有者进程/transfer、用途、状态；按稳定 owner 去重；无自动过期回收，无仅依内存的引用计数 |
| `cleanup_operations` | cause/原 object ID、待处理 candidate/staging key、phase、worker fence、明确 storage 结果和安全错误；明确 `delete|zero_marker` 终局及 marker 核实；同 cause 重试，最终确认前不能报 completed |
| `store_identity` | DB instance UUID 与私有 bucket control marker 对应；仅新库+空 bucket 可初始化，持久初始化 phase 支持提交 unknown/外写丢响应后按原 UUID 核实；已初始化 marker 缺失/不符拒绝启动，不无条件覆盖或把误连空 bucket 当新系统 |

B02 的 `agenteam_artifact.artifacts` 保存 artifact_id、独立业务 file_id、ProjectID、stored_object_id、kind、name/description、来源稳定 IDs、创建者/创建时间；同一 Artifact 只有一个不可变文件。`artifact_commands` 分列原始请求摘要、首次resolved source固定内容事实/摘要、阶段和结果；`download_grants` 保存 token 所指 grant、user/业务引用/object/用途/到期/撤销，无 payload/token 明文。B03 `object_transfers` 保存 D01 grant 身份、direction、runner/operation/owner/object/attempt、expected length/SHA、期限、状态、完成/停止证据和关联 lease，不能只保内存 grant。

本域 FK 保护 object/attempt/reference/grant 关系，CHECK 固定状态、非负 size、32-byte digest、owner 闭集和 scope 分区；Avatar 绑定 UserID，其余按既定 Project 归属，不允许跨分区 Attach。Project/User/Agent 真表尚未存在，只保存 typed ID 并通过正式 authority 核验，不提前建业务表或跨未来 schema 假 FK。list 与恢复按 scope/status/创建序建有界索引；各表恢复事实随原业务生命周期清理，无通用结果仓库。

public state 严守 D01：pending→available/failed；明确清理且 payload 已删除才→deleted。available表示上传时已验证的存储事实，不表示已绑定业务或每次读取必成功；reference/receipt disposition单独表达绑定。available字节不可改写；更新业务内容创建新ObjectID，再在业务Tx中替换引用。外部损坏/缺失不改成空内容；Stat只读metadata，Read按真实payload返回错误。

锁预收集并一次 `AcquireAll`：command→必要 User/Project gate→业务 owner 锁→object aggregate（多对象按 ID 排序）→reference/记录。Artifact 私有互斥使用 D03 `RecordLock(ReferenceRecordLock, "artifact:<id>")`，在所有 object aggregate 之后取得；不新增/伪装 foundation aggregate rank。任何 Attach/Release/AcquireLease/cleanup 准入都持同 object 锁；新发现低序资源整体回滚重采集，不升级 shared 锁。

外部 I/O 从不持 DB Tx/锁。删除先短 Tx 重验全部 references/leases/active writer 并持久 cleanup gate，此后新 attach/lease/publish 拒绝；再执行外部删除/内容清除与确认，最后短 Tx 持相同锁落终态。单个 abandoned attempt 的 gate 只禁止该 attempt 发布，不阻止同 ObjectID 的合法新 current attempt；整对象 cleanup gate 禁止全部尝试。引用不存在不是授权结论；缺真实 authority 或引用读取失败不能当“无引用”。

## 5. 流式准备、幂等与发布

单对象允许 **0–1 GiB**，声明 length 必填；MIME 经 `mime.ParseMediaType` 规范化、≤256 bytes，不信任其“可 inline”含义。expected_sha256 可选，提供则必须是全文 SHA256 且匹配。单对象过大沿用 `PAYLOAD_TOO_LARGE`/413；客户端短读、超长、声明 SHA 不符用 `INVALID_ARGUMENT`/400，`OBJECT_INTEGRITY_MISMATCH`/502 仅表示已存储/上游 payload 损坏。先当前授权/资源和限额预检，再读取任何正文；正式 Tx 仍重新执行当前授权→command 同义重放→gate/expected_version。

为满足可选checksum的幂等语义，raw-body型Put/CreateFromContent每次提交先用固定 **64 KiB** buffer流式写入owned spool，同时计算全文SHA，包括成功重放的本次正文。读取至声明长度后再探测一个字节，短读、超长、错误、SHA不符都失败；不能只依Content-Length、SDK前缀截断、ETag或caller metadata。prepared文件sealed后只读；同长度/MIME不同正文仍得到不同command digest。source/upload-ref型先按下段查成功结果，只有新建/恢复才读取来源。

spool 限额：同时 **2** 个准备/上传，每请求 1 GiB、总预留 **2 GiB**，入场先预留声明长度并保留 128 MiB 空闲余量；可重试的并发/预算准入不足沿用 `RATE_LIMITED`/429、不新增容量码、不等待无限队列；真实 ENOSPC/文件系统或后端不可用是 `DEPENDENCY_UNAVAILABLE`/503。目录 0700/文件0600，随机子名、拒绝 symlink、不使用业务 filename。准备受 ctx/15min 上限，I/O 源必须可取消/Close；取消关闭源/文件、释放预算。所有远程读写有 ctx、connect5s/header15s/idle60s/单请求15min/整个 Put30min 上限；不能通过放弃 goroutine 假装取消。

raw-body语义digest为canonical-v1：command/owner/scope/稳定Actor/expected_version、全部展示参数、规范MIME、**实测length+SHA**。提供正确expected_sha256与省略视为同义完整性约束；不含trace/session/临时路径。source/upload-ref型分列 `request_digest`（相同公共输入及原ref variant/业务ID/显式revision或upload receipt ID、展示参数）与 `resolved_source`（首次捕获的object_id/固定revision/length/SHA及其digest）；调用方无需知道第一次解析的内部digest。

source/upload-ref命令已completed时，先验证**当前目标结果可见权**，再核原request_digest一致，返回原结果；不重新解析、授权或读取source，不重新消费receipt。源后来删除/失权/receipt已消费不阻断该分支；输入ref/revision/展示参数变更仍 `IDEMPOTENCY_KEY_REUSED`。新建/未完成恢复则必须当前target/source授权及gate，短Tx持久首次resolved_source和source lease后才能复制；恢复沿原捕获事实重验，不静默改为最新源。最终发布前再验当前源权限，commit unknown先查目标命令是否已completed，不能先因源已消失误判失败。只读LookupPut不读正文，仍检查当前结果可见权。

新命令在短 Tx 持久 pending object、原 command/owner reservation、唯一 attempt key 与必要 lease；确认 commit 后才向 MinIO 写。预留 commit unknown 先按原 command/attempt 查询，未核实时不开始外部写。普通失败允许原 key 同语义恢复，不生成新 ObjectID 假装另一命令；已发布成功返回原结果，不重传/覆盖 canonical。

从 sealed spool 向全新 private candidate key 做 **DisableMultipart + length + 完整 SHA header + If-None-Match:\*** 单 PUT。每个 physical attempt 独立 key、仅一次 SDK 尝试；412/丢响应均转原 attempt 核实，不能认为未提交或无条件覆盖。恢复重新发外部尝试必须仍当前授权、同 command/digest、独立 key并追踪旧 unknown key；每命令最多2个、全局64个未收敛 attempt，超限 `RESOURCE_BUSY`、只核实/清理旧尝试。读取 candidate 的完整流重新计算 SHA/实际长度（内存固定 buffer）并对照准备事实后，保存 verified 事实；HEAD metadata/ETag/checksum 不能代替这一校验，尤其不接受 composite checksum。

最终发布Tx重验actor/owner/gate/原命令及verified candidate；**existing owner**由authority证明业务实体与引用目的已存在，在同Tx置available、reservation→canonical并记录command/Audit。**prospective owner**只有持久创建cause、实体尚不存在：置available但保留reserved，签发未消费UploadReceipt，成功仅表示payload已存储；不生成canonical、不授予普通Read/Stat/下载。Artifact内联/复制组合在最终Tx先创建真实业务行，再完成对应canonical转换；发布失败留下可定位candidate，提交unknown沿原事实查询、不重写available对象。

AttachObjectInTx先验证真实业务实体已在同Tx存在、exact owner/scope/cause、available/non-cleaning、当前授权；若本对象属于prospective上传，必须原子消费匹配reservation及receipt，转为canonical并记录消费目标。ConsumeUploadInTx是显式携receipt的同一逻辑入口，receipt绑定原稳定actor/owner/object/创建cause；裸object_id不能绕过此校验。重复同一已完成消费只返原reference，换owner/重复异义消费拒绝。与Cancel竞争共用object锁：Attach先提交则Cancel不得撤canonical；Cancel先提交则Attach拒绝，不能同时成功。

spool不是唯一恢复依据。启动持目录锁后按自有manifest/DB attempt对应关系检查遗留；只清确属已终止进程且不再需要的owned文件。payload已在MinIO时按固定key/length/SHA核实后恢复；spool不在且payload不完整时需原调用方重交同语义输入，不能造空对象。自动恢复只推进存储核实/清理，失效业务权限不能被后台Service代替Owner来发布。

CancelUpload覆盖pending及**available但仍reserved/未Attach**：原actor通过当前身份/上传归属核验，或可信cleanup通过持久cause核验后，在gate允许的短Tx撤销receipt、仅释放本上传reservation、禁止Attach/发布并创建可恢复cleanup operation；无TTL推断，也不能拒绝一切已存储对象而留下永久孤儿。活动source/read/transfer lease仍保护实际I/O，远端unknown按第6节收敛。原成功storage command仍保留committed历史；取消后LookupPut只返授权可见的revoked/object_id/cleanup_state，Put重放返回 `RESOURCE_DELETED`（原提交committed），不再交可消费receipt、不重传或复活。已有canonical者只能由真实业务生命周期Release，不能用Cancel越权撤回。

## 6. 读取、reference/lease 与清理

StatObject仅查询DB metadata：当前actor/owner/scope/read gate及canonical reference或受保护固定版本使用事实仍须核验，但不打开MinIO、不创建reader lease、不证明payload存在/可读；stored state=available不是本次存储健康结论。prospective reservation不授权普通Stat，其原actor经LookupPut查看上传状态。

ReadObject在短Tx做相同当前读取授权，再验available/non-cleaning并持久reader lease，commit确认后才打开MinIO。真实GET的状态/headers/首段或空流必须在交付reader/HTTP成功头前验证，不能留下GetObject的惰性404尚未触发。返回 `ObjectReader{Meta,ResolvedRange,ReadCloser}`；EOF/错误/Close都先真正关闭并join源I/O，再释放lease，提交unknown则保留lease按ID重试。

OpenUploadSource仅供新建/恢复的内部复制：当前actor、原owner/cause、unconsumed receipt、available+reserved及non-cleaning全部核验后取得source lease，才开流。它不把reserved变为canonical、不开放普通Read/下载或raw-ID授权。源上传若被Cancel撤销，则阻止后续读取/发布，已有流的lease保留到实际Close/join；复制成功不隐式消费别的owner的reservation，原actor或可信cleanup仍可显式撤销该源上传。

ByteRange为单一 `[offset,length]`，非负offset、正length，检查整数溢出/是否超对象；末段按声明上限截取，offset越界返回range error，空对象只支持全读。拒绝多range，不把服务端忽略Range的200当206；验证Content-Range/实际长度。全文读计算SHA；range只验证区间边界/长度并依赖上传时验证及不可变存储，不把局部digest冒充全文。payload404=`OBJECT_PAYLOAD_MISSING`；存储长度/SHA错=`OBJECT_INTEGRITY_MISMATCH`；Stat仍只表达metadata。

全文reader保留固定 **64 KiB末段holdback**（不足则全部保留），流式累计SHA及精确length，到源EOF、额外字节检查和SHA均通过才向caller释放末段；0字节也先完成空流校验。内存仅固定buffer/holdback，不整份缓存。小对象因此在首字节前即可检错；大对象可先交付已读前缀，末尾校验失败时返回明确错误并扣留末段，使caller不会先拿齐声明Content-Length再才得知失败。range末段可沿同一机制确认区间长度，但无全文SHA保证。

首字节/成功头前失败可投影安全错误；HTTP状态/前缀已发出后只能失败中断stream，不能改状态、追加Problem JSON或召回字节。HTTP适配须执行真实abort（如 `http.ErrAbortHandler`），使完整Content-Length响应不被伪报完成；内部copy/SDK caller同样必须检查最终read error。sent_bytes仅计输出接口实际接受的Write字节，不表示远端已收妥；最终SHA、读写错误及sent/failed阶段按实际attempt审计，不把started当完整交付。无论在哪一阶段失败，实际close/join之后才可释放lease。

read lease 只在本实例 reader 实际关闭/join 后释放；跨重启按确认死亡的进程实例恢复，不能按 TTL 猜测另一个活实例已经结束。长时间持有/取消由实际 ctx/stream 跟踪，清理见到活动 reader 必须 pending。永久 ref/运行/history lease 无自动过期；D10/D22/各领域按正式生命周期释放，D05 不从运行时间推断无引用。

Release 只撤指定 owner 引用，不隐式删 payload；DeleteUnreferenced 需要正式 cleanup cause，检查所有有效 ref、reservation、reader/transfer/writer lease。明确失败/孤儿也经持久 cleanup operation：请求停止本域 writer、等待 owned goroutine join、禁止新写和新授权，再清精确 key，确认后提交 deleted；无效对象 ID 不能变成 bucket 批量删除权限。已经放弃的 writer/PUT transfer lease 只保护其受控收敛；可经可信取消先禁发布并清其 staging 内容，reader/有效业务引用仍不能被越过，正式 lease 未收敛前整体仍 pending。

**迟到写入边界：**一次网络错误/HEAD404/expiry/客户端 join 不证明外部 PUT 已结束。只有 key 从未暴露外部写权限且全部写已取得确定终局响应、无待发/在途时，才采用 DELETE→实际 absence 核实。其他 canceled/unknown/Runner staging 在持久 cleanup gate 后，以 fresh bounded ctx **无条件 PUT 0 bytes 的技术 tombstone**，仅成功响应并完整读回0 bytes/空SHA、无旧user metadata 后确认业务内容消除；同 key 的旧 payload 请求全部带 If-None-Match:*，不得再覆盖 marker。每次清理≤15s，超时保持 pending/checkpoint，不无界等待。[R02 实测](d05-object-storage-research.md#81-实际并发顺序与完整读取)为慢旧PUT先200、无条件zero随后成功、完整空读与旧签名/SDK条件重放412后仍空；root已采纳。它不证明删除marker安全，也不替代B01的领域gate/恢复并发验收。

可能存在迟到写/旧 grant 的技术 marker **首版永不自动删除**，不按 TTL、HEAD404、URL expiry 或一次成功 fence 移除。marker 只有原随机 key、固定技术 Content-Type/空payload，无 Project/Owner/名称/原checksum/业务 metadata；DB 项目事实清理后不保留项目关联，其不是用户 payload/归档副本。即使内容已清空，业务 deleted/Project completed 仍须正式 ref/lease 收敛；不能用 marker 跳过 D17 停止确认。已发出未终局的 marker PUT 只可能继续写相同空内容，不能使业务正文复活。

取消/进程中断后，恢复程序按 persisted key/attempt/marker 逐项核实；清理成功而 DB commit unknown 时按原 cleanup operation 重读/重新验证空 marker，绝不重新发布该 key。未见对象只记 not_observed，不把未知归为已清空。健康/清理有界重试，无通用结果仓库或自动 TTL。若未来加入 multipart，须在新规格实现独立 fresh bounded ctx 的 abort/list-incomplete 恢复，本次禁用后仍应实测无残留 multipart。

Project archive 保留内容，禁止新上传/Artifact创建/Runner grant；合法人类 read/下载及其 Read Audit 继续。Project permanent delete：D08停止各领域，Artifact释放本域引用，Object按原ProjectID清候选/对象/transfer/receipt，完成全部业务内容删除或经核实的零marker替换后才能清本域项目metadata；他域 protected reference/未知Runner transfer均阻止 completed。不删其他用户/项目对象，不在System scope保存项目正文或Audit副本；单域通过不代表全Project删除已绑定。

## 7. B02：Artifact 组合服务

`ArtifactRef` 与 `file_ref/image_ref` 只含 Project/Artifact/file 业务身份和安全 metadata。kind 首版 `generated|user_upload`；name 为1–255 UTF-8 bytes、无路径分隔/控制字符，description≤4KiB；不以 name 作唯一键。来源 Execution/Operation/creator 均由受信调用上下文确定，不接受模型伪造 User/Project 权限字段。

`CreateFromContent(ctx,actor,meta,project,name,media_type,utf8_content,description?)` 的inline UTF-8内容≤1MiB（D18/D21 Tool参数可更小）；`CreateFromSource(...,BusinessFileRef)`；`CreateFromUpload(...,UploadReceipt)` 给人类业务上传。后者仅对exact目标Artifact prospective owner完成首次绑定：同Tx先建Artifact、ConsumeUploadInTx转换reserved→canonical并记录command/Audit，不重传已验证payload。已有其他业务owner的文件仍走CreateFromSource复制，不将两Artifact共享同Object。三路径均不能先返可见Artifact再异步补payload。

BusinessFileRef 先覆盖 D01 UploadedObject/ArtifactFile/KnowledgeFile/ExecutionFile，拒绝 raw object ID 冒充 source。ArtifactFile 必须匹配真实 artifact_id+file_id；UploadedObject 需校验单 actor/owner/cause 的 UploadReceipt，不能按可猜 ObjectID 接管。MCP/Runner 来源经 D18/D20 正式 resolver 返回既有业务 ref；未来增加 ref variant 要显式注册真实 provider，本次未知 variant/未绑定 provider 拒绝，不默认读取 MinIO。

CreateFromSource先执行第5节completed重放分支；新建/恢复才当前source授权→在command事实中持久首次object/revision/length/SHA并取得source lease→**同一条源流**读入受限spool并算SHA→全新ObjectID/key→发布新Artifact。UploadedObject来源通过OpenUploadSource，其余来源要求合法canonical/固定版本引用。不共享StoredObject、不dedup、不采用先校验再复制可变源的TOCTOU。未完成命令发布前重验已捕获source的当前可读权，删除/失权则拒绝；已completed只检查目标结果与原输入，不再要求源存在/仍可读。源以后删除也不影响新Artifact内容或同义成功重放。

`ListArtifacts(ctx,actor,project,filter,page)` 只查询当前可见 Artifact metadata。过滤 execution_id/kind/media_type/name_query；name_query≤256 UTF-8 bytes、按文字子串转义SQL通配符。默认50、1–200；`created_at DESC,id DESC` keyset，复用 D04 cursor，以 resource=`artifact` 的查询域+Project/filter/order完整绑定，防Audit cursor被同scope复用。未知来源标签按安全ID保留，不复制正文。

`ReadArtifact(ctx,actor,artifact_ref,offset?,limit?)`：文本默认8KiB、最多64KiB预览；offset按字节且须UTF-8边界，limit尾部截到完整rune，返回实际offset/next_offset/truncated+file_ref；无效UTF-8退回明确file投影，不造替换后的canonical正文。图片返回image_ref，PDF/Office/音频/压缩包/其他binary返回file_ref+MIME，不引入解析/embedding。小范围文本可有界缓冲，canonical对象始终流式存储。

list/read/create 的 Audit 保留真实 Agent/Project/Execution/Operation/Tool/Artifact/source/object IDs、MIME/size/phase/result；列表用 artifact_collection，不存结果正文/名称搜索串。稳定调用attempt/cause参与AppendKey，重放不重复成功事件；HTTP trace不混成业务attempt。文本/列表先完成业务读取，再在返回前当前授权+Read Audit；审计提交失败/unknown则不交付结果。暂无 delete-artifact Core Tool；Project/业务清理走正式生命周期端口。

## 8. 浏览器短期业务下载 URL

root 已确认：浏览器使用 **Central 签名业务下载 URL**，每次使用重新校验当前 Session/Owner，经 ReadObject 流式返回；Runner另走下节直连。这符合 D01 的 streaming 内部适配，不让 Agent 取得URL，也不暗中把 Runner 下载改为Central中转。

`IssueDownload(ctx,Human,business_ref,mode:download|preview,expires_in?) -> PrivateSignedURL` 创建持久grant；默认60s、最大300s，绑定原user_id、业务ref/version、object_id、GET、mode/安全filename/MIME、expiry；它不固定长期AccessGrant。URL token=`base64url(canonical{v,kid}).base64url(canonical payload).base64url(mac)`，严格无padding；独立HMAC-SHA256输入为 `agenteam.object.download.v1`+零字节+前两段原始ASCII（含点）。payload含grant_id和全部上述绑定，编码复用canonical-v1，常量时间验证、整体≤8KiB。仅current kid签发，旧kid只验证至既有grant到期，未知kid拒绝，不静默换key。URL仅经当前Human的安全专用响应投影，无普通JSON/fmt展开，Cache-Control no-store。

`OpenDownload(ctx,currentHuman,token,range?)` 验签/期限/DB grant未撤销/当前Session与原user一致/当前业务Owner及scope，然后开真实ObjectReader；当前权限失败不透露对象是否存在。到期/撤销拒绝**后续请求**，同用户可在有效期内多次GET/range，不伪称一次性；每个真实reader独立lease。已开始stream按其实际reader追踪，撤销不声称已召回发出的字节；D07/D08后续可通过明确取消端口关闭相关reader。

成功输出字节前完成Read Audit；issued/started只表示准入。全文下载沿第6节64KiB末段holdback；仅完整校验且输出接口接受全部字节后记sent，读写/SHA失败记failed与真实sent_bytes，不能声称客户端已收妥。首字节后失败中断连接，不改已发状态，不因尾部Audit追加失败重发payload。Audit失败/unknown或打开MinIO失败时实际关闭/join reader后释放lease；未交付的流不算成功下载。

preview必须依据服务端sniff/允许集，而非信用户MIME：首版text/Markdown/JSON仅 `text/plain; charset=utf-8`，实际支持的PNG/JPEG/GIF/WebP才可image inline；HTML/SVG/XML/PDF/Office/未知内容默认attachment。不执行HTML转换或脚本，带 `X-Content-Type-Options:nosniff`、`Content-Security-Policy:sandbox; default-src 'none'`、安全Content-Disposition（RFC5987 filename、无CRLF）、private/no-store。HTTP绑定需单range206/不满足416/完整200的真实长度，不把token放日志instance/query或跨站Referer。

## 9. B03：Runner 单对象传输

实现 D01 `IssueTransfer/InspectTransfer`，另有 `CompleteTransfer/CancelTransfer/ConfirmStopped` 正式服务端口。所有输入先经真实 RunnerTransferAuthority 校验 actor/source、Runner/Operation归属、Project gate/取消；D17未绑定不得生成可用生产grant。grant绑定 transfer_id/runner_id/operation_id/object_id/direction/length/全文SHA/expiry，private material只经受信Runner通道。

GET 只签已 available immutable key，PUT 只签独立 staging key，绝不把canonical candidate写权限给Runner。PUT 必须先取得 D17 已核验输出manifest的length/全文SHA并形成pending metadata，缺任一拒绝，不在上传之后补猜。默认有效期60s、最大300s；`PresignHeader` 显式签Content-Length、全文SHA校验头和If-None-Match:*，不能使用仅签host的PresignedPutObject。预签名GET同样固定object/method；Runner收到后必须检查length/SHA，自己声称完成不代替D17的可信操作证据。单次直传≤1GiB，全局未收敛grant最多32个，超限拒绝，不无限占staging资源。

S3 URL是短期bearer，签名无法把存储端HTTP调用者密码学绑定为某一Runner；Runner/Operation绑定由受信通道、DBgrant、完成/停止校验执行。泄露者在存储仍接受签名时可能重放；条件PUT在key存在时拒绝覆盖，**key删除后原URL可能再次成功**，所以它不是one-time token。逻辑撤销立即禁止再签发/发布/接受不合法完成，但不宣称立即撤回已签URL或已发stream。

PUT完成：先验证可信Runner回执/当前授权，再将staging的**一次完整读取**写到owned spool或直接校验流并写全新private candidate，末尾对expected length/SHA确认，再依第5节验证/原子发布；不能先GET校验staging后无条件Copy该可变化源。旧grant最多影响staging，不能修改已发布对象。失败candidate/staging均有持久attempt进入恢复，不以外部200直接报complete。

GET只有D17确认对应实际传输完成且完整性匹配才complete；断连/缺确认为unknown，绝不自动重复Runner业务操作。过期仅关闭新传输资格，不自动expire Operation/业务等待。GET/PUT grant相关lease至少保留到**grant无法再用于新请求且已获得可信完成/停止、无实际在途**；未满足即pending/unknown，不能凭TTL、一次HEAD或Runner连接断开释放。PUT的staging marker保留/清理规则沿第6节；transfer complete与临时staging物理收敛分开记录，不把业务成功回执当所有旧HTTP请求结束。

相同 Runner/Operation/object/direction 的 Issue 重试查原grant/事实；未过期同义返回原逻辑grant，签名材料重新生成不能扩大原expires_at；到期需要经过新的当前授权形成新的transfer attempt，不借旧key续期。Inspect只读且按当前可信身份授权；complete幂等须核实原digest与持久结果，冲突拒绝。Runner的目标endpoint不可达/TLS错明确失败，不fallback Central，也不产生空文件成功。

## 10. 初始化、健康、关闭与验收

B03 config/check-config验证固定endpoint、TLS/CA、凭据/独立签名keyring、spool路径语义，不连接、不输出原值；help/version不依赖配置。DB阶段预算沿D03；DB完成后Audit/Secret/Outbound/对象初始化共用D04既有 **30s SecurityStartupTimeout** context，更短parent优先。对象不得再独立追加30s、15min I/O或恢复预算：在同一剩余context验证MinIO/bucket/control identity、spool，执行唯一probe的put/get+全文校验/delete并完成启动必需的有界恢复检查；失败/耗尽不监听。其余可恢复工作在已持久gate保护下交后台继续，不跳过必要门禁。probe只用control命名空间，不污染业务对象/Artifact/Audit。

健康沿D04 **10s采样间隔、同轮2s有界context、成功样本超过20s陈旧即unavailable**；DB/object可并行采样但不各自重置本轮预算，各组件保存自己的实际成功采样时间。任何检查错误立即置该组件unavailable；不得用旧成功或新建monitor重置时间。启动阶段较早的DB样本若已陈旧，须在剩余启动预算内重新核验；恢复healthy也须真实新成功。健康HTTP只读采样结果，object采样读control marker并核worker状态，不每请求新建payload；不可达/缺权限/marker错则ready503。授权/Runner等仍unbound，整体ready=false；清理积压/unknown保留安全可观察状态。

第一信号停新对象操作/grant与新恢复batch，在途HTTP、对象stream/上传及已有短Tx按原全局shutdown budget drain；不直接cancel全部HTTP/reader BaseContext，已准入operation的后续阶段可继续。Object worker/Reader/Transport join及CloseIdleConnections完成后才停DB。超时/第二信号沿D04同一个**额外最多1s force context**执行cancel/socket close、对象/HTTP/健康/Secret/Outbound/DB清理与join，不能按资源、goroutine或迟到初始化结果各加1s；更短parent不延长，未join必须报告forced/unknown，不能假称drained。保留checkpoint，不删仍由writer使用的spool；Runner直传不会因Central退出自动获得停止证明。

普通 `check-go.sh` 不启Docker。`test-objects.sh` 用nonce/label/exact-ID生成owned PG+MinIO+临时CA/spool，只读固定源码/产物创建fixture；成功/中断最终删除owned资源并检查残留。真实Central进程测试必须带MinIO，`test-postgres.sh/test-security.sh` 复用或创建已验证owned descriptor，不能跳过旧入口测试或注入禁用MinIO开关。fixture credentials随机，不读现有服务/外部秘密；专用套件缺fixture/构建/网络条件即失败。

| 块 | 必须实际证明的场景 |
| --- | --- |
| B01 完整性/流 | 0字节、小文件与≥64MiB对象真实put/get/range；长于声明/短读/错SHA拒绝；固定buffer/spool预算/取消/遗留清理；seekable前缀陷阱拒绝；Stat在payload缺失/MinIO不可达时仍只查授权metadata、无I/O/lease，Read才真实报错；0/≤64KiB错SHA在首字节前失败，大流末尾损坏保留末段、送出字节少于Content-Length并中断；无完整body后才报错、无整块缓冲 |
| B01 Tx/恢复 | 同key同长度不同body冲突，包括expectedSHA省略；权限先于重放；pending提交unknown不先外发；payload写成DB失败/COMMIT丢回包按原attempt恢复；成功重放不重写canonical；available缺payload/篡改明确失败；中断/late writer/cleanup gate/refs+lease竞争无误删；慢条件PUT与无条件零marker barrier、旧grant重放后仍空、marker提交unknown/重启恢复与永久保留；正式lease未知仍pending |
| B01 绑定/撤销 | existing Put原子available+canonical；prospective available+reserved不允许普通Read/Stat/下载；内部source只由匹配receipt及当前权限开lease；Attach/消费同Tx与Cancel竞争仅一方成功，重复消费幂等；未Attach成功上传可撤销并最终清理，跨actor/owner/cause拒绝；取消后原Put/Lookup重放仅安全revoked结果、无可消费receipt/新写/复活；active reader仍阻止物理清理 |
| B01 公共兼容 | 全部D04旧Audit/Actor/迁移输入仍合法；新增action/resource/producer/cause拒绝错配；archived合法读可追加Read Audit且同Actor写被拒、admin不代Owner、缺Audit不返回未交付内容；对象维护不能冒充Artifact业务主体 |
| B02 Artifact | inline/upload/source三路径真实payload与原子绑定；source复制后ObjectID/key不同；首次resolved事实持久化、未完成恢复重验当前源并沿原版本；completed后源删除/失权/receipt消费仍可按原输入重放且source resolver/存储调用0次，目标失权先拒绝、改ref/revision/展示参数冲突；commit unknown先查完成态，无双Artifact；cursor/UTF-8/binary/image安全投影 |
| B02 下载 | 真stream/range、到期/改签名/kid/跨user/跨Project/Session撤销/Owner变化拒绝；每次授权/lease；HTML/SVG/MIME伪装不inline，header不可注入；敏感canary不泄漏；Audit失败前不交付；尾部SHA/写失败真实中断、HTTP状态不重写/不追加JSON，sent_bytes与实际Write一致，未join不释放lease，最终Audit失败不重发 |
| B03 直传 | 真实presign PUT/GET、改method/key/length/SHA拒绝；重放条件PUT412、删除key后旧URL可重放的真实边界；上传校验期间重放staging也不能改变canonical；wrongbody/缺回执/断连unknown、过期不释放活动lease；可信停止与cleanup竞争；raw URL逻辑撤销不伪称存储即时撤回；endpoint/TLS不通无中转 |
| 最终进程 | 空库/升级与MinIO/凭据/CA/marker/spool门禁；真实TLS成功/错误及显式HTTP；不安全bucket配置或查询无权拒绝；前序安全初始化已耗时后对象只用剩余30s/更短parent，不监听超时结果；健康真实故障/恢复/20s陈旧、无重置时间续命；真实上传/reader/恢复中首信号drain，第二信号/超时所有资源共享额外1s force、DB最后关闭，不按资源叠加；无owned残留，整体ready=false |

每块从仓库根执行 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/check-go.sh` 和 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/test-objects.sh -run '<实际场景名>'`；最终无过滤 `test-objects.sh` 并覆盖D04 `test-security.sh` 受影响真实输入。网络/存储barrier检查服务端实际key/版本/请求计数与DB事实，不只检查客户端error；真MinIO证据、fixture指纹、命令exit和清理状态一并交接。

技术输入为已冻结[研究修订2](d05-object-storage-research.md)，SHA256 `b4a0462d255910443cbed31abbaeee6d17c133b74a2d997357e37d938242f038`；其协议局部观察不是本模块实现验收。无新产品决定或设计阻塞；S01按冻结指纹独立静态审查，再由root采纳并下发实现。本文未实现业务、改动代码/迁移或创建运行资源。
