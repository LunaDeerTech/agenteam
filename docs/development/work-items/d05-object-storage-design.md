# D05 对象存储与 Artifact 实施规格

- 修订：6；B03输入 `main@c950e35`、[D05 主卡修订 3](d05-object-storage-artifact.md)；B01 `d31aecd`、B02最终 `2c1dca3`已独立验收。本次只细化Runner transfer、必要对象协议补口和Central生命周期；本文是实施规格，不是B03实现通过声明。
- 依据：[对象存储](../../architecture/platform-infrastructure/object-storage.md)、[Artifact](../../architecture/tool-system/artifact-tools.md)、[D01 资源](d01-contracts/resources-skills.md#对象与业务引用)、[生命周期](d01-contracts/domain-lifecycle.md)、[权限/幂等/Tx](d01-contracts/foundation.md)、[部署运行](../../architecture/platform-infrastructure/deployment-runtime.md)。
- 已核对实际 [D03 Tx](../../../internal/central/postgres/transaction.go)、[identity](../../../internal/central/identity/contract/identity.go)、[Audit contract](../../../internal/central/audit/contract/types.go)/[授权](../../../internal/central/audit/service.go)、[cursor](../../../internal/central/cursor/cursor.go)、[Central](../../../internal/central/app/app.go)。复用 Go **1.27.1 / GOTOOLCHAIN=local**、pgx **5.11.0**、Goose **3.28.0**、既定 PG17.8/vector0.8.1 fixture。

## 1. 分块与所有权

按以下完整结果串行实施并独立验收。root 拥有主卡/计划/台账；以下是实现阶段所有权建议，S01 仅写本文。

| 块 | 结果与独占新增范围 | 全局迁移/公共改动 |
| --- | --- | --- |
| B01 对象与可靠存储 | `internal/central/object/`（含 `contract/`、MinIO adapter、spool、引用/lease、上传/清理恢复）；`tests/objects/` 的 object 场景；`tests/testsupport/objectstore/`；`scripts/test-objects.sh` | `00005_object_storage.sql`；本节列明的最小 Audit/identity 扩展；`go.mod/go.sum` 仅本块锁定已核验 SDK/依赖 |
| B02 Artifact 与浏览器访问 | `internal/central/artifact/`（含 `contract/`）；`internal/central/object/download*.go` 及测试；`tests/objects/` 的 Artifact/download 场景 | `00006_artifact_download.sql`；D04 cursor 直接复用，不改变原编码；B01 object 仅增加本文已固定的组合接口 |
| B03 Runner transfer 与进程 | `internal/central/object/transfer*.go` 及测试；`tests/objects/` 的真实直传/恢复场景；Central MinIO 初始化/健康/关闭 | `00007_object_transfer.sql`；配置/进程/fixture 文档与本节共享范围串行移交 |

B02必要补口另含 `object/source*.go`、`object/contract/source_lease.go`及测试，`object/access.go`、`object/contract/access.go`及测试仅增Source acquire操作；新增download contract/provider仍归B02。独立 `object.NewSourceReads(service,resolver)` 持有已验Service和正式SourceResolver，不改Service.New/Authorizations、旧Objects/Leases及D03/D04既有行为；复用00005现有source lease，00001–00005不改，Artifact command的source LeaseID归00006。既定签名用途隔离仅增 `internal/central/secret/keyring_material.go`、`keyring_material_test.go`，不改旧Secret文件；下载keyring实现/测试仍为B02的 `object/download_keyring*.go`。

共享文件按 B01→B02→B03 单作者移交：`internal/central/config/`、`internal/central/app/`、`internal/platform/logging/` 的中立阶段/错误枚举及测试、`tests/process/`、`tests/testsupport/postgres/cmd/fixture/main.go`、`tests/testsupport/outbound/cmd/fixture/main.go`、`docs/development/backend/README.md`、`AGENTS.md`。fixture 调度改动仅为给真实 Central 进程提供全部必需的 owned PG/MinIO；不回退 D04 入口验证。B03时`00001–00006`及`go.mod/go.sum`均冻结，不改历史SQL；`00007`为D03事务migration。

B03独占新增 `object/contract/transfer.go`、`transfer_authority.go`，`object/transfer.go`、`transfer_store.go`、`transfer_upload.go`、`transfer_storage.go`、`transfer_recovery.go`及对应测试；技术进程组合新增 `object/runtime.go`、`process.go`、`startup.go`及测试，不移入中立platform。旧文件仅作下表必需增量，不重构已验API；新增内部函数可按职责拆分同前缀文件，公共签名和下述边界保持。

| 已验文件/接口 | B03最小兼容改动 |
| --- | --- |
| `object/contract/access.go`、`object/access.go`及测试 | 增加§9闭集TransferAccess、完整请求绑定/锁发现；旧Kind/操作不改语义；新的transfer planner组合器只分派正式端口，不默认授权 |
| `object/service.go`、`object/upload.go`及测试 | 仅提取共用的规范内容digest/命令预约/private candidate创建；旧Reserve仍须真实Prepared registry；UploadPrepared和PublishVerified明确只接受private_candidate；公开Uploads签名不变 |
| `object/repository.go`、`object/cleanup.go`、`object/recovery.go`及测试 | 读取typed attempt kind/nullable process，按kind处理恢复/清理；精确staging marker分支；Project最终删除前同Tx清本对象transfer事实；不放宽普通lease、reader或canonical保护 |
| `object/initialize.go`及测试 | 保留Initialize的bucket/control绑定职责；由新startup端口补真实probe/恢复门禁，不能把旧Initialize成功当完整启动成功 |
| `object/contract/authority.go`、`read.go`、`spool.go`、SourceReads、Artifact/download、Audit/Secret/D03 | 既有签名与业务行为保持；pending PUT的transfer lease走新专用事务代码，不放宽旧AcquireLeaseInTx的available条件；不新增Audit动作/通用结果库 |

00007只追加本域transfer/process/probe事实及§9必要ALTER；全部旧attempt自动为private_candidate且原约束仍生效。D17以后适配Runner域与真实权限，不能借本次组合器写未来业务表。实现前root采纳本表移交，独立验证覆盖旧B01/B02兼容。

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

实际组合入口为 `LoadDownloadKeyring(raw, cursor.Keyring, secret.Keyring)`：两环均须真实Validate成功，再排除全部当前/历史材料复用；Secret仅新增 `Keyring.ContainsMaterial([]byte) bool` 常量时间比较，不导出key、不重新解析Secret配置。下载签名环与AES用途隔离规则不变。

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
ReserveUploadInTx(ctx, tx, actor, owner, command, PreparedPayload, plan, locked) -> UploadAttempt
PublishVerifiedInTx(ctx, tx, actor, owner, verified_attempt, plan, locked) -> ObjectMeta/UploadReceipt
AttachObjectInTx(ctx, tx, actor, owner, object_id, plan, locked) -> ObjectReference
ConsumeUploadInTx(ctx, tx, actor, owner, UploadReceipt, plan, locked) -> ObjectReference
OpenUploadSource(ctx, actor, owner, UploadReceipt) -> ObjectReader // 仅内部复制准备，不是普通Read
SourceReads.AcquireSourceInTx(ctx, tx, actor, ResolvedSource, plan, locked) -> SourceLease
SourceReads.OpenLeasedSource(ctx, actor, SourceLease) -> ObjectReader
SourceReads.CancelSourceLease(ctx, SourceLease) -> error
ReleaseObjectInTx(ctx, tx, actor, owner, object_id, plan, locked) -> error
ReadObject(ctx, actor, owner, object_id, ByteRange?) -> ObjectReader
StatObject(ctx, actor, owner, object_id) -> ObjectMeta
AcquireLeaseInTx/ReleaseLeaseInTx(ctx, tx, trusted_actor, object_id, LeaseOwner, plan, locked) -> lease/result
InspectReferences(ctx, object_id) -> {references[], active_leases[]} // 内部清理端口
DeleteUnreferenced(ctx, ObjectCleanupCause, object_id) -> completed | pending | failed
```

`PutObject` 是事务外 wrapper，复用准备/预留/上传/发布流程；InTx 端口仅 DB metadata/ref，不隐藏外部 I/O、nested Tx 或 commit。低层组合端口供 Artifact 与未来领域把真实业务发布、object reference、成功 command receipt/Audit 放入同一 Tx；`verified_attempt` 只能由存储适配器生成，客户端不能提交布尔 verified。

正式必需出口：`ResourceAuthority.AuthorizeOwner[InTx]` 验证真实 owner/调用 actor、prospective creation cause、可见性与 intent，并从持久实体/创建cause区分 existing/prospective，不能接受调用方boolean；`ProjectGate.CheckInTx` 校验同 Tx 生命周期；`SourceResolver.Resolve/ValidateInTx` 解析固定业务引用与版本；`RunnerTransferAuthority` 校验真实 Runner/Operation/当前 Project/取消及后续完成/停止证据。以下 AccessPlanner 提供全部实际 gate 的非授权预收集；缺任一必需端口即 `DEPENDENCY_UNBOUND`，不能默认空锁、空引用、匿名grant或成功；只在测试构造可拒绝的替身。

受保护固定版本读取增加专用 `ObjectReadAuthority.AuthorizeObjectReadInTx(ctx,tx,actor,owner,objectID) -> OwnerAuthorization`，必需绑定本次exact ObjectID、existing+Read；grant携ReadObjectID及可选ProtectedLease，后者仅execution/history/transfer稳定用途且ObjectID一致。opaque构造和Details均复制可变嵌套值，不能用外部pointer修改授权。canonical读取仍经ResourceAuthority；没有canonical时必须调用上述正式端口（缺失DEPENDENCY_UNBOUND），不能以任意active lease代替读取授权。对象服务在同Tx重读该exact lease ID/object/owner kind+ID/active状态及对象分区，当前主体/owner权限仍重验；读取规划必须在取Object锁前覆盖可能使用的固定版本/lease保护方，不能失败后现场补父gate。

### 对象操作的锁计划

仅新增对象域的规划/组合端口，不扩展D03锁框架、owner ID含义或未来业务表。`AccessRequest` 为闭集opaque tagged union，由按用途的构造器校验必填/禁填字段；Kind固定为 `owner|object_read|lease|object_cleanup|project_cleanup|source|maintenance`，B03仅追加§9的`transfer`；内部Operation再区分本域实际动作，不能用任意字符串选择未来能力。

| Request Kind | 必须不可变绑定的请求与发现事实 |
| --- | --- |
| owner | Actor、ObjectOwner、Intent、具体操作；如有则exact ObjectID、CommandMeta/command identity、PreparedPayload、UploadAttempt或UploadReceipt的稳定身份。Reserve绑定原command实际ObjectID；新命令未见时绑定prepared的新ObjectID |
| object_read | Actor、owner、Read、exact ObjectID、stat/read/open-source用途；open-source含receipt；覆盖canonical或protected-use的真实依赖，不以未知fallback补锁 |
| lease | Actor、exact ObjectID、LeaseOwner kind+ID、acquire/release；不虚造一个caller owner |
| object_cleanup / project_cleanup | 原typed cleanup cause及exact ObjectID，或Actor+ProjectCleanupCause；绑定稳定operation/project/version，不借普通Owner授权 |
| source | Actor、原BusinessFileRef、ResolvedSource的owner/ObjectID/固定revision及完整metadata；ExecutionFile保留ExecutionID与PayloadID两个身份；ValidateSourceAccess保留原义，新增AcquireSourceAccess预定Object EX；只在新建/未完成命令规划来源 |
| maintenance | 本实例/恢复职责、既有upload/attempt/cleanup等持久cause与目标身份；只允许已授权的技术收敛，不授予后台Service读取/发布Owner业务内容 |

```text
// 以下均在 object/contract；依赖只有既有 foundation/identity。
AccessDependencies   // opaque: 真实依赖映射指纹 + []foundation.LockRequest 的复制投影
AccessLockPlan       // opaque: service/完整请求绑定 + dependencies + 本域锁集合
LockedAccess         // opaque: service + exact Tx + 完整plan集合/union/modes；不是授权grant

AccessPlanner.Discover(ctx, AccessRequest) -> AccessDependencies
AccessPlanner.ValidateInTx(ctx, tx, AccessRequest, AccessDependencies) -> error

ObjectService.DiscoverAccess(ctx, AccessRequest) -> AccessLockPlan
ObjectService.AcquireAccessPlansInTx(ctx, tx, []AccessLockPlan,
                                    extraLocks: []foundation.LockRequest) -> LockedAccess
ObjectService.ValidateAccessPlanInTx(ctx, tx, AccessRequest, AccessLockPlan, LockedAccess) -> error
SourceResolver.ValidateInTx(ctx, tx, actor, ResolvedSource, plan, locked) -> error
```

Discover只读预扫描、不取得业务事务锁、不作最终授权、不持Tx等待外部I/O；端口由可信authority组合绑定，必须覆盖Actor的User/Agent/Execution gate、当前owner真实父实体、固定版本保护方及其他必要低序gate。AccessDependencies记录锁依赖身份而非缓存权限/owner existence；同Tx按既定creation cause创建prospective实体，只要父身份/所需锁未变，仍由实际授权检查决定existing。plan不进入command语义摘要、不作为持久AccessGrant；其构造/投影复制slice、pointer等可变值，拒绝JSON反序列化，通用fmt/日志不展开。

DiscoverAccess在上述依赖上加入本域command、quota、User/Project、Object/record锁；每个plan是该操作的**完整集合及预定模式**，Read intent不等于全部shared，例如建立reader lease仍需Object exclusive。object service只从已明确身份构造锁：ExecutionPayload的owner.ID是PayloadID，ExecutionAggregate必须取真实ExecutionID；SkillRevision/MeetingFile不能假设owner.ID等于SkillID/MeetingID，须由持久映射/创建或清理cause提供。正式Meeting文件仍走Artifact链路，不新增Meeting存储实体。Actor gate也不能只用Project共享锁替代；父映射或planner缺失必须拒绝/保留pending，不猜空集合。

AcquireAccessPlansInTx是唯一组合取锁入口：先核全部plan的service/请求绑定，合并其完整集合与外层extraLocks，重复key取最强模式，调用D03 AcquireAll **一次**后返回Tx绑定的LockedAccess。调用方不得先取得某个plan子集；extraLocks及每个plan原集合/模式都纳入token，不能只凭“其中一把锁相同”匹配。token由此入口铸造并校验同service、同live Tx，不能由调用方填held=true；零值、别的Tx/请求/plan、跨service token均拒绝。

所有对象操作InTx显式追加 `plan, locked`，首先调用ValidateAccessPlanInTx核对**本次完整操作请求**及token，再由AccessPlanner.ValidateInTx在同Tx重读依赖映射。当前所需key/模式有新增或身份变更即 `RESOURCE_BUSY`（安全原因lock_plan_changed、not_committed），外层必须回滚整笔Tx；不补高/低序锁、不shared升级、不自动重试。依赖核验通过后才调用现有Resource/ObjectRead/Lease/Cleanup/Project等authority做当前权限与gate检查；这些回调不得自取遗漏锁、嵌套Tx或提前被调用。权限仍先于幂等结果及version，不因plan成功而跳过。

普通Put/Lookup/Read/Stat/Cancel/恢复wrapper在每个DB阶段开Tx前DiscoverAccess，Tx内复用同一Acquire/Validate流程。外层Artifact/Skill等组合方先发现所有对象/source/lease计划和自身extraLocks，开Tx后一次AcquireAccessPlansInTx，再把匹配plan/token交各InTx；禁止在已开Tx中跑外部Discover来修补遗漏。Reserve预扫描若未见command而锁后发现其实际ObjectID不同，整体未提交并交调用方重采集，不能锁住prepared新对象却返回旧对象结果。Source的ValidateInTx消费同一组合token并重新授权原固定来源；completed重放只规划目标，保留§5不访问旧源的分支。

这是一项明确的Go调用兼容调整：B01修改 `contract/authority.go`、新增 `contract/access.go`（上述请求/计划/端口）、`contract/source.go` 及契约测试；object service与现有InTx调用点同步。D03既有API、00001–00004迁移及业务owner ID不变。验证探针只机械补Discover/合并Acquire/plan/token参数和Authorizations.Planner，保留原断言、故障、时序与真实资源；不为兼容旧签名保留不安全旁路。


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

锁按§3计划完整预收集，并由AcquireAccessPlansInTx一次交D03 AcquireAll：command→system/User→Project及其他前序gate→Agent/真实父aggregate→object aggregate（多对象按ID排序）→reference/记录。Artifact私有互斥仍用 `RecordLock(ReferenceRecordLock, "artifact:<id>")`；不新增/伪装foundation rank。所有后续对象InTx消费同一组合token，不再各自AcquireAll；任何Attach/Release/AcquireLease/cleanup仍受同Object锁保护。映射、集合或所需模式变化整笔回滚，不逆序补锁或shared升级。

外部 I/O 从不持 DB Tx/锁。删除先短 Tx 重验全部 references/leases/active writer 并持久 cleanup gate，此后新 attach/lease/publish 拒绝；再执行外部删除/内容清除与确认，最后短 Tx 持相同锁落终态。单个 abandoned attempt 的 gate 只禁止该 attempt 发布，不阻止同 ObjectID 的合法新 current attempt；整对象 cleanup gate 禁止全部尝试。引用不存在不是授权结论；缺真实 authority 或引用读取失败不能当“无引用”。

## 5. 流式准备、幂等与发布

单对象允许 **0–1 GiB**，声明 length 必填；MIME 经 `mime.ParseMediaType` 规范化、≤256 bytes，不信任其“可 inline”含义。expected_sha256 可选，提供则必须是全文 SHA256 且匹配。单对象过大沿用 `PAYLOAD_TOO_LARGE`/413；客户端短读、超长、声明 SHA 不符用 `INVALID_ARGUMENT`/400，`OBJECT_INTEGRITY_MISMATCH`/502 仅表示已存储/上游 payload 损坏。先当前授权/资源和限额预检，再读取任何正文；正式 Tx 仍重新执行当前授权→command 同义重放→gate/expected_version。

为满足可选checksum的幂等语义，raw-body型Put/CreateFromContent每次提交先用固定 **64 KiB** buffer流式写入owned spool，同时计算全文SHA，包括成功重放的本次正文。读取至声明长度后再探测一个字节，短读、超长、错误、SHA不符都失败；不能只依Content-Length、SDK前缀截断、ETag或caller metadata。prepared文件sealed后只读；同长度/MIME不同正文仍得到不同command digest。source/upload-ref型先按下段查成功结果，只有新建/恢复才读取来源。

spool 限额：同时 **2** 个准备/上传，每请求 1 GiB、总预留 **2 GiB**，入场先预留声明长度并保留 128 MiB 空闲余量；可重试的并发/预算准入不足沿用 `RATE_LIMITED`/429、不新增容量码、不等待无限队列；真实 ENOSPC/文件系统或后端不可用是 `DEPENDENCY_UNAVAILABLE`/503。目录 0700/文件0600，随机子名、拒绝 symlink、不使用业务 filename。准备受 ctx/15min 上限，I/O 源必须可取消/Close；取消关闭源/文件、释放预算。所有远程读写有 ctx、connect5s/header15s/idle60s/单请求15min/整个 Put30min 上限；不能通过放弃 goroutine 假装取消。

raw-body语义digest为canonical-v1：command/owner/scope/稳定Actor/expected_version、全部展示参数、规范MIME、**实测length+SHA**。提供正确expected_sha256与省略视为同义完整性约束；不含trace/session/临时路径。source/upload-ref型分列 `request_digest`（相同公共输入及原ref variant/业务ID/显式revision或upload receipt ID、展示参数）与 `resolved_source`（首次捕获的object_id/固定revision/length/SHA及其digest）；调用方无需知道第一次解析的内部digest。

source/upload-ref命令已completed时，先验证**当前目标结果可见权**，再核原request_digest一致，返回原结果；不重新解析、授权或读取source，不重新消费receipt。源后来删除/失权/receipt已消费不阻断该分支；输入ref/revision/展示参数变更仍 `IDEMPOTENCY_KEY_REUSED`。新建/未完成恢复则必须当前target/source授权及gate，短Tx持久首次resolved_source和source lease后才能复制；恢复沿原捕获事实重验，不静默改为最新源。最终发布前再验当前源权限，commit unknown先查目标命令是否已completed，不能先因源已消失误判失败。只读LookupPut不读正文，仍检查当前结果可见权。

新命令在短Tx持久pending object、原command/owner reservation、唯一attempt key与必要lease；确认commit后才向MinIO写。预留commit unknown按原command/attempt查询，未核实不外发；即使查到committed，也须比较**本次请求的完整语义digest**再返同义成功，不能把只读LookupPut的“该key成功”当作另一份正文成功。异义仍IDEMPOTENCY_KEY_REUSED且不外发；普通失败沿原key/语义恢复，不另造ObjectID，已发布对象不重传/覆盖。

从 sealed spool 向全新 private candidate key 做 **DisableMultipart + length + 完整 SHA header + If-None-Match:\*** 单 PUT。每个 physical attempt 独立 key、仅一次 SDK 尝试；412/丢响应均转原 attempt 核实，不能认为未提交或无条件覆盖。恢复重新发外部尝试必须仍当前授权、同 command/digest、独立 key并追踪旧 unknown key；每命令最多2个、全局64个未收敛 attempt，超限 `RESOURCE_BUSY`、只核实/清理旧尝试。读取 candidate 的完整流重新计算 SHA/实际长度（内存固定 buffer）并对照准备事实后，保存 verified 事实；HEAD metadata/ETag/checksum 不能代替这一校验，尤其不接受 composite checksum。

最终发布Tx重验actor/owner/gate/原命令及verified candidate；**existing owner**由authority证明业务实体与引用目的已存在，在同Tx置available、reservation→canonical并记录command/Audit。**prospective owner**只有持久创建cause、实体尚不存在：置available但保留reserved，签发未消费UploadReceipt，成功仅表示payload已存储；不生成canonical、不授予普通Read/Stat/下载。Artifact内联/复制组合在最终Tx先创建真实业务行，再完成对应canonical转换；发布失败留下可定位candidate，提交unknown沿原事实查询、不重写available对象。

AttachObjectInTx先验证真实业务实体已在同Tx存在、exact owner/scope/cause、available/non-cleaning、当前授权；若本对象属于prospective上传，必须原子消费匹配reservation及receipt，转为canonical并记录消费目标。ConsumeUploadInTx是显式携receipt的同一逻辑入口，receipt绑定原稳定actor/owner/object/创建cause；裸object_id不能绕过此校验。重复同一已完成消费只返原reference，换owner/重复异义消费拒绝。与Cancel竞争共用object锁：Attach先提交则Cancel不得撤canonical；Cancel先提交则Attach拒绝，不能同时成功。

spool不是唯一恢复依据。启动持目录锁后按自有manifest/DB attempt对应关系检查遗留；只清确属已终止进程且不再需要的owned文件。payload已在MinIO时按固定key/length/SHA核实后恢复；spool不在且payload不完整时需原调用方重交同语义输入，不能造空对象。自动恢复只推进存储核实/清理，失效业务权限不能被后台Service代替Owner来发布。

正常Discard/关闭清理也可能在删除body与manifest/清理标记之间崩溃。必须凭owned身份、持久manifest/DB状态及实际终止或清理事实识别这些半完成状态，幂等删除残项、重复unlink的ENOENT按已完成该步处理；不能仅因“manifest在而body已删”永久拒绝启动。缺失仍必需的正文保持原attempt待重交/恢复，未知归属文件不批量删除；这是临时文件清理可恢复性，不放宽业务payload校验。

CancelUpload覆盖pending及**available但仍reserved/未Attach**：原actor通过当前身份/上传归属核验，或可信cleanup通过持久cause核验后，在gate允许的短Tx撤销receipt、仅释放本上传reservation、禁止Attach/发布并创建可恢复cleanup operation；无TTL推断，也不能拒绝一切已存储对象而留下永久孤儿。活动source/read/transfer lease仍保护实际I/O，远端unknown按第6节收敛。原成功storage command仍保留committed历史；取消后LookupPut只返授权可见的revoked/object_id/cleanup_state，Put重放返回 `RESOURCE_DELETED`（原提交committed），不再交可消费receipt、不重传或复活。已有canonical者只能由真实业务生命周期Release，不能用Cancel越权撤回。

## 6. 读取、reference/lease 与清理

StatObject仅查询DB metadata：当前actor/owner/scope/read gate及canonical reference或受保护固定版本使用事实仍须核验，但不打开MinIO、不创建reader lease、不证明payload存在/可读；stored state=available不是本次存储健康结论。prospective reservation不授权普通Stat，其原actor经LookupPut查看上传状态。

ReadObject在短Tx做相同当前读取授权，再验available/non-cleaning并持久reader lease，commit确认后才打开MinIO。真实GET的状态/headers/首段或空流必须在交付reader/HTTP成功头前验证，不能留下GetObject的惰性404尚未触发。返回 `ObjectReader{Meta,ResolvedRange,ReadCloser}`；reader与其他opaque handle一样显式拒绝JSON反序列化，构造器拒绝所有可nil动态类型的nil body；EOF/错误/Close都先真正关闭并join源I/O，再释放lease，提交unknown则保留lease按ID重试。

OpenUploadSource仅供新建/恢复的内部复制：当前actor、原owner/cause、unconsumed receipt、available+reserved及non-cleaning全部核验后取得source lease，才开流。它不把reserved变为canonical、不开放普通Read/下载或raw-ID授权。源上传若被Cancel撤销，则阻止后续读取/发布，已有流的lease保留到实际Close/join；复制成功不隐式消费别的owner的reservation，原actor或可信cleanup仍可显式撤销该源上传。

### 与业务事务组合的SourceReads

已验ReadObject/OpenUploadSource是自行Tx并立即开流的wrapper，不能用于“首次resolved_source、source lease与业务command同Tx”的步骤。B02使用独立SourceReads适配器；旧AcquireLeaseInTx仍只接受execution/history/transfer，不用它伪造Process/source lease。SourceReads复用同一Object Service的Store/Process/AccessPlanning与生命周期跟踪，缺正式resolver或planner明确DEPENDENCY_UNBOUND。

`SourceLease`是私有issuer签发并登记的opaque句柄：绑定本SourceReads适配器/Service、当前Process、origin Tx、完整Actor与ResolvedSource、服务生成的随机LeaseID。对外仅投影安全LeaseID用于command checkpoint；不暴露issuer、Tx、locator，不接受JSON重建，通用fmt/日志安全且嵌套值复制。公共类型构造/调用方持有LeaseID均不等于登记，零值、foreign issuer/service/process、替换Actor/source/origin Tx一律拒绝。

外层在Tx前规划 `SourceAccess{AcquireSourceAccess,Actor,ResolvedSource}`，与target/command/Audit等所有计划及extraLocks一次Acquire；完整request绑定原ref及receipt、owner、ObjectID/scope、固定revision、MIME/length/SHA和metadata版本。Source acquire模式为Object EX，不能拿ValidateSourceAccess的共享计划代替；依赖漂移沿§3整Tx回滚。AcquireSourceInTx先验证同live Tx的plan/token，再实际调用resolver.ValidateInTx重读原业务ref→owner/object/revision及当前权限；resolver接受对应source操作的完整计划，不自行Discover、补锁、nested Tx或外部I/O。

Acquire随后在同Tx重验对象available/non-cleaning、精确分区与内容事实；Uploaded来源还须原actor/cause/unconsumed receipt+reserved，其他来源须canonical或已有exact protected-use授权。新source lease本身不能反过来证明源可读。只插入本Process的 `owner_kind=source, owner_id=LeaseID` active行并返回句柄，不读取MinIO、不另开Tx/commit；Artifact在**同一外层Tx**首次持久原输入摘要、resolved_source和该LeaseID。失败须整笔回滚，不能先提交command再补lease。

Open只能在外层WithinTx返回后调用。拒绝仍live的origin Tx；Tx失效、拿到句柄或callback成功均不证明提交。Open在新短Tx前重规划完整source依赖，在锁下读取**已提交**的exact LeaseID/object/source kind/owner_id/当前Process/active事实，并重验原内容事实、resolver当前来源授权及Object/Project gate；该lease行是与原command同Tx写入的提交证据，外层也沿原command核对checkpoint，不能用caller布尔。行未观察到、已释放/错配或任一权限拒绝均不开流；原提交unknown时保留checkpoint等待核实，不能仅凭暂未读到行宣称回滚。**本次Open核实Tx本身也必须明确committed**，unknown/not_committed均不得启动GET；不增加D03提交钩子或通用结果表。

句柄的单次开流/取消由适配器私有状态线性化，再核DB事实：并发Open最多一个取得开流资格，Cancel先胜则永远不能再Open；一旦尝试外部GET，同句柄不得再发第二次，失败重试须重新授权取新lease。仅提交核实unknown、尚无GET时可沿原checkpoint再核实。Open与Cancel竞争中，取消须关闭真实源并join实际I/O后才能release；reader EOF/错误/Close同样复用§6完整性、末段holdback和关闭规则，不把局部状态标closed当作网络已停。

CancelSourceLease在外层Tx回调外使用，未开流/开流失败也必须调用或由适配器收敛；不需重新取得业务读取权，因为它只撤自己的技术保护，不授予读取。先永久禁止该句柄新Open、终止/join本句柄实际工作，再以预规划的内部maintenance阶段有界释放本Process/exact LeaseID；重复Cancel/Close幂等。ctx耗尽、未join、释放commit unknown或DB不可用保留可观察checkpoint/lease，后台只重试同一技术收敛，不假完成或改用后台Service授权开流；沿既定全局shutdown/force预算，不新增无限等待。

重启不从安全LeaseID反序列化旧开流能力。先由ProcessAuthority证明exact旧实例确已停止，才回收其source lease；另一活实例/未知死亡仍受保护。恢复未完成Artifact命令须当前原主体授权、重验原已捕获source事实，在新组合Tx取得本Process新lease并替换command checkpoint，之后才Open；不得静默解析最新source或凭TTL猜死。若旧实例仍活且原工作未确认停止，保留pending，不并发接管同command；completed重放继续不访问source。

### 流式读取与清理

ByteRange为单一 `[offset,length]`，非负offset、正length，检查整数溢出/是否超对象；末段按声明上限截取，offset越界返回range error，空对象只支持全读。拒绝多range，不把服务端忽略Range的200当206；验证Content-Range/实际长度。全文读计算SHA；range只验证区间边界/长度并依赖上传时验证及不可变存储，不把局部digest冒充全文。payload404=`OBJECT_PAYLOAD_MISSING`；存储长度/SHA错=`OBJECT_INTEGRITY_MISMATCH`；Stat仍只表达metadata。

全文reader保留固定 **64 KiB末段holdback**（不足则全部保留），流式累计SHA及精确length，到源EOF、额外字节检查和SHA均通过才向caller释放末段；0字节也先完成空流校验。内存仅固定buffer/holdback，不整份缓存。小对象因此在首字节前即可检错；大对象可先交付已读前缀，末尾校验失败时返回明确错误并扣留末段，使caller不会先拿齐声明Content-Length再才得知失败。range末段可沿同一机制确认区间长度，但无全文SHA保证。

首字节/成功头前失败可投影安全错误；HTTP状态/前缀已发出后只能失败中断stream，不能改状态、追加Problem JSON或召回字节。HTTP适配须执行真实abort（如 `http.ErrAbortHandler`），使完整Content-Length响应不被伪报完成；内部copy/SDK caller同样必须检查最终read error。sent_bytes仅计输出接口实际接受的Write字节，不表示远端已收妥；最终SHA、读写错误及sent/failed阶段按实际attempt审计，不把started当完整交付。无论在哪一阶段失败，实际close/join之后才可释放lease。

read lease 只在本实例 reader 实际关闭/join 后释放；跨重启按确认死亡的进程实例恢复，不能按 TTL 猜测另一个活实例已经结束。长时间持有/取消由实际 ctx/stream 跟踪，清理见到活动 reader 必须 pending。永久 ref/运行/history lease 无自动过期；D10/D22/各领域按正式生命周期释放，D05 不从运行时间推断无引用。

Release 只撤指定 owner 引用，不隐式删 payload；DeleteUnreferenced 需要正式 cleanup cause，检查所有有效 ref、reservation、reader/transfer/writer lease。明确失败/孤儿也经持久 cleanup operation：请求停止本域 writer、等待 owned goroutine join、禁止新写和新授权，再清精确 key，确认后提交 deleted；无效对象 ID 不能变成 bucket 批量删除权限。已经放弃的 writer/PUT transfer lease 只保护其受控收敛；可经可信取消先禁发布并清其 staging 内容，reader/有效业务引用仍不能被越过，正式 lease 未收敛前整体仍 pending。

**迟到写入边界：**一次网络错误/HEAD404/expiry/客户端 join 不证明外部 PUT 已结束。只有 key 从未暴露外部写权限且全部写已取得确定终局响应、无待发/在途时，才采用 DELETE→实际 absence 核实。其他 canceled/unknown/Runner staging 在持久 cleanup gate 后，以 fresh bounded ctx **无条件 PUT 0 bytes 的技术 tombstone**，仅成功响应并完整读回0 bytes/空SHA、无旧user metadata 后确认业务内容消除；同 key 的旧 payload 请求全部带 If-None-Match:*，不得再覆盖 marker。每次清理≤15s，超时保持 pending/checkpoint，不无界等待。[R02 实测](d05-object-storage-research.md#81-实际并发顺序与完整读取)为慢旧PUT先200、无条件zero随后成功、完整空读与旧签名/SDK条件重放412后仍空；root已采纳。它不证明删除marker安全，也不替代B01的领域gate/恢复并发验收。

可能存在迟到写/旧 grant 的技术 marker **首版永不自动删除**，不按 TTL、HEAD404、URL expiry 或一次成功 fence 移除。marker 只有原随机 key、固定技术 Content-Type/空payload，无 Project/Owner/名称/原checksum/业务 metadata；DB 项目事实清理后不保留项目关联，其不是用户 payload/归档副本。即使内容已清空，业务 deleted/Project completed 仍须正式 ref/lease 收敛；不能用 marker 跳过 D17 停止确认。已发出未终局的 marker PUT 只可能继续写相同空内容，不能使业务正文复活。

取消/进程中断后，恢复程序按 persisted key/attempt/marker 逐项核实；清理成功而 DB commit unknown 时按原 cleanup operation 重读/重新验证空 marker，绝不重新发布该 key。未见对象只记 not_observed，不把未知归为已清空。健康/清理有界重试，无通用结果仓库或自动 TTL。若未来加入 multipart，须在新规格实现独立 fresh bounded ctx 的 abort/list-incomplete 恢复，本次禁用后仍应实测无残留 multipart。

批处理必须在安全前提下公平前进：Project cleanup通过持久游标/checkpoint或等效轮转越过本批仍有lease/ref的对象，后续批可处理其后的可清对象，到尾后再检查先前阻塞项；不能反复LIMIT同一批阻塞前缀。Recover对其它活实例的ConfirmStopped返回RESOURCE_BUSY时，只保留该实例/条目pending并继续本实例和其它独立可收敛工作，不伪造死亡也不终止全轮。单项依赖失败保留可观察结果、不误报completed；全局DB不可用或本轮ctx到期可结束本轮。无论暂停或重启，checkpoint不能丢弃尚未处理项或把剩余计数当0。

Project archive 保留内容，禁止新上传/Artifact创建/Runner grant；合法人类 read/下载及其 Read Audit 继续。Project permanent delete：D08停止各领域，Artifact释放本域引用，Object按原ProjectID清候选/对象/transfer/receipt，完成全部业务内容删除或经核实的零marker替换后才能清本域项目metadata；他域 protected reference/未知Runner transfer均阻止 completed。不删其他用户/项目对象，不在System scope保存项目正文或Audit副本；单域通过不代表全Project删除已绑定。

## 7. B02：Artifact 组合服务

`ArtifactRef` 与 `file_ref/image_ref` 只含 Project/Artifact/file 业务身份和安全 metadata。kind 首版 `generated|user_upload`；name 为1–255 UTF-8 bytes、无路径分隔/控制字符，description≤4KiB；不以 name 作唯一键。来源 Execution/Operation/creator 均由受信调用上下文确定，不接受模型伪造 User/Project 权限字段。

`CreateFromContent(ctx,actor,meta,project,name,media_type,utf8_content,description?)` 的inline UTF-8内容≤1MiB（D18/D21 Tool参数可更小）；`CreateFromSource(...,BusinessFileRef)`；`CreateFromUpload(...,UploadReceipt)` 给人类业务上传。后者仅对exact目标Artifact prospective owner完成首次绑定：同Tx先建Artifact、ConsumeUploadInTx转换reserved→canonical并记录command/Audit，不重传已验证payload。已有其他业务owner的文件仍走CreateFromSource复制，不将两Artifact共享同Object。三路径均不能先返可见Artifact再异步补payload。

BusinessFileRef 先覆盖 D01 UploadedObject/ArtifactFile/KnowledgeFile/ExecutionFile，拒绝 raw object ID 冒充 source。ArtifactFile 必须匹配真实 artifact_id+file_id；UploadedObject 需校验单 actor/owner/cause 的 UploadReceipt，不能按可猜 ObjectID 接管。MCP/Runner 来源经 D18/D20 正式 resolver 返回既有业务 ref；未来增加 ref variant 要显式注册真实 provider，本次未知 variant/未绑定 provider 拒绝，不默认读取 MinIO。

CreateFromSource先执行第5节completed重放分支；新建/恢复才当前source授权→用§6 AcquireSourceInTx在command同Tx持久首次object/revision/length/SHA和source LeaseID→确认提交后OpenLeasedSource→**同一条源流**读入受限spool并算SHA→全新ObjectID/key→发布新Artifact。UploadedObject同样走该组合端口并验receipt/reservation，其余来源要求合法canonical/固定版本引用；不能用自行Tx的OpenUploadSource/ReadObject替代原子获取。所有未开流/失败路径CancelSourceLease，已开reader必须Close/join。不共享StoredObject、不dedup、不采用先校验再复制可变源的TOCTOU。未完成命令发布前在完整锁计划下由resolver重验已捕获source的当前可读权，删除/失权则拒绝；已completed只检查目标结果与原输入，不再要求源存在/仍可读。源以后删除也不影响新Artifact内容或同义成功重放。

`ListArtifacts(ctx,actor,project,filter,page)` 只查询当前可见 Artifact metadata。过滤 execution_id/kind/media_type/name_query；name_query≤256 UTF-8 bytes、按文字子串转义SQL通配符。默认50、1–200；`created_at DESC,id DESC` keyset，复用 D04 cursor，以 resource=`artifact` 的查询域+Project/filter/order完整绑定，防Audit cursor被同scope复用。未知来源标签按安全ID保留，不复制正文。

`ReadArtifact(ctx,actor,artifact_ref,offset?,limit?)`：文本默认8KiB、最多64KiB预览；offset按字节且须UTF-8边界，limit尾部截到完整rune，返回实际offset/next_offset/truncated+file_ref；无效UTF-8退回明确file投影，不造替换后的canonical正文。图片返回image_ref，PDF/Office/音频/压缩包/其他binary返回file_ref+MIME，不引入解析/embedding。小范围文本可有界缓冲，canonical对象始终流式存储。

list/read/create 的 Audit 保留真实 Agent/Project/Execution/Operation/Tool/Artifact/source/object IDs、MIME/size/phase/result；列表用 artifact_collection，不存结果正文/名称搜索串。稳定调用attempt/cause参与AppendKey，重放不重复成功事件；HTTP trace不混成业务attempt。文本/列表先完成业务读取，再在返回前当前授权+Read Audit；审计提交失败/unknown则不交付结果。暂无 delete-artifact Core Tool；Project/业务清理走正式生命周期端口。

## 8. 浏览器短期业务下载 URL

root 已确认：浏览器使用 **Central 签名业务下载 URL**，每次使用重新校验当前 Session/Owner，经 ReadObject 流式返回；Runner另走下节直连。这符合 D01 的 streaming 内部适配，不让 Agent 取得URL，也不暗中把 Runner 下载改为Central中转。

下载核心使用正式typed Download provider，避免把generic BusinessFileRef强行审计成Artifact；最小端口放B02的object download contract：

```text
ResolveDownload(ctx, Human, BusinessFileRef) -> DownloadTarget{ResolvedSource,安全filename}
ValidateDownloadInTx(ctx, tx, Human, DownloadTarget, plan, locked) -> error
AppendDownloadInTx(ctx, tx, Human, DownloadTarget, DownloadEvent, plan, locked) -> error
```

DownloadTarget固定真实provider/业务ref、owner/object/版本/内容事实及安全filename；闭集DownloadEvent仅含grant/真实attempt ID、issued/started/sent/failed、实际sent_bytes和固定reason，核心不接受任意Audit action/resource/producer。Resolve作当前业务解析，Validate在完整预收集锁下重读精确映射、当前Session/Owner/Read gate；Append由该业务provider构造真实typed Audit并调用既有AppendInTx，仍过D04当前权限与CheckAppendInTx。provider不能自行开Tx/commit、补锁或开流；核心始终先验证Human/原user/签名/当前grant，不能把provider存在视为身份授权。

B02真实绑定Artifact provider：必须读到真实Artifact/file→object事实，Audit使用原ArtifactDownload+ArtifactResource/真实ArtifactID+ArtifactProducer；Uploaded ref只在确为Artifact owner且真实Artifact已绑定canonical时可由此provider处理，prospective未Attach不因此可下载。Knowledge/Execution等及**非Artifact Uploaded owner**的provider未绑定即DEPENDENCY_UNBOUND，无payload或可用grant；不能拿DocumentID/ExecutionID/ObjectID冒充ArtifactID、使用object维护身份，也不新增公共object.download动作。D07/D08及D12/D18/D20等各领域负责以后真实解析/授权与本域typed Audit绑定，D27负责HTTP/UI；D05保留完整核心与拒绝未绑定行为。

issued与持久grant、started与本次下载attempt、终局sent/failed与真实计数分别同Tx追加对应typed Audit，Append失败整笔回滚；commit unknown沿原grant/attempt+phase核实，不换AppendKey或重发payload。issued/started未确认提交前不交付URL/输出字节；最终Audit失败保留真实传输结果及待核实状态，不把已发字节写成未发。后续每次GET/range仍重新调用当前provider验证，provider缺失、失权或Audit失败均按下文安全关闭，不能回退匿名/raw object读取。

`IssueDownload(ctx,Human,business_ref,mode:download|preview,expires_in?) -> PrivateSignedURL` 创建持久grant；默认60s、最大300s，绑定原user_id、业务ref/version、object_id、GET、mode/安全filename/MIME、expiry；它不固定长期AccessGrant。URL token=`base64url(canonical{v,kid}).base64url(canonical payload).base64url(mac)`，严格无padding；独立HMAC-SHA256输入为 `agenteam.object.download.v1`+零字节+前两段原始ASCII（含点）。payload含grant_id和全部上述绑定，编码复用canonical-v1，常量时间验证、整体≤8KiB。仅current kid签发，旧kid只验证至既有grant到期，未知kid拒绝，不静默换key。URL仅经当前Human的安全专用响应投影，无普通JSON/fmt展开，Cache-Control no-store。

`OpenDownload(ctx,currentHuman,token,range?)` 验签/期限/DB grant未撤销/当前Session与原user一致/当前业务Owner及scope，然后开真实ObjectReader；当前权限失败不透露对象是否存在。到期/撤销拒绝**后续请求**，同用户可在有效期内多次GET/range，不伪称一次性；每个真实reader独立lease。已开始stream按其实际reader追踪，撤销不声称已召回发出的字节；D07/D08后续可通过明确取消端口关闭相关reader。

成功输出字节前完成Read Audit；issued/started只表示准入。全文下载沿第6节64KiB末段holdback；仅完整校验且输出接口接受全部字节后记sent，读写/SHA失败记failed与真实sent_bytes，不能声称客户端已收妥。首字节后失败中断连接，不改已发状态，不因尾部Audit追加失败重发payload。Audit失败/unknown或打开MinIO失败时实际关闭/join reader后释放lease；未交付的流不算成功下载。

preview必须依据服务端sniff/允许集，而非信用户MIME：首版text/Markdown/JSON仅 `text/plain; charset=utf-8`，实际支持的PNG/JPEG/GIF/WebP才可image inline；HTML/SVG/XML/PDF/Office/未知内容默认attachment。不执行HTML转换或脚本，带 `X-Content-Type-Options:nosniff`、`Content-Security-Policy:sandbox; default-src 'none'`、安全Content-Disposition（RFC5987 filename、无CRLF）、private/no-store。HTTP绑定需单range206/不满足416/完整200的真实长度，不把token放日志instance/query或跨站Referer。

## 9. B03：Runner 单对象传输

### 正式端口与授权事实

`TransferService`消费真实Object Service、Audit及`RunnerTransferAuthority`，不实现Runner身份/Operation表。新增contract只依赖foundation/identity/object类型；RunnerID、OperationID、TransferID、EvidenceID为不同typed ID。最小公开形状如下，均返回既有Fault/commit_state：

```text
TransferSpec {runner_id, operation_id, direction, target, expires_in_seconds?}
target = Get {owner, object_id}
       | Put {owner, upload_command: CommandMeta, manifest: TransferManifest}
TransferManifest {media_type, length, sha256} // 规范MIME；0..1GiB；全文SHA必填
TransferEvidenceRef {evidence_id, kind: completed|stopped} // 仅持久证据索引
IssueTransfer(ctx, actor, issue_command: CommandMeta, TransferSpec) -> TransferGrant
InspectTransfer(ctx, actor, TransferID) -> TransferStatusView
CompleteTransfer(ctx, actor, TransferID, completed_evidence) -> TransferStatusView
CancelTransfer(ctx, actor, TransferID, command: CommandMeta) -> TransferStatusView
ConfirmStopped(ctx, actor, TransferID, stopped_evidence) -> TransferStatusView
RunnerTransferAuthority.Discover(ctx, TransferAccessRequest) -> AccessDependencies
RunnerTransferAuthority.ValidateInTx(ctx, tx, TransferAccessRequest, AccessDependencies)
    -> TransferAuthorization
```

TransferGrant安全投影固定transfer/runner/operation/object/direction、manifest、原expires_at；私有`TransferMaterial`闭包只向受信D17适配器显式投影method/URL/必需headers/**实际wire_expires_at**，复制可变数据、拒JSON解码、fmt/log不展开。Inspect只返D01 `pending|complete|failed|unknown`及revoked/lease/cleanup安全状态、ID/稳定错误；不返回URL、key、证据正文。D17将material映射到独立Runner协议，Runner不得import Central。无真实authority时所有业务端口返回DEPENDENCY_UNBOUND，不装匿名HTTP或默认成功provider。

TransferAuthorization是当前回调的opaque结果，绑定完整请求摘要/Actor、真实Project/owner、Runner认证代次、Operation/Execution、允许的当前操作与版本；证据按用途分支：`completed`绑定exact完成evidence ID/digest及可信单次完整length/SHA，`lease_retirement`另绑定停止/准入核实的证据ID/digest，只有该grant全部在途已join且签名新请求准入已关闭才授予lease释放，部分证明只记待收敛checkpoint。前者不要求deadline已到或后者已成立，不能用前者替代释放lease的证明。服务逐项比对，不能缓存为长期bearer、接受调用方构造的“已授权/已停止”值。Resource/ObjectRead/Lease/Cleanup/Project/Audit等旧必需端口仍真实绑定，TransferAuthority不默认为这些端口授权。

authority必须从当前Runner注册/认证代次、Operation/Execution及其固定输入/输出事实核实本Actor、Project、指定Runner实际承接本Operation和目标用途；PUT的owner/manifest必须与可信输出声明逐项相同，不能凭调用方自报owner、boolean或“上传成功”字符串。GET另过当前exact ObjectRead/Resource授权及Project gate，目标须available、non-cleaning且有canonical或正式固定版本保护；pending/reserved不能读。PUT沿当前owner创建cause和Mutate gate，Avatar/System不是本Runner项目传输的旁路。已归档不签发新传输、不发布新payload；精确终局/取消证据可按注册Converge职责收敛，不复活Operation或授予后台读取权。

EvidenceRef本身不可信。ValidateInTx须重读D17已认证、已持久且绑定**同Runner代次/Operation/Transfer/direction/object/manifest**的事实：completed证明实际单次对象传输完整length/SHA，可在deadline前驱动GET complete或PUT校验/业务发布；stopped证明该grant对应所有已启动请求确实结束/join且不再由该Runner继续，仍须另核签名新准入关闭才足以retire lease。两者均不能由RPC断线、超时、Central死亡或单个HTTP200推断。`ConfirmStopped`只登记停止/retirement事实，条件未齐保持lease，不把未传完对象标complete；GET Complete不由Central重复GET“证明Runner收到”。取消或权限变化后仍可验证限定终局证据，但不得继续读/发布业务内容。

### 00007事实与完整锁

| 事实 | 固定字段、约束与状态 |
| --- | --- |
| `object_transfers` | issue command identity/semantic_digest、原稳定Actor、Project/真实owner、Runner/Operation、direction、ObjectID/UploadID、固定manifest、原deadline、lease ID；PUT staging attempt ID及current private candidate ID；version、phase=`issued|completing|complete|failed|unknown`、revoked_at、分列的completed与lease_retirement证据ID/digest（后者不覆盖原完成事实）、cleanup checkpoint/公平扫描序；唯一issue identity及lease/staging关联，无URL/credential/业务正文 |
| 既有`upload_attempts`追加kind | `private_candidate`为旧行default，仍要求真实process/spool、`candidate/<随机ID>`；`runner_staging`要求process/spool为NULL、`staging/<随机ID>`及exact transfer关联，禁止本地writer伪身份；旧phase/checkpoint沿用，但staging永不进入verified/published、永不成为objects.available locator。用分支CHECK替代原全局NOT NULL/key CHECK，旧分支约束不弱化；双向关联可用同Tx可延迟FK |
| 清理/lease复用 | staging从首次Issue起即有原upload_attempt与cleanup_operations可用关系，不另造“无attempt预约”；PUT专用事务可为同Tx新pending对象建立`TransferOwner(TransferID)` lease，process为空；旧公共AcquireLeaseInTx仍只对available工作。GET复用旧available lease协议 |

外部状态pending映射issued/completing，未知I/O映射unknown；明确失败/取消映射failed，已确认complete保留历史结果且可另标revoked。lease与storage cleanup单独记载，complete不等于物理清理完成。PUT的upload_command是D17同一输出的稳定对象命令；新的传输attempt不能换此命令另造ObjectID。原issue key相同但Actor/runner/operation/direction/owner/object/manifest/期限请求异义即IDEMPOTENCY_KEY_REUSED；trace/session不入摘要，当前授权仍先于幂等/version。旧key重放不换deadline/key/TransferID；已撤销、过期或不再允许传输的终局，Issue返回INVALID_STATE而Inspect保留安全原状态，无新material。新的传输尝试须新issue key、当前授权和原输出身份；已available输出不能再签PUT。

`NewTransferAccess`闭集操作为issue/material/inspect/capture/reserve_candidate/publish/cancel/confirm_terminal/cleanup；完整绑定Actor、命令、spec或持久grant身份、证据/cleanup cause、exact object/upload/staging/candidate/lease。`NewTransferAccessPlanner(basePlanner,runnerAuthority)`把transfer请求交后者提供**完整owner/Actor父依赖及Runner/Operation依赖**，其余请求仍交base；真实authority实现负责通过各领域正式映射组成完整集合，不只返回Runner锁。Discover只收集锁与映射，Validate才授权。无相应provider拒绝，不能以空base替未来owner规划。维护请求绑定原持久技术cause，不能用后台Service替Owner通过读取授权。

新增本域command和`SystemConfigLock("object-transfer-admission")` EX（32个未收敛grant）、原`object-attempt-admission` EX（64全局/每upload2个未清attempt），并合并真实Actor/User/Project/Agent/Execution/Operation、owner父gate、Object EX、transfer/lease/reference记录；inspect仅无写部分可SH。Runner注册变更与校验统一使用`SystemConfigLock("runner-transfer-authority")`，签发/核验SH、D15/D17变更EX；这是精确端口约定，不虚造Runner aggregate rank。所有plan和调用方extraLocks仍一次AcquireAccessPlansInTx，不在authority回调补锁。issue预扫未见而锁后出现原grant/不同ObjectID，或任何父映射/所需模式变化，整Tx RESOURCE_BUSY/not_committed并交外层重采集；不自动重试或偷补锁。

### Issue、实际签名与PUT组合

Issue的短Tx先当前授权、幂等、gate，再同Tx写grant/固定UTC整秒deadline/外部lease及`object.transfer.issue` Audit。PUT同时写原objects.pending/uploads/reserved reference与runner_staging attempt；GET捕获已available不可变candidate key及完整metadata。未确认commit不得交URL；unknown先等原command锁终局，按完整请求核实原事实，不能HEAD404后重建。默认60s、允许1–300整秒，deadline一次取DB时钟截秒加duration；等待/恢复耗时不续期。所有SDK签名/可能的region lookup均在Tx外，region固定us-east-1、path-style，无默认credential链。

仅`transfer_storage.go`访问SDK私有backend：PUT用PresignHeader签**host、Content-Type、Content-Length、x-amz-checksum-sha256（32字节digest的base64）、If-None-Match:\***；GET固定available key/GET，响应部分读取不构成完整成功证据。不得用只签host的PresignedPutObject、暴露candidate PUT或COPY。SDK v7.3.0内部取time.Now并向下取整duration；生成后必须解析/严格核method、配置origin/bucket/exact key、签名header集合/值、X-Amz-Date与X-Amz-Expires，实际wire截止不得晚于原DB deadline。重放按剩余整秒签，只能缩短；跨秒/暂停导致超界的材料直接丢弃并失败，不能顺延DB expiry。返回前再短Tx重验当前授权、同grant未撤销/未到期及固定事实，提交unknown仍不公开material。

OBJECT_TRANSFER_ENDPOINT和主endpoint均是固定部署origin，使用同bucket/region/credential/TLS策略；§10启动必须通过两origin读到同store identity及同一实际probe内容，拒绝误接不同后端。签名safe projection不允许调用方改host/path/header或任意endpoint。这里只验证Central到配置地址与存储同一性，不冒称真实Runner网络已验证；D17负责Runner可达性，失败不能fallback Central。

PUT Complete先同Tx验证当前权限/可信completed证据、固定manifest、未撤销且可继续的原上传，取得本Process读取staging的真实source lease和恢复checkpoint（完整plan预含Object EX）；confirmed commit后才读。**一次实际GET流**进入原PreparePayload的有界spool，独立检测短/长读并确认全文SHA，然后创建全新private candidate。不存在先GET校验后再GET/Copy取另一版的路径；Preparing的本地reader须Close/join才释放source lease，外部transfer lease仍保留。组合的最小私有入口在新transfer_upload.go：

```text
reserveTransferPUTInTx(ctx,tx,transferBinding,manifest,plan,locked) -> UploadAttempt
prepareTransferPUT(ctx,transferBinding,stagingAttempt) -> PreparedPayload
reserveTransferCandidateInTx(ctx,tx,transferBinding,stagingAttempt,PreparedPayload,plan,locked)
    -> UploadAttempt
```

transferBinding只能由TransferService从当前authority+持久grant构造，绑定原Actor/owner/两个command/ObjectID/manifest/lease；不是公开的authorized boolean。第一入口不伪造PreparedPayload/SpoolPayloadID；第三入口必须查真实Prepared registry且内容逐项匹配manifest，在**同原uploads/ObjectID**新增标准private candidate。其后复用原UploadPrepared→全文verify→PublishVerifiedInTx；最终外层Tx一次取得transfer publish、普通owner publish等完整plans，重验当前Runner/Operation/source/owner gate并同时提交原对象结果、transfer complete及Audit。内部InTx不另开Tx/取锁或外部I/O；普通wrapper只在外层调用，不能嵌进InTx。

staging也计入原attempt限额，因此第二个candidate失败须先安全清理后才可再建；不得排除staging偷扩配额。Complete重试先查当前授权及原结果，已有verified/published candidate走原事实，**不再读staging/上传已发布对象**；还需读取时仍重新当前授权并沿原manifest，不自动改最新输出。完成/预留/lease checkpoint的unknown均先核实，不凭callback成功继续I/O。成功上传但prospective未绑定的receipt/canonical转换仍沿§5，transfer成功不代替D17业务实体的原子引用绑定。

### 撤销、外部lease与恢复

Cancel短Tx持久revoked/禁止后续签发与发布、同义幂等revoke Audit，并按真实cause撤销本PUT尚未附着的reservation/gate原attempt；已经canonical不借Cancel删除业务引用。S3 URL是bearer，不能密码学绑定HTTP调用者为该Runner；逻辑撤销/到期无法召回已发URL/stream。条件PUT在key存在时412，删除后原URL可能再成功，因此staging永远按§6零marker收敛，首版不自动DELETE marker，也不把Central本地writer结束误作远端已停。

在持久staging cleanup gate下可清零该exact staging并完整读回空内容，即使其外部transfer lease尚active；这是**只清staging**的窄例外，仍保留lease和总体pending。不得绕过活动本地source reader、canonical或其他用途lease，也不得将该例外套到private candidate。原cleanup_operations的claim/fence/checkpoint继续防迟到worker；失败有界返回pending/unknown，不无限等慢Runner。原service gateAttempt对staging固定zero_marker；stopWriters/recoverAttempt/joinedAttempt/releaseStopped/spool恢复只处理真实private_candidate/本Process，不能把核实staging置verified或因Central死亡释放TransferOwner lease。

外部lease释放使用独立lease_retirement授权，必须同时满足：原DB deadline及所有已签材料已不能发起新请求、D17可信证据确认该grant全部在途已结束、无本地source/writer I/O；PUT另需staging marker已核实。单次completed不自动证明以上条件；到期本身、一次HEAD/404、断连或“设置revoked”也不满足终局。为避免时钟推断，retirement证据还须明确按受信存储时钟/服务端拒绝结果确认签名不再接受新请求；D05未收到该证据时保留lease，不能只看Central wall clock。已complete但仅缺retirement证据时不降级业务结果，lease/cleanup仍pending；GET缺可信completed证据才保持pending/unknown，不重发Runner操作。staging长期marker无业务内容/Project/Owner/name，删除Project时只保留该随机空key。

transfer恢复按有界公平批次读取原checkpoint，逐项推进已授权技术核实/清零/释放，单项活实例、未绑定或未知不阻断其它可收敛项；不自动续签、重发Runner命令或代Actor发布。Project cleanup持同Project EX/完整对象计划，先撤销本Project grants，尚active lease阻止对象/metadata完成；staging清零与可信外部终局分别验证。最终在旧CleanupProject删除本对象metadata的同Tx，先删除00007对应grant/证据关联/manifest等业务事实，再删原attempt/lease/object；FK拒绝漏清。最终门禁阻止迟到Complete重建行；最小项目清理回执不存原参数/manifest，其它Project/System不受影响。Inspect/Complete遇已清除事实不能复活，D08以后通过正式生命周期组合端口推进。

Audit复用已验ObjectProducer、object/object-maintenance职责及ObjectTransferIssue/Complete/Revoke、真实TransferID资源；原initiator安全ID与Operation关联保留，cause绑定本次持久phase。issued/complete/revoke均与本域事实同Tx，权限仍走既有CheckAppendInTx；unknown不换AppendKey、失败不伪造sent/成功事件。PUT源码损坏返回OBJECT_INTEGRITY_MISMATCH，客户端错误声明为INVALID_ARGUMENT；存储不可用/缺payload及额度错误沿既有码，不泄漏URL、bucket/key或SDK原错误。

## 10. 初始化、健康、关闭与验收

B03 config/check-config验证§2全部必需字段、两个固定endpoint/TLS/CA、独立下载keyring及spool绝对路径语义，未知OBJECT配置键拒绝；check-config不连接或创建目录、不输出原值，help/version无需配置。真实启动仍检查实际文件owner/mode/symlink/锁。`config`仅保存安全opaque配置，只有object组合拥有SDK/backend/spool；不会把存储凭据或下载签名环放进中立platform/Runner。

新增`OpenProcessGuard(spool,ProcessID) -> *ProcessGuard`实现现有ProcessAuthority；组合根把它绑定进Service.Authorizations.Processes。`NewRuntime(service,guard,transferService) -> *Runtime`核同一Service/Spool/Process/Store绑定但只构造，随后`Runtime.Initialize(ctx)`完成下列真实初始化；`StartMaintenance/Check/StopAdmission/Drain/Force`分别承担后续worker、健康和生命周期。业务authority可保持未绑定，但构造成功绝非初始化/恢复成功；不注册业务HTTP，不把nil provider改为生产allow实现。

DB阶段预算沿D03；DB完成后Audit/Secret/Outbound/对象共用D04既有 **30s SecurityStartupTimeout** context，更短parent优先。Runtime按剩余预算完成Service.Initialize的bucket/control绑定、ProcessGuard的DB绑定、存储probe和恢复门禁；不能再追加30s、15min I/O或独立恢复预算。失败/耗尽不监听，迟到初始化得到的FD/Transport仍纳入同一次关闭。旧Initialize仅验证bucket/control身份，旧Check只检查该存储控制面；新增Runtime入口不得把二者当作已完成所有安全步骤。

00007追加`process_claims`：ProcessID、store identity、专用spool deployment UUID、实际目录/claim文件身份、host/boot身份、claim nonce、`claimed|stopped`和终局时间。ProcessGuard在spool已取得排他目录锁后，于同一owned父目录的固定`OBJECT_SPOOL_DIR + ".processes"` sibling保存0700目录/0600单链接文件及deployment identity，不向旧spool manifest目录插入新格式；拒绝symlink、身份/权限不符，fsync后持每实例claim文件lifetime flock。DB绑定必须核已confirmed store identity与磁盘claim一致并确认commit，之后才准入业务I/O；ProcessID随机且不可复用，不能凭调用方ID新造“旧实例已死”证明。

ConfirmStopped核原受信DB claim、同专用deployment/spool/file身份、可信稳定host身份，并非阻塞取得exact旧实例flock；当前boot ID必须读取真实kernel身份，不能来自调用方。相同boot下的旧锁释放可证明该本地实例终止；同一受信稳定host上的boot ID与原claim不同，则是旧kernel中该实例已终止的正向证据，可自动推进恢复，不要求人工删checkpoint。已持久的同实例真实join终局也可核实。缺claim、文件替换/失配、活锁、跨host或无法确认的host/boot身份均拒绝，不用可复用PID、TTL/心跳或仅目录锁；旧ProcessID不可重用。正常关闭须Runtime与Service全部本地准备/读写/probe/恢复I/O真实Close/join后才持久stopped并释放FD；未join的Force路径保留claim直到实际进程退出让OS释放。同host SIGKILL或受信boot变化只证明**本地进程**死，绝不证明远端MinIO请求或Runner已终止；旧unknown写沿marker策略，外部lease仍待§9证据。此机制不增加分布式进程判活平台。

00007另有有界`startup_probes`技术checkpoint：随机ProbeID/ProcessID、`control/probe/<随机ID>`、固定length/SHA、`reserved|verified|cleaning|complete`及cleanup mode/fence，不含业务ID/正文。正式`Service.ProbeStorage(ctx,guard)`先短Tx预留并确认，再以32随机字节做条件single PUT，主/transfer两origin实际GET核同一全文SHA，最后删除并经两侧读回缺失确认；全过程无DB Tx跨网络。仅本地writer真实join且获得确定写终局、从未对外签发的probe可DELETE；丢响应/commit unknown保留原事实核实，可能迟到的写只能zero_marker并读回空，不能DELETE→重写空档。probe错误使本次启动失败，后续启动先按原checkpoint恢复；不列bucket删除未知key、不改control/store-identity、不追加业务Audit。正常路径必须实际证明put/get/delete权限，不能以bucket查询代替。

恢复门禁先真实读DB、spool/claim及probe状态。已绑定正式Planner/authority时，启动完成有界必需恢复扫描：验证持久gate/引用/lease/attempt关系，按kind核实旧本地进程、候选与staging；已安全保护的活lease/远端unknown可保留pending交worker，结构/权限/存储错误不得伪称已恢复。当前生产Planner/D17未绑定时，只有真实证明**无业务objects/uploads/attempts/references/leases/transfer及待处理业务下载事实**，且spool无未决文件，才可完成技术空态启动；已有最小清理receipt不充作活动业务。可凭ProcessGuard与DB无引用事实收敛确属本域的空态孤儿/probe；任何需要未来域授权/父映射的遗留，或未知归属文件，均DEPENDENCY_UNBOUND/UNAVAILABLE拒绝启动并保留原数据。保持原Service.Recover对缺Planner的拒绝，不增加skip-recovery配置。

Central只调用Runtime.StartMaintenance；Runtime与旧Service.StartMaintenance共用Service现有mu保护的workerDone/workerStop/maintenance.Running槽位，任一入口已占用即拒绝第二个worker，不并行启动两套循环。Runtime同步推进core与transfer/probe，全部本地I/O（含初始化及迟到结果）登记同Service的operation/cleanup生命周期，整个worker存续期保持Running，原Service.Drain不能漏见它。每10s、每轮最多100项/15s（更短ctx优先）复用公平checkpoint；未绑定业务依赖时实际重验技术空态并维护probe，不假调用Recover成功。异常/依赖错误可观测，安全pending与已完成有别；活实例/未决grant不吞其它进展。D07/D08/D17以后沿同一组合根绑定正式分派，不直接读写未来业务表。

健康沿D04 **10s采样间隔、同轮2s有界context、成功样本超过20s陈旧即unavailable**；DB/object可并行但不重置本轮预算，各组件保存实际成功采样时间。Runtime.Check检查真实bucket/control、两个origin同backend及worker的运行/安全恢复状态，健康HTTP仅读样本、不每次造payload；错误立即unavailable，恢复须新真实成功。启动早期DB/对象样本若在listen前已陈旧，须在剩余启动预算内重验；不能新建monitor重置时间续命。诊断新增object_storage及object_authorization/runner_transfer_authorization的安全状态，不输出endpoint/bucket/路径或证据正文；后两者仍unbound，整体ready=false/503。

第一信号停新对象操作/grant和新恢复batch，已准入HTTP/对象stream/上传/短Tx按原全局shutdown budget drain，不立即cancel全部HTTP/reader BaseContext。StopAdmission不得禁止同一已准入操作必要的后续DB checkpoint/join；持久外部transfer lease不是本地goroutine，不等待Runner的未来证据才让Central退出。先等待唯一Runtime worker及Service所有本地I/O真实join，再关闭idle连接/spool、释放ProcessGuard，最后关闭DB。超时/第二信号沿D04同一个**额外最多1s force context**取消全部登记操作并关闭主/transfer两套Transport，Runtime/Service都在此ctx内join；DB在剩余force预算最后关闭，不无界等未join资源，也不按资源/迟到初始化各加1s。未join报告forced/unknown，保留spool/checkpoint及claim直到实际进程退出；不能伪称drained或释放外部lease。

普通 `check-go.sh` 不启Docker。`test-objects.sh` 用nonce/label/exact-ID生成owned PG+MinIO+临时CA/spool，只读固定源码/产物创建fixture；成功/中断最终删除owned资源并检查残留。真实Central进程测试必须带MinIO，`test-postgres.sh/test-security.sh` 复用或创建已验证owned descriptor，不能跳过旧入口测试或注入禁用MinIO开关。fixture credentials随机，不读现有服务/外部秘密；专用套件缺fixture/构建/网络条件即失败。

| 块 | 必须实际证明的场景 |
| --- | --- |
| B01 完整性/流 | 0字节、小文件与≥64MiB对象真实put/get/range；长于声明/短读/错SHA拒绝；固定buffer/spool预算/取消/遗留清理，正常spool删除body后/删除manifest前crash与反复重启恢复；seekable前缀陷阱拒绝；Stat在payload缺失/MinIO不可达时仍只查授权metadata、无I/O/lease，Read才真实报错；0/≤64KiB错SHA在首字节前失败，大流末尾损坏保留末段、送出字节少于Content-Length并中断；无完整body后才报错、无整块缓冲 |
| B01 Tx/恢复 | 同key同长度不同body冲突，包括expectedSHA省略；权限先于重放；Reserve COMMIT丢回复后查到旧成功，异义本次正文仍冲突且无外发；payload写成DB失败/unknown沿原attempt恢复；成功重放不重写canonical；available缺payload/篡改明确失败；late writer/refs+lease竞争无误删；慢条件PUT与零marker barrier、旧grant重放仍空、marker提交unknown/重启及永久保留；正式lease未知仍pending |
| B01 完整锁计划 | ExecutionID≠PayloadID真实互斥，Skill/Meeting child ID不冒充父ID；Actor Agent/Execution gate实际挡住状态变更；规划后映射/command实际ObjectID改变整Tx未提交且不自动重试；owner/read/protected-use/lease/cleanup/maintenance全部覆盖；多plan+extra完整union一次AcquireAll、强模式预定；错误Actor/intent/object/cause/source/plan/Tx/service/token拒绝，不能只匹配subset；缺planner拒绝；阶段内不得补锁/升级/再Discover |
| B01 公平恢复 | 第1–100对象长期lease阻挡时，第101个可清对象在后续有限批次真实清理，前100仍pending；重启checkpoint继续且解除lease后旧项可完成；其它活实例拒绝死亡确认时，本实例cleanup在同轮仍推进、活实例保护不变；单项失败不吞无关进展、总体不假completed |
| B01 绑定/撤销 | existing Put原子available+canonical；prospective available+reserved不允许普通Read/Stat/下载；内部source只由匹配receipt及当前权限开lease；Attach/消费同Tx与Cancel竞争仅一方成功，重复消费幂等；未Attach成功上传可撤销并最终清理，跨actor/owner/cause拒绝；取消后原Put/Lookup重放仅安全revoked结果、无可消费receipt/新写/复活；active reader仍阻止物理清理；无canonical的固定版本须exact read grant和active稳定lease同Tx核实，错object/owner/类型/已释放lease拒绝，外部pointer变更不改变grant；Reader拒JSON及typed-nil body |
| B01 公共兼容 | 全部D04旧Audit/Actor/迁移输入仍合法；新增action/resource/producer/cause拒绝错配；archived合法读可追加Read Audit且同Actor写被拒、admin不代Owner、缺Audit不返回未交付内容；对象维护不能冒充Artifact业务主体 |
| B02 Artifact | inline/upload/source三路径真实payload与原子绑定；source复制后ObjectID/key不同；首次resolved事实持久化、未完成恢复重验当前源并沿原版本；completed后源删除/失权/receipt消费仍可按原输入重放且source resolver/存储调用0次，目标失权先拒绝、改ref/revision/展示参数冲突；commit unknown先查完成态，无双Artifact；cursor/UTF-8/binary/image安全投影 |
| B02 SourceReads组合 | 首次source事实/command/Process-source lease同Tx：rollback三者均无、Acquire无存储I/O；callback内/live或foreign Tx、伪issuer/Process/Actor/source/plan拒绝；原commit unknown以及Open核实Tx unknown均无GET，已提交exact lease/command核实后才可开；Acquire/Object EX与Validate共享计划不可替换；获取后来源撤销/映射漂移开流前拒绝、发布再重验；并发Open/Cancel至多一次GET，取消先胜零GET，真实Close/join前lease保护删除；未开流/失败Cancel、释放unknown、重启exact-death与当前授权重领，活旧实例不接管；不得用旧wrapper拆散原子步骤 |
| B02 下载 | 真stream/range、到期/改签名/kid/跨user/跨Project/Session撤销/Owner变化拒绝；每次授权/lease；HTML/SVG/MIME伪装不inline，header不可注入；敏感canary不泄漏；Audit失败前不交付；尾部SHA/写失败真实中断、HTTP状态不重写/不追加JSON，sent_bytes与实际Write一致，未join不释放lease，最终Audit失败不重发 |
| B02 下载provider | 真实ArtifactID/resource/producer及issued/started/sent/failed审计和grant/attempt同Tx；provider验证后权限变化仍锁后拒绝，归档Read合法；非Artifact Uploaded/Knowledge/Execution未绑定无URL/字节/伪Artifact Audit，prospective上传不授权下载；错ref/object/版本/filename/provider匹配拒绝；Audit rollback/unknown无未确认URL或前置字节，已发前缀只记录真实计数且不重发；纯核心provider替身不能充当真实Artifact绑定验收 |
| B03 权限/锁/unknown | 缺D17/owner/Lease/Audit provider无URL；错Runner代次/Operation/Actor/Project/output manifest/evidence拒绝；真实Runner变更/Operation取消/Project gate与issue、publish竞争；所有plan一次完整Acquire，漂移回滚；Issue/材料准入/Complete各真实COMMIT丢响应或回滚，未核实无URL/新I/O，异义冲突；完成重放零staging GET/PUT |
| B03 直传/完整性 | 真实presign PUT/GET，改method/key/length/SHA/签头拒绝；32grant及原attempt配额；实际query到期精度、跨秒/暂停不延长deadline、旧issue key不续期换key；条件重放412及删除key后旧URL可重放的真实边界；一次staging流→真实spool→不同private candidate，注入读取期间替换/损坏仍不发布错误字节；空/大对象及wrongbody/断连unknown；canonical从未有对外PUT材料 |
| B03 外部终局/清理 | 自报HTTP200/TTL/断线/Central SIGKILL不能release；deadline前可信单次completed可完成GET/PUT业务发布，即使其它在途/新准入终局尚未知，lease仍active；仅齐备独立retirement证据才释放，缺该证据不降级complete，wrong/重复证据不误收敛；staging源reader活动阻止清零，源join后外部lease仍在可清零但总体pending；慢旧PUT/zero marker真实barrier、旧URL重放仍空；staging不进入通用verify/publish或本地Process释放；Project最终事实清空、迟到Complete不复活，未决首批不阻断后续 |
| B03 migration/兼容 | 00001–00006输入不变，已有private candidate/spool/ref/lease/Artifact/download完整回归；00007旧行kind迁移、staging NULL分支/FK/禁止canonical约束；Issue后尚无正文即取消可走原cleanup，不出现无attempt永久孤儿；每upload stage+candidate实占原2attempt限额 |
| 最终初始化/恢复 | 真实空库/升级、主/transfer origin同identity+probe及错误alias；probe确实put/get/delete，任一步失权/故障与unknown重启保留精确key且不删他物；缺Planner真实空态可启动诊断，存在待授权事实/未知spool拒绝；claim创建/DB绑定unknown、活FD/伪造或缺失claim/跨host及未知身份拒绝判死；实际子进程SIGKILL同host恢复，受信同stable host的旧boot绑定/exact flock分支可恢复、错host/文件反例拒绝，两者均不释放外部lease；TLS/私有bucket/CA/目录门禁不省略 |
| 最终预算/进程 | 前序安全初始化耗时后对象只用剩余30s/更短parent，超时不监听；健康同轮2s、真实故障/恢复/20s陈旧，旧DB样本不重置时间；Runtime/Service两入口任一先启动均拒第二worker，Runtime工作可被原Service.Drain观测；真实上传/reader/probe/恢复中首信号drain，第二信号/超时主与transfer Transport全部共享额外1s force、DB最后，不按资源叠加；全部本地I/O真实join或OS退出前claim不释放，checkpoint保留；无owned fixture残留，整体ready=false |

每块从仓库根执行 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/check-go.sh` 和 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/test-objects.sh -run '<实际场景名>'`；最终无过滤 `test-objects.sh` 并覆盖D04 `test-security.sh` 受影响真实输入。网络/存储barrier检查服务端实际key/版本/请求计数与DB事实，不只检查客户端error；真MinIO证据、fixture指纹、命令exit和清理状态一并交接。

技术输入为已冻结[研究修订2](d05-object-storage-research.md)，SHA256 `b4a0462d255910443cbed31abbaeee6d17c133b74a2d997357e37d938242f038`；其协议局部观察不是本模块实现验收。rev6另核实际[Objects/Uploads/Process端口](../../../internal/central/object/contract/authority.go)、[Access](../../../internal/central/object/contract/access.go)、[上传](../../../internal/central/object/upload.go)、[恢复](../../../internal/central/object/recovery.go)、[初始化](../../../internal/central/object/initialize.go)及[00005](../../../db/migrations/00005_object_storage.sql)，SDK v7.3.0的api-presigned.go/api.go和signer源码仅作签名API证据。B03新增行为仍须上表真实验收；无新产品决定或设计阻塞。本文未实现业务、改动代码/迁移或创建运行资源，按冻结指纹独立静态审查后再由root下发实现。
