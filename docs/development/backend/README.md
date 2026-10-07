# 后端开发

根 module 为 `github.com/LunaDeerTech/agenteam`，固定 Go 1.27.1。Central 已装配固定 pgx/Goose 数据库库，见[数据库说明](database.md)。Audit 的同事务追加、授权查询/生命周期清理端口与独立签名 cursor 已实现，见[Audit 说明](audit.md)。Secret 的 envelope、引用/lease、加密 receipt 和可恢复主密钥维护见 [Secret 说明](secret.md)。动态策略、受控 HTTP 与 SMTP 拨号端口见[出站说明](outbound.md)。D05 对象库提供受限流式存储、授权 reference/lease、Artifact/下载和 typed Runner transfer，接口与阶段见[对象实施规格](../work-items/d05-object-storage-design.md)。Central 与 Runner 分别装配；Runner 不导入 Central。中立 `internal/platform` 只处理进程日志和关闭协调，不提供授权、业务幂等、数据库事务或 Runner 设备协议。

Central 已在真实数据库、安全初始化、Object/Outbox 注册恢复及 Account/Mail 启动后提供诊断、正式账户 HTTP、System Model 配置 HTTP 和 Project Owner Usage 只读 HTTP。默认根已绑定 System Provider/Model CRUD、平台 selector、Model credential 独立写入及 Model 配置和 credential 两类原命令查证，使用真实 Account Session/admin、同一 Secret/Audit/Outbox；接口见 [System Model OpenAPI](../../../api/openapi/model-system.json)，装配边界见[根装配规格](../work-items/recovery-d09-system-model-root.md)。当前 Session/System 授权已接入 Audit、Secret、出站与 Outbox；Object 绑定本人当前头像，SMTP 使用受控出站和真实 Secret lease，Outbox 唯一生产 handler 仍为 `account.mail-enqueue`，没有 Model consumer。对象 Runtime 实际核 store identity、双 origin probe、ProcessGuard 与恢复门禁，见[对象 Runtime 说明](object-runtime.md)；已证实的 [Object runtime join 缺陷](../agent-team/object-runtime-join-regression.md)尚未修复，本次 System Model 根装配不解除该限制。Artifact/通用下载 HTTP、Project 创建与生命周期、Runner/Operation、Model Resolver/Invocation Facts 写入和实际 Provider/MCP 调用仍未绑定。系统管理员统一选择 Meeting Summary 模型的 S1 持久化库和 S3 解析库已实现，默认根 Resolution 与实际 Meeting 消费仍未绑定，详见下文；完整 D09 未完成。Runner 是未连接进程，整体 `ready=false`、`/readyz` 仍为 503。账户接口见 [OpenAPI](../../../api/openapi/account.json)，Artifact 与浏览器下载库边界见 [Artifact 说明](artifact.md)。

System Model 管理读口已实现并通过[独立验收](../agent-team/system-model-management-reads-verification.md)：GET/HEAD `/api/v1/system/model-credentials/{id}` 只返回当前安全 metadata（credential ID、purpose、version）；GET/HEAD `/api/v1/system/models/{id}/deletion-impact` 返回有界精确引用统计、替换要求和已知 adapter 阻断。两类读取各自拥有完整锁 union 的读取事务，在同一 Tx 内重验当前 Session/admin 后读取；最长 3 秒预算包含 HTTP 认证、锁等待和 SQL，并继承更早的调用方取消。仅确认 Committed 且 context 仍有效才返回结果，失败、Unknown 或取消不返回候选数据，也不自动重读。预览不授予删除权限，后续 DeleteModel 仍重验当前引用与替换事实；外域引用 adapter 仍未绑定。精确接口与边界见[管理读口规格](../work-items/recovery-d09-system-model-management-reads.md)。

[Meeting Summary S1](../work-items/d09-system-meeting-summary-selection.md) 已实现迁移 `00020` 下的系统独立 selector、独立 ID/version 和精确引用。`InitializeMeetingSummarySelection` 在旧四用途技术 singleton 已存在后，幂等建立 version=1、model=NULL 的新技术行；`GetMeetingSummarySelection` / `UpdateMeetingSummarySelection` 在事务内重验当前 Session/System 管理员。显式选择只要求 enabled System Provider 下的 enabled chat 模型；命令沿原 `model.selection.update` action 和 `(kind,user,key)` 幂等命名空间，原 lookup 可读取其安全历史回执。未配置时不猜默认模型，也不是 Project 创建的前置条件；本库不创建项目；后继 Meeting 消费系统统一选择，不允许项目覆盖或复制创建时默认值。

删除模型时 Summary 引用必须提供替代；同时承担原 memory 与 Summary 的模型须在同一事务更新两个 owner，各自推进一次版本，替代还须满足 memory 的 `json_schema` 要求。现有 deletion-impact GET/HEAD、DELETE replacement、generic lookup 及既有客户端已兼容第八种闭合引用分组 `platform_selector/meeting_summary`；旧七组与四用途 JSON 保持原义。Summary 专属 GET/PUT、设置编辑器、默认根的 Summary 初始化、Resolver 和 D24 真实 initial/update（含首轮标题）生成仍未绑定；旧四用途选择、Execution Summary read model 与既定 compaction snapshot 不变。Object runtime join、OpenAI tools 独立验收和 SPA 并发发布三项停止边界保持，`/readyz` 仍为503。

[Meeting Summary S3](../work-items/d09-system-meeting-summary-resolution.md) 的 current-resolution 库已支持 initial/update 共用 `platform.meeting_summary`，首轮标题属于 initial 的同次逻辑调用；不猜默认模型，也不允许项目覆盖或复制默认值。Summary 无需 `json_schema`：普通 text 模型沿 text-v1，具有真实 `json_schema` capability 的模型保留 structured-v1，后继 Summary 请求仍为普通 text，其他协议与能力沿原已接受闭集。`SelectModel` 只返回当前 Session/Project Read 下的安全 ID/version，不授予生成权限；Resolve 在完整锁内重验当前 Session、Project 与 Meeting/Operation/Call/input/phase/version 事实，并持有独立 Summary selector Shared 锁和原依赖锁。准备态可重规划；已提交 snapshot/binding 保持不可变，同一逻辑单位重入复用原 snapshot 与 canonical Secret lease，已释放 lease 不复活；后续选择、停用或删除替换不改历史，新调用才消费当前选择。

固定 candidate03 的作者五个新顶层与八个旧回归、独立 A/B 真实测试均通过；A 核当前授权、新 selector 锁与双 owner 删除竞争，B 核历史 snapshot/lease、原子回滚与 Unknown 后态。新 fixture 使用正式账户身份链和严格测试专属 Meeting canonical 行，不能代替 D24 生产事实；Unknown 是真实提交或回滚后的结果装饰，不是物理 COMMIT ACK 丢失证明。原错误码断言失败与两份测试文件的精确修正保留，生产授权未为测试放宽。各轮七个 owned 资源均实际等待并双清，daemon 侧未 wait 的僵尸差量另记，不声称全机清零。该结果只验库：生产 `Authorizations.Resolution` 与 `Invocations` 仍为 nil，不发送 Provider 请求，不创建 Meeting/Invocation/Usage，也不完成 D09/D24 或解除上述三停止与 ready503 边界。

[Invocation/Usage 账本库与 00018](../agent-team/invocation-usage-ledger-verification.md)已独立验收并以 `36e5ff1` 提交推送，该结果固定的连续已验迁移前缀至 18。可信 InvocationFacts 在完整同 Store/Tx/锁计划下驱动调用状态、历史回执与执行汇总三表原子更新；当前 Human Owner 的 List/Aggregate/Get/Rebuild 和 exact Lookup 均有 2 秒预算，提交后取消不发布候选 DTO，实际 Committed/Unknown 状态保留。PG17.x（最低17.8）fresh/populated、约束、nullable 历史链接、真实 wire/Secret 链及相关旧回归通过；独立最终 11 组与叠加已提交管理读口的 3 组均 PASS。当次结果是库/schema 验收；当前 Usage 只读 HTTP 已绑定默认根，见下文。生产 Facts owner、Runtime 发送/重试、lease/Process/lifecycle 仍未绑定，测试回执不授重发权限。

OpenAI Chat wire 库已验 `openai-chat-text-v1` 与 `openai-chat-structured-v1`：后者支持有界 strict `json_schema` 的请求、普通响应及 SSE 完整结果验证，保留原文本、usage、安全错误、同一 Budget 和实际 join；text 修订闭集不放宽。子集、资源上限与拒绝语义见[structured 规格](../work-items/recovery-d09-openai-chat-structured-wire.md)，原失败及真实验收见[报告](../agent-team/d09-openai-chat-structured-wire-verification.md)。这是受控本地服务上的库能力；生产 root 尚未组合 Resolver/consumer、Invocation/Usage 写入或真实 Provider 账号。OpenAI Chat tools wire 规格已采纳、14 路径实施中，尚无产品验收；不改变上述 root、Object 缺陷和 ready503 边界。

OpenAI Embeddings wire 已验 `openai-embeddings-float-v1`，由 `NewOpenAIEmbeddings` / `Start` 接收文本批量、空参数对象 `{}`，固定发送 `encoding_format=float`。调用方必须显式给出 `ExpectedDimensions`：它是本地结果要求（1–4096、数量×维度≤262144），不代表 Provider 默认维度或能力发现；原生 `dimensions`、token 数组、base64 等不在本修订支持范围。每次 Start 只调用一次正式 D04 Do、发送 POST，不做 adapter 重试或自动拆批，保留 D04 既有零字节透明网络重试。响应须实际 EOF、严格有界 UTF-8 JSON、完整唯一 index 和有限 float 向量；最多16MiB响应，usage 按原整数精确读取，零与缺失分开，不经 float64 丢失大整数精度。完整合法 usage 可在后续向量语义失败或取消时保留，向量候选归零；详细闭集见[Embeddings wire 规格](../work-items/d09-openai-embeddings-wire.md)。

Chat text、structured 与 Embeddings 必须注入同一 `Budget`，共用全局64/每 canonical Project8。`Result` 只允许一次串行消费，成功前要求 writer/callback、读体/parser、Close 与 Client.Drain 实际 join；取消、可见超时或 HTTP 对象已关闭本身不释放材料引用和槽。`Close` 只在调用方剩余预算内取消并等待，尾部未完由原 handle 保持，caller 仍负责所属清理窗口。沿[原 D04 限制](outbound.md)：当前正式 Response.Close 返回 nil，不宣称它暴露原生 Body 内部 Close 错误；本次不修改 D04 或旧 Chat 流程。

作者三组新真实测试及六组旧 Chat/structured 回归分五轮通过，最终独立两个真实代表通过；纯/容量与受控尾部证据按固定阶段复用，不称同轮整套全绿。真实验证使用任务拥有的 Account/Policy 与受控 TLS Provider fixture；官方 profile 仅绑定 SDK 静态 revision/SHA，不是 SDK 执行或真实 Provider 账号 smoke。这仍是专用 wire 库，没有实现 `Nonchat.Embed`、InvocationID、Resolver/Facts/Usage consumer 或生产 root/Provider 账号组合，不提供调用资格，不解除 `ready=false`/`readyz` 503，也不表示完整 D09 已完成。

[Central 嵌入 SPA 规格](../work-items/d28-central-spa-hosting.md)已在 `7a490ac` 经独立有界静审采纳，[原稿、审查与当前行政状态](../agent-team/system-central-spa-hosting-spec-verification.md)已归档。规划的显式 Web 发布将真实前端闭包嵌入 Central；普通无 tag Go 构建仍沿原行为。16路径私有实施已启动，但尚无发布构建、native HTTP或正式二进制浏览器验收；本段不宣称 Central 已提供生产 SPA，也不改变下列现有命令、运行依赖和 ready503。

## 构建与验证

D07 的账号、Session、邀请、密码恢复、挑战、Profile/Avatar/偏好和 System HTTP 已装配进 Central，构造、恢复与官方 Vue 浏览器 harness 见[账号说明](account.md)。SMTP 三模式、受限恢复日志、持久 attempt 与人工重试见[账号邮件说明](accountmail.md)。D26 [认证](../agent-team/d26-authentication-verification.md)和[个人设置](../agent-team/personal-settings-verification.md)已有各自产品页面验收，[公开 Account 入口](../agent-team/public-account-entry-verification.md)的 25 路径范围已验收并以 `787a5c7` 提交推送；生产 SPA 托管及完整 D26/D27 仍未完成，独立测试 harness 本身不作为产品 UI。

```sh
# 指向实际 Go 1.27.1；该环境可使用 /workspace/toolchains/go1.27.1/bin/go。
export AGENTEAM_GO=/path/to/go1.27.1/bin/go
sh scripts/check-go.sh
sh scripts/build-go.sh
```

未指定 `AGENTEAM_GO` 时脚本使用 PATH 的 go，并先核对精确 `GOVERSION=go1.27.1`。所有构建与检查设置 `GOTOOLCHAIN=local`，不会自动下载或换工具链。`go.mod` 保留 `go 1.27.1`；同值 toolchain 指令会被该版本的 `go mod tidy` 删除，因此不重复声明。

`check-go.sh` 依次执行普通 `go test ./...`、普通及 integration 源码的 vet、`go test -race ./...` 和两个真实二进制构建，不连接数据库。产物在被忽略的 `bin/`。单独运行进程验证：

```sh
GOTOOLCHAIN=local "$AGENTEAM_GO" test -count=1 ./tests/process
GOTOOLCHAIN=local "$AGENTEAM_GO" test -count=1 ./internal/central/app
# 真实数据库、迁移及 Central 进程；需要 Docker 和固定 fixture 镜像。
sh scripts/test-postgres.sh
# 受影响场景可定向运行；最终覆盖须按实际库存证明，不能只靠此 filter。
sh scripts/test-postgres.sh -run '^TestSecret'
sh scripts/test-postgres.sh -run '^(TestRealSecret|TestCentralSecret)'
# 真实私网 socket、生成 CA、DNS/策略/HTTP/SMTP 端口和整组数据库/进程验证。
sh scripts/test-security.sh
# Embeddings 三新组及旧 Chat/structured 两组；使用 owned fixture，分别运行。
sh scripts/test-security.sh -run '^TestModelOpenAIEmbeddingsWireHTTP$'
sh scripts/test-security.sh -run '^TestModelOpenAIEmbeddingsWireTerminal$'
sh scripts/test-security.sh -run '^TestModelOpenAIEmbeddingsWireBudget$'
sh scripts/test-security.sh -run '^(TestModelOpenAIChatWireHTTP|TestModelOpenAIChatWireStream|TestModelOpenAIChatWireJoinAndBudget)$'
sh scripts/test-security.sh -run '^(TestModelOpenAIChatStructuredHTTP|TestModelOpenAIChatStructuredStream|TestModelOpenAIChatStructuredCloseAndBudget)$'
# 上述 integration 脚本也自动取得真实 MinIO；均需同一固定 binary。
# 真实 PG + TLS MinIO + 出站/进程套件；需按固定研究构建并提供 MinIO binary。
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio sh scripts/test-objects.sh
# 对象定向验收；这不会把未命中的 D04 测试视为回归通过。
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio sh scripts/test-objects.sh -run '^TestObject'
# Outbox 数据库状态机、真实 Central 与共享 ProcessGuard。
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio sh scripts/test-postgres.sh -run '^(TestOutbox|TestCentralOutbox|TestRealOutbox)'
# Account 固定分组；all 为默认，按顺序运行九组，浏览器组需先装锁定依赖。
npm ci --prefix tests/account-captcha-web
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio GOFLAGS=-p=1 sh scripts/test-accounts.sh all
# 单组示例；不会把其他 no-tests 包计为兼容通过。
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio GOFLAGS=-p=1 sh scripts/test-accounts.sh app
```

`test-accounts.sh` 的固定组为 `mutations`、`identity`、`mail`、`profile`、`avatar`、`avatar-recovery`、`http`、`app`、`library`；前八组沿原 `test-objects.sh` 的精确选择，`library` 完整执行 `internal/central/account/...`。真实 fixture 保留 `-race -count=1 -timeout=6m` 和包级 `-p=1`，内部并发断言不变。完整兼容采用旧域完整包覆盖、运行前固定的 Account 穷尽分组及普通 `check-go.sh`，不把增长后的 Account 累计执行硬塞进一个 6m 包预算。D07 已采纳结果是明确输入上的分组、受影响补验和未变证据复用；原失败及限制见[D07 当前进度](../work-items/d07-account-session-smtp.md#当前进度)，不称一次整套全绿。临时验收 wrapper/overlay 不是生产接口或上述脚本的默认行为。

`tests/process` 在临时目录构建真实 cmd，普通测试检查纯 CLI、配置拒绝、Runner SIGINT/SIGTERM 和依赖方向；Linux 下检查未连接 Runner 没有 socket descriptor。实际 Central 成功启动、迁移/对象初始化失败、监听冲突、启动信号、数据库/存储故障恢复与健康超时放在 integration suite；所有成功启动均使用真实 MinIO，不跳过对象阶段。Central app 普通测试覆盖装配顺序和时钟边界；integration 中的测试进程调用真实 Store/Migrator，通过真实 HTTP+Tx、Secret worker 与受控出站验证正常 drain、阻塞查询、第二信号和不合作 callback 的有限退出。测试专属 route/barrier 不进入生产入口。并发顺序用 channel、数据库锁和观测事实协调；测试不读取外部 `.env`、凭据或已有服务，本机监听用 loopback port 0，出站成功路径使用 owned internal Docker 私网与精确规则。

前端依赖和检查仍独立；本次 Go 工程变动无需无条件执行前端全量检查。

## 配置与命令

仅读取进程环境，不自动加载 `.env`。参考 [Central 示例](../../../deploy/central.env.example) 和 [Runner 示例](../../../deploy/runner.env.example)。一般配置存在但为空的变量无效；沿既有对象库规则，空 `OBJECT_CA_FILE` 等同未设置，缺省或空 `OBJECT_TLS_MODE` 取 `verify-full` 并要求 HTTPS。必需对象字段及显式空 `OBJECT_TRANSFER_ENDPOINT` 仍拒绝；当前进程前缀中的未知变量拒绝，另一进程前缀及一般系统环境忽略。Central 另外拒绝非空隐式 PG 配置来源。错误只报告固定字段名与原因，不打印配置值。

| 变量 | 默认值 | 约束 |
| --- | --- | --- |
| `AGENTEAM_CENTRAL_LOG_LEVEL` / `AGENTEAM_RUNNER_LOG_LEVEL` | `info` | 精确 `debug/info/warn/error` |
| `AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT` / `AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT` | `10s` | Go duration，100ms–5m；第一次停止后的总预算 |
| `AGENTEAM_CENTRAL_HTTP_ADDR` | `127.0.0.1:8080` | host:port，1–65535；仅显式 loopback IP 允许 port 0 |
| `AGENTEAM_CENTRAL_CURSOR_KEYRING` | 无，必填 | format=1、current_kid 与 1–32 把独立随机 32-byte key；严格带 padding base64，JSON ≤16 KiB，见 [Audit 配置](audit.md#cursor-与部署配置) |
| `AGENTEAM_CENTRAL_SECRET_KEYRING` | 无，必填 | format=1、规范正 int64 current_version 与 1–32 把独立随机 32-byte AES key；材料不得与 cursor 重复，见 [Secret 配置](secret.md#部署配置与启动) |
| `AGENTEAM_CENTRAL_ACCOUNT_KEYRING` | 无，必填 | format=1、current_kid 与 1–32 把独立随机 32-byte key；严格带 padding base64，JSON ≤16 KiB，与全部当前/历史 cursor、Secret、download 材料不同，见[账号部署](account.md#部署输入与首次管理员) |
| `AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG` | 无，必填 | 规范绝对文件路径；配置阶段只验证路径，启动核服务 UID、0700 父目录、0600 常规文件及无符号链接，不自动创建父目录 |
| `AGENTEAM_CENTRAL_OUTBOUND_CA_FILE` | 无，可选 | 追加到系统 trust roots 的部署 PEM CA，纯配置验证即读取，最多 1 MiB；与数据库 CA 独立，见[出站说明](outbound.md) |
| `AGENTEAM_CENTRAL_PUBLIC_ORIGIN` | `http://localhost:8080` | 正式部署 HTTPS，仅字面 localhost/loopback 允许本地 HTTP；无 userinfo/query/fragment，路径仅空或 `/`；规范化主机、IP、默认端口和尾 `/`，代理须保留 canonical Host |

Central 还必须配置 `AGENTEAM_CENTRAL_DATABASE_URL`；TLS 默认 verify-full，显式 CA 文件会在配置检查时读取验证，其余数据库参数及范围见[数据库配置表](database.md#版本与配置)。连接、迁移 guard、全部迁移和首次 Check 共用 `DATABASE_STARTUP_TIMEOUT`，随后在独立且共享的 30s 安全初始化预算内构造真实账户依赖并打开受限 Sink，验证 Account key registry、cursor/Audit、Secret registry/canary/write fence。仅在 Secret 初始化成功且原 ctx 仍有效后，才用同一 ctx/deadline 依次调用同一 Model Service 的 `Initialize` 和 Usage Service 的 `Initialize`，两者在 Secret maintenance 及后续业务启动、监听之前完成，不另起预算。Usage 只检查已有表结构，不建表、填默认行或启动 worker；Model 的原 `Initialize` 首次只建立未配置的四用途技术 selector，重启保留已有配置和命令历史，不补默认模型，也不代调 `InitializeMeetingSummarySelection`；后者尚未接入默认启动根。随后验证 DB 出站策略、对象 bucket/双 origin 实际写读删 probe、ProcessGuard 与恢复门禁、Outbox 唯一 handler 注册/恢复，再完成 Account bootstrap/账户及头像恢复、Mail canonical/恢复与技术 Check，最后 HTTP bind。后续步骤不能重置前序消耗的预算；Model 初始化失败或 Unknown、Usage 结构检查失败均不放行监听，SMTP 是否配置或远端可达不作为启动探测。两个阶段都受启动停止信号取消。配置缺失退出 2，连接、版本、迁移、受限日志打开或安全初始化失败退出 1。

两个二进制接受 `--help`、`--version`、`--check-config`，无参数启动进程。未知参数和多余位置参数返回 2，不回显输入。help/version 不加载配置或启动服务；Central check-config 验证当前配置，包括四用途独立 keyring、Account 恢复日志路径、安全 PublicOrigin、必需存储坐标/凭据、spool 路径和显式 CA，不连接、创建目录或打开恢复日志。当前输出仍为 `scope=d05, valid=true, ready=false`，help/version 也保留 D05 标识，这不表示配置只到 D05；Runner 保持 `scope=d02`，另有 `connected=false, authenticated=false`。check-config 不证明日志权限、bootstrap、canary/策略或任一 Runtime 已初始化。出站策略由 DB 管理，不接受环境规则绕过；RunnerLocalConfig 仍未实现。

Central 另接受成对的 `--repair-migration <version> --expected-checksum <sha256:...>`，只使用已编译迁移和精确指纹，不接受 SQL/文件路径。`022dcea` 已提交正式迁移为 00001–00013，均为仅 Up 的 tx 迁移，不写 Down；修复明确失败为 `MIGRATION_REPAIR_UNSUPPORTED`；不得将 CLI 存在理解为任意版本都可强制修复。使用规则见[迁移与修复](database.md#迁移与修复)。

```sh
./bin/agenteam --check-config
./bin/agenteam-runner --check-config
AGENTEAM_CENTRAL_HTTP_ADDR=127.0.0.1:8080 ./bin/agenteam
./bin/agenteam-runner
```

上述 Central 命令要求事先设置本部署的数据库、MinIO、四用途 keyring 和 Account 恢复日志路径，并预建受限日志父目录；示例文件中的占位材料故意无效，必须由部署独立生成和替换。纯 help/version 和 Runner 不需要 Central 数据库或 keyring 环境。

## 诊断与日志

| 路径 | GET / HEAD | 含义 |
| --- | --- | --- |
| `/livez` | 200 | `status=alive`，仅 HTTP loop 存活 |
| `/readyz` | 503 Problem | 数据库、Secret、出站、对象、Outbox 或 Account 基础异常为 `DEPENDENCY_UNAVAILABLE`；已实现组件健康时仍为 `DEPENDENCY_UNBOUND`，后续产品能力未齐 |
| `/diagnostics` | 200 | `ready=false`；DB/Account 健康时 identity/outbox_handlers 为 `available`，Audit/Secret/outbound/Outbox 授权为 `system_bound`，对象授权为 `avatar_bound`；Project、Runner transfer 授权与 Runner protocol 的既有诊断项仍报告 `unbound`，Usage 读口不改变这些标志；只含安全技术字段 |

数据库、对象、Outbox 与 Account 健康由同一调度每 10s 并行采样，同轮共用 2s。Account 检查真实 Account/Mail 技术状态，不发 SMTP 探测；未配置 SMTP 不产生依赖故障。每项实际 Check 返回前不能再次采样，即使原轮已超时；迟到成功被丢弃，不能恢复旧健康。样本超过 20s 或检查失败不再显示旧健康；新检查成功才恢复，独立组件不能刷新旧 DB 样本时间。HTTP 仅读快照，健康故障不会使 `/livez` 失败。健康循环停止只表示不再调度，已发 SQL 和 I/O 仍由真实资源 owner 及 DB 最后关闭收束。

上述三个诊断路径只允许 GET/HEAD，HEAD 无 body；其他方法 405 且包含 Allow。账户表面另有 `/api/v1` 的 34 个显式 method/path、26 条路径，包含 Session、本人资料/头像和账户系统写操作，详见[账号 HTTP](account.md#正式-http-与资料头像)。账户边界先核 canonical Host/Origin，再路由和解码；通过边界的未知 API、页面、静态资源返回 404，错误 Host 返回 403，不回退 HTML。组合根唯一一层公共 middleware 负责 request ID、安全 Problem、panic 恢复和 HTTP 日志，流式错误中断后仍关闭并等待 reader。

System Model 路由使用同一 Account 安全边界，覆盖 `/api/v1/system/` 下 `model-providers`、`models`、`model-selection`、`model-commands`、`model-credentials`、`model-credential-commands` 六个精确路径根及其子路径，详情见 [System Model OpenAPI](../../../api/openapi/model-system.json)。配置写入、credential 写入及两类 lookup 均保持原当前授权、CSRF、幂等和 Unknown 规则；credential 写入与 Provider 绑定是两个独立命令。各 handler 继承原 request context，两条管理读口额外施加上述 3 秒预算，纳入同一 HTTP admission/drain 和 DB 最后关闭协议，不新增 Model runtime、后台调用或重复 middleware；force 有界退出不证明所有 writer 已 join 或回滚。

日志用 `slog.JSONHandler` 写 stderr；stdout 仅输出 CLI 结果。正常日志包含 UTC 时间、level、service、event、随机进程 run_id。HTTP 另有 request_id、method、声明的 route、status、duration、bytes；未匹配路由用 `unknown_route`。数据库日志仅增加白名单阶段/错误码、五位 SQLSTATE 和迁移版本；安全日志仅输出 cursor_initializing/audit_initializing/secret_initializing/secret_maintenance_starting/secret_unavailable/outbound_initializing/object_initializing/object_available/object_unavailable/outbox_initializing/outbox_available/outbox_unavailable/initialized/failed 固定阶段。不记录原始错误、panic/堆栈、SQL/参数、DSN、证书路径、配置、body、query、Authorization、Cookie 或其他任意 header。原始 net/http 错误文本只投影为固定 `HTTP_SERVER_ERROR`。启动在实际 bind 后记录监听地址，Runner 明确 `unconnected`，不尝试连接、注册、认证或监听。

受限 Account recovery log 是独立敏感渠道，不是上述普通日志：首次管理员密码只尝试写一次；SMTP 未配置时邀请/reset 链接可写入，配置后发送失败不改渠道。不得复制其正文到 stderr、Audit、诊断或报告；部署操作者管理读取、备份与保留权限。文件/目录安全检查、written/unknown 与真实 Close/join 语义见[账号邮件说明](accountmail.md#smtp-与日志)。

## Project Owner Usage 只读 HTTP

默认 Central 根已装配三个 GET/HEAD 资源，接口与闭合字段见 [Project Usage OpenAPI](../../../api/openapi/project-usage.json) 和[实施规格](../work-items/d09-project-usage-read-http.md)。沿同一 Account Store/Authority 构造 Project Authority（Sessions、Routes 均为原 Account Authority），再构造引用同一 Project Authority 的 Usage Authority/Service，并复用已加载的 cursor keyring。handler 持有这些同实例及原 Account Service 的 HTTPBoundary；读取在实际事务内重验当前 Human Session、Project Owner 与项目状态，系统管理员没有跨 Owner 读取豁免。

| GET / HEAD 路径 | 读取职责 |
| --- | --- |
| `/api/v1/projects/resolve?username=…&project_name=…` | 按当前名称解析正式 ProjectRef 与稳定 ProjectID |
| `/api/v1/projects/{id}/model-usage` | 读取当前 Owner 项目的分页 Invocation 安全投影 |
| `/api/v1/projects/{id}/model-usage/summary?group_by=…` | 沿原聚合读取 consumer、agent、model、provider、execution、meeting、purpose 或 day 分组 |

resolve 不授予后续 Usage 读取权限；稳定 ID 请求每次自行授权。旧名称返回404，不提供 alias 或重定向；名称复用可指向另一 ProjectID，同一项目改名不使其稳定 ID 游标失效。查询严格拒绝未知或重复键，GET/HEAD 不接受实体。计数、token 和版本使用无损十进制 JSON 字符串，保留 null 与已知零的区别，不暴露内部运行身份、原始错误或凭据。

两秒总预算从 HTTP 认证前开始，并继承更早的 parent deadline；真实读写 deadline、Body.Close、Flush 和取消 callback 均在返回前实际完成或 join。只有正常 Committed 且 context 仍有效才发布完整有界投影；Unknown、回滚或取消保留原安全 Fault/Problem 状态并丢弃候选。HEAD 完成同样的认证、查询和完整编码检查，返回与 GET 表示一致的头和 Content-Length，成功与错误均无 body。

生产 `Invocations` 仍为真正的 nil；本次只读取经正式契约持久化的 Project/Usage，不绑定 Runtime Facts、发送/重试 consumer、Project 初始化或生命周期、生产 Skills，也不为新部署制造项目或账本数据。默认根沿原 startup context 顺序完成 Secret → Model → Usage 结构检查。原 Account/System/Outbound/Audit 路由及关闭链保持，`/readyz` 仍返回503 Problem，`/diagnostics` 的 `ready` 仍为 false；完整 D08/D09、Usage UI 与 Meeting Summary 服务均不由此完成。

## System 运行信息

`GET /api/v1/system/runtime-information` 是本阶段唯一新增的运行信息只读口，仅当前系统管理员可用；不接受 query 或 body，其他方法为 405（`Allow: GET`，HEAD 无 body）。接口与闭合字段见 [OpenAPI](../../../api/openapi/runtime-information.json) 和[实施规格](../work-items/d28-system-runtime-information-http.md)。

HTTP 预认证后，在同一 Store 的新事务内持有当前 User Shared 锁，重验当前 Session/admin，再读取一次快照；只有实际 Committed 且 context 仍有效才发布完整候选。失败、Unknown 或取消不发布候选。最长 3 秒预算从预认证前开始，继承更早 parent，覆盖空体 EOF、授权事务、编码、实际 Write/Flush、Body.Close 和取消回调 join；成功 JSON 完整 UTF-8 编码上限为 16 KiB，不截断字段或分段发布。

数据来自同一 Central root 的原健康缓存，GET 不调用 Check/probe 或触发刷新。`observed_at` 是本次复制缓存的观察时刻；数据库 `last_success` 内的 PostgreSQL/pgvector 版本、检查与接收时间始终属于原历史成功样本。当前检查失败优先报告 unavailable，否则成功样本超过 20 秒报告 stale，恰 20 秒仍不过期；失败或陈旧不会把历史版本改成本次查询结果。Object 字段只提供 MinIO 后端的 Object Storage 聚合观测，不提供 MinIO server 版本、部署坐标或子检查归因。Central 构建版本尚未记录，明确返回 `version:null`、`safe_reason:build_version_not_recorded`，不以 CLI 阶段标签或 Git 值补造版本。

`readiness.ready` 始终为 false：原六项基础能力任一不可用时为 `DEPENDENCY_UNAVAILABLE`，均满足仍为 `DEPENDENCY_UNBOUND`。200 只表示管理观察读取成功，原 `/readyz` 仍为 503；此口不证明平台 ready、完整 D27/D28、页面或任何外部调用已交付。

以下命令分别复现最小原生 HTTP/旧诊断代表与两个真实 fixture 顶层；使用上文固定 Go 和 MinIO。第一条实际开启自有 loopback listener，后两条需要原 PostgreSQL/MinIO/安全 fixture，不属于无资源纯测：

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly \
  "$AGENTEAM_GO" test -race -count=1 -timeout=45s \
  -run '^(TestRuntimeInformationHTTPNativeEOFAndKeepAlive|TestRuntimeInformationHTTPNativeSlowBodyBudgets|TestRuntimeInformationHTTPNativeWriteAndFlushAbort|TestDiagnosticRoutes)$' \
  ./internal/central/runtimeinfo/http ./internal/central/app
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
  sh scripts/test-security.sh -run '^(TestSystemRuntimeInformationCurrentAuthorityAndTerminal)$'
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
  sh scripts/test-security.sh -run '^(TestSystemRuntimeInformationRootSnapshotBinding)$'
```

本卡 13 源的固定候选已完成受控纯测、race/vet、编码/schema 及构建检查。作者 `native02` 四顶层、`new-account01` 六子例和 `new-root01` 分轮实际通过；`native01` 原解释器门禁拒绝未启动测试，保留该原失败。独立同轮通过 `TestIndependentRuntimeCurrentAuthorityAndTerminal` 与 `TestIndependentRuntimeRootCacheAndCompatibility`：前者是真实 PG 授权/事务与受控 Source/writer，不作为 TCP 提交前零字节证据；后者在同一真实 TCP 连接核 Runtime→Session→Runtime→ready503 的完整 EOF、原缓存、零新增 Check 和正常 root 关闭。自然 3 秒期限、Unknown 及受控尾部由各自纯阶段证明，不以数据库 1 秒锁超时替代。真实各轮均实际退出并核清自有资源；未进行 MinIO 故障注入或解除既有 Object join 限制。

## 停止与退出

入口在启动前注册 SIGINT/SIGTERM。第一次信号或调用方取消只进入一次 stopping，第二次信号强制关闭；重复程序化 Stop 不重置预算。Central 的 serving context 独立于停止请求，首次停止不取消正在执行的 handler；Shutdown 使用独立 deadline。

仍交给 handler 的迟到请求返回 503 `SHUTTING_DOWN`。net/http Shutdown 接管监听或连接后，可直接拒绝连接或返回 EOF，不保证每个尚未分发的请求都经过 Problem handler。首信号停止健康领取、新 Secret batch、出站请求、对象/transfer、Outbox 及 Account/Mail 新准入；在途工作继续使用原预算。Account 组合先等待 Mail 完成真实协议 I/O、FinishDelivery 与 join，再关闭 Account Runtime/Core 的最终收尾准入。HTTP/Outbox、LoginResponse 材料使用、Avatar 解码/reader、Mail worker 和整个 Sink 的 Write/Sync/Close 都须实际 join，才允许 Object Runtime 释放共享 ProcessGuard。HTTP、维护、对象与出站排空后才 Store.StopAdmission，已有 Tx/Rows 按同一剩余 deadline 排空并关闭池。

超时或第二信号取消 Outbox、HTTP、Account/Mail 和 owned 出站；Account Force 同样按 Mail→Runtime/Core 顺序，把原 context 传给每一项，即便前项耗尽预算也实际发起其余取消。HTTP、出站、对象主/transfer 两个 Transport、worker join 与最后的 DB ForceClose（含有界 CancelRequest）共用最多额外 1s，不逐阶段重置。任一 HTTP/Outbox/Account 材料、reader 或 Sink 尚未真 join 时，只强关 Object Service 的 I/O，不释放共享 guard；guard 保持到实际 join 或 OS 退出。DB 最后发起关闭，剩余预算已耗尽也不跳过。启动失败或信号覆盖已取得的部分账户/日志资源，晚返回资源仍清理。日志区分 drained/forced；强制退出不证明事务、副作用已回滚或业务已停止。

| 退出码 | 含义 |
| --- | --- |
| 0 | help/version/check-config 成功，或进程干净停止 |
| 2 | 参数或配置拒绝 |
| 1 | 初始化、监听、意外 Serve 错误、drain 超时或第二信号强关 |

Runner 当前没有 RPC、子进程或长连接；第一次信号停止真实未连接进程即可。D15–D17 后续绑定其 Managed Process 和通道关闭顺序。HTTP hijack/WebSocket 不由 Server.Shutdown 自动等待；当前生产没有该连接，后续连接 owner 必须登记自身停止与关闭责任，不将当前 HTTP 测试当作未来长连接或持久恢复验收。

## D05 B01 对象库

`internal/central/object` 是组合根可使用的库；Central 已装配 Runtime、必填 MinIO，并通过 D07 的真实 AvatarAuthority/ProfileService 提供本人头像 HTTP。通用 Object、Artifact、下载和 transfer HTTP 仍未注册。`object/contract` 的 AccessPlanner、ResourceAuthority、精确 ObjectReadAuthority、ProjectGate、CleanupAuthority、LeaseAuthority、ProcessAuthority 均是正式端口，Avatar 以外未绑定分支拒绝依赖该端口的操作。只有领域确认的 existing owner 会在上传发布 Tx 获得 canonical reference；prospective owner 收到的 receipt 只证明存储成功，不能作为普通读取权限。Avatar 属 System 分区且普通权限限 exact User 的当前对象；维护取消经固定 ObjectMaintenance 与持久 cleanup cause 单独核验，不创建 Avatar 读取 grant。

每个数据库阶段先在 Tx 外调用 `DiscoverAccess`，取得绑定完整请求、真实 Actor/owner 父映射及模式的不可变计划。组合方把全部对象/source/lease 计划与自己的 `extraLocks` 交给 `AcquireAccessPlansInTx`，一次合并取最强模式的锁；各 InTx 显式接收对应 plan 和同 live Tx 的 token。`ValidateAccessPlanInTx` 在当前 Tx 重读映射，随后才检查当前权限；依赖变化返回 `RESOURCE_BUSY`，调用方必须回滚并重新规划，不补锁、不升级、不自动重试。ExecutionPayload 的 ID 是 PayloadID，Execution gate 来自真实映射；SkillRevision、MeetingFile 同样不能用 owner.ID 猜父实体。计划不是权限，也不参与业务语义摘要。

低层组合按 PreparePayload → ReserveUploadInTx → 确认外层提交 → UploadPrepared → PublishVerifiedInTx 工作。InTx 不开另一个 Tx，也不执行 S3 I/O；业务调用方须管理外层提交与 unknown 查询。PutObject 包装此流程。每次 raw-body 重放仍以 64 KiB buffer 完整计量并计算 SHA；不依赖 seekable reader 前缀、ETag 或 caller 声称的 checksum。上传前确认 pending/attempt/lease 提交，每个随机候选 key 仅一次 SDK 条件 PUT，完整读回后才能 verified/发布，成功重放保留原 ObjectID、key 和 Audit。COMMIT unknown 核实同样比较本次完整语义摘要，包括正文、MIME 与 expected version；另一个请求抢先提交同 key 时不冒用其成功结果。取消不抹去原命令已提交事实，也不返回可再次消费的 receipt。

单对象为 0–1 GiB；spool 同时两个准备/上传、总预留 2 GiB，并留 128 MiB 磁盘余量。目录 0700、文件 0600、进程独占锁与持久 manifest；恢复只能清理已证明 exact ProcessID 终止且 DB attempt 已不需要的本域文件。不根据时间、心跳或网络失败猜测进程死亡。全局 64、每命令两个未收敛 attempt；全局准入有共同 DB 锁，不能靠各 object 锁分别计数。首次 JSON 写入前先持久化只含随机 PayloadID/ProcessID 的零字节 init claim；JSON 和目录确认后改为 owner claim，再创建正文。启动仅把匹配 init claim 的空文件/严格初始序列化前缀视为不可读残留，损坏 JSON、矛盾身份和无归属文件仍拒绝；正文/manifest 删除后才删除 claim。遗留清除仍需 exact-process death 与 DB 不再需要的证据，不能从缺正文猜测原业务失败。

StatObject 只读已授权 metadata，不探测 payload；ReadObject 先确认内部 reader lease，再完成真实 GET 的状态/长度/首段检查。全文读保留 64 KiB 末段，实际 EOF、长度及 SHA 全部通过才交付最后一段；错误时调用方必须中断尚未完整的输出流。Range 只证明区间长度和边界，不宣称已重算全文 SHA。Reader 必须 Close，直到源 I/O 真正关闭/join 后才释放 lease；提交 unknown 保留精确 lease checkpoint。历史/执行/transfer lease 只保护用途，普通授权不能从任意 active lease 推导。

清理先持久 gate，阻止新发布/绑定/读取，再核验引用、reservation 和实际 lease。所有外写取得确定终局且无外部 grant 时才删除 key 并核实 absence；可能存在迟到写时使用有界、无条件零字节 marker，成功响应并完整读回空 SHA、无旧 metadata 后只确认内容消除。marker 首版永不自动删除；它不能替代正式 lease 的可信 terminal 证据。Project 清理保留他域未收敛 lease，全部业务内容与本域事实清理后才能 completed。Recover/维护 worker 只核实存储和推进已授权清理，不能以后台 Service 代替 Owner 发布结果。

库配置由 `LoadStorageConfig` 的显式 lookup 提供下列固定命名；Central 已将其并入必填配置、共享启动/健康/停止预算，另需下文 Runtime 配置。它不读取默认 AWS 凭据、代理或业务 Secret。该存储 endpoint 是可信部署通道，不经过动态出站策略；默认校验证书，显式 HTTP 仅用于部署明确选择的可信通道。

| 库变量 | 规则 |
| --- | --- |
| `AGENTEAM_CENTRAL_OBJECT_ENDPOINT` | 固定 http/https origin，无 userinfo/query/fragment/路径前缀；禁止重定向和环境代理 |
| `AGENTEAM_CENTRAL_OBJECT_BUCKET` | 预建私有 bucket；固定 path-style、`us-east-1` |
| `AGENTEAM_CENTRAL_OBJECT_ACCESS_KEY` / `AGENTEAM_CENTRAL_OBJECT_SECRET_KEY` | 本部署显式凭据；opaque 配置/错误不输出原值 |
| `AGENTEAM_CENTRAL_OBJECT_TLS_MODE` | 默认 `verify-full` 对应 HTTPS；`disable` 必须显式且对应 HTTP |
| `AGENTEAM_CENTRAL_OBJECT_CA_FILE` | 可选追加 PEM trust roots，常规文件且最多 1 MiB |
| `AGENTEAM_CENTRAL_OBJECT_TRANSFER_ENDPOINT` | 可选同 bucket/store 的固定 origin；默认主 endpoint；不是业务可选 URL |
| `AGENTEAM_CENTRAL_OBJECT_SPOOL_DIR` | 默认 `/var/lib/agenteam/object-spool`；规范绝对路径；运行时验证真实 owner/mode/锁及 sibling ProcessGuard |
| `AGENTEAM_CENTRAL_OBJECT_DOWNLOAD_KEYRING` | 必填；与全部当前/历史 cursor、Secret 材料独立，见[Artifact 下载](artifact.md) |

Project 清理用持久批次计数轮转，每批至多 100 个对象，前缀的 protected lease 不会饿死后面的可清对象。Recover 遇另一活实例、无法证明死亡或单项规划/恢复失败时，保留该项 lease/pending checkpoint，并继续其它已获授权的独立 reader/writer 释放、attempt 核实和清理；首个错误仍返回，不能被当成全部清理完成。取消后不再领取下一项，失败 checkpoint 只在后续确认成功后移除。

bucket 必须从未启用 versioning、未启用 ObjectLock、无 lifecycle 和 bucket policy；Suspended 或配置查询失败也拒绝。初始化将持久 DB instance UUID 与 `control/store-identity` 绑定，只有新库+空 bucket 可建立原 UUID marker；confirmed marker 丢失/不符不能被自动覆盖。库提供真实 Check、Recover/维护状态、StopAdmission/Drain/Force；首停保留既有操作，force 使用调用方共享 context 取消实际 socket/源流并 join，不为每个资源追加一秒。

SDK 固定 `github.com/minio/minio-go/v7 v7.3.0`（自身要求 Go 1.25，项目仍精确 Go 1.27.1），`MaxRetries=1`、`DisableMultipart=true`、known length、预计算全文 SHA header 与 payload `If-None-Match:*`。本次 tidy 保持既有 pgx/Goose 及既有 root require 版本，新增 17 项显式 indirect require；SDK 与其运行期依赖共 19 个 module，其中部分原已在依赖图中。实际选中 `x/crypto v0.55.0`、`x/net v0.58.0`、`x/sys v0.47.0`、`x/text v0.41.0`，精确完整集合由 go.mod/go.sum 固定；不引入 MinIO server 的 Go 依赖到项目。

真实 fixture 使用[研究修订2中的固定来源与构建步骤](../work-items/d05-object-storage-research.md)，不是已取得官方 MinIO 镜像。server 为 RELEASE.2025-10-15T17-29-55Z，源码 commit `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`；linux/amd64 binary SHA256 必须为 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`。`AGENTEAM_MINIO_BINARY` 指向按该步骤构建的非秘密产物；未指定时可复用研究记载的临时构建缓存，缺失或 SHA 不符则失败，不下载一个不明版本替代。

`test-objects.sh` 验证 binary 后，在固定基础镜像中以 host UID/GID 执行它；没有声称该基础镜像本身是 MinIO。每轮生成新 nonce、内部网络、TLS CA、凭据和隔离数据目录；套件验证 label、精确 container/network ID、IP 和 mount，再连接。结束及信号中断只清理本轮 owned 资源并核对残留；研究源码/binary 缓存可保留，随机凭据和数据挂载不得残留。套件组合原 PG/出站/进程验收；普通 check-go 不启动 Docker。当前阶段验收及尚未绑定内容以[D05 主卡](../work-items/d05-object-storage-artifact.md)为准，不把局部库测试或诊断存活当成完整产品 ready。


## D06 事务 Outbox

`event/contract` 仅依赖 foundation，保存 typed Event/Header/schema/codec；领域生产者通过 `outbox/contract` 在原业务 Tx 中 Append。组合方预先收集 Actor/Project、注册屏障和业务锁，一次 AcquireAll；InTx 只验证已经持有的模式，不暗中取锁或升级。注册边界基于 CACHE 1 的数据库 sequence 与 SH/EX 屏障，晚注册不会回放旧 payload。投影 generation、canonical 扫描与 dirty 队列由未来领域自己的事务管理；注册成功不代表投影 ready。

Runtime 默认 4 个全局/2 个每 handler 回调、每页 64、每 cycle 至多 8 次实际 claim，退避 1/2/4/8/16/32/60s。handler 副作用、processed marker 与 delivery 成功同 Tx；Retry、终局拒绝和 panic 整体回滚。claim/Apply 提交未知先等原 Delivery 锁终局再核事实，不把空查询或超时当未提交；取消后仍活的回调继续占槽，跨实例只能凭正式 exact-process 证明收敛。安全保护项不会阻断独立项，结构/数据库错误也不能被折算为健康。

Human 重投先验证当前 Session/Owner 或 SystemAdmin，历史 receipt 也重新授权；首次重投另验当前 gate、版本、handler 和同 Tx Audit。归档只终止 domain_ingress，Restore 不自动重开旧 terminal；从未 claim 的停止项保留零 attempt/零 latency，显式重投后首次真实 claim 才建立 attempt。Project 删除先 stopping 允许真实收束事实，再 cleaning 封闭所有 Append（包括同 ID 重放）；每批至多 100 个 Event，保留未知原 writer，最后实际删除本域正文、attempt/marker/command，仅留技术 gate。恢复 worker 仅从已装配的 Project provider 检测可选 LifecycleActorResolver，按 exact 持久操作取当前注册 Actor，随后仍完整取锁与当前校验；有待恢复项但未绑定时明确拒绝，空态或仅 completed receipt 不需要伪造身份。预取消授权在 Project SH 下保留 provider 的全部锁，持锁捕获精确 run，提交确认后只取消原 handle，再以 EX 重验；提交未知或迟到旧操作不能广播取消 Restore 后的新任务。

授权诊断库逐次验证当前权限；System 与 Project 独立，Summary 来自同一 SQL snapshot，固定 5m/1h/24h 窗口，旧积压不被窗口裁掉。分页只裁 Items，统计使用精确十进制标量，已知与未知延迟分开，最多 128 个 handler/type 与 20 条固定安全理由，共用 2s 上限；不返回 payload/原 error。生产根在同一 catalog Seal 前注册 Account 事件及 `model.configuration_changed`、`model.embedding_selection_changed` 两类 Model 事件，producer 绑定各自真实 Authority。唯一生产 handler 仍为 `account.mail-enqueue`，并保留真实 Session/System 授权；handler 只在原 Tx 建立邮件任务，实际投递由 Mail Runtime 完成。Model 事件随原配置命令同 Tx 持久追加，没有 Model 订阅或 deliveries，不伪造已消费。通用 Outbox 管理 HTTP、Project 生命周期 provider 与其它消费者仍未绑定，`ready=false`，没有默认允许身份或自动历史 replay。接口和验收边界见 [D06 实施规格](../work-items/d06-transactional-outbox-design.md)。
