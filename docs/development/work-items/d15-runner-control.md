# D15 Runner 身份与 Control Channel

状态：rev2 已获独立有限SPEC接受，进入实施；共享wire首片段作者离线通过，身份/控制通道/真实平台尚未运行。

## 1. 结果、依据与边界

本卡交付可由真实 `agenteam` / `agenteam-runner` 消费的系统设备管理、一次性登记、Ed25519 身份、出站 WSS、连接代际、hello/heartbeat、双端 RPC 关联与明确的断线结果。管理员可创建设备、修改描述元数据、签发或撤销登记材料、重登记；Runner 从私有本地身份文件启动并持续重连。没有 Agent/Skills/Object 实现也可开发这些功能；测试替身只能验证明确的 Runtime/Mount port，不能成为生产成功实现。

正式依据为 [D15 计划](../development-plan.md#d15-身份与-control-channel)、[D01 固定 Runner v1.0](d01-contracts/model-tool.md#runner-与-mcp-通信边界)、[设备管理](../../architecture/runner/runner-management.md)、[Control Protocol](../../architecture/runner/control-protocol.md)、[Runner 架构](../../architecture/runner/README.md)、[Audit](../../architecture/security-governance/audit.md)、[基础 Tx/身份](d01-contracts/foundation.md)。D01 的 `workspace_id`、`operation_name`、`operation_revision`、`safe_message` 优先于架构示意字段；本卡不能复活 `workspace` / `operation` / `message` 的旧别名。

不包含 Agent Mount 配置/授权生产者、Workspace/文件/命令/进程实现、Data Channel 传输、Desktop/Tunnel、Tool Runtime 自动重试、运行日志 UI、Runner 自动升级或 Windows。D10/D16/D17/D18 的真实绑定分别保留 gate。当前生产 Runner 的 operation registry 为空，实际 capability 列表为空；能连上不等于可以执行命令。未来绑定必须同时通过 operation schema 与真实 capability probe，不允许用成功 stub 或固定宣称能力补齐。

## 2. 所有权与文件闭包

工作树 `/workspace/agenteam-runner-control`，`ai/runner-control` 基于正式 `f1c94ee5`。本作者负责设计与实现，自查不能代替独立验收。Git/整合/真实资源由根线程掌管；本卡独审接受前不实施产品。

| 范围 | 唯一用途 |
| --- | --- |
| `internal/runnerprotocol/` | 双端共用、无 Central import 的 v1.0 DTO、严格 wire、版本、限额、关联与错误码 |
| `internal/central/runner/contract/` | 管理命令/回执/Reader、类型化身份与当前连接信息；不输出秘密 |
| `internal/central/runner/service/` | PG 事实、当前 Admin、登记/挑战、代际注册、RPC dispatcher、Audit authority、生命周期 |
| `internal/central/runner/http/` | Admin REST、设备 HTTPS 与 WSS upgrade，实际 root 消费 |
| `internal/runner/identity/` | Ed25519、严格本地文件、登记恢复、TLS 设备认证 |
| `internal/runner/control/` | WSS client、探测/hello、心跳、单 writer、RPC port、断线与 Stop/Drain/Force |
| `tests/runnercontrol/` | 作者/独立协议、真实 PG/TLS/WebSocket 与真实入口验证；测试端替身不导入产品装配 |
| `db/migrations/00026_runner_control.sql` | 根预留；Runner 事实与 Audit 闭集增量，同一事务迁移 |
| 本卡、`.agent-state/current.md` | 当前规格与短可恢复状态，不复制历史报告 |

根已原则授权、仅本树使用的共享写域：`internal/runner/config/{config.go,config_test.go}`、`internal/runner/app/{app.go,app_test.go}`、`cmd/agenteam-runner/{main.go,main_test.go}`；`internal/central/app/account.go` 与新 `runner_control.go`、`runner_control_test.go`、`runner_control_process_test.go`；`internal/central/audit/contract/{types.go,metadata.go,runner.go,runner_test.go}`、`internal/central/audit/{service.go,query.go,runner_test.go}`；`go.mod/go.sum` 仅新增 `github.com/gorilla/websocket v1.5.3` 及必要依赖；新 `api/openapi/runner-control.json`，现 `docs/development/backend/README.md` 与新 `docs/development/backend/runner.md`。根已补充授权 `internal/central/identity/contract/{identity.go,runner_test.go}` 注册闭集 `runner-identity` ServiceName；只允许 System scope、同事务真实身份事件的 Audit authority，未知 service actor 仍默认拒绝。任何其它共享源不自动获权。

不修改 Foundation ID/锁/通用 Fault 或原迁移。Runner 事务用现有 CommandLock、SystemConfigLock、UserLock，预收集排序；本卡不新增全局锁 rank。现有 PG 真正拒绝迁移版本洞，00026 必须待根整合 00024/00025 后按完整连续源验证，不能填空迁移或改旧 checksum。SPEC/pure 阶段可以先行，真实 PG gate 不因编号预留而自动就绪。

## 3. Central 持久事实、权限与管理协议

系统级 Runner 不属于 Project。`runner_id` 为 canonical 小写 UUIDv7；version 是 canonical 正整数字符串，数据库 bigint、不回绕。`name` UTF-8 1–128 B，`description` 0–4096 B，tags 最多32个、每个1–64 B、唯一且按字节排序；拒 NUL/控制字符。`root_path` 必须是 Linux/macOS 的绝对 clean 路径、≤4096 B、无 NUL/反斜杠/`.`/`..` 路径段，创建后不可改。平台路径检查不宣称命令 sandbox。

00026 创建 `agenteam_runner.runners`：id、name/description/tags/root_path、version、credential_generation、device_public_key(nullable 32 B)、enrolled_at、last_seen_at、最后安全 hello 快照、incompatible 标记、created_at/updated_at。顶层状态仅 online/offline/incompatible；online 由当前 authenticated+hello 完成且未过期的连接事实推导，不单凭持久布尔；capability fault 不改 incompatible。撤销后的记录保留 metadata/Mount 引用，key=NULL、epoch 递增、offline；本卡无硬删除 API。

另有 `commands`（稳定命令身份、语义 digest、public receipt、原 Human 摘要、业务/提交时刻）、`enrollment_tokens`（随机摘要、generation、expires/consumed）、`identity_events`（登记/撤销的安全事实与 Audit append 身份）、`challenges`（随机摘要、同 generation、expires/consumed）、`connections`（runner 唯一当前 generation/connection_id、进程 owner、hello/last_seen/lease 信息）。只存公钥，不存 enrollment token、nonce、signature、private key 或 environment。token/nonce 原始材料只在一次请求/响应的私有内存中存在，不进 receipts、日志、错误或 Audit。

持久模型细化：创建时 `version=1`、`credential_generation=1`、持久 `connection_generation=0`。首次登记绑定该已签发generation的key，业务version+1；显式撤销/重登记清key并使credential_generation/version各+1。每次认证仅递增connection_generation，heartbeat/hello/lease不改业务version/updated_at；移除当前connection也不重置持久代际。commands只在业务最终事务写入完整public receipt，不新增独立prepared事务；未知提交查证仍由原Command锁及真实COMMIT终态决定。commands/identity_events不可改写，token/challenge消耗和撤销不可逆；当前connection以延迟FK关联Runner当前两代际。

所有 Admin 入口以现 Account `HTTPBoundary.RequireSystem` 为 HTTP 身份边界，在真实事务里持 User SH 后再次 `AuthorizeSystem(Read|Mutate)`；不同 User、过期/撤销 Session、被降为普通 User 均不得读取管理记录/receipt。当前 User 存在检查沿 Account；不补不存在的停用 User 能力。设备认证不使用 Human Session，设备请求拒 cookie/CSRF/浏览器 Origin，不能通过提供 Actor 字段得到管理员权限。

Admin 前缀 `/api/v1/system/runners`：

| 方法/相对路径 | 契约 |
| --- | --- |
| GET/HEAD `/` | `limit` 默认50、1..200；按 id 升序 keyset，`after_id` 为 canonical UUIDv7；非快照页，不接受其它 query |
| GET/HEAD `/{runner_id}` | 当前 public Runner，公钥仅输出 sha256 fingerprint，不输出 raw key/nonce/token |
| POST `/` | `{runner_id,name,description,tags,root_path}`；创建 offline 设备并签发一次登记 token |
| PATCH `/{runner_id}` | `{expected_version,name?,description?,tags?}`；至少一项、presence 严格，无隐式 root 修改 |
| POST `/{runner_id}/enrollment` | `{expected_version}`；显式重登记：撤旧 key、旧 tokens/challenges、旧连接，保留 metadata，签发新 token |
| POST `/{runner_id}/revoke` | `{expected_version}`；撤 key、tokens/challenges、连接；无新 token |
| POST `/{runner_id}/commands/lookup` | `{command,request}` 为首次原意图；服务重算 digest 查 public receipt，不接受任意摘要 |

全部管理 mutation 与 Lookup 要求唯一合法 Idempotency-Key；命令身份为 `runner-management/[UserID,RunnerID]/command/key`，command 精确为 `runner.create`、`runner.update`、`runner.enrollment.issue`、`runner.revoke`；Lookup command 只能选这四种且request为该种原DTO。同 User 新 Session 可查，但每次重验当前 Admin。摘要包含语义 version、原 DTO presence/defaults/ID，不含当前 Session/HTTP RequestID；异意图同 key 冲突。事务顺序：Command EX →完整已排序 SystemConfig(`runner-management`) EX + User SH →查当前身份、命令记录与 Runner →version/业务校验→记录与 typed Audit 同 Tx→发布 receipt。读为同组 SH+User SH，同事务取当前事实。global 配置锁仅串行短管理/身份事务，不跨网络等待，也不是 operation 并发锁。每个既有 Runner 另使用 `SystemConfigLock("runner-control-"+RunnerID)` 作为连接代际门；管理撤销/登记必须预收集这两锁并沿 Foundation 排序后获取，不能取得其它锁后临时补低 rank 锁。§5 的有界 send gate 仅持该 Runner 的连接锁，不持全局管理锁/User锁。

PATCH值与当前事实完全相同是已知no-op：保存public命令回执，version不变、不伪造changed_fields/Audit修改事实。revoke/enrollment为显式安全epoch操作，即使当前key已空仍产生新generation/version以淘汰已有登记材料；不会回绕，max值拒新操作而不拒历史Lookup。

token 为 crypto/rand 32 B，wire 是无 padding base64url（43字节），TTL 10分钟，DB UTC 判时；只存 SHA-256，常量时间比较。首次已知 committed 响应可带 `{enrollment_token,expires_at}`，重复/Lookup 仅返回相同 public receipt 与 `token_available:false`，不再生成或重放秘密；lost response/Unknown 后管理员先原意图 Lookup，再以新显式 enrollment 命令签发，不能把重试当成新 token。任何 prepared public receipt 不得提前发布；Unknown 保留原 Attempt/Cause，不自动写重试，当前已授权 Lookup 用同命令事实确认。注册命令不要求 token 明文可恢复。

所有 HTTP JSON 对象闭集、拒未知/重复（含 escaped key）/别名/null/尾随内容/非法 UTF-8/surrogate；总 body≤32 KiB、depth≤16，列表响应≤2 MiB；Content-Encoding 只要出现就拒，JSON charset 只 UTF-8。GET/HEAD 禁实际 body；path canonical、无 RawPath/ForceQuery，query 只各入口明确字段且不重复。HTTP 管理/设备有限请求使用原生2s读/30s写上界、ctx cancellation 与实际 body/Tx/handler join；后续 keepalive 不继承旧 deadline。错误复用正式 Problem 安全投影，仅复用Foundation已有码，不新增`Conflict`：expected_version不匹配为`VersionConflict`，同command/key异意图为`IdempotencyKeyReused`，当前状态不容许为`InvalidState`；其它输入、身份、容量、依赖和Unknown分别沿`InvalidArgument`、`Unauthenticated`、`Forbidden`、`NotFound`、`ResourceBusy`、`RateLimited`、`DependencyUnbound`、`DependencyUnavailable`、`CommitUnknown`。不扩大未知code回退；下述Runner wire `CONFLICT`属于独立协议错误码，不是Foundation枚举。写成功后 projection/write失败 abort，不伪造 not_committed。

## 4. 登记、认证与本地身份

设备 HTTPS 使用固定路径，禁止 redirect、userinfo、fragment、query；仅 TLS1.2+，验证系统 CA/显式 CA 文件与配置 hostname，不允许 insecure skip verify。`central_url` 是 HTTPS origin；Control URL 由它机械派生 WSS。Central 可在既有受控 TLS reverse proxy 后运行 HTTP；部署必须保护代理到 Central 的 listener，不能信任客户端伪造 X-Forwarded 头来当 TLS/身份事实。真实验收至少有 native TLS 端点与坏证书/错 host 负向。

`POST /api/v1/runner/enroll` 的闭集正文为 `{runner_id,token,public_key,root_path,os,arch}`；key 为 Ed25519 32 B 无 padding base64url。先界定合法长度再查受保护行，root_path 必须与管理员固定值逐字相同。短事务在完整管理/该 Runner 连接锁下检 token/current generation/未消费/期限，一次原子消费+绑定 key/enrolled_at+identity_event+Audit。并发只有一方消费；无效凭据统一安全401，不披露 Runner 是否存在。事务 rollback 后 token 可再用；Unknown 不假称没消费。

本地首次 `--enroll` 从标准输入读取恰一个 token（≤128 B，允许末尾一个 LF），不得 CLI argv/env 放 token。`AGENTEAM_RUNNER_IDENTITY_FILE` 指向绝对私有路径；未有身份时同时要求 `AGENTEAM_RUNNER_CENTRAL_URL`、`AGENTEAM_RUNNER_ID`、`AGENTEAM_RUNNER_ROOT_PATH`。Runner 在发 enrollment 前将新 Ed25519 seed 与公共配置作为 `pending` 身份原子持久化；这是防止 Central 已绑定而本地无 key 的恢复准备，不代表已完成登记。已知成功后改成 `active`；响应丢失时用已持有 key 走真实 challenge/connect，身份被接受即可收敛为 active，不复用 token 自动重绑。鉴权仍失败则保留 pending、安全退出，需管理员显式新登记材料。重登记不得覆盖仍在运行的实例或误恢复已废弃 key。

文件 schema v1：state(pending/active)、central_url、runner_id、root_path、private_seed；≤8192 B、strict JSON、安全 Formatter/LogValue。身份文件的直接父目录必须同有效 UID 所有、0700，文件0600、regular、nlink=1；各祖先目录仅允许当前UID或root所有且不可由其它用户写。拒 symlink（各路径段）/错误owner/越权mode/非regular；沿已打开的directory FD逐段 no-follow 验证和写入，不能用先Lstat后跟随路径的TOCTOU实现。新临时文件同目录 O_EXCL/no-follow 0600→写全→file fsync→原子 rename→directory fsync；失败不得截断旧可恢复文件。只处理本任务文件，安全清除本次未提交临时文件；不承诺磁盘/Go内存不可恢复擦除。进程持同目录专用 `identity-file-name.lock`（同安全owner/mode、不得随身份rename换inode）的OS排他锁直到最终 join 后释放，防两个 Runner 同时改 key/争连接。Unix实现用既有 x/sys；Windows 构建明确不支持，不能退回弱文件权限。`--check-config` 只离线验证/读取文件，绝无网络，输出 scope=d15 且 connected/authenticated/ready 均 false；实际 Run 不能仅因配置通过就报 ready。

`POST /api/v1/runner/challenge`：`{runner_id}`，返回 `{nonce,expires_at}`，nonce为32随机B/base64url43B、TTL30s、DB UTC；每 Runner 至多4个未过期 challenge，已发 nonce 不因新请求被替换；服务待验证 challenge 总量上限16384，耗尽返回 ResourceBusy，不驱逐有效 nonce。过期消费行有有界 cleanup，Stop 后不新开维护 Tx。统一安全401不泄露撤销/未知状态；网络边界按资源保护拒绝，不实现第二套业务授权。

WSS `/api/v1/runner/control`，subprotocol 精确 `agenteam.runner.v1`；凭据只在唯一 `Authorization: Runner <base64url(strict-auth-json)>`，JSON 为 `{runner_id,nonce,timestamp,signature}`，≤2048 B，禁止 query/日志回显。timestamp 为 canonical 整数 Unix seconds 字符串。签名精确字节为 UTF-8：`agenteam-runner-control-v1\n` + runner_id + `\n` + nonce + `\n` + timestamp + `\n`，各行仅上述canonical字符，不 trim、不再JSON编码；Ed25519 signature 64 B/base64url86B。固定 golden vector/改任一字节负向。Central 在同 Tx、已排序管理/该 Runner 连接锁下检当前 key/generation、nonce hash绑定/未消费/未过期、`abs(DB_now_seconds-timestamp)<=30`，再验签并消费；重放/旧key/其它Runner/过期/未来/错签都拒。成功认证但 upgrade失败也不恢复 nonce，客户端新 challenge；Unknown消费结果不自动重用 nonce。升级前完成校验，不接受连接后才证明身份。

## 5. 单连接代际与状态

每次认证成功预留新的 canonical UUIDv7 connection_id 与递增 connection_generation，绑定 credential_generation。HTTP upgrade/hello失败仍不会复用旧代际。有效登记发布必须先使旧 generation 的 admission 无效；同进程原 socket 主动关闭且旧 pending 统一收束，旧 reader/heartbeat/response/cleanup 都不能覆盖新连接状态。nonce消费与新代际事实同 Tx；upgrade之后成功 hello 才置在线。原连接即使尚未完成物理 close 也无权更新任何当前事实。

跨 Central 进程不把内存 map 当全局真相：generation/current owner 存DB，每个新 request 的实际 write 前与每个会改变/发布事实的 inbound message 都核对应 generation。发送门在短事务持该 Runner 连接锁验证，真正 write 和该短事务共用≤5s上界，才允许新代际发布；不得持锁执行 operation。替换后旧 socket 最多在下一次≤1s代际观察中主动关闭；期间旧 inbound 不能发布，原 pending 可保守 unknown。各独立 registry 必须通过真实双实例竞争验收。仅支持当前进程所属连接的 dispatch；无跨 Central 的 RPC 转发，连接归别进程时返回 backend_unavailable 而非删除配置/伪造offline。此限制须出现在生产说明，不能宣称 HA dispatch。

Central 启动不把旧 online 行直接当在线；lease超时/旧owner不存在时读取为offline。registry异常退出/重启不恢复旧request。身份撤销/re-enroll与接收/发送共用同代际门，撤销提交后不能用旧key创建新连接，旧连接不能发布成功；已发送的外部副作用不宣称回滚。管理员元数据version与高频heartbeat独立，heartbeat不造成每秒配置version冲突。

只版本 major 不兼容或必需协议条件不满足记 incompatible；错签/普通断网/单项capability故障不冒版本不兼容。同 major不同minor允许 hello，以 `min(peer_minor,0)` 协商为本实现1.0；feature集合只能是双方明确支持的交集，不因minor更高接收未知字段或启用能力。

## 6. v1.0 严格 wire 与有限资源

Envelope 精确 `{protocol_version:{major:1,minor:0},type,message_id,request_id?,operation_id?,timestamp,payload}`。版本raw decoder先安全解析0..65535整数major/minor以便产生明确incompatible结果，不能先把所有非1.0头当普通JSON坏形；进入active后按协商版本校验。ID皆协议自有 string DTO，独立验 UUIDv7；timestamp为RFC3339Nano UTC `Z`，≤30B、解析后重新格式化相同；无 Foundation import。强类型 variant控制哪些correlation必须/不得出现，Envelope与payload重复ID必须一致。一次atomic decode到临时对象，失败不污染目标；嵌套闭集、presence/null、duplicate/escape/UTF-8规则同§3。request payload中 operation schema专属字段不由Control层猜测，但必须是合法非null JSON对象并受同深度/大小限制。

| 项目 | 固定值/行为 |
| --- | --- |
| WebSocket | RFC6455 text only、禁止 permessage-deflate、binary关闭1003；fragment累计完整message≤1 MiB，不只每frame限额；越限1009 |
| JSON/字段 | depth≤16、最多4096对象成员/数组元素合计；type/code等闭集、message_id每连接防重复 |
| 队列 | 每方向最多256条且合计≤4 MiB已编码字节；单writer；control/terminal预留32条+256 KiB，data/stream不得挤占 |
| 关联表 | 每连接最多4096个未终态request；最近终态/dedupe集合最多8192项；达到容量拒新admission或关闭重连，不自动遗忘后接受重复request |
| 连接 | hello首帧在upgrade后5s内；单帧write最多5s；close握手最多1s并实际close/socketjoin，不当成operation deadline |
| 认证 | challenge/enroll body≤32 KiB，auth header≤2048 B；独立inflight认证最多128个，超额明确429/ResourceBusy |
| operation环境 | ≤128项，name `[A-Za-z_][A-Za-z0-9_]{0,127}`、value≤8192 B、合计≤64 KiB；无NUL；不写日志/磁盘 |
| stream | 单条解码data≤16 KiB、stdout/stderr/progress，序号canonical unsigned十进制字符串，首1后严格+1；不丢序再假成功 |

这些是传输/内存保护上限，不是 Runner 独立的业务并发调度。满表/满队列时尚未开始写的 request 明确 not_sent；一旦尝试写或入wire后的异常，未收到合法terminal则 unknown。不能为了给heartbeat让路静默丢terminal/stream；terminal必须排在同request已接纳stream之后，不能被优先队列提前越过；持续满队列使连接失败并统一收束pending。关闭/替换/Force能打断阻塞producer与writer，禁止goroutine无界缓冲。单连接寿命到达message去重容量时可主动重连，但必须先停止admission，已有pending按真实terminal/unknown处理，不能重放。

关联规则冻结：hello/hello_ack/heartbeat/heartbeat_ack/runner_status/protocol_error无request_id/operation_id；request/response/cancel/stream两ID都必须有且与payload一致；data_channel三种message两ID同有或同无，有时与payload一致。`cancel`只有request_id/operation_id/reason；`stream`只有request_id/operation_id/stream/sequence/data/timestamp；`runner_status`只有headless/capabilities；`protocol_error`只有code/safe_message/offending_message_id?/fatal。协议error code闭集 `INVALID_ENVELOPE`、`INCOMPATIBLE_VERSION`、`HELLO_ORDER`、`CORRELATION_INVALID`、`DUPLICATE_MESSAGE`、`UNSUPPORTED_MESSAGE`、`RESOURCE_EXHAUSTED`；输出固定文案且不回显raw输入。13种payload字段闭集如下；`?`表示省略合法、显式null仍拒绝，其余字段必须出现。字段取值限制继承本节/§7；表中别名与未列字段一概拒绝，不以任意map扩展。

| type / 方向 | payload完整字段 |
| --- | --- |
| hello / Runner→Central | runner_id, runner_version, protocol_version, os, arch, headless, capabilities, feature_flags |
| hello_ack / Central→Runner | accepted, negotiated_protocol_version, heartbeat_interval_ms, heartbeat_timeout_ms, enabled_features |
| heartbeat / Runner→Central | sequence, runner_time；本版无health扩展 |
| heartbeat_ack / Central→Runner | sequence；值精确回显已接受heartbeat的sequence，无server_time/配置/其它字段 |
| request / Central→Runner | request_id, operation_id, execution_id, project_id, agent_id, mount, operation_name, operation_revision, deadline?, environment?, idempotency_key?, payload（§7） |
| response / Runner→Central | request_id, operation_id, outcome, code?, safe_message?, payload?；四variant精确presence见§7 |
| cancel / Central→Runner | request_id, operation_id, reason |
| stream / Runner→Central | request_id, operation_id, stream, sequence, data, timestamp |
| runner_status / Runner→Central | headless, capabilities |
| data_channel_open / Central→Runner | channel_id, request_id?, operation_id?, direction, purpose, size?, media_type?, checksum?, expires_at, one_time_credential |
| data_channel_ready / Runner→Central | channel_id, request_id?, operation_id?；无ready bool、endpoint、URL或credential |
| data_channel_close / 两方向 | channel_id, request_id?, operation_id?, status, transferred_size, checksum?, error? |
| protocol_error / 两方向 | code, safe_message, offending_message_id?, fatal |

D17三帧这里只冻结control metadata的可判定shape，不启用传输：channel_id按本卡UUIDv7；可选request/operation同有或同无，并与Envelope一致。direction闭集runner_to_central/central_to_runner/bidirectional_stream；purpose是1..64B的`[a-z][a-z0-9_.-]*`标识，具体已启用用途由D17决定；size/transferred_size为0..MaxInt64的canonical十进制字符串；media_type为≤255B ASCII无参数`type/subtype`；checksum为`sha256:`加64个小写hex；expires_at沿本卡Instant；one_time_credential为32随机B的无padding base64url，仅作为秘密wire字段、不输出日志。close.status闭集completed/failed/cancelled/expired：completed禁止error、checksum可选；其余禁止checksum、必须error。error精确`{code,safe_message}`，failed只允许TRANSFER_FAILED或DATA_CHANNEL_FAILED，cancelled只CANCELLED，expired只TIMEOUT；文案沿§7表。当前未启用data-channel feature，合法形状也明确UNSUPPORTED_MESSAGE，不能回复ready；D17未来启用不改变本卡对当前未绑定的拒绝行为。

hello首帧精确 runner_id/runner_version/protocol_version/os/arch/headless/capabilities/feature_flags。runner_version为构建安全版本串≤64B；os闭集linux/darwin、arch闭集amd64/arm64、能力/flags各≤32个唯一≤64B稳定名。当前空能力是合法且诚实的在线身份节点；未注册/探测失败不宣称支持。hello_ack精确 accepted、negotiated_protocol_version、heartbeat_interval_ms、heartbeat_timeout_ms、enabled_features；1.0默认 interval=10000、timeout=30000，Central只可选择interval 1000..30000且timeout在3×interval..120000内，Runner严格校验。拒绝不进入active。后续runner_status只发布实际变化的headless/capabilities快照，不改凭据/业务授权。

Runner heartbeat含递增sequence与runner_time，每interval发送；Central只认有效current generation的新sequence，DB更新last_seen并回相同sequence ack。Central超时未收心跳、Runner超时未收对应ack，都取消连接；其它message不能无限延长heartbeat寿命，duplicate/out-of-order不续命。interval/timeout随每次hello协商，本卡不新增未冻结的热更新message。网络deadline使用本地单调时钟，wire/DB UTC仅记录身份/业务时刻。

重连 exponential full jitter：第一次上限1s，之后2/4/8/16/30s封顶；稳定active60s后reset。每次全新challenge/auth/hello/probe，无旧request replay；停止context即时中止backoff/DNS/connect/handshake/reader/writer。认证撤销/未知不疯狂循环（同30s封顶）；配置/本地权限错误安全退出而非不断重试。

## 7. RPC、端口与 Unknown

生产RPC只有Central→Runner；Runner反向request为fatal protocol_error。`RunnerRequest` 精确D01：request_id、operation_id、execution_id、project_id、agent_id、mount{mount_id,workspace_id}、operation_name、operation_revision、deadline?、environment?、idempotency_key?、payload。request/operation ID必须Envelope一致；operation_name/revision来自已注册闭集schema，不猜别名。Mount是逻辑UUID，不接任意绝对路径；D16按固定root与已授权Mount映射路径。

Central dispatcher接受已由当前业务授权port验证的dispatch票据；仅信任自己绑定的Mount/Execution authority，不能把任意调用者拼出的DTO当授权。端口最小分两面：`MountAuthority.ResolveMount(ctx,actor,project,agent,mount)` 给当前合法目录与capability信息；`Control.Dispatch(ctx,verifiedDispatch)` 负责一次真实wire attempt。前者未绑定时 DependencyUnbound，不fallback；真实业务授权/approval/Secret解析留D10/D18/D19，D15测试可注入严格替身。协议测试用端点私有TestDispatch seam，不从公开HTTP暴露任意command执行。

Runner operation port是 `Execute(ctx,Request,StreamSink) (Terminal,error)`、Capability probe与生命周期所有者的组合；构造时冻结唯一 operation_name+revision注册，无动态任意map执行。D15生产无handler，未知operation返回 failure/UNSUPPORTED_OPERATION，无副作用；以后注册每种operation必须有真实probe且不得以测试stub进入cmd。Data Channel open/ready/close保持协议schema可识别，未启用feature时返回协议上明确unsupported、绝不创建或确认通道；D17补协商处理与真实传输后才启用。不得为测试echo新增生产operation。

Central每次技术Attempt生成新request_id/message_id，业务operation_id由上游给定且重试不变。调用者cancel只请求Runner取消，不直接把已发送请求归为cancelled。request未尝试write可返回not_sent；attempted write后无合法terminal为unknown（哪怕只有局部帧发出），必须上交Runtime而非自动重发。收到terminal后再断线保留known结果。重复terminal、错operation关联、未请求的response、terminal后stream为fatal；只能影响当前代际，不能污染新连接。

Terminal四variant都必须有request_id/operation_id/outcome；其它字段由下表决定，不靠zero value忽略非法presence。可选payload若出现必须是非null JSON对象，具体operation schema再验证；D15无生产operation成功payload。

| outcome | code | safe_message | payload |
| --- | --- | --- | --- |
| success | 禁止出现 | 禁止出现 | 可省略；出现时按operation result schema |
| failure | 必须，取下表除CANCELLED外任一项 | 必须，恰为该code对应固定文案 | 禁止出现 |
| cancelled | 必须，CANCELLED或TIMEOUT | 必须，恰为该code对应固定文案 | 禁止出现 |
| unknown | 禁止出现 | 必须，恰`Operation outcome is unknown.` | 禁止出现 |

Runner code与安全文案是协议闭集，不接受任意backend错误字符串；大小写、标点逐字一致。TIMEOUT只有已知期限导致确定失败/取消时可用于failure/cancelled，副作用未确认必须unknown。

| Runner code | safe_message |
| --- | --- |
| INVALID_REQUEST | `Invalid request.` |
| UNSUPPORTED_OPERATION | `Operation is unsupported.` |
| NOT_FOUND | `Resource was not found.` |
| CONFLICT | `Resource conflict.` |
| WORKSPACE_NOT_FOUND | `Workspace was not found.` |
| PATH_OUTSIDE_WORKSPACE | `Path is outside the workspace.` |
| PROCESS_NOT_FOUND | `Process was not found.` |
| PROCESS_ALREADY_EXITED | `Process has already exited.` |
| TIMEOUT | `Operation timed out.` |
| CANCELLED | `Operation was cancelled.` |
| TRANSFER_FAILED | `Transfer failed.` |
| DATA_CHANNEL_FAILED | `Data channel failed.` |
| CAPABILITY_UNAVAILABLE | `Capability is unavailable.` |
| INTERNAL_ERROR | `Internal error.` |

failure/cancelled必须由实际runtime证明，cancel ack不是terminal。未知副作用必须unknown。stream按request+stream独立sequence，terminal封口；发送前调用真实masking port，未绑定secret masking时带敏感environment的operation不得开始，不能默认原文透传。D15无命令输出生产者，Secret masking真实集成保留D16 gate。

可选deadline是原absoluteInstant，不给所有operation补统一业务timeout；期限已过不开始，尚未到建立子ctx；重试不刷新。cancel闭集`caller_cancelled`/`deadline_exceeded`/`connection_closed`/`runner_stopping`，关联request/operation；同时成功与取消依实际terminal为准。断线取消该连接下active RPC并等待其runtime退出；不将“发出cancel”当副作用已撤回。没有持久RPC journal、旧连接查询或自动结果恢复。

## 8. Audit、迁移与生产 Stop/Join

typed Audit action闭集 `runner.create` / `runner.update` / `runner.enrollment.issue` / `runner.enroll` / `runner.revoke`；resource=runner，System scope、RunnerID关联、producer=runner。管理动作Actor为当前Human；设备完成登记为注册的`runner-identity` service actor，cause指向同Tx identity_event；不借管理员历史Session伪造当前Human。metadata仅RunnerID、version、credential_generation、changed_fields、public_key_fingerprint（仅相关动作），不存name/path/token/nonce/signature。Audit authority验证同Tx命令/身份事件/current postimage、正确action、appendkey与Actor，foreign Tx/issuer、假记录/错generation/缺事实拒绝。有旧key的enrollment.issue在同Tx先写`runner.revoke` ordinal0，再写`runner.enrollment.issue` ordinal1；无旧key时只写issue ordinal0。旧token/nonce退休仍属于同新generation事实，无隐含第三条Audit。身份/命令与Audit必须全写或全回滚；Audit unavailable不放行登记。heartbeat/challenge/stream不是DomainEvent、不写Outbox或每次Audit；鉴权失败只安全限量诊断，本卡不新增业务安全事件消费者。

metadata wire固定必填 `runner_id/version/credential_generation/changed_fields`：create为`["created"]`，update为description/name/tags实际变更字段的非空字节排序子集，三种安全动作均为`["credential"]`。`public_key_fingerprint`仅enroll必填、revoke有旧key时填写，其余禁止；指纹为`sha256:`加64小写hex。管理Audit的cause_ref为同Tx command UUID，登记为identity_event UUID；RunnerID association必须等于resource_id，其余association均空。此闭集由00026及typed producer共同验证，不以schema替代当前事务授权。

00026所有新表check/unique/FK及Audit action/resource/producer/service_actor闭集在单事务增量更新；新DDL错误全rollback，历史Audit仍可读，新Runner行不会被旧code误当已识别；旧二进制面对更新schema沿D03版本检查拒启动，不能允许运行后静默忽略新Action。00026的Audit约束增量须以根整合后的1..25真实闭集为输入，不能用f1c94ee5旧集合覆盖D12/其它并行新增动作。新表约束限制key长度/状态组合/正generation，消耗的token/nonce不可恢复有效，旧generation不可重新绑定。Root整合后从00023+真实00024/25升级、重跑、迁移中断全rollback及旧Audit读取各有真实验收。

Central 在现Account assembly装配同一Store、Account authority、Audit与Runner service，再挂管理/设备routes；WSS hijack后**不能依赖**http.Server.Shutdown自动close/join。Runner service显式拥有所有upgrade后socket、reader/writer、认证/注册回调、维护timer、pendingRPC与connection观察者；构造失败立即关闭已取得资源。StopAdmission先拒所有新管理/认证/upgrade/dispatch，旧callback/unknown确认仍归该owner。Drain使用root原ctx不刷新预算；先取消active请求、等待runtime/实际socket退出，然后返回Joined。Force原ctx传至每个子owner，原deadline到期不能冒join成功；已close但goroutine未退出仍非Joined。DB必须晚于依赖它的Runner回调与审计事务退出，httpActive=0不足以证明WSS已退役。

Runner生产app沿既有lifecycle原shutdown总预算与第二信号force语义。顺序：拒新request→cancel active→D16 graceful/force active command→所有managed process含persistent退出→D17 Data/Tunnel退出→Control socket/reader/writer/重连退出→释放身份文件锁→进程actual Wait。当前无D16/D17资源，空owner明确为空不能宣称其已验。ctx done/返回Close都不能代替真实Join；底层拒退出要失败/forced exit，不宣称clean shutdown。

Config只新增上述身份/登记参数与可选`AGENTEAM_RUNNER_CA_FILE`，已有LOG_LEVEL/SHUTDOWN_TIMEOUT语义保留；无明文token参数。正式启动需identity，未配置返回安全配置错误，不再以D02空循环冒D15产品就绪；`--help/--version/--check-config`保持离线。中央HTTP根复用现middleware RequestID/安全日志/账户Session，设备秘密header与URL不进accesslog；整条失败链的Format/JSON/slog均有canary反例。

## 9. 支持矩阵与验收门槛

库固定 Go1.27.1、gorilla/websocket1.5.3，TLS1.2+。首期目标平台：Linux kernel≥5.15、amd64/arm64（基准Ubuntu22.04/24.04）；macOS≥14、amd64/arm64。文件权限、fsync/rename/flock与网络取消分别在真实OS验证。Windows明确不支持。当前环境只可提供Linux的实际运行证据；macOS及未实际CPU矩阵保持未验，不能用GOOS/GOARCH交叉编译或fakefs改写成全平台PASS。D16 Bash版本/D17桌面支持另卡，不被D15 hello空能力覆盖。

| 验收组 | 必须可判定的刺激与事实 |
| --- | --- |
| wire pure/race/fuzz | Golden签名字节；所有13种message方向/presence/correlation；Unicode/duplicate/depth/rawcaps、atomic decode/Clone、安全日志；frame碎片累计与压缩/binary拒绝 |
| Admin/持久真实PG | 真实Account Admin种子；普通User/降权/旧Session与同User新Session；异意图key；current Tx权限竞争；command/Audit原子性；真实COMMIT响应丢失，Lookup保原attempt/cause，token不重放 |
| enrollment真实PG+TLS | 一次消费并发双赢家、过期/错误/旧token；wrong root/publickey；绑定后lost response用同本地keychallenge恢复；re-enroll/revoke与connect双赢家、旧key拒；未知COMMIT不假known |
| credential真实FS | owner/mode/symlink各层/nlink/目录权限、短写/fsync/rename错误、Crash阶段恢复、锁竞争、旧文件不损坏；stdin token及CLI/log/Audit canary；Linux/macOS分栏 |
| auth真实WSS | nonce重放/同秒并发消费、clock±30边界、nonce过期、改ID/签名、wrong TLS/host、redirect、origin/cookie；auth成功但upgrade失败必须新nonce |
| connection真实竞争 | hello前request、wrongmajor/高minor/未知必需字段；两个真实实例同时接同Runner，旧heartbeat/terminal/cleanup晚到不能覆盖；revoke后旧发布拒；物理socket全部join |
| RPC真实wire+明确Runtime替身 | 多active交叉stream/terminal；未发送/局部write/已执行lostresponse；sameoperation不同request；deadline与cancel实际终态；blockedhandler使Force不得冒join；重连绝零自动重发 |
| boundedness | 控制队列/字节/关联/dedupe边界满值±1；停consumer/慢reader/断写，heartbeat与terminal保留；持续背压明确收束；高频观察无goroutine增长，资源cap不冒业务调度策略 |
| heartbeat/reconnect | native定时器自然超时、ack错sequence/丢失/重复、其它帧不能续命；fulljitter有界/稳定reset；Stop中断睡眠/DNS/handshake；capability单失败不置incompatible |
| 真实双cmd root | 管理创建→stdin登记→默认Runner Run→Central可读online→撤销/再登记→重复连接；graceful/第二信号/Force原deadline，7类owned callback/socket/process/文件锁/PG borrower实际join；不只编译/list |
| 迁移/回归 | 连续源00023→26（含真实24/25）、重跑与DDLrollback、新旧Audit记录；原Account/Admin/启动失败/离线CLI/原Runner D02配置差异逐项明确，不扫无关全部矩阵 |

独立验证者不能参与本卡设计/产品修复；可在`tests/runnercontrol/`拥有自己明确命名的验证源，用公开API、真实native资源和原协议独立oracle。PG/native/双cmd任何真实资源只在根fresh grant后启动，复用既有fixture与监督器最小接缝、实际Wait/精确资源ID/runtime/TCP清理尾；不得造假RETIRE或把空目录视为socketjoin。根协调资源与平台窗口，不因模块并行绕开单窗。

本卡完整接受需identity/control上述必需矩阵与独立验收；无真实macOS则只能报告Linux限定结果并保留平台gate，不能标整个D15双平台完成。Mount业务dispatch、实际D16 operation、D17数据面与D18ToolRuntime真实绑定仍需后续正式集成，不因测试替身通过而消失。若scope与端口改变，先修卡/独审受影响部分再实现，不把consumer缺失藏进TODO。

## 10. 当前状态与下一步

rev1独审发现payload闭集不完整及HTTP误列不存在Foundation Conflict，两项原结论保留；rev2仅补本卡§6/7的闭集表与§3既有码映射，原审者差异复审已接受；这不是产品运行接受。规格阶段作者自查完成正式来源/现Account/Audit/Runner D02依赖核对。根授权一次固定依赖准备，`go mod download github.com/gorilla/websocket@v1.5.3`实际exit0，使用本树任务缓存；不等于产品/协议构建或测试通过。当前未启动任何server/PG/browser，未实施migration；已落盘共享wire首片段，普通test/race/短时fuzz作者自测actual0，尚未独立实现验收。

本卡7个文档链接（含fragment）及current链接作者自查通过；限定diff whitespace通过，新文件亦逐行核无尾空白。SPEC已接受（identity共享路径已获根授权）。按shared wire/identity/service、WSS与双rootconsumer推进，有限稳定片段交叉独审；真实资源/其余平台gate保持显式未验。

本地identity首片段已实现pending/active身份、strict file、安全默认输出、FD相对权限验证、稳定排他锁及原子持久化；Linux实际文件与受控I/O失败race作者自测通过。首次文件权限反例因umask收紧测试创建mode而失败，已显式chmod恢复反例，原失败保留。尚无跨UID/真实进程Crash、macOS、登记或WSS结果，不能据此关闭完整credential/平台gate。

认证header与nonce严格解析、固定签名字节/Ed25519黄金向量、pending身份恢复签名已实现，protocol/identity两包作者race通过；无DB nonce消费/时窗、TLS或真实WSS验证。本地算法向量不代独立实现验收。
