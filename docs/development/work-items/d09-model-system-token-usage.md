# D09 Model System 与 Token Usage

修订 1，2026-10-05。归位基线 `a4b728bf2cc5f38fb0c626c4cd793456f3b35b90`；设计基线及来源见[工程规格修订 2](d09-model-system-token-usage-design.md)。状态：**设计部分采纳；B01 纯契约候选尚未实施；完整 D09 未开工、未完成。** 本卡不是源码、迁移、网络实验或真实 Provider 调用授权。

依据：[开发计划 D09](../development-plan.md)、[D01 模型契约](d01-contracts/model-tool.md)、[D08 规格](d08-project-owner-design.md)。必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)；实施/独立验收按团队角色与各自技能执行。root 持本卡、台账、计划、迁移编号及最终 Git 交付。

## 已采纳与未满足

- 设计修订 2 的 R01（精确 Invocation planned read）、R02（实际 PurposeModel+execution/model_call 的 legacy wrapper 拒绝、保 MCP）、C01（immutable serving snapshot 与新 call lease 分层）已独立定向通过。当前只归位规格，不声称 Secret 或 Model 已实现这些增量。
- OpenAI/Anthropic 固定官方 SDK 仅证明采用字段及 usage 口径；不引入 SDK 运行依赖，不证明真实 Provider、账号权限或型号矩阵。Jina、完整 schema/型号 conformance 保持未闭合。
- Summary 初值仍待用户决定；不新增 Project 创建输入、默认值或用 NULL 宣称必填已满足。配置/初始化/完整 D09 验收受此约束，纯类型不代决。
- 已验 D08 B02 `6319d03` 的 Human Owner/gate、创建/恢复可作为后续真实依赖；B03 lifecycle/Service validators/参与者适配尚须其正式验收。D10/D13/D14/D19/D21/D22/D24 各自绑定真实引用、serving generation、consumer/cause/输入/死亡或终局事实；不得用成功 stub、跨域 SQL 或通用 Owner 豁免替代。

## B01 纯契约候选范围

这是工程规格 §1 宽 B01 的**首阶段候选**，只形成可编译、可验证的类型/闭集与规划绑定；不等于配置/Usage 存储结果已经交付。下表为本阶段唯一候选新源白名单，各路径相对 `internal/central/`，同时间仅一位实现者写这些共享契约；root 冻结具体 Go 形状并发出源码任务后才实施。

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

不写现有文件、Secret 新读实现/旧 RequestID 载体、Actor/Audit 注册、DB 表/迁移、Model/Usage 服务、Provider adapter、HTTP/app、脚本、依赖或未来域实现。工程规格 §1 的这些后续增量继续逐项授权；纯 interface 无实现不是生产已绑定。出现白名单之外的必要代码时先报告，不能用空结构/静默成功来绕过依赖。

## 开工前最小工程形状

设计已固定语义，但尚未把以下所有名字展开为完整 Go 公共字段/构造器；因此本卡保留“候选”，不让实现者在多个模块内各猜一份接口。这些属于工程定型，不重新询问已定产品规则：

1. 固定 `ConsumerRequest` 各 action 的必填/禁带字段、各 opaque Plan 的 issuer/request/mapping/复制锁投影与构造/匹配签名；包括 prospective 同 Tx 事实、exact Invocation 与 serving source 的字段归属。复用现有 D03/Secret/D05 模式，不新增通用授权框架。
2. 将配置命令、`SelectionRef`、`ReferenceChange`/replacement、nonchat 请求和 Usage query/result 的剩余 Go 载体列齐；只验证平台已定闭集/范围，不把未核 Jina/型号能力包装成合法 native 参数。需要未答 Summary 初值的初始化/创建载体暂缓。
3. 新 Secret `CredentialUsageReader` 及 RequestID 语义已定，但 `UsageRequest` 是旧公共文件；其载体兼容与实际 wrapper/planned read 为后续受权块，不在上述纯新源白名单偷偷修改。本阶段不得声称 R01/R02 已运行；Model 的跨域依赖声明须明确引用这个待实现正式口。

允许先仅在 `/tmp` 给出上述最小 typed 候选并独立复核；通过后 root 发精确源码卡。不得因此重复改写产品架构或整份工程规格。

## 固定编译依赖与验收

- 编译基线固定 `a4b728bf2cc5f38fb0c626c4cd793456f3b35b90`；Go 精确 1.27.1，`GOTOOLCHAIN=local`、`-mod=readonly`。`go.mod`/`go.sum` 不变，不安装 Provider SDK。开工/交付记录真实 `go list` 编译闭包指纹；本次文档归位未执行编译，不把候选列表称实际编译闭包。
- 既有只读依赖限 `internal/central/foundation`、`identity/contract`、`secret/contract`、`object/contract`，事件如需复用仅 `event/contract`；及其该固定提交的传递闭包/标准库。Usage 可引用本次 model contract，反向不得依赖 Usage 实现。禁止导入 Tool/Execution 或任何 Central 实现包。新依赖或旧源差异须显式核定，不能改工作树依赖救编译。
- 有意义纯测试覆盖工程规格 T01、T06/T07 的可静态验证部分：闭集/必填/禁带组合、稳定请求 binding、计划 issuer/mapping/复制切片、防反序列化造权限、nullable 与 0、>2^53 的 int64 精确编码及溢出拒绝、安全格式化不带材料。无源码时不编造 PASS；真实锁/Secret/Unknown/provider/join 留到相应运行块。
- 实施后执行以下命令，并由未参与实现的审查者验证公共形状、拒绝分支、包分层和真实结果。纯契约不需要 Docker/PG/MinIO/模型 API，不占并行验收资源。

```sh
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -race ./internal/central/model/contract ./internal/central/usage/contract
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go vet -mod=readonly ./internal/central/model/contract ./internal/central/usage/contract
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go build -mod=readonly ./internal/central/model/contract ./internal/central/usage/contract
```

B01 首阶段通过仅表示类型与契约可供下游编译；完整 B01 尚需实际配置/用量原子存储及集成，B02/B03 仍依工程规格验收。未满足的 Summary/协议/生命周期/真实 consumer 条件不因纯契约提交而自动解除。
