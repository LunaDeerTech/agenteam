# 后端开发

根 module 为 `github.com/LunaDeerTech/agenteam`，固定 Go 1.27.1，只使用标准库。Central 与 Runner 分别装配；Runner 不导入 Central。中立 `internal/platform` 只处理进程日志和关闭协调，不提供授权、业务幂等、数据库事务或 Runner 设备协议。

当前 Central 是诊断程序，Runner 是未连接进程。配置正确、程序存活与完整产品 ready 是不同状态。数据库/pgvector/迁移、Secret、对象存储、身份和 Runner 协议均未绑定；后续责任见 [D01 契约目录](../work-items/d01-contracts/README.md) 和 [D02 规格](../work-items/d02-engineering-foundation.md)。

## 构建与验证

```sh
# 指向实际 Go 1.27.1；该环境可使用 /workspace/toolchains/go1.27.1/bin/go。
export AGENTEAM_GO=/path/to/go1.27.1/bin/go
sh scripts/check-go.sh
sh scripts/build-go.sh
```

未指定 `AGENTEAM_GO` 时脚本使用 PATH 的 go，并先核对精确 `GOVERSION=go1.27.1`。所有构建与检查设置 `GOTOOLCHAIN=local`，不会自动下载或换工具链。`go.mod` 保留 `go 1.27.1`；同值 toolchain 指令会被该版本的 `go mod tidy` 删除，因此不重复声明。

`check-go.sh` 依次执行 `go test ./...`、`go vet ./...`、`go test -race ./...` 和两个真实二进制构建。产物在被忽略的 `bin/`。单独运行进程验证：

```sh
GOTOOLCHAIN=local "$AGENTEAM_GO" test -count=1 ./tests/process
GOTOOLCHAIN=local "$AGENTEAM_GO" test -count=1 ./internal/central/app
```

`tests/process` 在临时目录构建真实 cmd，检查 CLI、诊断、监听冲突、两进程 SIGINT/SIGTERM 和依赖方向；Linux 下检查未连接 Runner 没有 socket descriptor。Central app 测试通过真实 HTTP 和仅存在于测试二进制的 fixture 验证在途 drain、超时、第二信号、启动中断及不合作 handler 的有限退出。并发顺序用 channel/barrier 协调，超时只保护测试不永久等待。测试不读取 `.env`、凭据或已有服务，监听仅用 loopback port 0。

前端依赖和检查仍独立；本次 Go 工程变动无需无条件执行前端全量检查。

## 配置与命令

仅读取进程环境，不自动加载 `.env`。参考 [Central 示例](../../../deploy/central.env.example) 和 [Runner 示例](../../../deploy/runner.env.example)。存在但为空的变量无效；当前进程前缀中的未知变量拒绝，另一进程前缀及一般系统环境忽略。错误只报告固定字段名与原因，不打印配置值。

| 变量 | 默认值 | 约束 |
| --- | --- | --- |
| `AGENTEAM_CENTRAL_LOG_LEVEL` / `AGENTEAM_RUNNER_LOG_LEVEL` | `info` | 精确 `debug/info/warn/error` |
| `AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT` / `AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT` | `10s` | Go duration，100ms–5m；第一次停止后的总预算 |
| `AGENTEAM_CENTRAL_HTTP_ADDR` | `127.0.0.1:8080` | host:port，1–65535；仅显式 loopback IP 允许 port 0 |
| `AGENTEAM_CENTRAL_PUBLIC_ORIGIN` | `http://localhost:8080` | 单一 http/https origin；无 userinfo/query/fragment，路径仅空或 `/`；规范化主机、IP、默认端口和尾 `/` |

两个二进制只接受 `--help`、`--version`、`--check-config`，每次最多一个；无参数启动进程。未知参数和多余位置参数返回 2，不回显输入。help/version 不加载配置或启动服务；check-config 仅验证 D02 字段，成功结果包含 `scope=d02, valid=true, ready=false`，Runner 另有 `connected=false, authenticated=false`。未来数据库、密钥、对象及 RunnerLocalConfig 参数当前不被当作可用配置。

```sh
./bin/agenteam --check-config
./bin/agenteam-runner --check-config
AGENTEAM_CENTRAL_HTTP_ADDR=127.0.0.1:8080 ./bin/agenteam
./bin/agenteam-runner
```

## 诊断与日志

| 路径 | GET / HEAD | 含义 |
| --- | --- | --- |
| `/livez` | 200 | `status=alive`，仅 HTTP loop 存活 |
| `/readyz` | 503 Problem | `DEPENDENCY_UNBOUND`，当前不能产品 ready |
| `/diagnostics` | 200 | `ready=false` 与静态 `unbound` 能力名称；不是基础设施健康检查 |

只允许 GET/HEAD，HEAD 无 body；其他方法 405 且包含 Allow。未知 API、页面、静态资源均 404，不回退 HTML 或成功空结果。没有 Session 或业务写路由。HTTP 保留 B01 的服务端 request ID、安全 Problem、严格 JSON 和流式 writer 能力。

日志用 `slog.JSONHandler` 写 stderr；stdout 仅输出 CLI 结果。正常日志包含 UTC 时间、level、service、event、随机进程 run_id。HTTP 另有 request_id、method、声明的 route、status、duration、bytes；未匹配路由用 `unknown_route`。不记录原始错误、panic/堆栈、配置、body、query、Authorization、Cookie 或其他任意 header。原始 net/http 错误文本只投影为固定 `HTTP_SERVER_ERROR`。启动在实际 bind 后记录监听地址，Runner 明确 `unconnected`，不尝试连接、注册、认证或监听。

## 停止与退出

入口在启动前注册 SIGINT/SIGTERM。第一次信号或调用方取消只进入一次 stopping，第二次信号强制关闭；重复程序化 Stop 不重置预算。Central 的 serving context 独立于停止请求，首次停止不取消正在执行的 handler；Shutdown 使用独立 deadline。

仍交给 handler 的迟到请求返回 503 `SHUTTING_DOWN`。net/http Shutdown 接管监听或连接后，可直接拒绝连接或返回 EOF，不保证每个尚未分发的请求都经过 Problem handler。已开始的请求在预算内完成后释放 serving context；超时或第二信号取消它、关闭 server 及已拥有资源，只额外等待 Serve/Shutdown worker 最多 1s，随后返回失败。日志区分 drained/forced；强制退出不证明业务副作用回滚或业务已停止。

| 退出码 | 含义 |
| --- | --- |
| 0 | help/version/check-config 成功，或进程干净停止 |
| 2 | 参数或配置拒绝 |
| 1 | 初始化、监听、意外 Serve 错误、drain 超时或第二信号强关 |

Runner 当前没有 RPC、子进程或长连接；第一次信号停止真实未连接进程即可。D15–D17 后续绑定其 Managed Process 和通道关闭顺序。HTTP hijack/WebSocket 不由 Server.Shutdown 自动等待；当前生产没有该连接，后续连接 owner 必须登记自身停止与关闭责任，不将当前 HTTP 测试当作未来长连接或持久恢复验收。
