# D02 工程基础实施规格

- 修订：1；阶段：B01/B02 已验收，D02 已完成；基线 `main@bb89da2`。
- 受 [D02 主规格](d02-engineering-foundation.md)修订 1 管理；本文件不表示 B01/B02 或产品行为已经验收。
- 依据：[D01 基础](d01-contracts/foundation.md)、[依赖与责任](d01-contracts/README.md#代码依赖与运行时依赖)、[部署运行](../../architecture/platform-infrastructure/deployment-runtime.md)、[Runner Management](../../architecture/runner/runner-management.md)、[仓库结构](../repository-structure.md)。
- 目标是两个可分别构建、测试、冻结并提交的结果；当前不引入数据库、对象 SDK、认证、业务 handler、Runner 协议或前端嵌入。

## 1. 工具链与结果所有权

固定根 module `github.com/LunaDeerTech/agenteam`，`go.mod` 声明 `go 1.27.1`；不保留与最低版本相同的冗余 toolchain 指令（实际 Go 1.27.1 的 `go mod tidy -diff` 要求删除该行）。只使用标准库，不创建空 `go.sum`、`go.work` 或未来领域包。

主线程已用本机 Go 1.27.1、`GOTOOLCHAIN=local` 编译并运行临时 net/http + encoding/json 程序，exit 0；S01 作者复核 `go version`、`go env GOOS GOARCH GOTOOLCHAIN` 为 Go 1.27.1、linux/amd64、auto。构建/测试必须显式 `GOTOOLCHAIN=local` 并核对 `GOVERSION=go1.27.1`，不自动下载或悄悄换工具链。实际仓库编译证据仍由 B01/B02 产生。

| 卡 | 完整结果与验收边界 | 独占写入范围 |
| --- | --- | --- |
| B01 | 公共标量、HTTP Problem/JSON/追踪与 schema；可独立 `test/vet/build` 的真实库，无业务服务启动要求 | `go.mod`；`internal/central/foundation/`、`internal/central/httpapi/` 及同目录测试；`api/openapi/common.json`；`internal/central/.gitkeep` |
| B02 | 两个真实入口、环境配置、诊断、日志与可停止进程；构建两二进制并验证真实信号 | `cmd/agenteam/`、`cmd/agenteam-runner/`；`internal/central/app/`、`internal/central/config/`；`internal/runner/app/`、`internal/runner/config/`；`internal/platform/logging/`、`internal/platform/lifecycle/`；`tests/process/`；`scripts/check-go.sh`、`scripts/build-go.sh`；`deploy/central.env.example`、`deploy/runner.env.example`；`docs/development/backend/README.md`、根 `README.md`、`AGENTS.md` 的现状/命令说明；`docs/development/repository-structure.md` 必要工程现状同步 |

B02 依赖 B01 独立验收提交，默认只消费其公共接口；需要改 B01 范围先由主线程移交，避免同文件双写。S01 文件、主规格、台账和计划由主线程维护后续修订。各卡只移除已有真实文件目录的 `.gitkeep`；B02 同步负责其替换的 cmd/internal/runner/scripts/deploy/tests 占位。`internal/runnerprotocol/.gitkeep` 保留。

`foundation` 仅依赖标准库；`httpapi` 可依赖 foundation。两个 app 是各自组合根，`cmd` 只做参数/信号/退出码接线。中立 `platform/logging` 与 `platform/lifecycle` 只含进程日志、停止协调和超时工具，不导入 Central、Runner 或业务类型；Runner 传递依赖不得包含 `internal/central`。不建立万能服务容器或无实现端口包。

## 2. B01 标量与错误

Go 类型/方法名以下列边界为准，文件划分可由 B01 在拥有目录内细化：

```go
type ID[K any] struct { /* private UUID bytes */ }
func NewID[K any]() (ID[K], error)
func ParseID[K any](string) (ID[K], error)
type Instant struct { /* private time.Time */ }
func ParseInstant(string) (Instant, error)
func NewInstant(time.Time) (Instant, error)
type Version int64
type Revision int64
type Sequence int64
type Progress int64
type DurationMS int64
type Digest string
type IdempotencyKey string
// 上述类型提供显式验证与 JSON/Text 编解码；无业务 marker 预创建。
type Request struct{}
type CommandMeta struct { RequestID ID[Request]; IdempotencyKey IdempotencyKey; ExpectedVersion *Version }
```

- UUIDv7 用 `crypto/rand` 实现 RFC 9562 的时间戳/version/variant/random 位；拒绝零、错误 version/variant、非规范输入。输出小写连字符格式，不承诺进程内严格单调或用内存替代数据库唯一约束。随机源失败不能回退伪随机。
- Instant 入站接受 RFC 3339 时区偏移并归一 UTC；超过六位小数、非法日期/偏移拒绝。内部从 clock 构造时显式截断至微秒，JSON 固定六位小数及 `Z`，不靠 `time.Time.MarshalJSON` 的可变精度。
- Version/Revision/Sequence 是正 int64；Progress/DurationMS 可为零。JSON 必须为规范十进制字符串，拒绝数值 token、负值、加号、空白、前导零和越界；缺值使用指针/null，不把 0 解释为 editable version。DurationMS 不改变原生 Tool timeout 的整数参数规则。
- Digest 与 IdempotencyKey 按 D01 格式验证；PageRequest 默认 50、范围 1–200，`Page[T]` 明确 items 与可选 next_cursor，空 items 序列化为 `[]`。
- 实现 D01 Fault/FieldError/CommitState 与共同 code；原始 `error` 可内部关联但不得被 JSON、`Error()` 或日志格式化顺带输出。unknown code 安全映射 INTERNAL_ERROR，不接受客户端提供 HTTP status/detail。
- 此阶段不实现 Cursor 签名、业务摘要 canonical-v1、CommandRecord 存储、Actor 授权或 Tx adapter。它们由 D03/D04 及首次消费模块按 D01 绑定；没有假的提交/幂等成功。

## 3. B01 HTTP 边界

使用 `net/http` + `ServeMux`。基础包提供 `RequestID(ctx)`、`WithRequestID`、`Recover`、`WriteProblem`、`WriteJSON`、`DecodeJSON`；组合顺序由外到内为 request tracing/access log、recover、路由及 handler。HTTP 状态映射只在 httpapi，foundation 不依赖 HTTP。

| 行为 | 确定的接口语义 |
| --- | --- |
| request_id | 每请求由服务端新生成 UUIDv7，写 context、`X-Request-ID`、Problem 与 access log；忽略入站同名值，不回显未验证 trace header；404/405/错误也有 ID |
| Problem | D01 `application/problem+json` 字段/code/status；instance 仅安全路径，不含 query/fragment；默认安全文本，field path 是 JSON Pointer；`Cache-Control: no-store` |
| commit_state | 输入/路由拒绝为 not_started；领域传来的明确状态原样保留；无法知道已发生何种副作用的 panic/未知错误为 unknown，不宣称回滚 |
| JSON 写出 | 普通 DTO 先编码再提交 status/header，编码失败返回安全 500；流式输出另走原生 writer，不强制全量缓冲 |
| panic | 未提交响应时返回安全 500；已提交后不追加 Problem 或第二响应，记录安全故障码并用 `http.ErrAbortHandler` 中止连接/stream；既有 ErrAbortHandler 原样传播 |
| writer 包装 | 记录真实 status/bytes；提供 `Unwrap() http.ResponseWriter` 供 ResponseController；保留实际支持的 Flush/Hijack 能力，unsupported 不伪装成功；正确区分 1xx 与最终响应 |

recover/access log 不格式化 panic 值、原始 error、请求/响应 body、Authorization/Cookie/CSRF、query 或任意 header。诊断可记录安全 code、request_id 和静态函数位置；不输出带参数的原始堆栈。客户端断连遵循 request context cancellation，不因此创建业务 cancel 命令。

`DecodeJSON(w, r, dst, maxBytes)` 只用于明确 JSON DTO 的路由，默认上限 1 MiB，调用方可显式收紧。D05 上传/未来 stream 不套此全量 decoder；本阶段生产路由没有业务写入口，使用测试 handler 验证下列行为：

1. 仅接受 `application/json`（可附 UTF-8 charset），不自动解压 Content-Encoding；其他类型/编码返回 415 `UNSUPPORTED_MEDIA_TYPE`。
2. 用 `http.MaxBytesReader` 限制实际读取量，超限返回 413 `PAYLOAD_TOO_LARGE`；不能仅信 Content-Length。
3. 拒绝非法 UTF-8、空 body、非法 JSON、非 object 根、超过 64 层嵌套、重复 key（含 escape 后相同 key）、尾随第二值/垃圾。
4. DTO 字段名称严格匹配声明的 JSON 标签，拒绝未知字段及大小写别名；保留数字字面量，整数不经 float64。命名标量负责范围/编码验证，不静默损失精度。
5. 以上结构/字段错误为 400 `INVALID_ARGUMENT`，不回显 decoder 原始错误中的输入片段。完成一次解码后确认 EOF。

405 `METHOD_NOT_ALLOWED` 附 Allow；404 `NOT_FOUND`、413、415、503 `SHUTTING_DOWN` 与上述共同码一起进入稳定映射。未知 `/api/v1/*`、页面或静态文件路径都返回真实错误，不返回 HTML/空列表成功。D07 后续接 Session/Origin/CSRF；本阶段不注册 `/api/v1/session` 或成功授权替身。

`api/openapi/common.json` 为 OpenAPI 3.1 文档基础片段，`paths` 为空，定义 ID/Instant/int64-string、Problem/FieldError、PageRequest/Page envelope。实际字段/枚举与 Go JSON 同步；只记录已实现边界，不复制未来业务 API。B01 验证 JSON 可解析与契约样例；完整前端生成链由 D26 接入。

## 4. B02 配置与日志

配置仅从进程环境读，不自动加载 `.env` 或把运行平台配置移到环境。加载使用可注入 `LookupEnv` 便于隔离测试；实例保存不可变 typed Config，不在请求中重新读取环境。下表 `C` 表示 `AGENTEAM_CENTRAL_`，`R` 表示 `AGENTEAM_RUNNER_`。

| 环境名 | 缺省值 | 校验与用途 |
| --- | --- | --- |
| `C/R LOG_LEVEL` | `info` | 精确 `debug/info/warn/error`，分别组成 `AGENTEAM_CENTRAL_LOG_LEVEL` 等完整名称 |
| `C/R SHUTDOWN_TIMEOUT` | `10s` | Go duration，100ms–5m；首次停止后的总 drain 预算，不因再次等待重置 |
| `C HTTP_ADDR` | `127.0.0.1:8080` | 必须 host:port，端口 1–65535；端口 0 仅允许显式 loopback IP，供隔离测试获得实际地址 |
| `C PUBLIC_ORIGIN` | `http://localhost:8080` | 单一绝对 http/https origin，无 userinfo/query/fragment，路径只能空或 `/`；规范化后保存，D07 绑定同源校验 |

环境变量存在但为空视为错误，不悄悄用默认值。当前二进制自有前缀内未知 key 拒绝，另一二进制前缀与普通系统环境忽略。错误只含字段名和稳定原因（missing/invalid/unsupported/unreadable 等），不打印原始值、URL、完整 config 或 `os.Environ()`。

CLI 只提供 `--help`、`--version`、`--check-config`；未知参数/多余位置参数拒绝且不回显输入值。前两者不启动服务；check-config 只验证 D02 字段，成功也输出 `scope=d02, ready=false`，Runner 另含 `connected=false, authenticated=false`。它不宣称数据库/Secret/对象/设备配置已通过验证。

D03 新增 PostgreSQL/迁移参数；D04 新增主密钥环/信任材料；D05 新增 MinIO 参数；D15 落实 RunnerLocalConfig 文件、private key 权限、central_url/runner_id/root_path、enrollment 与连接。当前不接收这些字段后当作可用，也不读取现有凭据或连接环境服务。生产 TLS/证书部署与前端资源由 D28 和 D26 接入；本阶段仅提供明确非 ready 的 HTTP 诊断。

`slog.JSONHandler` 写 stderr，stdout 仅给 CLI 结果；每行含 UTC 时间、level、service、event、进程级随机 run_id。HTTP 行另含 request_id、method、匹配的 route template、status、duration、bytes；未匹配路径只记固定 unknown_route，不记用户原始 URL。启动/退出只记录当前阶段、安全 error code 与有限绑定状态，不 dump Config。

run_id 是中立进程诊断标记，不是 Runner ID、HTTP request_id 或业务幂等键。日志包只接受明确安全的字段，不能靠通用正则掩码保证任意 error/对象安全；D07 已定的敏感初始化/投递日志另由其受控接口实现，不在 D02 增设通用 secret log。

## 5. 诊断、装配与启动

Central 完整产品在 D02 必然非 ready。配置有效可以进入 `diagnostic_serving`，不得记录 `service_ready` 或注册业务 handler；这不是架构中的部分业务降级模式。

| 路径 | GET/HEAD 结果 | 内容 |
| --- | --- | --- |
| `/livez` | 200 | `{"status":"alive"}`，仅说明进程 HTTP loop 活着 |
| `/readyz` | 503 | D01 Problem，`code=DEPENDENCY_UNBOUND`；没有成功空探针 |
| `/diagnostics` | 200 | `ready=false`；PostgreSQL/pgvector/migrations/Secret/object 等静态能力标记 unbound，仅名称/状态，无配置或凭据 |

三条路径仅 GET/HEAD，HEAD 不写 body，其他方法 405；无 Session/用户信息。未绑定模块不是实时健康探测结果，不显示 `ready/connected/authenticated`。未来真 adapter 加入时必须验证 mandatory dependencies；外部某个 Model/MCP/Runner 不在线仍不等于 Central 必需基础设施失败。

Central 按配置校验→logger→构建 router→同步 `net.Listen`→启动 Serve 的顺序装配。bind 失败立即退出且不打印 listening。记录实际监听地址，支持 loopback port 0 测试。`ReadHeaderTimeout=5s`、`IdleTimeout=60s`、`MaxHeaderBytes=1 MiB`；不设全局 WriteTimeout 截断未来 stream，JSON body 的大小由显式 decoder 控制。

Serve 的非预期错误为进程失败，须停止其他已启动资源并退出；只在已请求停止时将 `http.ErrServerClosed` 解释为正常。app 自己持有 listener/server/关闭责任，不通过 HTTP handler 装配服务。配置失败无资源副作用；启动中断只关闭已经取得的资源，不能访问 nil/未初始化依赖。

Runner 没有入站 HTTP listener；有效进程配置后进入 `unconnected` 等待停止信号，日志明确 D15 未绑定、未认证、未连接。无 enrollment、WSS、heartbeat、workspace、进程执行或模拟 online 状态；不把进程存活当成 Central 已登记的设备。

## 6. 信号、drain 与强制关闭

两个入口监听 SIGTERM/SIGINT，注册在可阻塞启动之前；第一次信号或调用方停止请求只触发一次状态转换，第二次信号强制关闭。app/lifecycle 返回结果，由最外层 main 决定退出码；库和 goroutine 不直接 `os.Exit`。

```text
Run(ctx, typed_config, logger, signals) -> error
central: starting -> diagnostic_serving -> stopping -> stopped
runner:  starting -> unconnected -> stopping -> stopped
exit 0: help/version/check-config 成功或干净停止
exit 2: 参数/配置错误
exit 1: 初始化/监听/Serve 失败，drain 超时或第二信号导致强制退出
```

Central 正常停止的顺序必须可测试：

1. 原子进入 stopping，使 readiness 仍为 false，拒绝新接入；仍被交给 handler 的迟到新请求返回 503 `SHUTTING_DOWN`。调用 `Server.Shutdown` 停止监听/keep-alive 接入；Shutdown 接管后，net/http 可直接关闭连接或拒绝接入（客户端 EOF/连接失败），不承诺这类请求仍有 HTTP 503。
2. 给 Shutdown 创建独立、未被停止信号取消的 deadline context；在途 handler 可在预算内结束。**不能把第一次 signal 的已取消 context 直接作为 HTTP BaseContext 或 Shutdown context。**
3. HTTP serving context 与停止请求分开；成功 drain 后再取消/释放它。后台业务领取任务的停止端口未来由所属模块接入，不等待所有 Agent 自然运行完。
4. deadline 到达或第二信号：取消 serving context、调用 `Server.Close` 并执行已注册资源的 force-close；额外等待最多 1s 后返回强制停止结果，不能被不合作 handler 永久卡住。
5. 关闭与结果通知幂等；不在已结束 worker channel 重复写入，不遗留不受管 goroutine。最后日志区分 drained/forced，强制关闭不记录已确认业务停止。

HTTP hijack/WebSocket 不受 Shutdown 自动等待：D02 包装器保留能力，但生产尚无该连接；D15/D17/D25 接入时必须由连接 owner 注册停止/关闭，不把空 registry 当成完成其清理的证明。普通 HTTP 与未来长连接的取消、结果未知均不替代领域 checkpoint/恢复。

Runner 第一次信号就停止接新工作并取消自己的运行 context；当前没有 RPC/子进程需要等待，退出真实 unconnected 进程即可。后续 D15–D17 按 Runner Management 绑定 cancellation→command graceful/force→全部 Managed Process→channels 的关闭顺序，不引入协议 draining 或等待 persistent 进程自然退出。

## 7. 验收场景与实际命令

以下是实施验收要求，不是 S01 已执行的产品测试。测试只用 `t.TempDir()`、loopback 临时端口和测试注入，不读生产 `.env`/凭据，不连接已有数据库/对象/Runner。并发交错用 channel/barrier，避免 sleep 猜时序；超时只作有界失败保护。

| 编号 / 卡 | 必须验证的行为 |
| --- | --- |
| F01 / B01 | UUID version/variant/拒绝格式与并发生成；时间偏移/微秒/非法时间；int64 极值和拒绝 number token/null/负值/越界；digest/key/page 边界 |
| F02 / B01 | Problem mapping 与安全字段；请求 ID 不信入站值、并发不串 context；404/405/失败响应 header 一致；unknown error/panic 中放敏感 sentinel，body/log 均不出现 |
| F03 / B01 | JSON chunked 超限、重复/escape key、大小写别名、未知字段、非法 UTF-8/深度、尾随值、超大整数、media type；编码失败未提前提交 200 |
| F04 / B01 | 真实本机 HTTP streaming/flush 与 ResponseController，受支持 Hijack 能力仍在；部分响应后 panic 中止而不拼接 JSON；请求取消能到 handler |
| P01 / B02 | 两个配置 loader 的缺省/空值/未知项/错误 duration/address/origin；CLI 不回显注入 sentinel；check-config 明确仅 D02 合法，敏感 env 不进日志 |
| P02 / B02 | 构建后的两个真实二进制逐一 SIGTERM、SIGINT 干净退出；Central 端口 0 诊断/HEAD/错误路由；Runner 没有入站监听或连接尝试；两者始终非产品 ready |
| P03 / B02 | 已占监听端口、无效配置、启动期间停止、Serve 非预期失败；无假 listening/ready，退出码和资源释放正确 |
| P04 / B02 | 测试 helper 使用真实 app/lifecycle 与注入的 barrier handler：首次信号在途正常完成、新请求拒绝；不合作 handler 超时强关；第二信号提前强关；并发重复 stop 无竞态 |
| P05 / B02 | `go list -deps` 检查 Runner 无 Central 传递依赖；中立包无业务依赖；脚本固定工具链；web/db/runnerprotocol 无提前产品实现 |

P04 的慢路由、panic 路由和辅助子进程只在测试 fixture 中存在，不添加生产 `--test-*`、后门路由或延时配置；P02 必须运行实际 `cmd` 构建产物，不能只测同进程函数。race 覆盖 HTTP writer 状态、stop 状态、日志/配置并发读取。

从仓库根执行（B01 不运行尚不存在的 cmd；B02 完整运行）：

```sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go
GOTOOLCHAIN=local "$AGENTEAM_GO" env GOVERSION GOOS GOARCH
GOTOOLCHAIN=local "$AGENTEAM_GO" test ./...
GOTOOLCHAIN=local "$AGENTEAM_GO" vet ./...
GOTOOLCHAIN=local "$AGENTEAM_GO" test -race ./...
GOTOOLCHAIN=local "$AGENTEAM_GO" build ./internal/...
# 以下为 B02；脚本创建现有 .gitignore 覆盖的 bin 目录。
AGENTEAM_GO="$AGENTEAM_GO" sh scripts/build-go.sh
GOTOOLCHAIN=local "$AGENTEAM_GO" test -count=1 ./tests/process
GOTOOLCHAIN=local "$AGENTEAM_GO" list -deps ./cmd/agenteam-runner
git diff --check
```

`build-go.sh` 必须实际运行 `go build -o bin/agenteam ./cmd/agenteam` 与 `go build -o bin/agenteam-runner ./cmd/agenteam-runner`；两个脚本用 `AGENTEAM_GO` 或 PATH 上的 go，先核对精确版本并设置 local，错误不静默继续。check 脚本串行执行相关 test/vet/race/build；不重复安装或无条件运行前端全量检查。

B01/B02 各自记录冻结指纹、真实命令/退出码、工具链、测试范围、未绑定项后交独立验证；主线程验收一块即提交推送，D02 全部通过才进入 D03。没有数据库事务/领域事件/业务幂等或持久恢复可在本阶段声明成功；未来真实依赖的配置与绑定验证仍由 D03/D04/D05/D15 等负责。
