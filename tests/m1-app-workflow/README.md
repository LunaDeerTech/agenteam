# M1 真实 App 工具工作流

此入口启动正式 `tool-loop-v1` App、原 Scheduler/Associated、多轮 JSON Model、实际 Runner read/write/run 和真实浏览器。确定性 Provider 使用自有 TLS HTTP 服务，五轮依次读取 input.json、写 stats.py、运行程序、读取 result.json、输出成果；它证明运行图和副作用，不证明外部模型自行规划。

执行配置仍为已有 `AGENTEAM_CENTRAL_EXECUTION_RUNTIME` JSON 的 `profile` 字段，取值 `tool-loop-v1`；原 `direct-text-v1` 不变。产品 UI 无新增 profile 选择。测试沿原 App 配置入口装配，并从正式 Owner 创建/指派 Task，原 Scheduler 自动触发；不手调 Claim/Launch/Advance。

## 运行

准备 Go 1.27.1、项目锁定版本的 MinIO 与 Docker 镜像、锁安装的 browser node_modules，以及当前 Web production dist。依赖安装和缓存可在树外共享；本入口不安装依赖、不读取真实 Provider 凭据、不接外部数据库。

```sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go \
GOMODCACHE=/workspace/go/pkg/mod \
GOCACHE=/workspace/.cache/go-build \
GOPROXY=off GOSUMDB=off \
AGENTEAM_MINIO_BINARY=/workspace/shared/agenteam-m1-tools/minio/minio \
AGENTEAM_M1_BROWSER_MODULES=/workspace/shared/agenteam-m1-tools/node-browser/node_modules \
AGENTEAM_M1_WEB_DIST=/absolute/path/to/web/dist \
sh tests/m1-app-workflow/run.sh
```

入口构建原 Runner 和 `internal/central/app` integration 测试二进制，然后调用既有 objects → outbound → PostgreSQL fixture。每次使用新 nonce 资源、隔离 DB 和临时工作目录；结束等待原 fixture 清理。临时 browser dependency symlink 只在入口创建时由入口删除，不改共享安装。

## 证据

每轮输出在 `output/ai/m1-app-integration/run.*`。`native.log` 保存实际测试及 fixture Wait；`owned-resources.json` 仅保存资源身份；`browser/TestM1AppToolWorkflow` 保存浏览器阶段、脱敏控制台及截图。Session/Runner enrollment 材料放私有目录并清理，不写证据。

通过必须同时包含同一 Task/Execution 的实际多轮工具结果、文件内容与 command stdout/exit、v2 最终输出、唯一 Owner 评审转移、刷新一致，以及 App/Runner/浏览器原进程返回。编译、环境准备或受控 Provider 通过均不标外部模型推理通过。旧 STOP 驱动不由本入口运行。
