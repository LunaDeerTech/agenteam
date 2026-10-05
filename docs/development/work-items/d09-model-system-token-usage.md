# D09 Model System 与 Token Usage

修订 4，2026-10-05 进度同步。C0 固定编译基线 `16595ad1e78e5283dfe85fb812095acde382edd1`，已独立验收并提交 `e6e94c4`；设计基线及来源见[工程规格修订 4](d09-model-system-token-usage-design.md)。状态：**B01/C0、[B01-K System 配置原子存储](d09-b01-system-configuration.md)及 [Project chat 配置与安全目录窄块](recovery-d09-project-configuration.md)均已独立验收；Audit 六源 `26622bc`、System 二十九源（含 00015）`543511c`、Project 配置二十一源 `de00c610` 已提交推送。完整 D09 未完成。** 下列 C0/B01-K 阶段记录保留当时事实；后续 Project 配置、Secret planned usage、OpenAI text/structured wire、System 配置生产根及 [current-selection Resolver 库](../agent-team/current-model-resolution-verification.md)已有各自独立验收。Resolver 精确 20 路径与 00017 已以 `4295df7` 提交推送；生产 Resolution 仍 nil，Invocation/Usage、真实 consumer/Provider 调用及 Project HTTP/root 未交付，Summary 初值/Settings 仍待定。

依据：[开发计划 D09](../development-plan.md)、[D01 模型契约](d01-contracts/model-tool.md)、[D08 规格](d08-project-owner-design.md)。必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)；实施/独立验收按团队角色与各自技能执行。root 持本卡、台账、计划、迁移编号及最终 Git 交付。

## 已采纳与未满足

- 设计修订 2 的 R01（精确 Invocation planned read）、R02（实际 PurposeModel+execution/model_call 的 legacy wrapper 拒绝、保 MCP）、C01（immutable serving snapshot 与新 call lease 分层）已独立定向通过。C0 只落实纯载体，不声称 Secret planned read、旧 wrapper 拒绝或 Model 运行服务已实现。
- OpenAI/Anthropic 固定官方 SDK 仅证明采用字段及 usage 口径；不引入 SDK 运行依赖，不证明真实 Provider、账号权限或型号矩阵。Jina、完整 schema/型号 conformance 保持未闭合。
- Summary 初值仍待用户决定；不新增 Project 创建输入、默认值或用 NULL 宣称必填已满足。Project summary 配置/初始化及完整 D09 验收受此约束；System 配置首块不代决、不依赖该初值。
- 已验 D08 B02 `6319d03` 的 Human Owner/gate、创建/恢复可作为后续真实依赖；B03 lifecycle/Service validators/参与者适配尚须其正式验收。D10/D13/D14/D19/D21/D22/D24 各自绑定真实引用、serving generation、consumer/cause/输入/死亡或终局事实；不得用成功 stub、跨域 SQL 或通用 Owner 豁免替代。

## B01/C0 已验纯契约范围

这是工程规格 §1 宽 B01 的**首阶段 C0**，只形成可编译、可验证的类型/闭集与规划绑定；不等于配置/Usage 存储结果已经交付。下表保留 C0 已验白名单，各路径相对 `internal/central/`，合计 10 个生产源和 10 个相邻测试；原实现单作者为 architecture_worker，本轮不解冻这些源。

| 生产新源 | 仅对应的新测试 | 职责 |
| --- | --- | --- |
| `model/contract/types.go` | `model/contract/types_test.go` | typed IDs、consumer/purpose、snapshot 投影、能力/错误/Usage 安全类型 |
| `model/contract/configuration.go` | `model/contract/configuration_test.go` | Provider/Model/selector DTO 与已定结构约束；不假造逐型号 wire 能力 |
| `model/contract/resolution.go` | `model/contract/resolution_test.go` | current_selection/serving_snapshot 闭集、Resolve 请求/结果与 opaque 计划 |
| `model/contract/chat.go` | `model/contract/chat_test.go` | ModelMessage/Part/Tool、请求/响应/Frame/Stream 正式形状 |
| `model/contract/nonchat.go` | `model/contract/nonchat_test.go` | embedding/rerank/image 的独立平台 DTO/端口；不实现 Provider 参数协议 |
| `model/contract/authority.go` | `model/contract/authority_test.go` | ConsumerRequest、Discover/Validate 与计划的完整身份/映射/锁绑定 |
| `model/contract/references.go` | `model/contract/references_test.go` | 正式 reference change/replacement 的请求、计划及安全结果 |
| `model/contract/events.go` | `model/contract/events_test.go` | 已定 model 配置事件的 typed 安全 payload/版本；不注册 handler |
| `usage/contract/types.go` | `usage/contract/types_test.go` | Invocation/Usage/summary 的 nullable 数值与来源、真实 attempt 身份 |
| `usage/contract/query.go` | `usage/contract/query_test.go` | Owner 查询/筛选/分页/统计的安全 DTO/端口，cursor 不作权限 |

C0 阶段除本卡与工程规格外不写现有文件；不写 Secret 新读实现/旧 RequestID 载体、Actor/Audit 注册、DB 表/迁移、Model/Usage 服务、Provider adapter、HTTP/app、脚本、依赖或未来域实现。后续 B01-K 仅按[实施卡](d09-b01-system-configuration.md)列明的 System 配置范围另行授权；工程规格 §1 的其他后续增量继续逐项授权；纯 interface 无实现不是生产已绑定。出现白名单之外的必要代码时先报告，不能用空结构/静默成功来绕过依赖。

## 已采纳的精确 Go 形状

独立静态采纳的 C0 声明稿 SHA256 为 `bec826d7cef677352e36c8c3113f541c8a237b48044084d8a960d203ad0328eb`，已按其 8+2 文件职责落源码并独立验收；精确字段/指针/构造器及接口以工程规格 §1.1 链接的 Go 声明为准，不由下游再猜一套载体。

- `ConsumerRequest` 五 action 明确必填/禁带；exact Invocation/Process/fence/Input/lease 绑定不从“最新 call”推断。current/serving 分支互斥，旧配置身份与新 call lease 分离。
- 四 concrete opaque plan 提供 `Validate/Details/RequiredLocks/Matches`，实例 issuer 与完整 Actor（含 Session）/request/mapping 绑定；复制锁集/可变载体、拒 JSON 造计划。结构有效不代表持锁、当前授权、提交或 actual join；命令持久摘要仍另按稳定 Actor 主体排除 Session。
- 平台 DTO 校验已定结构/闭集/范围；不以合法 JSON 宣称 profile/型号支持。Usage 只返回历史身份，不携 System endpoint/overwrite/SecretRef；nil、零及 overflow 保持区别。
- Secret 新 `CredentialUsageReader`、旧 `UsageRequest.RequestID` 和实际 wrapper/planned read 不在本阶段；Summary 初始配置、Jina/型号规则、未来 consumer/reference/lifecycle 绑定继续暂缓，不新增空结构或默认成功。

## 固定编译依赖与验收

- 编译基线固定 `16595ad1e78e5283dfe85fb812095acde382edd1`；Go 精确 1.27.1，`GOTOOLCHAIN=local`、`-mod=readonly`。`go.mod`/`go.sum` 不变，不安装 Provider SDK。在该提交的隔离副本叠加本轮 20 新源执行验证；记录真实 `go list` 编译闭包及日志，不消费活动 D08/D05/00014 工作树。
- 既有只读依赖限 `internal/central/foundation`、`identity/contract`、`secret/contract`、`object/contract`，事件如需复用仅 `event/contract`；及其该固定提交的传递闭包/标准库。Usage 可引用本次 model contract，反向不得依赖 Usage 实现。禁止导入 Tool/Execution 或任何 Central 实现包。新依赖或旧源差异须显式核定，不能改工作树依赖救编译。
- 有意义纯测试覆盖工程规格 T01、T06/T07 的可静态验证部分：闭集/必填/禁带组合、稳定请求 binding、计划 issuer/mapping/复制切片、防反序列化造权限、nullable 与 0、>2^53 的 int64 精确编码及溢出拒绝、安全格式化不带材料。无源码时不编造 PASS；真实锁/Secret/Unknown/provider/join 留到相应运行块。
- 实施后执行以下命令，并由未参与实现的审查者验证公共形状、拒绝分支、包分层和真实结果。纯契约不需要 Docker/PG/MinIO/模型 API，不占并行验收资源。

```sh
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -count=1 ./internal/central/model/contract ./internal/central/usage/contract
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -race -count=1 ./internal/central/model/contract ./internal/central/usage/contract
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go vet -mod=readonly ./internal/central/model/contract ./internal/central/usage/contract
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go build -mod=readonly ./internal/central/model/contract ./internal/central/usage/contract
```

C0 首阶段通过仅表示类型与契约可供下游编译；现 [B01-K System 配置](d09-b01-system-configuration.md)已补齐真实 System 原子存储结果。作者 12 个真实顶层及独立跨 scope CredentialRef/receipt 提交后 Session 撤销 1 个顶层通过，独立报告 SHA `171c97cdb329e30dd08ccfc8db26e5192fe40b46400b92d560184644933da9b8`，位置及原始失败见 B01-K 卡。这是当时 B01-K 的验收范围；后续 Project 配置与其他受限结果见文末，Usage 及完整 B02/B03 仍依工程规格验收。Summary/协议/生命周期/真实 consumer 条件不因该库提交而自动解除。

## Project 配置窄块验收接续

[恢复卡修订 2](recovery-d09-project-configuration.md)仅同步已验状态，API 与产品规则保持原修订。Project chat Provider/Model CRUD、当前配置读、命令查证及安全目录以 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5` 提交推送；作者第二轮 43 顶层/117 子例、独立 2 顶层/5 子例通过。[正式报告及持久证据](../agent-team/d09-project-configuration-verification.md)已随 `965da5d` 提交；首轮 FAIL、5 个未变新项的非 verbose 未失败结果复用、早期纯检查未单独持久化 exact argv/exit 的限制均保留，不改写成单次全绿。

Summary 初值仍待用户决定，不新增默认值、NULL 完成声明或 Project 创建输入。Project 配置验收不自动交付真实 Agent/project_summary 引用替换、Invocation/Usage、Provider 调用或 Project HTTP/root；其后已验结果如下，完整 D09 继续未完成。

## 后继受限结果与当前边界

[Secret Model planned usage](../agent-team/d09-secret-model-usage-verification.md) `8ad6759`、OpenAI text wire `9c72190` 与 structured wire `be0bd07` 已验；wire 的受控 server 结果不等于实际供应商账号 smoke 或 Runtime。System 配置默认生产根 `457b197` 的[正式报告](../agent-team/system-model-root-verification.md)已验真实 Session/admin、Secret/Audit/Outbox 与同步 HTTP；这不解除 Project 或调用链的未绑定边界。

[current Model Resolution 库](recovery-d09-current-model-resolution.md)已独立 PASS，以 `4295df7d51c1f171df78ab3f0d9cef2fd241a505` 提交推送。测试输入为固定 `be0bd07` 加精确 20 路径；作者分版本 26 顶层/109 子例、独立 2 顶层/4 子例与原失败见[正式报告](../agent-team/current-model-resolution-verification.md)。00017、direct/platform.memory 当前选择、不可变 snapshot/binding 与同 Tx Secret planned Acquire 已验；默认生产根 Resolution=nil，严格 fixture 不代表实际 AgentRun/Memory/Tool consumer 已绑定。serving_snapshot、材料读取/退休、Invocation/Usage、Provider 发送和 Project cleanup 未交付，Summary 待决与 ready503 保持。
