# 后端开发

根 module 为 `github.com/LunaDeerTech/agenteam`，固定 Go 1.27.1。Central 已装配固定 pgx/Goose 数据库库，见[数据库说明](database.md)。Audit 的同事务追加、授权查询/生命周期清理端口与独立签名 cursor 已实现，见[Audit 说明](audit.md)。Secret 的 envelope、引用/lease、加密 receipt 和可恢复主密钥维护见 [Secret 说明](secret.md)。动态策略、受控 HTTP 与 SMTP 拨号端口见[出站说明](outbound.md)。D05 B01 对象库提供受限流式存储、授权 reference/lease 与可恢复清理，接口与阶段见[对象实施规格](../work-items/d05-object-storage-design.md)。Central 与 Runner 分别装配；Runner 不导入 Central。中立 `internal/platform` 只处理进程日志和关闭协调，不提供授权、业务幂等、数据库事务或 Runner 设备协议。

当前 Central 在真实数据库连接、迁移、首次读写检查及 Audit/cursor/Secret/出站策略安全初始化后提供诊断，Runner 是未连接进程。配置正确、程序存活与完整产品 ready 是不同状态。对象库尚未装配到 Central；Artifact、浏览器下载和 Runner transfer 留在 D05 B02/B03。身份、对象领域授权、Audit/Secret/出站管理授权、实际出站消费者和 Runner 协议仍未绑定；后续责任见 [D01 契约目录](../work-items/d01-contracts/README.md) 和 [D04 规格](../work-items/d04-security-foundation.md)。

## 构建与验证

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
# 修改相关场景后可以只跑对应测试；最终验收使用不带 filter 的完整命令。
sh scripts/test-postgres.sh -run '^TestSecret'
sh scripts/test-postgres.sh -run '^(TestRealSecret|TestCentralSecret)'
# 真实私网 socket、生成 CA、DNS/策略/HTTP/SMTP 端口和整组数据库/进程验证。
sh scripts/test-security.sh
# 真实 PG + TLS MinIO + 出站/进程套件；需按固定研究构建并提供 MinIO binary。
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio sh scripts/test-objects.sh
# 对象定向验收；这不会把未命中的 D04 测试视为回归通过。
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio sh scripts/test-objects.sh -run '^TestObject'
```

`tests/process` 在临时目录构建真实 cmd，普通测试检查纯 CLI、配置拒绝、Runner SIGINT/SIGTERM 和依赖方向；Linux 下检查未连接 Runner 没有 socket descriptor。实际 Central 成功启动、迁移失败、监听冲突、启动信号、数据库故障/恢复与健康超时放在 integration suite。Central app 普通测试覆盖装配顺序和时钟边界；integration 中的测试进程调用真实 Store/Migrator，通过真实 HTTP+Tx、Secret worker 与受控出站验证正常 drain、阻塞查询、第二信号和不合作 callback 的有限退出。测试专属 route/barrier 不进入生产入口。并发顺序用 channel、数据库锁和观测事实协调；测试不读取外部 `.env`、凭据或已有服务，本机监听用 loopback port 0，出站成功路径使用 owned internal Docker 私网与精确规则。

前端依赖和检查仍独立；本次 Go 工程变动无需无条件执行前端全量检查。

## 配置与命令

仅读取进程环境，不自动加载 `.env`。参考 [Central 示例](../../../deploy/central.env.example) 和 [Runner 示例](../../../deploy/runner.env.example)。存在但为空的变量无效；当前进程前缀中的未知变量拒绝，另一进程前缀及一般系统环境忽略。Central 另外拒绝非空隐式 PG 配置来源。错误只报告固定字段名与原因，不打印配置值。

| 变量 | 默认值 | 约束 |
| --- | --- | --- |
| `AGENTEAM_CENTRAL_LOG_LEVEL` / `AGENTEAM_RUNNER_LOG_LEVEL` | `info` | 精确 `debug/info/warn/error` |
| `AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT` / `AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT` | `10s` | Go duration，100ms–5m；第一次停止后的总预算 |
| `AGENTEAM_CENTRAL_HTTP_ADDR` | `127.0.0.1:8080` | host:port，1–65535；仅显式 loopback IP 允许 port 0 |
| `AGENTEAM_CENTRAL_CURSOR_KEYRING` | 无，必填 | format=1、current_kid 与 1–32 把独立随机 32-byte key；严格带 padding base64，JSON ≤16 KiB，见 [Audit 配置](audit.md#cursor-与部署配置) |
| `AGENTEAM_CENTRAL_SECRET_KEYRING` | 无，必填 | format=1、规范正 int64 current_version 与 1–32 把独立随机 32-byte AES key；材料不得与 cursor 重复，见 [Secret 配置](secret.md#部署配置与启动) |
| `AGENTEAM_CENTRAL_OUTBOUND_CA_FILE` | 无，可选 | 追加到系统 trust roots 的部署 PEM CA，纯配置验证即读取，最多 1 MiB；与数据库 CA 独立，见[出站说明](outbound.md) |
| `AGENTEAM_CENTRAL_PUBLIC_ORIGIN` | `http://localhost:8080` | 单一 http/https origin；无 userinfo/query/fragment，路径仅空或 `/`；规范化主机、IP、默认端口和尾 `/` |

Central 还必须配置 `AGENTEAM_CENTRAL_DATABASE_URL`；TLS 默认 verify-full，显式 CA 文件会在配置检查时读取验证，其余数据库参数及范围见[数据库配置表](database.md#版本与配置)。连接、迁移 guard、全部迁移和首次 Check 共用 `DATABASE_STARTUP_TIMEOUT`，随后在独立 30s 安全初始化预算内验证 cursor/Audit 存储及 Secret registry/canary/write fence、启动维护 worker、读取 DB 出站策略镜像，再 HTTP bind。两个阶段都受启动停止信号取消。配置缺失退出 2，连接、版本、迁移、读写或安全初始化失败退出 1。

两个二进制接受 `--help`、`--version`、`--check-config`，无参数启动进程。未知参数和多余位置参数返回 2，不回显输入。help/version 不加载配置或启动服务；Central check-config 只验证当前 D04 参数（包括两个必需且独立的 keyring 和显式 CA）、不连接，输出 `scope=d04, valid=true, ready=false`；Runner 保持 `scope=d02`，另有 `connected=false, authenticated=false`。check-config 不证明 DB canary/出站策略已初始化。出站策略由 DB 管理，不接受环境规则绕过；对象及 RunnerLocalConfig 参数仍未实现。

Central 另接受成对的 `--repair-migration <version> --expected-checksum <sha256:...>`，只使用已编译迁移和精确指纹，不接受 SQL/文件路径。当前正式迁移均为 tx，修复明确失败为 `MIGRATION_REPAIR_UNSUPPORTED`；不得将 CLI 存在理解为任意版本都可强制修复。使用规则见[迁移与修复](database.md#迁移与修复)。

```sh
./bin/agenteam --check-config
./bin/agenteam-runner --check-config
AGENTEAM_CENTRAL_HTTP_ADDR=127.0.0.1:8080 ./bin/agenteam
./bin/agenteam-runner
```

上述 Central 命令要求事先设置本部署的数据库环境和独立的 cursor/Secret keyring；示例文件中的占位口令必须替换。纯 help/version 和 Runner 不需要 Central 数据库或 keyring 环境。

## 诊断与日志

| 路径 | GET / HEAD | 含义 |
| --- | --- | --- |
| `/livez` | 200 | `status=alive`，仅 HTTP loop 存活 |
| `/readyz` | 503 Problem | 数据库、Secret 或出站基础异常为 `DEPENDENCY_UNAVAILABLE`，已实现组件健康但业务未绑定时为 `DEPENDENCY_UNBOUND` |
| `/diagnostics` | 200 | `ready=false`；数据库来自真实采样；cursor/Audit 存储、Secret 加密存储/维护及出站策略镜像报告各自状态；仅含版本/轮换计数，不含策略网段；audit_authorization/secret_authorization/outbound_authorization/identity 等仍 unbound |

数据库采样由单个 worker 每 10s 执行，单次 2s 超时且不重叠。样本超过 20s 或检查失败立即不再显示旧健康；重新验证成功后恢复数据库子状态。HTTP 仅读快照，健康故障不会使 `/livez` 失败。成功样本只含安全时间和版本等技术字段，健康写探针在事务中读回后 rollback，不留下历史记录。

只允许 GET/HEAD，HEAD 无 body；其他方法 405 且包含 Allow。未知 API、页面、静态资源均 404，不回退 HTML 或成功空结果。没有 Session 或业务写路由。HTTP 保留 B01 的服务端 request ID、安全 Problem、严格 JSON 和流式 writer 能力。

日志用 `slog.JSONHandler` 写 stderr；stdout 仅输出 CLI 结果。正常日志包含 UTC 时间、level、service、event、随机进程 run_id。HTTP 另有 request_id、method、声明的 route、status、duration、bytes；未匹配路由用 `unknown_route`。数据库日志仅增加白名单阶段/错误码、五位 SQLSTATE 和迁移版本；安全日志仅输出 cursor_initializing/audit_initializing/secret_initializing/secret_maintenance_starting/secret_unavailable/outbound_initializing/initialized/failed 固定阶段。不记录原始错误、panic/堆栈、SQL/参数、DSN、证书路径、配置、body、query、Authorization、Cookie 或其他任意 header。原始 net/http 错误文本只投影为固定 `HTTP_SERVER_ERROR`。启动在实际 bind 后记录监听地址，Runner 明确 `unconnected`，不尝试连接、注册、认证或监听。

## 停止与退出

入口在启动前注册 SIGINT/SIGTERM。第一次信号或调用方取消只进入一次 stopping，第二次信号强制关闭；重复程序化 Stop 不重置预算。Central 的 serving context 独立于停止请求，首次停止不取消正在执行的 handler；Shutdown 使用独立 deadline。

仍交给 handler 的迟到请求返回 503 `SHUTTING_DOWN`。net/http Shutdown 接管监听或连接后，可直接拒绝连接或返回 EOF，不保证每个尚未分发的请求都经过 Problem handler。首信号停止健康领取、新 Secret batch 与新出站请求，关闭出站 idle；在途 handler、当前维护 batch 与出站请求继续使用原预算。三者均 drain 后才 Store.StopAdmission，已有 Tx/Rows 按第一次停止的同一剩余 deadline 排空并关闭池。

超时或第二信号强制取消 owned DB 操作、发有界 CancelRequest，并同时 force-close owned 出站，再关闭 HTTP；数据库、HTTP、出站关闭和各 worker join 共用最多额外 1s，不逐阶段重置。启动中的信号也取消连接/迁移/Secret/出站策略初始化，第二信号受相同外层总预算约束；晚返回的已取得资源仍清理。日志区分 drained/forced；强制退出不证明事务、副作用已回滚或业务已停止。

| 退出码 | 含义 |
| --- | --- |
| 0 | help/version/check-config 成功，或进程干净停止 |
| 2 | 参数或配置拒绝 |
| 1 | 初始化、监听、意外 Serve 错误、drain 超时或第二信号强关 |

Runner 当前没有 RPC、子进程或长连接；第一次信号停止真实未连接进程即可。D15–D17 后续绑定其 Managed Process 和通道关闭顺序。HTTP hijack/WebSocket 不由 Server.Shutdown 自动等待；当前生产没有该连接，后续连接 owner 必须登记自身停止与关闭责任，不将当前 HTTP 测试当作未来长连接或持久恢复验收。

## D05 B01 对象库

`internal/central/object` 当前是组合根可使用的库；Central CLI/诊断仍遵循上文 D04 行为，尚不要求 MinIO 参数，也未注册对象业务 HTTP。`object/contract` 的 AccessPlanner、ResourceAuthority、精确 ObjectReadAuthority、ProjectGate、CleanupAuthority、LeaseAuthority、ProcessAuthority 均是正式端口，未绑定时拒绝依赖该端口的操作。只有领域确认的 existing owner 会在上传发布 Tx 获得 canonical reference；prospective owner 收到的 receipt 只证明存储成功，不能作为普通读取权限。Avatar 属 System 分区且普通权限限 exact User；维护取消经固定 ObjectMaintenance 与持久 cleanup cause 单独核验，不创建 Avatar 读取 grant。

每个数据库阶段先在 Tx 外调用 `DiscoverAccess`，取得绑定完整请求、真实 Actor/owner 父映射及模式的不可变计划。组合方把全部对象/source/lease 计划与自己的 `extraLocks` 交给 `AcquireAccessPlansInTx`，一次合并取最强模式的锁；各 InTx 显式接收对应 plan 和同 live Tx 的 token。`ValidateAccessPlanInTx` 在当前 Tx 重读映射，随后才检查当前权限；依赖变化返回 `RESOURCE_BUSY`，调用方必须回滚并重新规划，不补锁、不升级、不自动重试。ExecutionPayload 的 ID 是 PayloadID，Execution gate 来自真实映射；SkillRevision、MeetingFile 同样不能用 owner.ID 猜父实体。计划不是权限，也不参与业务语义摘要。

低层组合按 PreparePayload → ReserveUploadInTx → 确认外层提交 → UploadPrepared → PublishVerifiedInTx 工作。InTx 不开另一个 Tx，也不执行 S3 I/O；业务调用方须管理外层提交与 unknown 查询。PutObject 包装此流程。每次 raw-body 重放仍以 64 KiB buffer 完整计量并计算 SHA；不依赖 seekable reader 前缀、ETag 或 caller 声称的 checksum。上传前确认 pending/attempt/lease 提交，每个随机候选 key 仅一次 SDK 条件 PUT，完整读回后才能 verified/发布，成功重放保留原 ObjectID、key 和 Audit。COMMIT unknown 核实同样比较本次完整语义摘要，包括正文、MIME 与 expected version；另一个请求抢先提交同 key 时不冒用其成功结果。取消不抹去原命令已提交事实，也不返回可再次消费的 receipt。

单对象为 0–1 GiB；spool 同时两个准备/上传、总预留 2 GiB，并留 128 MiB 磁盘余量。目录 0700、文件 0600、进程独占锁与持久 manifest；恢复只能清理已证明 exact ProcessID 终止且 DB attempt 已不需要的本域文件。不根据时间、心跳或网络失败猜测进程死亡。全局 64、每命令两个未收敛 attempt；全局准入有共同 DB 锁，不能靠各 object 锁分别计数。首次 JSON 写入前先持久化只含随机 PayloadID/ProcessID 的零字节 init claim；JSON 和目录确认后改为 owner claim，再创建正文。启动仅把匹配 init claim 的空文件/严格初始序列化前缀视为不可读残留，损坏 JSON、矛盾身份和无归属文件仍拒绝；正文/manifest 删除后才删除 claim。遗留清除仍需 exact-process death 与 DB 不再需要的证据，不能从缺正文猜测原业务失败。

StatObject 只读已授权 metadata，不探测 payload；ReadObject 先确认内部 reader lease，再完成真实 GET 的状态/长度/首段检查。全文读保留 64 KiB 末段，实际 EOF、长度及 SHA 全部通过才交付最后一段；错误时调用方必须中断尚未完整的输出流。Range 只证明区间长度和边界，不宣称已重算全文 SHA。Reader 必须 Close，直到源 I/O 真正关闭/join 后才释放 lease；提交 unknown 保留精确 lease checkpoint。历史/执行/transfer lease 只保护用途，普通授权不能从任意 active lease 推导。

清理先持久 gate，阻止新发布/绑定/读取，再核验引用、reservation 和实际 lease。所有外写取得确定终局且无外部 grant 时才删除 key 并核实 absence；可能存在迟到写时使用有界、无条件零字节 marker，成功响应并完整读回空 SHA、无旧 metadata 后只确认内容消除。marker 首版永不自动删除；它不能替代正式 lease 的可信 terminal 证据。Project 清理保留他域未收敛 lease，全部业务内容与本域事实清理后才能 completed。Recover/维护 worker 只核实存储和推进已授权清理，不能以后台 Service 代替 Owner 发布结果。

库配置由 `LoadStorageConfig` 的显式 lookup 提供下列固定命名；B03 才将其并入 Central 的必填配置、共享启动/健康/停止预算。它不读取默认 AWS 凭据、代理或业务 Secret。该存储 endpoint 是可信部署通道，不经过动态出站策略；默认校验证书，显式 HTTP 仅用于部署明确选择的可信通道。

| 库变量 | 规则 |
| --- | --- |
| `AGENTEAM_CENTRAL_OBJECT_ENDPOINT` | 固定 http/https origin，无 userinfo/query/fragment/路径前缀；禁止重定向和环境代理 |
| `AGENTEAM_CENTRAL_OBJECT_BUCKET` | 预建私有 bucket；固定 path-style、`us-east-1` |
| `AGENTEAM_CENTRAL_OBJECT_ACCESS_KEY` / `AGENTEAM_CENTRAL_OBJECT_SECRET_KEY` | 本部署显式凭据；opaque 配置/错误不输出原值 |
| `AGENTEAM_CENTRAL_OBJECT_TLS_MODE` | 默认 `verify-full` 对应 HTTPS；`disable` 必须显式且对应 HTTP |
| `AGENTEAM_CENTRAL_OBJECT_CA_FILE` | 可选追加 PEM trust roots，常规文件且最多 1 MiB |

Project 清理用持久批次计数轮转，每批至多 100 个对象，前缀的 protected lease 不会饿死后面的可清对象。Recover 遇另一活实例、无法证明死亡或单项规划/恢复失败时，保留该项 lease/pending checkpoint，并继续其它已获授权的独立 reader/writer 释放、attempt 核实和清理；首个错误仍返回，不能被当成全部清理完成。取消后不再领取下一项，失败 checkpoint 只在后续确认成功后移除。

bucket 必须从未启用 versioning、未启用 ObjectLock、无 lifecycle 和 bucket policy；Suspended 或配置查询失败也拒绝。初始化将持久 DB instance UUID 与 `control/store-identity` 绑定，只有新库+空 bucket 可建立原 UUID marker；confirmed marker 丢失/不符不能被自动覆盖。库提供真实 Check、Recover/维护状态、StopAdmission/Drain/Force；首停保留既有操作，force 使用调用方共享 context 取消实际 socket/源流并 join，不为每个资源追加一秒。

SDK 固定 `github.com/minio/minio-go/v7 v7.3.0`（自身要求 Go 1.25，项目仍精确 Go 1.27.1），`MaxRetries=1`、`DisableMultipart=true`、known length、预计算全文 SHA header 与 payload `If-None-Match:*`。本次 tidy 保持既有 pgx/Goose 及既有 root require 版本，新增 17 项显式 indirect require；SDK 与其运行期依赖共 19 个 module，其中部分原已在依赖图中。实际选中 `x/crypto v0.55.0`、`x/net v0.58.0`、`x/sys v0.47.0`、`x/text v0.41.0`，精确完整集合由 go.mod/go.sum 固定；不引入 MinIO server 的 Go 依赖到项目。

真实 fixture 使用[研究修订2中的固定来源与构建步骤](../work-items/d05-object-storage-research.md)，不是已取得官方 MinIO 镜像。server 为 RELEASE.2025-10-15T17-29-55Z，源码 commit `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`；linux/amd64 binary SHA256 必须为 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`。`AGENTEAM_MINIO_BINARY` 指向按该步骤构建的非秘密产物；未指定时可复用研究记载的临时构建缓存，缺失或 SHA 不符则失败，不下载一个不明版本替代。

`test-objects.sh` 验证 binary 后，在固定基础镜像中以 host UID/GID 执行它；没有声称该基础镜像本身是 MinIO。每轮生成新 nonce、内部网络、TLS CA、凭据和隔离数据目录；套件验证 label、精确 container/network ID、IP 和 mount，再连接。结束及信号中断只清理本轮 owned 资源并核对残留；研究源码/binary 缓存可保留，随机凭据和数据挂载不得残留。套件组合原 PG/出站/进程验收；普通 check-go 不启动 Docker。当前阶段验收及尚未绑定内容以[D05 主卡](../work-items/d05-object-storage-artifact.md)为准，不把 B01 库测试当成 B02/B03 或完整产品 ready。
