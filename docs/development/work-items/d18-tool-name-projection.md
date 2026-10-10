# D18 Tool 引用名称投影

状态：有限实施 SPEC，待独立关系审查；尚未编码或运行。基线为 main `3a7a3fb5`。本批仅交付内存中的确定性名称表，不完成 Registry、Tool Runtime 或 D18，也不解除既有停止项。

## 1. 正式依据与模块关系

依据 [D01 ToolSpec、Binding 与可信调用](d01-contracts/model-tool.md#toolspecbinding-与可信调用)、[ToolSpec revision](../../architecture/tool-system/tool-definition-registry.md#5-toolspec-revision)、[Execution Tool Set](../../architecture/tool-system/tool-definition-registry.md#6-execution-tool-set)和[模型名称投影](../../architecture/tool-system/tool-definition-registry.md#7-model-visible-tool-projection)。D01 已明确数据库身份为 UUIDv7 `tool_id`，`stable_key` 是另一字段；不可变定义由 `(tool_id, spec_revision)` 引用。模型名称必须在同一 Execution 内唯一、稳定并能准确反解，具体名称算法由本卡固定。

当前真实依赖只有 Foundation 的 UUIDv7/Version/Fault 和 [R1](d01-resource-identities.md) 的 `identity.ToolID`。Model contract 已有 `Tool`/`ToolCallPart`，其中 ModelRequest 最多 128 个 Tool；其 JSON 结构检查不证明 Runtime input/output schema 已验证。当前 [Agent C1/F1](d10-agent-configuration.md#2-结果边界与分段门槛)仅 C1 纯契约已实现，F1 的真实目录、初始化和引用保护仍未闭合。

本批由 D18 执行者独占 `internal/central/tool/contract/` 中的 SpecRef、`internal/central/tool/projection/` 中的名称表、相邻测试及本卡；不改 Agent、Model、Runner、Foundation 或 Identity。两个新包只向下依赖 Foundation/Identity 和本域 contract，不 import Agent、Model、Runner、Governance 或 Executor。Agent contract 不能为消费名称表而反向依赖层 5 的 Tool 实现。

后继调用与责任如下；箭头不表示这些接缝已经实现：

```mermaid
flowchart LR
  Registry["D18 真实 Registry / immutable revisions"] --> Selection["D22 当前 Capability / Policy 与 Snapshot"]
  Selection --> Names["本批：纯 NameTable"]
  Names --> Model["D09 Model Tool 名称投影"]
  Model --> Calls["D22 原成功 terminal / ToolCall"]
  Calls --> Resolve["原 Snapshot 的 NameTable 反解"]
  Resolve --> Runtime["后继 D18 Schema / Operation / Attempt"]
  Runtime --> Auth["D19 当前授权 / Approval"]
  Auth --> Adapters["D21 Builtin / Runner 或 D20 MCP"]
  Adapters --> Runner["D15 transport → D16 / D17"]
```

D18 负责真实 Registry、immutable spec、Scope/引用保护和 Runtime；D22 负责将实际选择及名称表绑定到唯一 Execution Snapshot。D09 负责 Provider-specific projection 和 Model attempt terminal，D19 负责当前授权及持久人工等待，D20 负责实际 MCP 定义和连接，D21 负责正式领域/Runner 适配。D16 可独立推进本地文件库，不等待本名称表，也不因本名称表存在而绑定生产 executor。

## 2. 精确输入与接口

本批只定义以下 Go 内存接口；不增加 wire/schema、HTTP、数据库或持久化格式：

```go
// internal/central/tool/contract
type SpecRef struct {
    ToolID       identity.ToolID
    SpecRevision foundation.Version
}
func (SpecRef) Validate() error

// internal/central/tool/projection
const MaxTools = 128
type Entry struct {
    Reference        contract.SpecRef
    ModelVisibleName string
}
type NameTable struct { /* 私有不可变状态 */ }

func BuildNameTable(ctx context.Context, refs []contract.SpecRef) (*NameTable, error)
func (*NameTable) Entries(ctx context.Context) ([]Entry, error)
func (*NameTable) Resolve(ctx context.Context, name string) (contract.SpecRef, error)
```

SpecRef 复用唯一 ToolID marker，不声明第二种 Tool identity。ToolID 必须通过原 UUIDv7 校验；SpecRevision 为原正 int64 Version，不转浮点、不改为 hash、也不据数值推断历史 revision 存在。

构造接受 0..128 项。nil slice 和空 slice 都构造合法的空表，表示本次调用方提供了零个引用。任意非法引用或重复 ToolID 均拒绝整个输入；同 ID 同 revision 和同 ID 不同 revision 都算重复，不静默合并、不选择最大 revision。

成功构造的表只含输入的值副本。Entries 返回新 slice，按模型名称的 ASCII 字节升序排列；修改输入或返回 slice 不得改变表。合法空表的 Entries 成功返回非 nil 的空 slice，Resolve 返回 NotFound。nil `*NameTable` 或零值未构造表则返回 DependencyUnbound；不能把未构造状态当合法 no-tools 选择。

## 3. 固定名称算法与反解

名称唯一算法为 `tool_` 加 ToolID 的 canonical 小写 UUIDv7 去掉四个连字符，总长恰好 37 ASCII 字节。例如：

```text
01902e15-1000-7000-8000-000000000001
→ tool_01902e15100070008000000000000001
```

前缀、大小写和连接规则固定。名称不取 Tool display name、stable_key、MCP remote name、Backend 状态或输入顺序；revision 不进入名称。不同合法 ToolID 对应不同名称，不以截断/hash 碰撞分配或后缀重试生成名字。

Resolve 只在该表内按完整字节查找；不 trim、case-fold、Unicode 规范化、URL decode、前缀匹配或解析 UUID 后回退到外部 Registry。空串、错误大小写、格式近似、未收录 ToolID、非法 UTF-8 或任意其它未匹配名称均返回相同安全 NotFound；不得回显请求名称。长度不等于 37 时直接拒绝，避免扫描或分配任意长输入。

同一 ToolID 的新旧 revision 表会产生相同名称，但分别返回自己的原 revision。**不能拿新表解析旧 Snapshot 的 ToolCall。** 本库只有名称和输入引用，无法证明 ToolCall 属于哪个 Execution/Snapshot；后继 D22 必须按原可信 Snapshot/模型 invocation 关联选择表，D18 再核实际 Registry/绑定和授权。这一责任不能通过名称相同、表构造成功或新 revision 更大而省略。

当前 37 字节 ASCII 名称只满足本地工程约束，不证明任一 Provider 的完整 Tool Schema/conformance，也不恢复 OpenAI tools 独立验收停止项。

## 4. 取消、失败和安全输出

所有名称表方法同步执行，只处理最多 128 个引用。先检查 context，构造和复制逐 entry 检查，在成功返回前再次检查；排序前后也检查。nil context 为 InvalidArgument，原 `ctx.Err()` 保留 `errors.Is` 判断。输入及表均无需 I/O，没有后台 goroutine、timer、内部 deadline、Stop/Drain 或资源 Join；不以纯函数完成证明任何 Backend 已退出。

领域错误固定为 Foundation Fault，CommitState 均为 NotStarted：

| 触发 | Code |
| --- | --- |
| nil context、非法 ToolID/version、重复 ToolID | InvalidArgument |
| 构造输入超过 128 项 | PayloadTooLarge |
| nil/零值未构造名称表 | DependencyUnbound |
| 已构造表中没有完整匹配名称 | NotFound |

错误检查顺序为 context → 本方法表状态或输入长度 → 逐项引用验证/查找。任何失败或取消均返回 nil 表/nil entries/零 SpecRef，不返回前缀或部分表。Fault 不附未知名称、ID 列表、原始输入、cause 文本或业务字段；fmt/JSON/slog 的错误输出只能包含固定安全投影。NameTable 的默认格式化/日志为固定标签，明确 Entries 才取得本表公开映射。

构造后没有变更方法，允许并发调用 Entries/Resolve；调用方仍不得在构造读取 refs 时并发修改该输入 slice。名称表不是授权凭据，不证明 Tool 当前注册、scope/current Mount 合法、Agent 有权执行或 Provider 已成功。

## 5. 未实施边界与后继接入

本批不创建 Registry/ToolSpec 数据、stable_key parser、ToolBinding、Operation/Attempt ID 或状态、fingerprint、授权/审批、MCP SDK、Schema validator、参数 normalization、ToolResult、模型调用、Backend dispatcher、真实 Snapshot 或恢复。

不修改 Model Tool DTO、不注入统一 timeout，也不读取或改写模型 arguments。D09/D18/D20 的 JSON Schema dialect、标准实现及 conformance 未在本批冻结；不能把现有 objectJSON 结构检查、自写 Schema 子集或空 schema 当通过。后继真实 Runtime 必须先完成参数/结果 Schema 检查、Operation/Attempt 和当前授权门禁；人工 waiting 不被普通 timeout 结束，取消或 unknown 不证明远端没有副作用。

尚缺的真实 Agent F1、Registry 引用保护、Mount 目录与 D18–D22 接线仍由各域 owner 完成。本批不解除 Tools/Object/SPA 或其它既有 STOP，不声称完整 D18 或可执行 Tool 已交付。

## 6. 必要验证与当前状态

首批仅相邻纯测试：手算 ID→名称与最大 Version、输入乱序的确定性、0/128/129 项、同 ID 同/不同 revision 拒绝、非法 ID/version、空表与零值区别、大小写/近似/未注册名称不回退、旧表保持旧 revision、输入及 Entries copy 隔离、并发只读、取消零结果和未知名称 canary 安全错误。取消测试检验中途及返回检查，不依靠睡眠或外部服务。避免把引用列表纯验证当实际 Registry/授权测试。

SPEC 独立关系审后再实施。限定本包 pure/race/vet 一轮及源码静审；不启动 PG/socket/browser/Provider、不重跑 D16 或未变 Model/Agent 矩阵。首次 Go 前使用固定 Go1.27.1、同进程 fresh≥5 GiB、任务私有 telemetry off/去旁路、自有 cache 与只读模块 cache；命令实际 Wait 并记录私有运行目录正常退役。

当前仅本卡落盘，未编码、编译或运行。后续结果按实际输入记录，不将规格接受写成运行验收。
