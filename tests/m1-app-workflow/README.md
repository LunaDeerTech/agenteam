# M1 真实 App 工具工作流

此入口启动正式 `tool-loop-v1` App、原 Scheduler/Associated、多轮 JSON Model、实际 Runner read/write/run 和真实浏览器。确定性 Provider 使用自有 TLS HTTP 服务，五轮依次读取 input.json、写 stats.py、运行程序、读取 result.json、输出成果；它证明运行图和副作用，不证明外部模型自行规划。

执行配置仍为已有 `AGENTEAM_CENTRAL_EXECUTION_RUNTIME` JSON 的 `profile` 字段，取值 `tool-loop-v1`；原 `direct-text-v1` 不变。产品 UI 无新增 profile 选择。测试沿原 App 配置入口装配，并从正式 Owner 创建/指派 Task，原 Scheduler 自动触发；不手调 Claim/Launch/Advance。

正常工作流显式使用 `tick_interval: "250ms"`、`relaunch_skip_count: 240`，提供约 60 秒的有限 Owner 查看窗口。这是本验收部署的参数，系统没有默认启用的执行配置。调度器持续运行；窗口内须只有原工作 Execution。送审后按正式规则可启动独立的 `task/review` Execution，使用另一个真实 Model/Provider 和两响应场景，最多读取评审 Agent 自有的说明文件并确认收到任务。该说明文件不代表 worker 产物传递，本验收不声称完成自主评审决策。

目前 `tool-loop-v1` 首轮要求非空工具集并采用 required 选择，故两名 Agent 都有真实允许的工具；尚不声明无工具 Agent 兼容。工作五轮与评审最多两轮响应分别带受控 `X-Request-Id`，通过已有持久 Usage 归属到各自 Execution/Call，生产请求不添加测试 header。Owner 回验完成后按原 Runtime Stop/Drain → Runner Wait → App Join 顺序关闭，允许活跃评审沿原取消路径收口。

## 运行

准备 Go 1.27.1、项目锁定版本的 MinIO 与 Docker 镜像、锁安装的 browser node_modules、正常 workflow 所需的 web node_modules，以及当前 Web production dist。依赖安装和缓存可在树外共享；本入口不安装依赖、不接外部数据库。默认受控两个 case 不读取真实 Provider 凭据；显式 live 的条件见下文。

```sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go \
GOMODCACHE=/workspace/go/pkg/mod \
GOCACHE=/workspace/.cache/go-build \
GOPROXY=off GOSUMDB=off \
AGENTEAM_MINIO_BINARY=/workspace/shared/agenteam-m1-tools/minio/minio \
AGENTEAM_M1_BROWSER_MODULES=/workspace/shared/agenteam-m1-tools/node-browser/node_modules \
AGENTEAM_M1_WEB_MODULES=/workspace/shared/agenteam-m1-tools/node-web/node_modules \
AGENTEAM_M1_WEB_DIST=/absolute/path/to/web/dist \
sh tests/m1-app-workflow/run.sh
```

入口构建原 Runner 和 `internal/central/app` integration 测试二进制，然后调用既有 objects → outbound → PostgreSQL fixture。每次使用新 nonce 资源、隔离 DB 和临时工作目录；结束等待原 fixture 清理。入口按需创建 browser dependency symlink；正常 workflow 与显式 live 还要求 `AGENTEAM_M1_WEB_MODULES` 并按需创建 `web/node_modules` symlink，供浏览器 harness 读取正式前端依赖。原 fixture Wait 后，入口只清理本轮自行创建的链接，不改共享安装或接管已有安装；无需重新安装依赖。失败/取消场景不要求新增变量，也不创建 web 链接。

默认 `AGENTEAM_M1_CASE=workflow` 仅运行上述成功/评审场景。设置 `AGENTEAM_M1_CASE=failure-and-cancellation` 仅运行两个独立新库场景：真实 read missing 无结果载荷失败，以及实际命令启动后原 Runtime Stop/Drain 的取消与进程 Join。两个场景共用原 App/Runner/Provider 组图，不启动浏览器，不模拟 Human cancel 入口。这两个受控 case 保留原固定输入；各自需记录实际结果，未运行不计通过。

失败/取消场景继续使用原零冷却配置、单 Provider 和原提交后 observer；正常场景的 Owner 窗口与评审 Provider 选配不会进入这两个场景。

## 显式真实模型入口（准备中，未实跑）

`AGENTEAM_M1_CASE=live` 选择 `^TestM1AppLiveWorkflow$`，同时必须设置 `AGENTEAM_M1_LIVE_OPT_IN=external-model-read-write-run-review`。两层选择缺任一即拒绝；普通两个 case 会继续剔除所有 `DEV_CHAT_*` 与 live opt-in。实际运行沿上方同一命令/依赖，只增加这两个选择值，并使用会话已配置的 `DEV_CHAT_BASE_URL`、`DEV_CHAT_MODEL`、`DEV_CHAT_API_KEY`。不要把 KEY 写进命令文本、README、日志或证据。

测试先核非秘密 URL/Model 元数据，再调用正式 `outbound.SystemResolver()` 对真实 hostname 完成原 A/AAAA 门；不发探针 HTTP。只有显式 live 且预检合格后，才读取配置 KEY 并写入本轮新数据库的正式 Secret。App 保留原 system resolver、系统 CA 与出站 policy；不注入 ownedIP、fixture CA、私网 allowrule、预设模型响应或合成凭据。Runner 使用原环境白名单，浏览器子进程继续剔除 `DEV_CHAT_*`。独立 owned outbound fixture 可能由官方测试栈一并创建，但 live Model 不消费它。

live Task 仅提供输入数据、目标和文件说明，真实模型自行生成程序、选择 read/write/run 及后继轮次。核验不锁固定程序、正文、轮数或 token 数；必须有至少两次工具交互并实际读取输入、写程序、运行和读取结果，文件 JSON 与真实 command stdout/exit 相互印证，原持久 Round/Call/Operation/Usage/最终 v2 可追溯。浏览器预期来自独立核验后的原持久工作 E，而非预设答案。评审仍只是 Owner 送审及可能出现的 receipt，不代表自主评审决策。

本轮部署总期限300秒，单 Model attempt 最多60秒，输出上限4096。work、review 与所有 retry 合计 `sent` 观察阈值12是含在途量的软停止条件，不能作为硬请求/计费上限。截止、阈值或预算观测失败会停止原 Runtime、以独立30秒期限 Drain 并取消原 App 根；原 Runner/App/连接实际 Join 仍是成功门，超限不得报完成。

准备检查不接真实配置，也不启 native 栈：

```sh
GOTOOLCHAIN=local GOENV=off GOWORK=off GOMAXPROCS=2 \
GOMODCACHE=/workspace/go/pkg/mod GOCACHE=/workspace/.cache/go-build \
GOPROXY=off GOSUMDB=off /workspace/toolchains/go1.27.1/bin/go test \
-tags=integration -p=2 -mod=readonly ./internal/central/app \
-run '^TestM1LivePreparation$' -count=1
```

该选择器只用 fake metadata/resolver 测拒绝与资格边界，不读取进程中真实 KEY/DEV_CHAT 值，不发 DNS/HTTP。准备门、真实运行门分别记录于 [live卡](../../docs/development/work-items/m1-live-acceptance.md)。当前外部 DNS 尚不合格，不能将编译/准备通过记为 live PASS。

## 证据

当前实际通过、失败与下一步见 [M1 集成卡](../../docs/development/work-items/m1-app-integration.md#当前成果与验收边界)。正常第十轮已完整通过三次原成果消费、唯一 Owner 送审、刷新一致及正常收尾；失败/取消与旧 direct-text 按各自固定输入接受并复用，不表示外部模型或 M1 整阶段完成。第九轮只记录 Playwright `ERR_ABORTED`、完整消费未在该轮证明的 FAIL，不推断 native 正文不完整。

每轮输出在 `output/ai/m1-app-integration/run.*`。`native.log` 保存实际测试及 fixture Wait；`owned-resources.json` 仅保存资源身份；`browser/TestM1AppToolWorkflow`（live为 `browser/TestM1AppLiveWorkflow`）保存浏览器阶段、脱敏控制台及截图。Session/Runner enrollment 材料放私有目录并清理，不写证据。本次交付的必要日志、资源身份、browser 消费证明/阶段与截图保全到主工作区 `/workspace/agenteam/output/ai/m1-delivery-evidence/app-integration/` 的原轮次目录，精确复制经 bytes/SHA 核对；不带二进制、依赖缓存或私有材料。

通过必须同时包含同一 Task/Execution 的实际多轮工具结果、文件内容与 command stdout/exit、v2 最终输出、唯一 Owner 评审转移、刷新一致，以及 App/Runner/浏览器原进程返回。编译、环境准备或受控 Provider 通过均不标外部模型推理通过。旧 STOP 驱动不由本入口运行。
