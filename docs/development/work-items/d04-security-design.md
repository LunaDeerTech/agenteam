# D04 安全基础实施规格

- 修订：2；输入：`main@9beaa7f`、[D04 主卡修订 1](d04-security-foundation.md)及已采纳的 S01 集中审查修正。本文是 S01 设计，尚未实现或运行验收。
- 依据：[计划 D04](../development-plan.md#d04-secret-出站与-audit)、[部署密钥与恢复](../../architecture/platform-infrastructure/deployment-runtime.md#6-secret-management-与-envelope-encryption)、[出站规则](../../architecture/platform-infrastructure/outbound-network-policy.md)、[Audit](../../architecture/security-governance/audit.md)、[D01 基础](d01-contracts/foundation.md)、[生命周期](d01-contracts/domain-lifecycle.md)、[Model/MCP lease](d01-contracts/model-tool.md)。
- 已核对实际 [D03 Store/Tx](../../../internal/central/postgres/transaction.go)、[SQLExecutor](../../../internal/central/postgres/sql.go)、[锁构造器](../../../internal/central/foundation/lock.go)、[Central 装配](../../../internal/central/app/app.go)；沿用 Go **1.27.1 / GOTOOLCHAIN=local**、pgx **5.11.0**、Goose **3.28.0**、PG **17.8** / vector **0.8.1** 的既有隔离 fixture。不加运行依赖，不改已验收 D03 机制。

## 1. 分块、所有权与未绑定边界

按真实依赖调整为下列串行提交；主线程已采纳。每块包含实际实现、异常路径、相应集成测试和独立验收；不是以最后一块补空实现。

| 卡 | 完整结果 | 独占新增范围 | 顺序修改的共享文件 |
| --- | --- | --- | --- |
| B01 | typed Audit 同事务追加、授权查询/清理端口、独立签名 cursor、配置与诊断装配 | `internal/central/identity/contract/` 最小身份类型；`internal/central/audit/`（含 `contract/`）；`internal/central/cursor/`；`db/migrations/00002_audit.sql`；`tests/security/audit_*` | 下述共享范围；不实现账号/Session/Owner |
| B02 | Secret envelope、引用/lease、nonce 预留、启动验证、可恢复重保护与停机 | `internal/central/secret/`（含 `contract/`）；`db/migrations/00003_secret.sql`；`tests/security/secret_*` | 同下；消费 B01 真实 Audit |
| B03 | DB 策略保存、即时发出门禁、受控 HTTP/安全拨号、真实网络 fixture、最终装配 | `internal/central/outbound/`；`db/migrations/00004_outbound.sql`；`tests/security/outbound_*`；`tests/testsupport/outbound/`；`scripts/test-security.sh` | 同下；消费 B01 Audit、B02 凭据边界 |

共享写入按 B01→B02→B03 串行移交：`internal/central/config/`、`internal/central/app/`、`internal/platform/logging/` 中安全阶段的中立枚举/方法及测试、`tests/process/` 中 Central 配置/进程测试、`tests/testsupport/postgres/cmd/fixture/main.go` 的测试包选择、`docs/development/backend/README.md`、`AGENTS.md` 的已实现配置/命令说明。`db/migrations/embed.go` 仅在实际清单要求时修改；既有 `00001` 不回写。root 独占主卡/台账/计划；若需其他范围，先向 root 报具体原因。

所有 SQL migration 使用 D03 的事务标记与 Goose Up，追加同一序列；当前无非事务 SQL。不创建 User/Session/Project/Provider/Execution/Outbox/业务变量表，不添加后续模块 HTTP 路由。Runner 不 import Central；中立 logging 不接受 Secret/Actor/策略领域对象。

`identity/contract` 只落实 D01 Actor/Scope/AccessIntent/AccessGrant 及本次所需的当前 Session/System 授权窄端口；不生成身份、不提供默认成功。Audit/Secret 自有 Project 授权、gate、生命周期 cause 验证窄接口，由 D08 在组合根适配；不为本次复制 Project 事实表。Actor 不从 JSON 权限字段构造，Service 只由组合根注册固定职责与 cause。

D07 绑定 Session、SystemAdmin、CSRF、SMTP 协议和管理 HTTP；D08 绑定 Owner/gate/清理参与者；D09/D10/D20 分别绑定 Provider、变量白名单、MCP provider binding；D28 做全部组合验收。任何未绑定授权/引用解析端口返回 `DEPENDENCY_UNBOUND`，相关业务服务不能被调用；当前生产只装诊断、内部密钥维护和出站基础能力，无匿名 Audit/Secret/策略 API。

## 2. 公共执行与错误约束

直接复用 `Store.WithinTx(ctx,cause,callback)`、`InTx(tx)`、`AcquireAll(ctx,tx,requests)`。一次预收集锁集合，按 D01 command→system-config/user→Project→Agent→aggregate→记录顺序获取；共享锁不能升级，发现新低序资源整体回滚重采集。`...InTx` 不另开事务、不提交、不访问网络，不把 callback 返回 nil 当成提交成功。

业务写入在同一 Tx 中做当前权限→原 command identity/语义重放→当前 gate/expected_version→本域 mutation+Audit+结果 receipt。成功 receipt 由 Secret/策略各自保存，不建通用事务结果库；Human 摘要只用稳定 user_id，排除 session_id/trace。`unknown` 保留原 cause，沿本域 receipt、轮换 checkpoint 或策略版本查询，读取失败保持 unknown。

本域错误采用私有 cause 的安全 Error 类型，`Error/Format/MarshalJSON/LogValue` 只投影白名单 code/field/阶段；含敏感内容的对象用私有闭包/显式访问，不依赖“字段未导出”防递归 fmt。公共 Fault 仍映射既有 `INVALID_ARGUMENT`、`FORBIDDEN/NOT_FOUND`、`INVALID_STATE/RESOURCE_BUSY`、`DEPENDENCY_UNBOUND/UNAVAILABLE`、`COMMIT_UNKNOWN` 等；详细安全 reason 留本域，当前不扩展全部 HTTP code。

## 3. 部署配置与进程顺序

下列名称均带 `AGENTEAM_CENTRAL_` 前缀。JSON 拒绝未知/重复字段、重复版本、尾随值、超限；错误只含固定字段名和 reason，不输出输入、解析器原文、文件内容或 key fingerprint。

| 字段 | 值与默认 | 验证 |
| --- | --- | --- |
| `CURSOR_KEYRING` | B01 起必填；`{"format":1,"current_kid":"c1","keys":[{"kid":"c1","key_b64":"<部署生成>"}]}` | 1–32 把独立随机 32-byte key；标准有 padding base64 严格解码；kid 为 1–64 位 `[A-Za-z0-9_-]`，current 必须存在；JSON ≤16 KiB |
| `SECRET_KEYRING` | B02 起必填；`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"<部署生成>"}]}` | 1–32 把随机 AES 256-bit key；version 为规范正 int64 十进制字符串，当前版本必须存在；JSON ≤16 KiB；禁止重复材料 |
| `OUTBOUND_CA_FILE` | B03 可选，无默认文件；系统 trust roots 上追加 PEM CA | 显式配置却不可读/无 CA/格式错即启动失败；不用它替代 D03 DB CA；无 skip-verify 开关 |

Cursor keyring 与 AES keyring 必须独立；加载两者时拒绝相同 raw key，不从 AES key 派生 cursor 签名。Cursor 轮换保留旧 kid 只验签，新签名只用 current；移除旧 kid 后旧游标明确失效，不要求无限保留。keyring 在进程内不可变，改环境需要重启；不从 DB/UI 读取原始 key，不自动生成部署 key。

`--check-config` 只做语法/长度/重复材料/CA 读取校验，不连接 DB，不能宣称 canary 已通过。正常启动：配置→D03 DB/迁移/健康→cursor→Secret key registry/canary/写版本 fence 验证→恢复轮换 worker→DB 出站策略镜像→监听。每块仅增加当块已实现依赖；B03 最终缺必需密钥或策略载入失败不得监听。

新增安全初始化预算 **30s**（常量，受全进程 StopContext 取消），不挪用 DB startup timeout；全存量重保护后台分批，启动只做有限必需验证。诊断只返回组件 available/unavailable/unbound 与安全阶段，最多附写版本/轮换状态/剩余数量；不返回 key/ciphertext/credential_ref 清单/策略网段。`/readyz` 继续非 ready，身份/对象/业务未绑定仍明确显示。

第一信号停止接新业务与新后台 batch，既有 HTTP/受控出站/短 Tx 在原 shutdown budget 内 drain；不得先取消全部 HTTP BaseContext。维护 worker 完成当前短 Tx 后退出；受控 Client 关闭 idle 连接且拒绝新请求，在途保留至 drain。最后停止 DB admission/drain。超时或第二信号取消在途并 force-close socket/DB，保留 checkpoint；强关不伪装外部未发出。沿用 D02/D03 单一关闭预算与资源归属，不启动遗留 goroutine。

## 4. B01：Audit 与签名 cursor

`audit/contract` 只依赖 foundation、identity contract；typed `Entry` 输入包含 scope/project_id、真实 actor、人类/Agent/系统 kind、action/outcome、resource kind/id、typed metadata 及 D01 的可选关联 ID，**不接收 AuditID/created_at**。Audit 在首次持久 append 中生成并保存 UUIDv7 ID 与 UTC 微秒 created_at；同义重放保持原 ID/时间。scope 只能 System 或单 Project；System 不借可选 project_id 复制项目记录。actor 是代表谁行动，后台代执行不一律记 system。

提供 `AppendInTx(ctx,tx,Entry,AppendKey) -> AppendReceipt{audit_id,created_at}`、`LookupAppend(ctx,actor,scope,AppendKey,semantic_digest) -> committed{AppendReceipt}|not_observed`、`List(ctx,actor,scope,Filter,PageRequest) -> Page<SafeRecord>`、`Get(ctx,actor,scope,id)`、`CleanupProject(ctx,service,LifecycleCause,project_id,checkpoint) -> CleanupReport`。AppendKey 为 `(producer,cause_ref,ordinal)`；cause_ref 是稳定事实 ID 或 command identity 的 SHA256，不能放正文。重复 append 只在规范化语义 digest 相同才返回原 receipt，异义报错；caller 负责让业务和 append 共用外层 Tx，拿到 receipt 不等于外层已经 commit。

语义 digest 固定为 canonical-v1 的 `scope + 稳定 Actor 主体 + action/outcome/resource + typed metadata + 稳定业务关联 ID`：Human 只用 user_id、排除 session_id；Agent 使用固定 project/agent/execution；Service 使用注册职责和稳定业务 cause。排除服务端生成的 AuditID/created_at、HTTP trace/session 等传输易变字段；重复提交不覆写首条记录的诊断关联。`request_id` 若代表实际 Runner/Tool attempt 则是稳定业务 ID，参与 digest，与 HTTP trace 明确分字段；不同实际 attempt 必须使用独立 AppendKey，不能把尝试去重成一次事实。

提交 unknown 通过正式 `LookupAppend` 核实，先做当前 Session/scope 授权；内部 Service 查询还须匹配注册 producer/职责/cause，仅返回自己的最小 receipt，不成为通用审计浏览器。已见同 key 异义报错，未见返回 not_observed，读取失败保留 unknown；未见不证明旧 Tx 已终止。越权调用不得获知事件/receipt 是否存在，失效 Session、跨 Project、管理员非 Owner 仍拒绝；不新增 Agent 查询 Tool 或未经授权的 HTTP 路由。

入口不接 `map[string]any`/RawMessage/任意字符串 metadata。注册 action→固定 metadata struct/outcome/resource 约束，默认拒绝未知 action/字段；新增领域随所属模块显式扩展。D04 首批：`secret.create/update/delete/resolve`、`secret.master.register/rotation.start/rotation.complete/rotation.failed`、`outbound.policy.update`、`outbound.access.deny`。metadata 仅允许各动作需要的 ID、固定 reason/consumer、changed_fields 枚举、版本、数量；无 URL path/query、自由文本、密钥材料、命令、Prompt/Response、原始错误。通用框架不能通过“脱敏字符串”重新接受任意内容；规范化 metadata ≤4 KiB。

`audit_records` 保存上述列、受约束 JSONB metadata、安全内容 digest 和唯一 AppendKey；约束 scope/project_id 一致、outcome=`success|denied|failed|unknown`。提供 append/query/授权清理仓储，无普通 update/delete API、无 TTL、无启动删历史。数据库由迁移账号管理不等于防恶意 DBA 的不可篡改日志，本次不造额外密码学审计账本。

Project append 的外层命令已持相同 Project gate，并调用真实 gate 端口验证 cause；Audit 不在 aggregate 之后补拿低序锁。当前无 D08 绑定时生产 Project append/清理不可用；测试专用 Authorizer 必须能返回拒绝/取消/故障，不能进入生产装配。成功敏感 mutation 缺 Audit 即回滚；拒绝/外部结果的独立 Audit 使用稳定 cause 的新短 Tx，不能塞进将回滚的业务 Tx，也不能用 unknown Audit 宣称原 mutation 成功。

每页/详情先验证当前 Session 与 scope：Project 只接受真实 Owner；System 按 D07 系统审计权限；系统管理员无 Project 旁路，Agent 无 Audit 查询端口。允许读取 archived 项目既有审计；永久删除后不得靠旧 cursor/旧幂等结果重放正文。

Filter 固定为时间 `[from,to)`、actor kind/id、action、outcome、resource kind/id、tool/execution/operation/approval/runner ID；另 `agent_id` 代表 `actor=该Agent OR resource=该Agent`，与其余条件 AND。无 metadata/正文搜索。排序固定 `(created_at DESC,id DESC)`，查询 `tuple < cursor_position`，多取一条判断 next；默认 50、1–200，不用 offset，不承诺跨页快照。

Cursor 逐字实现 [D01 编码](d01-contracts/foundation.md#cursor)：签名 header 含 version/kid，canonical-v1 payload 含 scope、完整规范化 Filter digest、固定 order 与 typed position，token 三段 base64url；HMAC 固定域/零字节，常量时间校验，token ≤8 KiB。缺省筛选先展开，limit 不参与 digest，空/省略等价须显式定义；不接受算法切换、额外 JSON 字段或重复 key。非法签名/kid/版本/范围统一 `CURSOR_INVALID`，不能先返回越权结果。

索引至少有 Project/System 两种 scope 的时间/id、actor、action、resource、关联 ID 的适用部分索引；以参数化组合 SQL 和足量 fixture 的 EXPLAIN 验证常用过滤，不强制小表走索引。SafeRecord 仅返白名单字段，展示 summary 从 action/metadata 生成，关联对象缺失只保 ID，不复制被删正文。

清理仅接受 D08 已持久 `deleting + stopped` 的 LifecycleCause；分批 **500** 条、按原 ProjectID，短 Tx 取得 Project gate并重验，返回 pending/completed 及 checkpoint。归档不清理，普通 Owner/admin 调用不能跳过生命周期；迟到 append 不得在删除后重建记录。未知提交重读剩余记录继续，不用累计删条数证明完成；System 记录不动。D08 保存参与者进度，本次不复制完整 Project 删除引擎。

## 5. B02：Secret 存储、nonce 与使用

Secret contract 拥有稳定 `CredentialRef`、`CredentialLeaseOwner{kind:execution|model_call,id}`、lease ID 和私有 `SecretMaterial`；服务提供 create/update/delete、metadata、引用保留/释放和 D01 三个 lease 端口。普通 DTO/API 无 plaintext read；System 与 Project scope 不互换，Project Secret 元数据的 name/description/变量规则由 D10 拥有，本次存储不另建变量系统。单值 **1–65536 bytes**，按原字节加密，不 trim，不假定 UTF-8。

最小持久数据：`secrets(id,scope,project_id,purpose,version,current_payload_id)`；`secret_payloads(payload_id,owner_kind,owner_id,scope,format,algorithm,data_nonce,ciphertext,wrap_nonce,wrapped_dek,master_version,wrap_revision)`；`secret_references(credential_ref,consumer,owner_id)`；`secret_leases(id,credential_ref,owner_kind,owner_id)`；`secret_command_receipts`；`secret_master_registry`；singleton `secret_control`；`secret_rotation_runs`。约束/唯一键保证 scope、引用/lease 去重及版本一致，所有 project-scoped 数据都能按 ProjectID 授权清理。

每次创建/覆盖产生新的随机 32-byte DEK 与 payload UUIDv7；AES-256-GCM、96-bit nonce、128-bit tag，ciphertext 存 tag。DEK 只加密这一个 payload，data nonce 由 `crypto/rand` 产生；重试新值不重用旧 DEK。wrap 也是标准库 AES-256-GCM，master 包装恰好 32-byte DEK；重新保护只改 wrapped_dek/wrap_nonce/master_version/wrap_revision，不重加密业务 ciphertext、不推进 Secret.version。

AAD 用有版本的固定二进制编码，固定域+零字节后依次编码 format、scope kind、Project UUID（System 全零）、owner kind/UUID、payload UUID；data 与 wrap 域不同。wrap AAD 另绑定 master_version、data nonce 和 ciphertext 的 SHA256。所有固定长度/枚举/算法/正版本先验，格式未知/换 scope/换 owner/改任何 ciphertext/tag 都安全失败；不尝试旧算法或明文降级。nonce、AAD、ciphertext 不进入通用 DTO/日志。

`secret_master_registry` 永久保留 version、非敏感 key identity fingerprint、nonce high-water、canary nonce/ciphertext、retirement 标记；不存 raw key/DEK。fingerprint 固定为 `SHA256("agenteam.master.identity.v1" || 0 || raw_key)`，全历史唯一且不公开：同 raw key 换版本号也拒绝，退休后不删该防复用记录。

所有 **master AES-GCM** 加密共用 nonce 域：12 bytes=`ASCII ATK1`+big-endian uint64 counter；counter 从 1 开始，最多 `2^32−1`，溢出拒绝并要求部署换新 key。nonce 区间按 **1024** 个在独立短 Tx 原子推进 high-water，只有确认 commit 后才能取用；unknown 区间全部丢弃，不猜是否提交，重新申请。每次加密前原子消费一个，失败/回滚也烧掉；进程退出烧掉区间剩余部分。canary、DEK wrap、receipt digest wrap、rewrap 全部计入，不能为 canary 固定 nonce。

区间准备发生在最外层业务 Tx **之前**。`PrepareWrite/PrepareRewrap` 返回不可序列化的已密封候选，携带写 epoch/身份/AAD，不持明文；`ApplyPrepared...InTx` 只校验并写 DB。若实现需要 InTx 消耗预备 nonce，耗尽只能返回 `SECRET_PREPARATION_REQUIRED` 整体回滚、在外层补准备，不另开 Tx/倒序补锁。未经确认的预留区间和候选不能跨进程恢复使用。数据库备份回滚会回退 high-water；D28 恢复流程必须先部署全新 master version/material，再允许写入，不能沿回滚计数继续旧 key 加密。

canary 是固定已知域文本的 GCM 密文，AAD 绑定 registry version/fingerprint。新 key 先持久登记 fingerprint/high-water，再按已提交区间加密 canary 并条件落库；中断留下 pending registry，下次校验 fingerprint 后用新 nonce 补齐。未有 canary 的 key 不得保护业务 payload。启动对所有配置中已登记 key 真正 Open canary，并验证每种必需 master_version 至少一个受保护 DEK（有存量时）；同 version 换错材料、缺少任一 live payload/receipt 必需旧 key、canary 解密失败都拒绝启动，不只检查版本字符串。

`secret_control` 保存 current_write_version 与单调 write_epoch；首次初始化或提高版本在 `system-config:secret-write-key` exclusive 下进行，禁止版本回退/退休 key 再当写 key。准备候选可在锁外生成，应用 Tx 预收集该锁 shared、Project gate、credential_ref aggregate；重读 epoch，过期候选拒绝重准备。其他配置 reference/lease 同样在 credential_ref 锁下改，调用方须在捕获 Tx 前预收集低序锁；lease InTx 不解密、不网络、不另开事务。

Secret 命令 receipt 原子保存安全原结果和**另一个 envelope 加密的 canonical-v1 语义 digest**；不能把低熵 Secret 的裸 SHA256/HMAC 无保护地暴露在 DB。receipt digest 的 purpose/AAD 与业务值不同，其 wrapped DEK 同样纳入轮换/退休扫描；永久 Project 清理一并删除。重放先当前权限，解开原 digest 常量时间比较，同义返原 metadata 结果，异义冲突；不因旧 expected_version 失败，不回传 plaintext。准备失败不产生成功 receipt。

`Retain/ReleaseReferenceInTx` 由正式配置 owner 使用，未知 consumer/owner/scope 拒绝；future 模块在保存 canonical 引用同 Tx 注册保护，不让 Secret 猜测“没有引用”。Delete 当前授权且 version 匹配后，只在无 live reference、无 lease 时物理删值；否则 `RESOURCE_BUSY`。删除 Provider/MCP live 配置不自动删除共享 Secret，旧 lease 持续保护；历史 Audit 只留 ID，不阻止合法删除。

`AcquireCredentialLeaseInTx` 以 `(credential_ref,owner_kind,owner_id)` 去重，校验受信任服务职责、scope、真实 binding/执行身份与当前 gate；`Release...InTx` 依原 owner 幂等，不能释放别人的 lease。lease 无 TTL，不凭进程重启/断连回收；D09/D22 收敛与 D20 binding 生命周期负责合法释放，Project cleanup 必须停止并确认这些引用。

`ReadCredentialForRequest(ctx,service_actor,lease_id)` 重新验证 lease/当前执行 gate 与用途，在短 Tx 读 **stable ref 的当前值**、解密并追加 `secret.resolve` Audit；仅确认该 Tx committed 后把短寿命 SecretMaterial 交后端使用。Audit 失败/unknown 时销毁临时材料并返回错误，不外发；新的解析用独立 resolution ID，未知旧审计不代表网络已发出。禁止将 material 放 Snapshot/Transcript/普通 error/日志；用完尽早清零可控 byte slice，不承诺 Go GC 下完全擦除副本。

Model lease 保护稳定引用及删除保留，**不固定 Secret value revision**：轮换后下一 turn 取新值，已发请求不热换。MCP 的 shadow/credential provider binding 有自己的有效性与 revalidate 规则，D20 选择其真实引用/独立材料；不能把上述 Model 读取规则套给旧 MCP binding，本次不预建 MCP 固定版本协议。Runner environment/masking 留 D10/D16/D18 正式适配。

## 6. B02：可恢复重保护与旧 key 退休

部署加入新 key、保留所有必需旧 key并提高 current_version，重启成功后新 payload 只用新版本；DB 的持久 write epoch fence 拒绝旧进程/迟到候选继续按旧版本写入。正在旧 shared 锁中的短 Tx 必须先结束，版本切换才能提交，因此已提交旧值也在后续扫描范围；不能只凭 worker 游标扫到底推断已完成。

后台 `secret_rotation_runs(target_version,run_id,state,last_payload_id,processed,safe_error,updated_at)` 持久恢复，状态 `running|failed|completed`。每批 **100** 个仍非 target 的 payload：事务外读候选、解 DEK并生成新 wrap；短 Tx 按完整锁序重读控制 epoch/当前 payload+wrap_revision，仍相同才 CAS 重保护，与 checkpoint 同提交。值被覆盖/删除则丢弃候选再扫，不覆盖新值；外层未准备足够 nonce 先补足。failed 只存固定 code/安全 ID，启动从未完成 run 恢复，不自动换目标/跳过损坏行。

候选扫描涵盖值和加密 receipt digest，按稳定 payload_id 继续；每轮到尾后从头重扫非 target。最终在 `secret-write-key` exclusive 下确认：目标仍当前、所有表无旧 master payload/ref、无能再提交旧版本的 writer、无待生效旧 epoch 候选；以这次**完整反查**原子标记 completed/旧 key retireable，再允许部署下一次重启移除旧 key。旧版 nonce 区间/候选不能绕过 epoch；canary/高水位/防复用 fingerprint 作为安全 registry 保留，不算需保留解密 key 的业务引用。

轮换批次提交 unknown 按原 run/checkpoint 和各 payload 当前 wrap_version/revision 查询；重复 CAS 不能重复推进逻辑进度或遗漏旧值。worker crash/强关后只依 DB 恢复，不依内存计数。解密失败保留旧密文/错误和失败进度，read 涉及该记录明确失败，不能把损坏行标 migrated。单批失败时 Secret 组件明确 `unavailable`、`/readyz` 为 503，停止新业务写入，旧 key 不可退休；仅对已验证且不涉及失败 key/记录的无关值，在原权限/gate/lease 与成功 Audit 约束下允许有限合法读，不能据此把组件显示为 available。此运行中故障处置不放宽启动门禁：缺必需 key、坏 canary 或首次验证失败仍必须拒绝启动。

Project 归档保留 Secret 与 Audit；重保护只变加密封装，是受限系统维护，不读取/改写业务明文语义或启动 Model/Tool。永久删除由 D08 gate/cause 控制，Secret 参与者检查 refs/leases，不能伪报停止；cleanup 分批清本域 Project 数据和 receipt，后台重读不存在/正在清理对象后跳过，不重建。System registry/系统维护 Audit 保留最小版本/计数信息，不复制项目 Secret/Audit 内容。

## 7. B03：策略存储、分类与接口

`outbound_policy` singleton 默认 `version=1,rules=[]`，DB 权威；`outbound_policy_receipts` 存当前授权命令的原安全结果。规则为 `cidr,ports:all|sorted_unique[1..65535],allow_http:false`；private CIDR 必须规范 masked、属于 RFC1918/IPv6 ULA，可重叠（命中任一完整规则即允许），最多 **256** 条、selected ports 最多 **256**。固定禁止段不可覆写；规则排序规范化参与 digest，遗漏 ports 不等于 all。

`GetPolicy/UpdatePolicy(ctx,Human,CommandMeta,rules)` 是系统管理员服务；Update 在同 Tx 中持 `system-config:outbound-policy` exclusive、检查幂等与 expected_version、写规则/递增 version/receipt/Audit。无 D07 绑定不提供管理 HTTP。策略镜像只经此真实提交发布，不能依靠缓存 TTL/定时轮询。DB 载入失败或提交结果尚未核实，新的准入 fail closed，不沿旧策略继续。

业务入口 `Do(ctx,request,Profile)->Response`；需要 `*http.Client` 的 SDK 只能取已封装 Transport/redirect 规则的实例，不暴露可配置 dial/proxy/TLS 跳过开关。Profile 指定 consumer、允许 scheme、流式标志、收紧的限额及有界安全上下文。SMTP 使用 `DialTarget(ctx,host,port,SMTPProfile)->受控Conn` 的同一 DNS/IP/发出门禁；D07 实现 TLS/STARTTLS/none 和逐发送准入，不在 D04 伪装 SMTP 已能发邮件。

URL 只允许 http/https 与有效 ASCII hostname/IP、显式合法有效端口；拒绝 userinfo、fragment、zone ID、opaque URL、非网络 scheme、非规范 IPv4 数字缩写、空 hostname/port。DNS 名小写并规范尾点；首期非 ASCII 域名要求上层明确提供合法 ASCII A-label，不能用解析器差异猜地址。Host/SNI 固定来自规范 origin，拒绝 caller 另写 Host/连接控制 header；代理环境变量不生效。

每次请求/重试/redirect 获取全部 A/AAAA，去重，最多 **64** 个，超限/部分 family 查询错误不能视为成功的子集。规范 IPv4-mapped IPv6 后分类；RFC1918/ULA 仅按规则允许。固定拒绝 loopback、link-local、metadata、unspecified、multicast 与特殊保留段，不能只用 `IsGlobalUnicast`。混合结果只要一个地址不允许就拒绝全部，绝不从中挑一个 public 掩盖 forbidden。

分类表在源码固定并逐段测试：IPv4 除 RFC1918 外至少拒绝 `0/8,100.64/10,127/8,169.254/16,192.0.0/24,192.0.2/24,192.88.99/24,198.18/15,198.51.100/24,203.0.113/24,224/4,240/4`；IPv6 仅把 `2000::/3` 中排除特殊用途后的地址视为 public，另允许按规则放行 `fc00::/7`，拒绝 `::/96,64:ff9b::/96,64:ff9b:1::/48,100::/64,2001::/23,2001:db8::/32,2002::/16,3fff::/20,fe80::/10,ff00::/8` 等。保守排除注册表中特殊用途例外，不把 NAT64/6to4 当 public 绕过。metadata 固定匹配如 `169.254.169.254`、`169.254.170.2`、`100.100.100.200`、`168.63.129.16`、`fd00:ec2::254` 及规范 `metadata.google.internal`/`metadata.goog` 名称；最终清单引用 IANA/cloud 文档并作为实现常量接受独立审查。

DNS 审批得到不可伪造的完整地址集合；实际只 dial 集合中的**数值 IP**，TLS ServerName/HTTP Host 保持原域名；dialer 不第二次按 hostname 解析。新请求即使复用 idle socket 也重新解析/分类，其实际 peer IP 必须在新批准集合且满足当前规则，否则关闭/弃用该连接重新选择；不要信旧连接建立时的允许结论。DNS 结果不得经日志泄漏 query/凭据。

## 8. B03：实际发出与策略提交的线性化

适用正式部署的**单 Central**前提，不声称多实例一致性。进程内可取消、writer 优先的发出读写门禁协调全部 policy 更新、dial、HTTP 初写；DB Tx 绝不横跨外部网络 I/O。SQL 锁保护持久事实，进程门禁只补“提交与实际发出”的间隙；不能由各 consumer 各持一个锁或各缓存一份策略。

策略更新先取得进程 exclusive 门禁（此时未开 DB Tx），再执行短 DB 命令；仅本次实际更新并确认 commit 后在持锁内发布完整新版本才释放/返回。旧 command key 同义重放只返回原 receipt，不发布 receipt 中的历史规则/版本，不覆盖较新的 DB/current mirror。rollback 保留旧镜像；unknown 在持锁期间按原 receipt/current version 核实，预算内不能核实则置 unavailable 后释放，后续准入拒绝直到可信 DB 重载，不能先返回成功再异步刷新缓存。需要恢复镜像时持 exclusive 重读 DB 的当前完整策略，不能用命中的旧 receipt 代替重载，也不能覆盖更高版本。

DNS 可在门禁外进行。准备拨号时取得 shared 门禁，按**此刻**镜像复验完整 DNS 集合并只 dial pinned IP；持锁到这次有界 dial 返回/失败，最长 **5s**，因此更新不能越过尚未结束的拨号准入。开始 dial 是连接建立 attempt 的发出点：已按旧规则发出的 TCP/TLS 建连可结束，但不表示 HTTP 业务请求已发出。DNS 完成后、dial 前发生策略变化必须被这里拦下。

HTTP 每个实际业务 attempt 拥有一个贯穿整个受控 RoundTrip 的发送状态，跨 `GetConn/GotConn`、新建/复用连接及 Transport 内部 retry 保持同一身份；每次借连接的 permit 只能关联该状态，**不能新建 permit 就清零 sent**。在首次实际写 HTTP 字节前取得 shared 门禁，重验当前版本/完整地址集合/peer/origin/取消。真正 `Write` 返回 `n>0` 即原子标记 sent，至此才允许按在途请求继续；若仅解析/排队/dial/TLS 完成而尚无请求字节，不算已经发出，策略更新后仍需拒绝。首写仅持有有界 **2s** write deadline，失败/零字节释放门禁；不可因 slowpeer 无限拖住管理员保存。

首写结束释放门禁，原连接上同一次发送的后续 body/响应流可继续，策略变化不强制召回；每次 redirect/new request 创建新 attempt，重新完整验证。Go1.27.1 Transport 对 reused connection 上的 GET/带 Idempotency-Key 请求可能在已写后读响应失败时自行 retry，不能依赖其默认幂等判断：一旦 sent，必须拒绝该 attempt 后续内部 retry 的 dial/borrow/重新开始 write，跨连接也不能形成第二次发送；异常只上报 sent/unknown，由业务 retry owner 决定下一 attempt。仅全部 write 均为零字节时允许最多一次内部 retry，仍使用原未发状态并重新完整 DNS/当前策略准入；内部 retry 计数不能因换连接而清零。

首版固定 **HTTP/1.1**，禁用自动 HTTP/2、代理、连接 coalescing、cookie jar；保持 HTTP/1.1 keep-alive 与逐请求门禁。可使用标准 Transport 的受控 Dial/DialTLS、httptrace 借连接通知及包内 conn wrapper，但必须证明真正每次初写都受保护，不能把 `WroteHeaders` 事后回调当准入。内部实现由 B03 决定；SDK 不得替换受控 Transport。以后若启用 multiplexing，须先补逐 stream 同等线性化证据。

## 9. B03：TLS、凭据、redirect 与限额

TLS 最低 1.2，使用系统/部署 CA 验证 chain、有效期与原 hostname；不允许 InsecureSkipVerify、跳过主机名、TLS 失败降级或业务自带任意 CA。private 地址默认仍必须 HTTPS；HTTP 需要 Profile 允许，private 还必须同一命中规则显式 allow_http。SMTP 的显式 none 独立于 allow_http，不能把 HTTP 开关当 SMTP TLS 开关。

凭据仅由 trusted adapter 通过 typed `CredentialBinding{origin,header/query bindings}` 注入临时内存，完整 origin 比较 scheme+规范 host+有效 port，不按子域继承。普通 request 和响应 URL/headers/body 不可直接日志化。自动 redirect 只处理无 body 的 GET/HEAD，最多 **5** 跳；其他方法返回 `redirect_denied` 留 consumer 明确建立新请求。每跳重验 URL/DNS/策略，拒绝 HTTPS 降级；跨 origin 构造无原 binding 的请求、清除全部 caller headers、不合并原 query/body，不能保留 Cookie/API key/自定义认证。Location 若仍携原绑定材料跨 origin 则拒绝；外部响应不是认证授权。

平台上限固定：DNS **5s**、每次 connect **5s**、TLS handshake **10s**、response headers **15s / 64 KiB**、普通请求 overall **120s / 16 MiB 响应**、stream overall **30min / 256 MiB**、stream read idle **60s**、redirect **5**、request headers **64 KiB**、普通 request body **16 MiB**。Profile 只能收紧；stream 按块消费和计数，不整块 buffering，取消立即关闭/取消对应 request。平台总上限不是 Execution/Tool/Approval 期限；D09 可在此范围选择逐 attempt 渐进 timeout。

关闭 Transport 自动解压，默认 `Accept-Encoding: identity`，首版拒绝非 identity Content-Encoding，不能让压缩炸弹逃过 decoded 限额；后续消费者确需压缩时在受控层补双重计数。body 超限/空闲超时/总期限都关闭响应和不可复用连接，不只对 Content-Length 检查；正常 EOF 后才允许复用。处置 101/CONNECT 为不支持，WebSocket/隧道协议留正式模块扩展。

返回安全 `Decision/NetworkError`：reason、consumer、规范 origin（无 path/query/userinfo）、policy_version、地址分类、redirect_count、是否已发出及 trace ID；完整 IP 集合只作为包内审批事实，普通日志不写任意字段/原始 net/url/TLS 错误。沿正式 deny reason，补 `response_limit/timeout/cancelled/policy_unavailable`。安全拒绝按调用点写最小 Audit；普通网络错误不全量复制 Audit。Project Audit 不能写时保持原拒绝，不能改 scope 写项目正文到 System，也不能为补日志重新发请求。

## 10. 实施验收与交接

普通单元测试不得启动 Docker；真实 PG/网络测试必须运行 task-owned fixture，缺 fixture 明确 skip 只适用于普通 suite，专用脚本缺 Docker/建库/网络条件必须失败。保留 D03 nonce、label、exact ID 校验与 finally 清理，不连接既有 DB/读取真实凭据。

`scripts/test-postgres.sh` 的显式 integration 包清单追加 `./tests/security`（包不存在前不加空壳）；沿既有 fixture 提供独立 DB。B03 `scripts/test-security.sh` 组合它和新增网络 fixture：本机 Go1.27.1 构建测试 server、在沿用固定镜像的 owned container 中运行，使用 owned Docker 私网 IP 与精确 CIDR/端口规则。为真实 TLS 生成临时 CA/证书；成功请求走真实私网 socket，**不得把 loopback 分类器替换成 allow**。本地 DNS 替身只能提供解析结果，最终生产 classifier/pinning/transport 仍真实执行。fixture 自己记录命中、连接 ID、body 和 barrier 通知，用随机 canary 凭据；超时后查请求计数而非只检查客户端 error。

| 块 | 必须独立证明的场景与结果 |
| --- | --- |
| B01 | Tx rollback 无 Audit；提交响应丢失经 LookupAppend 找到一条；换 HTTP trace/有效 Session 同义重放仍返回原 ID/created_at、异义拒绝；实际 Runner/Tool attempt ID 变化用独立 key，不误去重；unknown 核实仍拒绝越权/失效授权；不合法 metadata/Secret canary 从 entry、JSON、fmt、slog、错误全部消失；真实 PG 同时间多行翻页不漏重；改 scope/filter/kid/MAC 拒绝；每页授权撤销/管理员非 Owner 拒绝；授权清理只删原 Project，archive/System 保留、late append 被 gate 拒绝 |
| B02 加密 | 缺失/短 key/重复材料/同 version 换材料/跨历史换 version 重用 key 启动拒绝；逐项篡改 AAD/tag/wrap/version 失败；并发预留不重复、rollback/unknown/crash 烧区间、canary 同域、溢出拒绝；InTx 不嵌套预留；receipt 密文无可见 Secret digest；配置/日志/诊断无 canary plaintext |
| B02 恢复 | 旧 key 缺失拒绝；新写/旧 writer/迟到 preparation/重保护/删除并发；在 checkpoint 前后和 COMMIT 回包丢失处断开重启，CAS 不回退新值且最终全表无旧引用；损坏行不跳过，组件 unavailable/ready503，新写拒绝，仅已验证无关值允许原授权下有限读；重启仍拒绝缺 key/坏 canary，不用运行中有限读绕过启动；仅最终 fence 判定完成才能移除 key；移除后重启读取通过 |
| B02 lease | 同 owner acquire/retry不泄漏，其他 owner不能release；live ref/lease阻止删除；value更新后同 lease下一次读新值，已取材料不热换；read Audit失败不返回材料；未知lease/gate/用途拒绝；MCP retained绑定不会被Model测试替代；Project清理遇引用pending不成功 |
| B03 网络 | 默认private拒绝、CIDR/端口/HTTP逐项放行；所有固定段、mappedIPv6、混合A/AAAA拒绝；DNS rebinding实际只连批准IP；可信/不可信/错hostname TLS；真实keep-alive第二次请求重新验证；真实 server 在 reused connection 收到 GET/带 Idempotency-Key 请求后断开响应，客户端报 sent/unknown 且 server 请求计数只能为1，跨连接不得隐藏重发；零字节 retry 至多一次且重验策略；跨origin所有认证丢弃、Location泄露拒绝、每跳重验；stream限额/idle/cancel关闭socket |
| B03 barrier | 卡住DNS→策略收紧提交→释放DNS：无dial；卡住dial后/HTTP首写前→提交收紧→释放：server未收业务请求；复用连接同场景仍拒绝；首字节已写→策略提交→旧响应可结束、后续请求拒绝；慢首写在2s上限内释放writer；策略rollback保留旧规则、unknown重载前fail closed、重启从DB读取新版本；K1提交v2、K2提交v3后重放K1仅返回原v2 receipt，DB/镜像/实际准入仍是v3，旧receipt不能恢复unavailable镜像 |
| 进程 | 真实二进制 check-config/帮助/版本、所有mandatory失败不监听、轮换中第一/第二信号、在途HTTP/出站先drain后DB、deadline强关返回真实失败；ready持续false且安全组件状态真实；owned fixtures无残留 |

每块在仓库根执行 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/check-go.sh` 与 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/test-postgres.sh -run '<本块实际 Test 名>'`；B03 再执行 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/test-security.sh`。先做相应局部真实场景，通过后复用稳定输入证据；最后检查受影响全 suite，不把 skip 或测试替身当真实网络/身份整合完成。

作者交付列出代码/迁移/fixture 指纹、实际命令/版本、结果、未绑定端口与停止写入。S01 自查只证明规格/链接/空白一致，不宣称上述行为已通过；当前没有需用户新定的产品问题，所有工程参数与拆分由 root 采纳后进入实施。
