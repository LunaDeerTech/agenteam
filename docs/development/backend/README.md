# 后端开发

根 module 为 `github.com/LunaDeerTech/agenteam`，固定 Go 1.27.1。Central 已装配固定 pgx/Goose 数据库库，见[数据库说明](database.md)。Audit 的同事务追加、授权查询/生命周期清理端口与独立签名 cursor 已实现，见[Audit 说明](audit.md)。Secret 的 envelope、引用/lease、加密 receipt 和可恢复主密钥维护见 [Secret 说明](secret.md)。动态策略、受控 HTTP 与 SMTP 拨号端口见[出站说明](outbound.md)。D05 对象库提供受限流式存储、授权 reference/lease、Artifact/下载和 typed Runner transfer，接口与阶段见[对象实施规格](../work-items/d05-object-storage-design.md)。Central 与 Runner 分别装配；Runner 不导入 Central。中立 `internal/platform` 只处理进程日志和关闭协调，不提供授权、业务幂等、数据库事务或 Runner 设备协议。

Central 已在真实数据库、安全初始化、Object/Outbox 注册恢复及 Account/Mail 启动后提供诊断、正式账户 HTTP、System Model 配置 HTTP、Project Owner 元数据读取与更新、Model 配置与安全 chat 目录读取、Project Owner 模型凭据管理、Provider/Model 配置写入与原命令查证，以及 Project Owner Usage 与 Audit 只读 HTTP。默认根已绑定 System Provider/Model CRUD、平台 selector、Model credential 独立写入及 Model 配置和 credential 两类原命令查证，使用真实 Account Session/admin、同一 Secret/Audit/Outbox；接口见 [System Model OpenAPI](../../../api/openapi/model-system.json)，装配边界见[根装配规格](../work-items/recovery-d09-system-model-root.md)。当前 Session/System 授权已接入 Audit、Secret、出站与 Outbox；Object 绑定本人当前头像，SMTP 使用受控出站和真实 Secret lease，Outbox 唯一生产 handler 仍为 `account.mail-enqueue`，没有 Model consumer。对象 Runtime 实际核 store identity、双 origin probe、ProcessGuard 与恢复门禁，见[对象 Runtime 说明](object-runtime.md)；已证实的 [Object runtime join 缺陷](../agent-team/object-runtime-join-regression.md)尚未修复，本次 System Model 根装配不解除该限制。Artifact/通用下载 HTTP、Project 创建与生命周期、Runner/Operation、Model Resolver/Invocation Facts 写入和实际 Provider/MCP 调用仍未绑定。系统管理员统一选择 Meeting Summary 模型的 S1 持久化库、S2 设置 HTTP 与默认根初始化、S3 解析库已实现，默认根 Resolution 与实际 Meeting 消费仍未绑定，详见下文；完整 D09 未完成。Runner 是未连接进程，整体 `ready=false`、`/readyz` 仍为 503。账户接口见 [OpenAPI](../../../api/openapi/account.json)，Artifact 与浏览器下载库边界见 [Artifact 说明](artifact.md)。

System Model 管理读口已实现并通过[独立验收](../agent-team/system-model-management-reads-verification.md)：GET/HEAD `/api/v1/system/model-credentials/{id}` 只返回当前安全 metadata（credential ID、purpose、version）；GET/HEAD `/api/v1/system/models/{id}/deletion-impact` 返回有界精确引用统计、替换要求和已知 adapter 阻断。两类读取各自拥有完整锁 union 的读取事务，在同一 Tx 内重验当前 Session/admin 后读取；最长 3 秒预算包含 HTTP 认证、锁等待和 SQL，并继承更早的调用方取消。仅确认 Committed 且 context 仍有效才返回结果，失败、Unknown 或取消不返回候选数据，也不自动重读。预览不授予删除权限，后续 DeleteModel 仍重验当前引用与替换事实；外域引用 adapter 仍未绑定。精确接口与边界见[管理读口规格](../work-items/recovery-d09-system-model-management-reads.md)。

[Meeting Summary S1](../work-items/d09-system-meeting-summary-selection.md) 已实现迁移 `00020` 下的系统独立 selector、独立 ID/version 和精确引用。`InitializeMeetingSummarySelection` 在旧四用途技术 singleton 已存在后，幂等建立 version=1、model=NULL 的新技术行；`GetMeetingSummarySelection` / `UpdateMeetingSummarySelection` 在事务内重验当前 Session/System 管理员。显式选择只要求 enabled System Provider 下的 enabled chat 模型；命令沿原 `model.selection.update` action 和 `(kind,user,key)` 幂等命名空间，原 lookup 可读取其安全历史回执。未配置时不猜默认模型，也不是 Project 创建的前置条件；本库不创建项目；后继 Meeting 消费系统统一选择，不允许项目覆盖或复制创建时默认值。

删除模型时 Summary 引用必须提供替代；同时承担原 memory 与 Summary 的模型须在同一事务更新两个 owner，各自推进一次版本，替代还须满足 memory 的 `json_schema` 要求。现有 deletion-impact GET/HEAD、DELETE replacement、generic lookup 及既有客户端已兼容第八种闭合引用分组 `platform_selector/meeting_summary`；旧七组与四用途 JSON 保持原义。Summary 设置 HTTP 与默认根初始化见下文；默认根 Resolution 和 D24 真实 initial/update（含首轮标题）生成仍未绑定；旧四用途选择、Execution Summary read model 与既定 compaction snapshot 不变。Object runtime join、OpenAI tools 独立验收和 SPA 并发发布三项停止边界保持，`/readyz` 仍为503。

[Meeting Summary S2](../work-items/d09-system-meeting-summary-settings.md) 已在默认根提供 `GET/HEAD/PUT /api/v1/system/model-selection/meeting-summary`，复用原 Account 边界并在服务事务内重验当前 Session/System 管理员。读取只返回闭合的 `id`、十进制字符串 `version`、显式 nullable `model`；null 仅表示已初始化但未配置，未初始化或坏事实沿原 Fault 拒绝。PUT 严格接受至多 16 KiB 的 `{id,expected_version,model}` JSON，不能清空 model；拒绝 query，GET/HEAD 拒绝实体，保留原 CSRF/Origin 与规范路径检查，不改变旧四用途接口。

Summary 读预算为 **3 秒**、写预算为 **30 秒**，从 `RequireSystem` 前开始并继承更早的 parent deadline，覆盖授权、读体、事务、编码及实际 Write/Flush、Body.Close 和取消回调 join；完成后才重置连接 deadline。HEAD 完成相同查询与编码检查，保留表示头和 Content-Length，不输出 body。PUT 只调用原服务一次并返回原 `model.selection.update` 回执；最终仍 Unknown 时沿 503/`retry_hint=lookup`，原 `POST /api/v1/system/model-commands/lookup` 只查证，不授予重发权限。显式重放须保留原 key/body/expected_version 并使用当前有效的同身份 Session/CSRF；HTTP 不自动重试，也不以重新 GET 的当前值覆盖历史回执。真实 Account/PG/root、原生 HTTP 尾部与公开 client/schema 同响应验证各自保留边界。

[Meeting Summary S3](../work-items/d09-system-meeting-summary-resolution.md) 的 current-resolution 库已支持 initial/update 共用 `platform.meeting_summary`，首轮标题属于 initial 的同次逻辑调用；不猜默认模型，也不允许项目覆盖或复制默认值。Summary 无需 `json_schema`：普通 text 模型沿 text-v1，具有真实 `json_schema` capability 的模型保留 structured-v1，后继 Summary 请求仍为普通 text，其他协议与能力沿原已接受闭集。`SelectModel` 只返回当前 Session/Project Read 下的安全 ID/version，不授予生成权限；Resolve 在完整锁内重验当前 Session、Project 与 Meeting/Operation/Call/input/phase/version 事实，并持有独立 Summary selector Shared 锁和原依赖锁。准备态可重规划；已提交 snapshot/binding 保持不可变，同一逻辑单位重入复用原 snapshot 与 canonical Secret lease，已释放 lease 不复活；后续选择、停用或删除替换不改历史，新调用才消费当前选择。

固定 candidate03 的作者五个新顶层与八个旧回归、独立 A/B 真实测试均通过；A 核当前授权、新 selector 锁与双 owner 删除竞争，B 核历史 snapshot/lease、原子回滚与 Unknown 后态。新 fixture 使用正式账户身份链和严格测试专属 Meeting canonical 行，不能代替 D24 生产事实；Unknown 是真实提交或回滚后的结果装饰，不是物理 COMMIT ACK 丢失证明。原错误码断言失败与两份测试文件的精确修正保留，生产授权未为测试放宽。各轮七个 owned 资源均实际等待并双清，daemon 侧未 wait 的僵尸差量另记，不声称全机清零。该结果只验库：生产 `Authorizations.Resolution` 与 `Invocations` 仍为 nil，不发送 Provider 请求，不创建 Meeting/Invocation/Usage，也不完成 D09/D24 或解除上述三停止与 ready503 边界。

[平台 Embedding current-selection Resolver](../work-items/d09-platform-embedding-resolution.md) 已为 `knowledge_embedding` 与 `memory_embedding` 接入 `platform.embedding`：Knowledge 绑定 Project/Operation，Memory 另需真实 Agent 归属，二者均沿当前四用途 selector 的 Embedding 字段与共享 version，只接受 enabled System Provider/Model 和已验 OpenAI Embeddings float profile。未配置或不支持时明确拒绝，不猜默认模型。原 `direct` 的合法 agent/tool chat 用途（含 `ApprovalAuto`）与 `platform.memory` 的 `json_schema` chat 选择保持；Memory 的 embedding 用途读取 `platform.embedding`，不是原 memory chat 选择。

`SelectModel` 仍只返回当前 Human Session/Project Read 下的安全 ID/version。Resolve 在完整锁计划中重验当前 Session、Project 和受信 consumer 的 subject/operation/input 事实，沿同一 Store、调用者 Tx 完成必要 SQL 与 planned Secret Acquire，使 input 接受、snapshot、binding 和适用 lease 全提交或全回滚；InTx 返回仍属 provisional。prepared 可重新发现计划，已提交的同 Project/model_call 保持原 snapshot、selector version、endpoint 与 canonical lease，新合法 Call 才取当前配置；released lease 不复活，新 Session 重入仍重验当前授权，旧 opaque plan 不跨 Session/实例使用。无更早错误时，已提交 canonical ref 的 Project/AgentID/ExecutionID 不相容先拒 `Forbidden`；其它前置均通过后的合法同单位改义沿 `IdempotencyKeyReused`，不能仅按有无 ref 判断错误码。

`ResolveModel` 仅在实际 Committed 且原 context 仍有效时返回。prepare/final Unknown 沿原 cause/attempt 与 writer 屏障确认，未确认、取消或锁忙均不返回候选，不凭缺行猜回滚或自动重发；取消返回不代表原 writer 已 join。当前固定版本的作者 Selection、Atomicity、Authorization、Replay、Unknown 五个新顶层、原 Resolver 六组与 S3 三组已分七轮实际通过并完整退休；独立 A/B 另以不同构造完成两个实际轮次并退休。这是各固定版本与 overlay 的证据组合，八条技术路径的最终组合独验已获接受，详见[平台 Embedding 验证](../agent-team/platform-embedding-resolution-verification.md)。原首次 Selection 失败、原清理不全及另行退休恢复记录保留。Unknown 证据限于真实 commit/rollback 后的结果装饰及取消边界，不是物理 COMMIT ACK 丢失证明。

该 selector 只解析未来 Knowledge/Memory 新索引的构建目标；已 serving generation 的查询仍须绑定其原 embedding snapshot，本卡未接 `serving_snapshot`。`ExpectedDimensions` 仍需后继受信 IndexProfile/generation 事实提供并冻结，不从型号或响应猜默认维度。测试专属 Knowledge/Memory canonical 行不代替生产事实；生产 consumer/Resolution Facts、`Nonchat.Embed`、凭据材料读取、Provider 发送、Invocation/Usage 写入及 Resolver 的默认根组合仍未绑定。既有三停止、Jina 取证限制及 ready503 保持，完整 D09/D13/D14 未由本库完成。

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

Central 还必须配置 `AGENTEAM_CENTRAL_DATABASE_URL`；TLS 默认 verify-full，显式 CA 文件会在配置检查时读取验证，其余数据库参数及范围见[数据库配置表](database.md#版本与配置)。连接、迁移 guard、全部迁移和首次 Check 共用 `DATABASE_STARTUP_TIMEOUT`，随后在独立且共享的 30s 安全初始化预算内构造真实账户依赖并打开受限 Sink，验证 Account key registry、cursor/Audit、Secret registry/canary/write fence。仅在 Secret 初始化成功且原 ctx 仍有效后，才用同一 ctx/deadline 依次调用同一 Model Service 的 `Initialize`、`InitializeMeetingSummarySelection` 和 Usage Service 的 `Initialize`，三步在 Secret maintenance 及后续业务启动、监听之前完成，不另起预算。Usage 只检查已有表结构，不建表、填默认行或启动 worker；Model 的原 `Initialize` 首次只建立未配置的四用途技术 selector，随后根显式初始化独立的 Summary 技术行。Summary 未配置不阻塞启动；重启保留已有 ID/version/model 和命令历史，不补默认模型。随后验证 DB 出站策略、对象 bucket/双 origin 实际写读删 probe、ProcessGuard 与恢复门禁、Outbox 唯一 handler 注册/恢复，再完成 Account bootstrap/账户及头像恢复、Mail canonical/恢复与技术 Check，最后 HTTP bind。后续步骤不能重置前序消耗的预算；Model 或 Summary 初始化失败或 Unknown、Usage 结构检查失败均不放行监听，SMTP 是否配置或远端可达不作为启动探测。两个阶段都受启动停止信号取消。配置缺失退出 2，连接、版本、迁移、受限日志打开或安全初始化失败退出 1。

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

## Project 初始化收敛授权库

[初始化收敛规格](../work-items/d08-project-initialization-convergence.md)的可选 `InitializationConvergenceAuthority` 由现有 `project.Authority` 实现，不扩展旧 `ProjectAuthority`。`ValidateInitializationConvergenceInTx` 只接受精确原 Creation/Project 的已注册 `ProjectInitialization` Service actor，核对 `CreationID`、`ProjectID` 与原 `InitializationKey`；调用方必须已按完整锁计划持有目标 Project EX，并传入该 Authority 同一 Store 的活 Tx。方法只检查已持锁并沿该 Tx 读取，不补锁、不新开事务；缺锁、foreign/ended Tx 仍沿原 Store 拒绝与 poison 语义。

门禁只观察 active Project：`accepted`、`initializing`、`failed` 必须对应 version=1 的未初始化项目，原请求名称/描述与当前值相同，且无 protected Skill/revision、safe result 或生命周期操作；安全原因分别满足原状态规则。`completed` 必须对应已初始化项目、成对有效的 protected Skill/revision 与合法初始 safe result，原请求字段已清空；当前项目后续合法改名、描述或版本增长不改历史初始快照，也不使该观察失效。双向 Project/Creation、原 owner、初始化 key 与四状态事实必须一致；非 active 状态拒绝，损坏事实不能视为 ready。

此口无写入、不拥有 Commit/Rollback，返回 nil 只在当前 Tx/持锁期间有效，不是 Owner grant、Skills 完成、发布许可、完成回执或 work 已 join 的证明。原 `ValidateInitializationInTx` 成功 gate 与其他写入、生命周期 gate 保持原义；新端口尚无生产 Skills/root 消费，不开放 Project 创建 HTTP，也不解除真实 Skills/初始化 Object 发布及共享 guard 依赖。Meeting Summary 继续由系统管理员统一选择 initial/update（含首轮标题）模型，项目不覆盖或复制默认值；生产 Resolution/Invocations、D24 仍未绑定，Object runtime join、OpenAI tools 独立验收、SPA 并发发布三项停止及 ready503 保持。

## Project Owner 列表与详情只读 HTTP

默认 Central 根已提供 `GET/HEAD /api/v1/projects` 与 `GET/HEAD /api/v1/projects/{id}`，精确查询和字段见 [Project Owner OpenAPI](../../../api/openapi/project-owner.json) 与[实施规格](../work-items/d08-project-owner-read-http.md)。窄 Reader 复用 Usage 的同一 Project Authority、原数据库和 cursor keyring；每次读取在真实事务与完整锁计划内重验当前 Human Session、Owner 和项目状态，管理员没有跨 Owner 豁免。列表支持 lifecycle 过滤和有签名的 keyset cursor，逐页重新授权，完整校验所有行及额外哨兵后才截取页面；deleting 仅有最小列表投影，详情拒绝返回其旧内容。version 使用无损十进制字符串，nullable 与缺席字段严格区分。

两秒总预算从 Account 边界检查和认证前开始，继承更早 parent deadline，覆盖空实体检查、锁与 SQL、完整投影和编码、实际 Write/Flush、Body.Close 及取消 callback join。只有实际 Committed 且 context 仍有效才发布候选；Unknown、回滚、取消或坏行均不返回成功数据，也不自动重读。HEAD 执行相同的完整查询与编码检查，返回准确 Content-Length，成功和错误均无 body。成功表示上限为 5 MiB；100 条各含 8192 B 最大转义描述的合法页面已实际编码为 4,948,120 B 并完整反解，未截断字段。

固定 candidate04 的作者三个新 PG 顶层、六个旧回归按保留原红的版本组合通过，三轮正式 native 验证覆盖 keepalive、慢读体与写入/关闭尾部；独立 A/B 验证当前权限、游标、隐藏坏行、真实 COMMIT ACK 丢失与默认根兼容，真实列表/详情原 body 均通过标准 Draft 2020-12 schema 检查。原测试断言、driver 环境失败及一次误跑全 app 普通包的授权偏差保留；该偏差未作为 native 或根验收证据。正式 native 均有实际等待和关联端口/进程双清，PG 各轮七个 owned 资源实际等待并双清；daemon 侧未由任务等待的僵尸差量另记，不声称全机零残留。

真实账户使用 Bootstrap/Invitation/Redeem/Login；测试项目由正式 Project.Create 配合持久 Skills 测试 fixture 建立，特殊状态 canonical 行仅为测试输入，不表示生产 Skills 初始化或生命周期已经绑定。该只读阶段的默认根只接入读取，保留原 resolve、Usage、Account/System/Summary 路由与启动关闭链；该阶段未提供创建、修改、生命周期命令或 UI。后继元数据更新见下节，创建与生命周期仍未绑定。生产 Resolution/Invocations 和 D24 消费仍未绑定，三项停止边界与 ready503 保持，完整 D08–D28/E01 未完成。

## Project Owner 元数据更新 HTTP

默认根已提供 `PATCH /api/v1/projects/{id}` 与仅接受 `command=update` 的 `POST /api/v1/projects/{id}/commands/lookup`，契约见[更新规格](../work-items/d08-project-owner-update-http.md)与 [Project Owner OpenAPI](../../../api/openapi/project-owner.json)。更新只接受当前 Owner 的 name/description，严格区分字段缺席与显式空 description、拒绝 null，要求原 Idempotency-Key 和十进制字符串 expected_version；管理员没有跨 Owner 豁免。查证仍重验当前 Session/Owner 与 CSRF，不开放 create、生命周期或任意命令枚举。同义重放返回原历史 ProjectRef，不以当前 GET 覆盖；异义 key 仍冲突，not_observed 不授予自动重发权限。

同一 Project Authority 接入真实 Audit、Outbox producer 与 Project 门禁，canonical、receipt、Audit/Event 与 Activity 沿既有同事务锁计划闭合；no-op 保留原 receipt+Touch，不推进配置或新增 Audit/Event。默认根使用正式 Project Service 与原 ProcessGuard，注册三类 Project 事件，不增加 consumer。Stop/Force 后必须实际 Drain 完成才 Joined；创建 initializer 和生命周期端口保持未绑定，原 Summary/Usage 初始化顺序不变。

PATCH 发布/I/O 预算为30秒、lookup为2秒，从认证前覆盖同一次服务调用、完整投影和实际 Write/Flush、同步 Body.Close 及 callback join；业务前无副作用地检查可达 I/O 能力。原 Project 库 Unknown 后的 `WithoutCancel` 至多3秒确认尾部保留，handler 同步拥有并实际等待；它不延长 HTTP 发布时间，也不保证整个 handler 在30秒内返回。过发布期限 abort，不补迟到200或 Problem。正常关停与预算耗尽分别记录，强制 root 返回不能冒充全部内部调用已 join。

固定 candidate06 的作者离线版本组合、三个真实 native 顶层、四个新 PG 顶层与九个旧回归，以及独立 A01/B02 均通过。A01 补验真实缺 User/Project/Outbox 锁、旧计划、已提交 Logout 和原子回滚；B02 验默认根、历史 receipt 与当前 GET 分离及实际 drain，真实 PATCH 8545 B、lookup 8607 B 原 body 均通过 Draft 2020-12 与 FormatChecker 标准解析。原编译失败、I/O 能力预检缺陷及循环 writer 测试首红、独立 B01 私有 SQL 探针首红与 B02 修正均保留；这是版本组合验收。物理 ACK 丢失、原 writer 确认尾部和在途 root 正常/耗尽关停由固定作者测试另行举证；forced root 返回后的私有 proxy/backend 等待不倒填 root 内部 join。新身份使用正式账户链，持久 Skills 与特殊 canonical 仅为测试前置。各轮 owned 实际等待、资源双清，daemon 未 wait 差量单列，不称全机零。创建、生命周期、UI、生产 Resolution/Invocations 与 D24 仍未绑定，三停止、ready503 和完整 D08–D28/E01 未完成边界保持。

## Project Model 配置与安全目录 Owner 只读 HTTP

默认根已提供以下五个 GET/HEAD 资源，精确字段见 [Project Model OpenAPI](../../../api/openapi/project-models.json) 与[实施规格](../work-items/d09-project-model-owner-read-http.md)。Model 的 `Authorizations.Projects` 绑定 Owner read、Update 与 Usage 共用的真实 Project Authority，沿原 Account Store、Session Authority 和 cursor keyring；每次读取在同一事务与完整锁计划内重验当前 Human Session、Owner 和项目 Read 门禁，管理员没有跨 Owner 豁免。

| GET / HEAD 路径 | 读取职责 |
| --- | --- |
| `/api/v1/projects/{project_id}/model-providers` | 本 Project Provider 配置分页，包含禁用项 |
| `/api/v1/projects/{project_id}/model-providers/{provider_id}` | 本 Project Provider 配置详情 |
| `/api/v1/projects/{project_id}/models` | 本 Project Model 配置分页，包含禁用项 |
| `/api/v1/projects/{project_id}/models/{model_id}` | 本 Project Model 配置详情 |
| `/api/v1/projects/{project_id}/available-chat-models` | enabled 本 Project 与 System chat 模型的七字段安全目录 |

Project 详情不返回 System 或其他 Project 的完整配置；安全目录不暴露 Provider 配置、Secret 引用或凭据材料。版本保持无损十进制字符串，nullable 与缺席字段严格区分。三个列表严格校验 query，游标绑定原查询且逐页重验当前权限；沿既有 keyring 签名规则，无新增 TTL。GET/HEAD 拒绝实体，HEAD 完成同样的完整查询、验证与编码，成功 Content-Length 与 GET 表示一致，成功和错误都不返回实体。

一次 2 秒发布/I/O 预算从认证前开始并继承更早 parent deadline，覆盖读取、完整投影、实际 Write/Flush、Body.Close 和取消 callback；业务前检查可达读写 deadline 与 Flush 能力，不提前写头或 Flush。取消与超时不等于实际 join，所有尾部结束后才 reset/返回；过期 abort，不补迟到 200或 Problem。只有原读取实际 Committed 且 context 仍有效才发布；原服务 Unknown/其它 Fault 优先保留，零成功候选，不自动重读。

完整成功表示上限为 8 MiB。在新增 DTO 复制、Validate 与 Marshal 前，先做有界、不会溢出的长度与 JSON 转义预估；无法证明整页或详情可在界内表示时，返回 503 `DEPENDENCY_UNAVAILABLE` / `not_started`，不裁剪数组、字段或行，不输出部分 200。这里的 `not_started` 指 HTTP 表示未开始发布，不表示前序数据库未读取或回滚；8 MiB 也不约束已接受库/DB 的前序分配或整个进程 RSS。服务原 Unknown 不被超限替换，已过 2 秒则沿原 abort 终局。

技术候选的受控纯测、真实 native、作者四新与十旧 PG 顶层及独立 native/A/B 已通过；实际原 body 另经 Draft 2020-12 与 FormatChecker 验证，原编译、测试代理与断言首红和版本组合保留。native 与受控 writer 证据分开，实际 wait、owned 资源/进程/端口双清不等于全机零残留，daemon 未 wait 差量单列。下例分别选择一个 native 和真实 fixture 顶层；其余精确分组见实施规格。使用上文固定 Go/MinIO，schema 解释器须已有固定 `jsonschema` 与 `referencing`；验收仍逐顶层保留 native 外围 45 秒/测试 40 秒、fixture 新 top 120 秒含 Cleanup/包 6 分钟及实际等待、双清。

```sh
AGENTEAM_PROJECT_MODEL_NATIVE=1 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly \
  "$AGENTEAM_GO" test -race -count=1 -timeout=40s \
  -run '^TestProjectModelHTTPNativeSlowBodyAndCallback$' ./internal/central/model
AGENTEAM_PROJECT_MODEL_SCHEMA_PYTHON=/path/to/fixed/python3 \
  AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
  sh scripts/test-objects.sh -run '^TestModelProjectConfigurationHTTPBoundedRepresentation$'
```

本阶段没有新初始化或 worker，保留原 Secret → Model → Summary → Usage 顺序、Update Drain、Account/System/Summary/Usage 路由及关闭链。该只读阶段未提供 Project 配置或凭据 mutation HTTP；后继凭据管理与配置写入见下文，Agent 引用替换与 Model UI 仍未绑定。生产 Resolution/Invocations、实际 Provider 调用与 D24 Meeting 消费仍未绑定，不完成整个 D09。原 Object runtime join、OpenAI tools 独立验收和 SPA 并发发布三项停止边界及 `ready=false` / `/readyz` 503 保持。

## Project Owner 模型凭据 HTTP

默认 Central 根已提供以下六个 method/path，契约见[凭据实施规格](../work-items/d09-project-model-credentials-http.md)与 [Project Model Credential OpenAPI](../../../api/openapi/project-model-credentials.json)。沿原 Account Session、Origin/CSRF 与规范路径边界，只接受当前 Human Owner，管理员没有跨 Owner 豁免；scope 由 Project 路径构造，Purpose 固定为 model。

| 方法与路径 | 职责 |
| --- | --- |
| POST `/api/v1/projects/{project_id}/model-credentials` | 创建凭据 |
| PUT `/api/v1/projects/{project_id}/model-credentials/{credential_id}` | 完整替换材料，沿原 update 命令 |
| DELETE 同 detail 路径 | 删除凭据 |
| GET / HEAD 同 detail 路径 | 当前安全 metadata，HEAD 无 body |
| POST `/api/v1/projects/{project_id}/model-credential-commands/lookup` | 原 create/update/delete 命令的被动查证 |

默认根在原同一 Store、同一 Project Authority 上绑定真实 Secret AuditFacts 与 Secret Project 授权端口，沿真实 Audit producer 和 Project 门禁验证，不使用占位授权。被动 lookup 在同一事务预持 Command/User/Project Shared 锁，重验当前 Session 与 Project Read 后只读安全 receipt；写入及同义历史重放仍经原 ExecuteWrite/Apply 的当前 Mutate 与完整材料校验。archiving/archived 可沿 Read 查历史，但 mutation/replay 仍拒绝；前置查证命中只影响是否需要当前 metadata，最终始终调用一次原 ExecuteWrite，不能直接返回观察结果绕过写授权。原引用/lease 的 ResourceBusy 与原子 Audit 规则保持。

Create/PUT 的 raw JSON 上限为400 KiB，解码材料为1–65536 B；DELETE/lookup 为1 KiB，完整成功响应上限同为1 KiB。严格拒绝未知/重复字段、null、非法 version 与尾随内容，保留原 Idempotency-Key、完整材料及 expected_version；不重新生成 key。Metadata 只含 credential_id、purpose、字符串 version；Mutation 增加 deleted，Observation 明确区分 observed=false/result=null 与原历史安全结果。接口不提供材料读取或凭据列表，响应与普通日志不含材料、密文或材料 hash。GET/HEAD 完成相同查询、验证与完整编码，Content-Length 一致。

三写发布/I/O 预算为30秒，GET/HEAD及显式 lookup 为2秒，从认证前开始并继承更早 parent deadline，覆盖所有顺序调用、编码与实际 Write/Flush。业务前检查可达 I/O 能力；deadline setter、Body.Close 与已启动取消 callback 的失败也必须同步收尾并实际等待，过期 abort，不补迟到成功或 Problem，不承诺整个 handler 必在30秒/2秒返回。Secret 写入没有 Project Update 的自动确认尾部：仍在发布期内的 Unknown 沿原503与 lookup 提示返回，不自动 lookup、追认成功或重试；客户端另行携原 key 和完整原命令字段显式查证。安全查证不替代材料重放校验，也不授予重发权限。

固定 candidate03 的离线版本组合、作者三个 native 顶层，以及六个实际 PG 轮次中最终组合的五个新顶层、十三个规定旧回归和已接受的 Project Model 只读根回归均通过；六轮包含保留的原 new2 失败及其重跑。独立 A02/B01 通过，独立三个 PG 轮次包含原 A01 私有 nested 测试失败；原编译首红、坏 metadata 投影与受控 callback 清理的静审修正、Unknown hook SQL 参数类型相关测试首红均保留。独立 B01 的四份真实安全响应原字节经 Draft 2020-12 与 FormatChecker 验证，未重编码；65536 B 最大合法材料、完整末字节与异义重放已实际验证。

作者 held-writer 证明 HTTP Unknown 时原 backend 仍 pending，放行后分别观察提交、回滚和撤权；独立 B 另证 server COMMIT 与 idle 已完成后丢失 ACK，不混称两种时序。默认根正常及100毫秒强制关闭由作者实际举证，forced root 返回不等于所有内部组件已 join。三轮 native 与作者六轮、独立三轮 PG 均保留实际 wait 和 owned 资源双清，daemon/PID1 未 wait 差量另记，不声称全机清零。测试正式账户链与持久 Skills/特殊 canonical 前置不表示生产 Project 创建或生命周期已绑定。Project 配置写入见下节；UI、生产 Resolution/Invocations、实际 Provider 调用与 D24 仍未绑定；系统管理员统一 Summary 无 Project override，三项停止边界及 ready503 保持，完整 D09 未完成。

## Project Owner Provider/Model 配置写入 HTTP

默认根已组合五个原 GET/HEAD 与以下六项配置写入、原命令查证，契约见[实施规格](../work-items/d09-project-model-configuration-write-http.md)和 [Project Model OpenAPI](../../../api/openapi/project-models.json)。读写复用真实 Model Service、Account 边界和 Project Authority；只扩展 `app/project_models.go` 的方法组合，原初始化顺序与关闭链不变。

| 方法与路径 | 职责 |
| --- | --- |
| POST `/api/v1/projects/{project_id}/model-providers` | 创建本 Project Provider |
| PUT / DELETE `/api/v1/projects/{project_id}/model-providers/{provider_id}` | 更新 / 删除 Provider |
| POST `/api/v1/projects/{project_id}/models` | 创建本 Project chat Model |
| PUT / DELETE `/api/v1/projects/{project_id}/models/{model_id}` | 更新 / 删除 Model |
| POST `/api/v1/projects/{project_id}/model-commands/lookup` | 查证原六种配置命令，返回 `found` / `receipt` |

七个操作的 raw JSON 上限均为1 MiB；严格输入与 Project CredentialRef 的 typed 投影先于任何 Service 调用，失败不以历史查证掩盖。保留原 key、完整命令字段与 expected_version，安全四字段 receipt 及 lookup 完整成功表示不超过1 KiB，不返回完整配置或凭据材料。服务 error 优先；六写 nil error 但坏 receipt 是本卡私有 `DependencyUnavailable` / `Unknown`，不附虚构 cause/attempt、不声称未写入；坏 lookup 则为 `DependencyUnavailable` / `NotStarted`，仅表示本次观察不可发布。

原库在当前 Human Session/Owner Read 后先核历史 receipt，再对首次新写执行 Mutate；管理员没有跨 Owner 豁免。archiving/archived 的同义历史重放与 lookup 可沿 Read 返回原历史，首次新写仍拒绝；撤权也拒绝历史读取。库内 Unknown 私有确认保持原 ctx、原 Command EX 等待与完整 semantic 校验，不增加宽限。公开 lookup 同样使用原 Command EX 与 User/Project、全局 Model 引用 Shared 锁，仅在读取实际 Committed 且 ctx 有效时发布；`found=false` 不证明此前 Unknown 已回滚，也不授予重发。handler 不自动重试、lookup 追认或换 key。

六写发布/I/O 预算为30秒，显式 lookup 为2秒，从认证前开始并继承更早 parent deadline，覆盖 body、库内原确认、编码与实际 Write/Flush。业务前预检 deadline/Flush 能力；Body.Close 与已启动取消 callback 实际结束后才 reset/返回，过期 abort，不补迟到成功或 Problem，不承诺整个 handler 必在预算内返回。原五读的2秒/8 MiB界不变。

固定 candidate06 的离线版本组合、作者三组 native 与最终五新八旧 PG 顶层，以及独立受控、native、A/B 和22份本卡安全原 body schema 均通过。保留 native HEAD 判据首红及九轮作者 PG 中的三次测试 setup 首红，不写成一次全绿；作者 client-close-first、独立 terminal-first、受控 read Unknown 与物理 writer Unknown 分列。全部实际 wait、owned 双清与 daemon 非 owned/未 wait 差量分别保留，forced root 返回不替代内部 join。下例各选一个顶层；使用上文固定 Go/MinIO，schema 解释器需已有固定依赖，仍须保留 native 外围45秒/测试40秒、PG新 top 120秒含 Cleanup/包6分钟及实际等待、双清。

```sh
AGENTEAM_PROJECT_MODEL_CONFIGURATION_NATIVE=1 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly \
  "$AGENTEAM_GO" test -race -count=1 -timeout=40s \
  -run '^TestProjectModelConfigurationNativeBodyDeadline$' ./internal/central/model
AGENTEAM_PROJECT_MODEL_CONFIGURATION_SCHEMA_PYTHON=/path/to/fixed/python3 \
  AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
  sh scripts/test-objects.sh -run '^TestModelProjectConfigurationWriteHTTPCRUDAndHistory$'
```

本卡不绑定 Agent/Project Summary 引用替换 adapter、UI、生产 Resolution/Invocations、实际 Provider 调用或 D24，不完成整个 D09。系统管理员统一 Summary 无 Project override；原三项停止边界、`ready=false` 与 `/readyz` 503 保持。

## Project Owner Audit 只读 HTTP

默认根已提供 `GET/HEAD /api/v1/projects/{project_id}/audit` 与 `GET/HEAD /api/v1/projects/{project_id}/audit/{audit_id}`，见 [Audit 说明](audit.md#project-owner-audit-http)、[实施规格](../work-items/d04-project-owner-audit-http.md)和 [Project Audit OpenAPI](../../../api/openapi/project-audit.json)。公开 `ListProject/GetProject` 在同一实际事务中按 User SH→Project SH 重验当前 Human Session、Owner 与 Read gate；管理员没有跨 Owner 豁免，归档可读，initializing/deleting 拒绝。列表默认50、最多200，完整验证全行及额外哨兵，沿原 scope/filter/order 签名 cursor；返回闭合安全 metadata、关联 ID 和固定 summary，不返回业务正文或认证材料。

局部3秒预算从预认证前开始并继承更早 parent，包含实际读写/Flush、Body.Close、事务与取消回调收尾；尾部实际等待，不承诺全 handler 三秒已返回。HEAD 完成同一授权/查询/编码，仅省略实体并保留表示长度。仅实际 Committed、callback 完整且 ctx 有效才发布；Unknown/取消/损坏行均零候选，原 State/Cause/Attempt 内部保留。没有 command/receipt、确认端点或自动重读，也不新增 initializer/worker；旧 System Audit、Project/Model/凭据/配置/Usage 路由保留。

作者四新八旧 PG top、三 native 轮和独立 A02/B01 真实代表通过，原作者 action 预期首红及独立私有注入首红均保留，修正仅限测试。独立 A01 实际只记录 DATABASE_SQL_FAILED、未进入待测 List；既有 Project typed CHECK 不接受该形状是固定 schema 的静态归因，原轮没有 SQLSTATE 或约束名。31种 typed 动作极值及有限可选分支的200行代表页实编码、341标准 schema 例及作者/独立真实原字节各自验证，不声称穷尽序列化组合；不把合成 cursor 容量输入当真实签名，也不把受控 writer 当 native。每轮资源实际等待并双清，daemon/PID1 未 wait 限制和强制 root 返回不证明全部 inner join 的边界保留。该结果不绑定生产 Resolution/Invocations/D24、Project 创建/生命周期或 UI，不解除 ready503、Object runtime join/OpenAI tools 独审/SPA 并发发布三停止，也不表示完整 D04 或 D09 已完成。

## Project Owner Usage 只读 HTTP

默认 Central 根已装配三个 GET/HEAD 资源，接口与闭合字段见 [Project Usage OpenAPI](../../../api/openapi/project-usage.json) 和[实施规格](../work-items/d09-project-usage-read-http.md)。沿同一 Account Store/Authority 构造 Project Authority（Sessions、Routes 均为原 Account Authority），再构造引用同一 Project Authority 的 Usage Authority/Service，并复用已加载的 cursor keyring。handler 持有这些同实例及原 Account Service 的 HTTPBoundary；读取在实际事务内重验当前 Human Session、Project Owner 与项目状态，系统管理员没有跨 Owner 读取豁免。

| GET / HEAD 路径 | 读取职责 |
| --- | --- |
| `/api/v1/projects/resolve?username=…&project_name=…` | 按当前名称解析正式 ProjectRef 与稳定 ProjectID |
| `/api/v1/projects/{id}/model-usage` | 读取当前 Owner 项目的分页 Invocation 安全投影 |
| `/api/v1/projects/{id}/model-usage/summary?group_by=…` | 沿原聚合读取 consumer、agent、model、provider、execution、meeting、purpose 或 day 分组 |

resolve 不授予后续 Usage 读取权限；稳定 ID 请求每次自行授权。旧名称返回404，不提供 alias 或重定向；名称复用可指向另一 ProjectID，同一项目改名不使其稳定 ID 游标失效。查询严格拒绝未知或重复键，GET/HEAD 不接受实体。计数、token 和版本使用无损十进制 JSON 字符串，保留 null 与已知零的区别，不暴露内部运行身份、原始错误或凭据。

两秒总预算从 HTTP 认证前开始，并继承更早的 parent deadline；真实读写 deadline、Body.Close、Flush 和取消 callback 均在返回前实际完成或 join。只有正常 Committed 且 context 仍有效才发布完整有界投影；Unknown、回滚或取消保留原安全 Fault/Problem 状态并丢弃候选。HEAD 完成同样的认证、查询和完整编码检查，返回与 GET 表示一致的头和 Content-Length，成功与错误均无 body。

生产 `Invocations` 仍为真正的 nil；本次只读取经正式契约持久化的 Project/Usage，不绑定 Runtime Facts、发送/重试 consumer、Project 初始化或生命周期、生产 Skills，也不为新部署制造项目或账本数据。默认根沿原 startup context 顺序完成 Secret → Model → Summary → Usage 结构检查。原 Account/System/Outbound/Audit 路由及关闭链保持，`/readyz` 仍返回503 Problem，`/diagnostics` 的 `ready` 仍为 false；完整 D08/D09、Usage UI 与 Meeting Summary 实际生成消费均不由 Usage 读口完成。

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

## D01 资源身份纯契约

[R1](../work-items/d01-resource-identities.md)在 `identity/contract` 提供唯一 `ToolID`、`MountID`、`ProjectVariableID`，复用 Foundation UUIDv7；普通与 Secret 变量共享变量身份，凭据身份仍独立。准确四依赖包 race/vet 与独立外部消费 probe 全部实际通过，可用 `.agent-state/resource-identity-recovery/run.sh` 重建验证。该结果没有 Registry、Mount/Variables 目录、授权、初始化或引用写入，完整 Agent F1 仍待真实前置。

## D10 Agent 核心契约

`internal/central/agent/contract` 已实现 [Agent 配置工作项](../work-items/d10-agent-configuration.md)的 C1：17 字段 `AgentCore`、三字段 `AgentRef`、审批策略和配置删除门禁枚举，以及 `WorkReferences` 接口声明。复用现有 Identity、Model 和 Foundation 类型，提供严格 JSON、完整输入大小限制、Unicode 校验、Clone 与直接 fmt/slog 安全投影。`AgentCore` 尚不含 Tool、Mount、SecretVariable 引用，不是完整 AgentConfig；合法 DTO 不证明 Agent 存在、已初始化、可指派或空闲。

六个纯契约文件已通过作者及独立验证，准确依赖的 pure、race、vet 均通过。当前没有 Agent 服务、数据库迁移、PG 验收或生产绑定；`WorkReferences` 只约定同 Store 活 caller Tx、完整锁与当前 Owner 授权责任，没有默认成功实现。F1 真实配置和当前事实能力仍等待 Model 引用校验与替换、默认 Skills、资源目录及引用保护；三类 ID 的低层位置已由 R1 闭合；真实目录 DTO 与窄适配端口仍须由所属模块冻结，不能复制 marker 或向上依赖 Tool contract 来绕过。

## D11 Milestone / Sprint 结构库

`internal/central/work` 已提供当前 Human Owner 的 Milestone / Sprint 创建、元数据更新与手工重排，六个命令为 `CreateMilestone`、`UpdateMilestone`、`ReorderMilestone` 及对应的三个 Sprint 命令。Reader 提供两个对象各自的 Get/List、每页当前授权与稳定 cursor；`ReadPlacementInTx` 在调用者真实 Tx 内返回同 Project 的 Sprint 及其真实 Milestone。迁移 `00021` 建立五张结构、排序组与命令表。placement 只证明结构关系，不证明 Task membership、占用或写权限；类型、分页与使用前提见 [D11 结构库规格](../work-items/d11-work-structure.md)。

组合方通过 `work.NewAuthority`、`work.NewReader`、`work.New` 注入真实 Project Authority、同一 Store，以及命令必需的 Outbox Appender、typed WorkEvents 和 Account Activity 端口；缺少必要绑定（含 typed nil）明确拒绝。`work.milestone_changed` 与 `work.sprint_changed` 须在 Outbox catalog Seal 前注册，Work producer 核持久 planned/committed 事实，Project gate 另核精确事件闭集与当前授权。完整 Actor/Project、Command、排序组与事件锁 union 按既有顺序一次取得；InTx 只验证同一 live Tx 已持的完整锁，不补锁、升级或另开事务。

命令准备阶段只保存不可变 planned 输入与拟事件，事务外 PrepareAppend 后，最终 Tx 重验当前权限、原语义、expected version、父映射和计划修订，再原子提交 canonical 结构、业务版本/时刻、排序组 generation、Outbox、成功 receipt 与 Activity。内部重规划至多三轮，不改原 key、target、expected version 或正文。真实 no-op 只保存原命令回执并 Touch Activity，不改 canonical/rank/generation 或发事件；历史 replay 不再 Touch。纯物理 rebalance 保留相对顺序与所有 sibling 的业务字段，不提供独立公开命令或事件。

`LookupCommand` 与原 writer 共用 Command EX，重验当前 Session/Owner 后只返回原 committed 回执、`in_progress` 或 `not_observed`；历史回执不是最新读取，后两种状态也不授权自动重发。COMMIT Unknown 仅做一次有界只读确认，未确认时保留原 Attempt/Cause；planned、取消、超时或读取失败不能冒称回滚，继续原命令必须由调用方显式保留原请求执行。归档允许读取与已完成 replay，拒绝 planned final 和新写。Lookup 仅在本次事务已明确 NotCommitted 且调用 context 已取消时返回零结果与可 `errors.Is` 的取消错误，不改写 Committed/Unknown。`Service.Stop` 取消本实例已登记调用，`Drain` 等待实际返回；它不关闭共享 Store/Outbox，也不代替组合方等待独立 Reader。

17 条技术路径的固定版本组合已获接受：作者六个新 PG 顶层、五个旧 Project/Outbox 回归及独立 A/B 均实际通过。前五个新轮保留原版本；原 Unknown 轮的 Lookup 取消断言失败经 U1 窄修后重跑通过，旧五轮与独立 A/B 消费修后版本，不能称当前源码的一次全套运行。Unknown 使用真实 PG COMMIT frame 代理与独立连接等候，测试专属 Session、归档事实和 Skill receipt 不代替生产登录、Archive 或 Skill 链。原失败保留；独立 B 的监督器内 driver/helper/Go 实际 Wait 与资源退休齐全，但环境恢复后外部工具 session 的 terminal/exit 未取得，另有有限只读清零补证，不回填该工具终态。详见 [D11 结构库验证](../agent-team/d11-work-structure-verification.md)。

Structure 本身不提供 Task canonical/membership、删除、Sprint start/complete/rollover、跨 Milestone 移动或 Project current_sprint pointer 写入；Task 规划库另见下文。Execution/Dispatch 占用 adapter 与 Work 生命周期清理仍待后继真实绑定，不能以空集合或 no-op 替代。本次结构库交付不包含 Tool、HTTP/UI 或 App/生产 root；后继 HTTP/root 接入见下文，不改变 ready503。该结果不完成整个 D11 或平台；Object runtime join、OpenAI tools 独立验收、SPA 并发发布三项停止及 Jina/Image 来源阻塞保持。

## D11 Human Task 规划库

[Task Planning 工作项](../work-items/d11-task-planning.md)的规划库已实现并通过本卡验收。`work.NewTask` 提供 Human Owner 的 `CreateTask`、`UpdateTask`、`ReorderTask` 与 `LookupTaskCommand`；仅允许未指派 backlog 的新写。`work.NewTaskReader` 提供当前授权的 Get/List 和 caller 同 Tx 的真实 `HasTasksInSprintInTx`。构造要求同一 Store、同一 Work Authority 及 Structure Reader，必要 Outbox、TaskEvents 与 Activity 全部绑定。

迁移 `00022` 新增 Task、Task 排序组、Project 查询代数、Task 命令及 TaskEvent 五表，并通过同项目复合 Sprint 外键保持 parent 一致。创建时由实际 Sprint placement 推导 Milestone；七种 canonical state 与已有 assignee 可读取，但状态转换、指派与 Agent 合法性不属于本卡。List 使用固定 Sprint/state/priority/rank/ID 总序和七个单值过滤，assignee 的省略、null 和 AgentID 分别表示不过滤、未指派和精确持久值；文本按区分大小写的字面 substring 匹配各字段。

Task 命令采用独立的持久两阶段计划，在同一最终事务提交 canonical、必要 rank 维护、组和 Project 查询代数、append-only TaskEvent、`work.task_changed`、原回执及 Account Activity。新事件须在 catalog Seal 前通过 `RegisterTaskEvents` 注册，并由原 Work producer 和 Project 精确事件门禁复核真实计划与历史。no-op 不产生历史或事件，历史 replay 不再 Touch；Lookup 和 Unknown 保留原 command identity、writer Attempt/Cause 与真实提交边界。`TaskService.Stop/Drain` 等待本实例真实调用退出，组合方仍须等待独立 Reader、Store 和协议代理。

作者 pure、race、vet、integration 编译及精确发现、Central/Runner 构建通过；20个技术路径全文独审及最后测试差量复核通过。七个新 Task PG top、八个指定旧 Project/Outbox/Structure 回归和独立不同构造 A/B 均实际通过。旧 Structure Migration 保持显式 `00021` 历史前缀，其余必要回归使用最新 `00022`。这些是按相关输入未变复用的有效版本组合，不声称当前 HEAD 一次全套执行，也不把编译或测试准备当业务通过。

每轮使用 `.agent-state/task-planning-recovery/` 中可恢复的两 ID PG-only driver、监督器与独立 probe，Go/driver/外层工具实际 Wait 完成，精确两 ID、owned runtime 和 host TCP delta 各两次为空；没有启动包含停止项的整套脚本。私有嵌套解码原缺陷、测试夹具及独立编排原失败均保留，修后范围和归档时间诊断的限制见工作项。直接 DTO 安全日志投影与业务 JSON 的边界沿该卡 §2，不能把任意嵌套 JSON 当日志净化。

Task 删除、完整 Timeline/context、Sprint lifecycle、reviewer、状态转换与指派、Execution/Dispatch、Work 清理与Tool/UI仍未提供；后继限定Blocker服务及HTTP/root范围见下文。测试专用的未来 state/assignee、归档、Session 与 Skill receipt 事实不代替真实 Agent、生命周期、登录或 Skills 生产链；本结果不完成 D11 或平台，也不改变 ready503 和既有停止项。

## D11 Task 纯状态核心

[流转工作项](../work-items/d11-task-transitions.md)的 T0a 与 T0b 纯契约已实现并独立验收。`contract.CheckTaskTransitionRule` 检查输入、49状态对及六角色规则；`TaskTransitionPosition` 提供独立严格六字段codec、8KiB边界、邻居校验和Clone。旧backlog-only Position与规划命令闭集保持原行为。作者/root race/vet与独立4顶层9子测试实际通过；本结果只提供纯契约，真实权限、当前Agent/Blocker、执行占用、流转事务和生产装配仍待实现。

## D11 Blocker 两类纯契约

[B0-C工作项](../work-items/d11-task-blocker-contracts.md)已实现并独立验收唯一 `TaskBlockerID`、`rely_on` 与无引用 `waiting_for_human` typed metadata、Create及小历史payload。严格codec校验三层原始大小和闭集、Clone与直接安全日志；五枚举识别中其余三类完整对象返回未绑定。作者pure/race/vet、独立公开API pure/race以及隔离候选Work race/vet/两入口build实际通过。该纯契约交付保持旧TaskEvent与规划schema闭集，后继B0-P的真实图/归属/权限/持久范围见下文，完整Transfer仍未提供。

T0b补齐Transfer/Lookup与新命令摘要、Human typed history、16KiB严格封套及纯多事实数据工厂；作者整包pure/race/vet与独立公开API各6顶层34子测试通过。仅消费B0-C两类metadata；T0b流转本身仍未绑定当前授权、Blocker图、持久提交与生产producer，详见流转工作项的实施结果。


## D11 backlog Blocker 持久服务

[B0-P 工作项](../work-items/d11-task-blocker-service.md)已正式交付：`work.NewBlocker` 为当前 Human Owner 的未指派 backlog Task 提供两类 Blocker 的 add、resolve、list 与原意图 Lookup。`rely_on` 检查同 Project 的真实任务依赖图，`waiting_for_human` 不带任务引用；迁移 `00023` 持久保存 Blocker、不可变命令计划和回执。变更与 Task.version、历史、`work.task_blocker_changed`、Outbox 和 Account Activity 在同一最终事务提交；真实权限、并发、Unknown、Planning/Structure 互操作与回滚验收已闭合，原失败及限定组合接受范围见工作项。

该服务不提供页面、Agent 自动阻塞/恢复、Task 状态推进或完整 Work 清理。内部全量历史读取不能直接用作公开 HTTP 列表；后继分页接缝仅扫描当前页，并用同一事务中的 Task.version 校验签名 cursor。

## D11 Work Owner HTTP 与默认根接入

[Work Owner HTTP/root 工作项](../work-items/d11-work-owner-http.md)正在实施验收，公开请求和响应见 [Work Planning OpenAPI](../../../api/openapi/work-planning.json)。已可构建的默认根接线将当前真实 Session/CSRF 与同 Store 的三套 Work 服务、三套 Reader、Account Activity 及精确 Outbox producer 组合起来。面向已有 initialized Project 的当前 Owner，接口覆盖 Milestone/Sprint 创建、更新、排序与读取，未指派 backlog Task 的规划，以及两类 Blocker 的添加、查询和解除；管理员没有跨 Owner 旁路。

命令调用者须在首次发送前保存 ID、原 `Idempotency-Key`、正文及适用的 `expected_version`；断连后以该原意图调用对应 Lookup，不从当前对象重建历史请求，也不自动重发。列表使用有界摘要分页，Blocker 默认仅返回 unresolved；整份请求上限 1 MiB，列表响应上限 5 MiB。读取和 Lookup 的总预算为 2 秒，变更为 30 秒，均包含认证和实际 I/O，继承更早的调用方期限。退出时三套命令服务先停止接收，再等实际调用返回；HTTP Reader、Account、Outbox 与数据库/对象 guard 继续沿根的真实退出链处理。

目前分页的作者和独立真实验收、HTTP/schema/根接线的限定纯检查，以及自然期限、连接/EOF 与故障/持有三组 native 验收已通过（实际 GET/Lookup/PATCH，HEAD仅pure/schema）；真实HTTP权限组亦已完整通过；事务恢复、独立HTTP及默认根动态验收仍待完成，不标为已交付。Project 创建 HTTP、生产 Skills 初始化及完整生命周期仍是独立前置；测试专用 Skills receipt 不证明新账号到 Project 创建的生产链已就绪。本范围不提供 UI、Agent 服务、状态转换、生产部署或整个平台 ready，也不解除既有停止项。
