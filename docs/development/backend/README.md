# 后端开发

根 module 为 `github.com/LunaDeerTech/agenteam`，固定 Go 1.27.1。Central 已装配固定 pgx/Goose 数据库库，见[数据库说明](database.md)。Audit 的同事务追加、授权查询/生命周期清理端口与独立签名 cursor 已实现，见[Audit 说明](audit.md)。Secret 的 envelope、引用/lease、加密 receipt 和可恢复主密钥维护见 [Secret 说明](secret.md)。Central 与 Runner 分别装配；Runner 不导入 Central。中立 `internal/platform` 只处理进程日志和关闭协调，不提供授权、业务幂等、数据库事务或 Runner 设备协议。

当前 Central 在真实数据库连接、迁移、首次读写检查及 Audit/cursor/Secret 安全初始化后提供诊断，Runner 是未连接进程。配置正确、程序存活与完整产品 ready 是不同状态。出站访问、对象存储、身份、Secret 业务授权和 Runner 协议仍未绑定；后续责任见 [D01 契约目录](../work-items/d01-contracts/README.md) 和 [D04 规格](../work-items/d04-security-foundation.md)。

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
```

`tests/process` 在临时目录构建真实 cmd，普通测试检查纯 CLI、配置拒绝、Runner SIGINT/SIGTERM 和依赖方向；Linux 下检查未连接 Runner 没有 socket descriptor。实际 Central 成功启动、迁移失败、监听冲突、启动信号、数据库故障/恢复与健康超时放在 integration suite。Central app 普通测试覆盖装配顺序和时钟边界；integration 中的测试进程调用真实 Store/Migrator，通过真实 HTTP+Tx 验证正常 drain、阻塞查询、第二信号和不合作 callback 的有限退出。测试专属 route/barrier 不进入生产入口。并发顺序用 channel、数据库锁和观测事实协调；测试不读取外部 `.env`、凭据或已有服务，监听仅用 loopback port 0。

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
| `AGENTEAM_CENTRAL_PUBLIC_ORIGIN` | `http://localhost:8080` | 单一 http/https origin；无 userinfo/query/fragment，路径仅空或 `/`；规范化主机、IP、默认端口和尾 `/` |

Central 还必须配置 `AGENTEAM_CENTRAL_DATABASE_URL`；TLS 默认 verify-full，显式 CA 文件会在配置检查时读取验证，其余数据库参数及范围见[数据库配置表](database.md#版本与配置)。连接、迁移 guard、全部迁移和首次 Check 共用 `DATABASE_STARTUP_TIMEOUT`，随后在独立 30s 安全初始化预算内验证 cursor/Audit 存储及 Secret registry/canary/write fence、启动维护 worker，再 HTTP bind。两个阶段都受启动停止信号取消。配置缺失退出 2，连接、版本、迁移或读写失败退出 1。

两个二进制接受 `--help`、`--version`、`--check-config`，无参数启动进程。未知参数和多余位置参数返回 2，不回显输入。help/version 不加载配置或启动服务；Central check-config 只验证当前 D04 B02 参数（包括两个必需且独立的 keyring）、不连接，输出 `scope=d04, valid=true, ready=false`；Runner 保持 `scope=d02`，另有 `connected=false, authenticated=false`。check-config 不证明 DB canary 已通过。出站策略、对象及 RunnerLocalConfig 参数仍未实现。

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
| `/readyz` | 503 Problem | 数据库或 Secret 异常为 `DEPENDENCY_UNAVAILABLE`，已实现组件健康但业务未绑定时为 `DEPENDENCY_UNBOUND` |
| `/diagnostics` | 200 | `ready=false`；PostgreSQL/pgvector/migrations/read_write 来自真实采样；cursor 初始化后 available，audit_storage 随 DB 健康；Secret 来自真实初始化和维护状态，最多返回写版本/轮换状态/剩余数量；audit_authorization/secret_authorization/identity/outbound 等仍 unbound |

数据库采样由单个 worker 每 10s 执行，单次 2s 超时且不重叠。样本超过 20s 或检查失败立即不再显示旧健康；重新验证成功后恢复数据库子状态。HTTP 仅读快照，健康故障不会使 `/livez` 失败。成功样本只含安全时间和版本等技术字段，健康写探针在事务中读回后 rollback，不留下历史记录。

只允许 GET/HEAD，HEAD 无 body；其他方法 405 且包含 Allow。未知 API、页面、静态资源均 404，不回退 HTML 或成功空结果。没有 Session 或业务写路由。HTTP 保留 B01 的服务端 request ID、安全 Problem、严格 JSON 和流式 writer 能力。

日志用 `slog.JSONHandler` 写 stderr；stdout 仅输出 CLI 结果。正常日志包含 UTC 时间、level、service、event、随机进程 run_id。HTTP 另有 request_id、method、声明的 route、status、duration、bytes；未匹配路由用 `unknown_route`。数据库日志仅增加白名单阶段/错误码、五位 SQLSTATE 和迁移版本；安全日志仅输出 cursor_initializing/audit_initializing/secret_initializing/secret_maintenance_starting/secret_unavailable/initialized/failed 固定阶段。不记录原始错误、panic/堆栈、SQL/参数、DSN、证书路径、配置、body、query、Authorization、Cookie 或其他任意 header。原始 net/http 错误文本只投影为固定 `HTTP_SERVER_ERROR`。启动在实际 bind 后记录监听地址，Runner 明确 `unconnected`，不尝试连接、注册、认证或监听。

## 停止与退出

入口在启动前注册 SIGINT/SIGTERM。第一次信号或调用方取消只进入一次 stopping，第二次信号强制关闭；重复程序化 Stop 不重置预算。Central 的 serving context 独立于停止请求，首次停止不取消正在执行的 handler；Shutdown 使用独立 deadline。

仍交给 handler 的迟到请求返回 503 `SHUTTING_DOWN`。net/http Shutdown 接管监听或连接后，可直接拒绝连接或返回 EOF，不保证每个尚未分发的请求都经过 Problem handler。首信号停止健康领取和新 Secret batch；在途 handler 与当前维护 batch 仍可使用数据库。HTTP 与维护 batch 均完成后才 Store.StopAdmission，已有 Tx/Rows 按第一次停止的同一剩余 deadline 排空并关闭池。

超时或第二信号强制取消 owned DB 操作、发有界 CancelRequest 并关闭其 socket，再关闭 HTTP；数据库关闭、HTTP 关闭和各 worker join 共用最多额外 1s，不逐阶段重置。启动中的信号也取消连接/迁移/Secret 初始化，第二信号受相同外层总预算约束；晚返回的已取得资源仍清理。日志区分 drained/forced；强制退出不证明事务、副作用已回滚或业务已停止。

| 退出码 | 含义 |
| --- | --- |
| 0 | help/version/check-config 成功，或进程干净停止 |
| 2 | 参数或配置拒绝 |
| 1 | 初始化、监听、意外 Serve 错误、drain 超时或第二信号强关 |

Runner 当前没有 RPC、子进程或长连接；第一次信号停止真实未连接进程即可。D15–D17 后续绑定其 Managed Process 和通道关闭顺序。HTTP hijack/WebSocket 不由 Server.Shutdown 自动等待；当前生产没有该连接，后续连接 owner 必须登记自身停止与关闭责任，不将当前 HTTP 测试当作未来长连接或持久恢复验收。
