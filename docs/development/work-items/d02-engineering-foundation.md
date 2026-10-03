# D02 工程与程序生命周期

- 修订：1；状态：B01 已验收，B02 待开始；基线：`main@bb89da2`，与 origin/main 一致，开工时工作区干净。
- 前置：[D01 规格](d01-cross-module-contracts.md)已完整通过静态接口走查并推送；契约入口：[D01](d01-contracts/README.md)。
- 目标：按[开发计划 D02](../development-plan.md#d02-工程与程序生命周期)建立根 Go module、Central/Runner 独立入口/装配、部署配置校验、结构化日志/关联 ID、net/http 错误边界和可靠取消/停机；保持前端独立。
- 当前没有生产 Go 文件、数据库迁移或后端构建。环境候选已执行 `/workspace/toolchains/go1.27.1/bin/go version` 与 `go env GOOS GOARCH GOTOOLCHAIN`：Go 1.27.1、linux/amd64、auto；Node v24.19.0、npm 11.9.0。编译兼容与真实进程行为仍待本模块验证。

## 实施边界与任务

| 卡 | 依赖 | 执行者 | 独占文件/资源 | 状态 |
| --- | --- | --- | --- | --- |
| S01 可执行工程规格 | D01 | architecture_worker | 新增 `d02-foundation-design.md`，其余只读 | 已完成，主线程确认 |
| B01 基础类型与 HTTP 边界 | S01 主线程确认 | backend_worker | `go.mod`、foundation/httpapi 目录及测试、`api/openapi/common.json`、central 占位，详见实施规格 | 已验收 |
| B02 配置、入口、生命周期及开发文档 | B01 验收提交 | backend_worker | 按实施规格 §1 独占入口/config/app/platform、scripts/process 测试及必要文档 | 待开始 |
| V01/V02 独立验证与整合 | 对应实现冻结 | verification_worker / 主线程 | 只读实现及运行隔离测试，主线程维护本规格/台账/提交 | 待开始 |

S01 必读 AGENTS、团队流程、agenteam-design 技能、D01 foundation/依赖目录、部署运行与 Runner Management 相关范围；B01/B02 使用 agenteam-go-development；验证使用 agenteam-verification。不得再委派或执行 Git 写操作。

## S01 要求

工程细节在已有授权内确定，规格保持简洁，只覆盖 D02。允许初始化根 module `github.com/LunaDeerTech/agenteam`，Go 1.27.1 作为首选候选，须有实际编译/测试证据再固定；本模块优先只用标准库，无需提前引入 PG/MinIO/WebSocket/业务包。

明确 B01/B02 的独立可验证交付、文件所有权、配置字段/默认/错误脱敏、HTTP 路由/Problem/request_id/JSON 安全边界、日志输出、signal/drain/force-close 与验证命令。公共标量按 D01 实现，暂不使用的业务端口不造空包；后续能力须明确绑定责任。Runner 不 import Central，必要纯技术基础在明确中立目录，不能借共享 lifecycle 包搬业务。

生产装配不得用成功 stub 代替后续数据库/对象/身份/Runner protocol。基础二进制可以明确非 ready 并只提供诊断；必须严格区分构建/配置正确、进程活着、完整产品 ready 和 Runner 已连接/已认证，不冒充生产能力。D03/D04/D05/D15 尚未实现的配置/能力不伪造校验通过，也不提前实现它们。

实际检查至少 `go test ./...`、`go vet ./...`、两个二进制 `go build`；涉及并发/取消的范围使用 race 和真实子进程 SIGTERM/SIGINT、监听冲突/无效配置等验证。不得使用现有业务数据库或环境凭据。测试端口使用本机 loopback 临时端口，产物到被忽略目录或临时目录；不覆盖其他服务。纯工程/后端变化不无条件跑前端全量测试，验证未变前端边界即可。

通过小块验收后及时提交推送，不等模块全部内容累计提交；当前模块全部通过后才推进 D03。规格、文档与任务台账记录真实检查/指纹/未验证范围。

## S01 确认与 B01 开工

主线程已审查并确认[实施规格](d02-foundation-design.md)修订 1，无未决产品问题。S01 原始冻结 SHA-256 为 `ae2c9448aabefe738f7a096cf98715268aae33fdd62d912665e968b29926f1af`；6 链接/1 fragment/格式检查通过。工具链已真实编译运行临时 net/http + encoding/json 程序，exit 0；这不代替仓库测试。B01 按 F01–F04 完成后冻结，交独立 V01；主线程维护规格/台账，实施者不写这些文件。

B01 工程校正：显式 Go 1.27.1 首次测试提示 go.mod 需更新，`go mod tidy -diff` 唯一变化为删除与 go 行相同的冗余 toolchain 指令。主线程采纳，仍固定 `go 1.27.1`、检查精确 GOVERSION 并使用 GOTOOLCHAIN=local；没有依赖或版本变更。

## V01a 分段验证记录（已完成）

B01 作者冻结 `go.mod` 与 foundation 8 文件，manifest SHA-256 `5dc45490468572ec865b37d7479ecb31ada0d3f4b973a349e0fd2e04a427c0f6`；显式 Go 1.27.1/local 的该包 test/race/vet/build 自测通过。HTTP 仍由作者独占实现，不在本次冻结范围。独立验证已确认以下需返修项，不构成 B01 验收：

- `Fault` 直接格式化安全，但被嵌入外层未导出字段时，fmt 反射路径和 slog.TextHandler 可暴露私有原始 cause。须改变内部存储并覆盖值、指针、error 接口与多种格式动词的回归。
- Go 1.27.1 对同时值/指针实现 error 的 Fault 包装 `%w` 给出 vet 错误；作者和独立 verifier 都已复现。采用仅指针实现 Error，保留值/指针安全格式化、JSON 和日志投影。
- 不同 ID marker 的赋值、参数传递和比较均被编译器拒绝；显式转换属于主动重标记，不要求在此基础类型阻止。

返修在独立审查完成并移交后进行；修改后重新冻结、重跑受影响检查。

### B01 全量冻结与 V01b

V01a 已结束，除上述两项外，冻结标量原有 6 项测试和独立 5 项边界测试在隔离副本中 race 通过；UUID 时间/熵失败、整数精度、解码失败保持原值、时间极值和 cause 身份/复制隔离通过。独立复现目录 `/tmp/agenteam-d02-v01a-9f1xp1_q`，初末指纹一致。

作者已修复两项 Fault 问题，新增嵌套私有字段/fmt/JSON/slog、errors.Is/As 回归。B01 19 个新增文件与删除 central 占位已冻结，manifest `/tmp/agenteam-b01-final.sha256` 的 SHA-256 为 `55dea781a3812416f0f63d25f191e80a0dc339b17963973681a7779af0a174eb`。作者实际以 Go 1.27.1/local 执行正常 `go test ./...`、`go vet ./...`、`go test -race ./...`、`go build ./internal/...`、`go mod tidy -diff`，均 exit 0；最后 schema 尾换行正则校正后定向 HTTP test 通过。jsonschema 4.26.0 校验 17 schema、18 正样例和 10 反样例通过。所有源码格式及 whitespace 检查通过。此为作者证据，V01b 正在独立复核全部 F01–F04 与 schema，尚未整体验收。

### B01 完成与提交

V01b 独立复核两项 Fault 修复通过，foundation/HTTP race 与原 `%w` vet 探针通过；额外真实 HTTP/2 Flush、不支持 Hijack、部分响应 panic 中止与脱敏通过，真实 HTTP 9 类 JSON 边界通过；重定向仅记录模板或 unknown_route。两类 int64 schema 各 10,066 个差分样例通过。唯一新发现为 FieldError.path 对仅换行的 schema 接受差异，主线程小范围增加空字符串或 `/` 前缀约束，保留合法路径内换行；定向 Go schema test 与 jsonschema 9 边界通过。V01c 独立 13 边界通过，确认其余 18 文件不变，复用 V01b 其余证据，B01 无剩余阻塞。

最终 19 文件 manifest SHA-256 `350f43cc6faf7f6bed8fe9fbec049ad3822b3068fd24c773f09f82ae579c5fd2`，另删除 central 占位；相关作者/验证者均停止写入和命令。主线程已审查关键代码与真实证据，B01 验收通过。提交范围为上述库/schema/module 和 D02 两份规格/台账；按已有授权提交推送，通过本节 Git 历史定位。B02 尚未实施，D02 未整体验收；数据库/身份/对象/协议/真实业务仍未绑定。下一步 B02 实现真实入口、配置、诊断与信号停机，按 P01–P05 完整验收。
