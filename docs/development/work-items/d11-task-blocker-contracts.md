# D11 Task Blocker B0-C 纯契约工程规格

> 状态：工程规格已独立接受；两文件纯契约实现已授权并进行中，产品与运行验收尚未完成。
>
> 上游：[Task transition 工程规格](d11-task-transitions.md) §4.2、§6.1、§10–13；[Task Blocker / Dependency](../../architecture/project-work-management/task-blocker-dependency.md)、[Task Timeline](../../architecture/project-work-management/task-event-timeline.md)。T0a 状态规则与新 Position 已交付；本卡只闭合两类 Blocker 纯类型，不表示 B0-P、T0b 或运行服务完成。

## 1. 完整结果与文件边界

本结果唯一定义 Work 的 `TaskBlockerID`，实现 `rely_on{related_task_id}` 与无外域引用的 `waiting_for_human{}` 两个 typed metadata 分支、`TaskBlockerCreate`、相应 added/resolved 小 payload，以及严格验证、codec、Clone、大小限制和直接安全日志投影。它可被后继 T0b 使用，但不包含完整 TaskTransfer、摘要、TaskTransitionEvent 封套或 Outbox 工厂。

实际依赖只有既有 [Foundation ID/Fault](../../../internal/central/foundation/id.go)、[Work TaskID](../../../internal/central/work/contract/task.go) 与 [Work 严格 JSON helper](../../../internal/central/work/contract/structure.go)。`identity.ProjectID/AgentID/ExecutionID` 已在 [identity/contract](../../../internal/central/identity/contract/identity.go) 唯一定义，`project.MeetingID` 已在 [project/contract](../../../internal/central/project/contract/types.go) 定义；不在 Work 重造这些 marker。仅有身份类型不证明对应对象、当前归属或授权存在。

后续实施拟且仅新增 `internal/central/work/contract/task_blockers.go`、`internal/central/work/contract/task_blockers_test.go`。独立 SPEC 接受后由主线程另授源码写域；开工重新核对路径与导出名，发生冲突先协调。无需修改 Foundation、identity、Project、既有 Work source/test、go.mod 或依赖锁；任何必须扩大该闭集的发现先升级，不通过放宽旧 helper 或 schema 绕过。

不声明 Blocker 完整持久 record、Repository/Service 接口、默认 provider、TaskBlocker 状态或权限模型；不占迁移编号，不改表，不生成事件，不增加 HTTP/Tool/Service 注册。所有函数均为有界内存纯操作，无 ctx、时钟、随机 ID 生成、锁、网络、数据库或全局可变状态。

## 2. 类型覆盖与未绑定分支

`TaskBlockerType` 识别架构的五个 canonical 枚举；枚举识别与可构造 typed 内容是不同检查。仅前两类在本结果中允许成功构造 Create/payload。

| type / 请求 | 本卡结果 | 后继仍需的事实或定义 |
| --- | --- | --- |
| `rely_on` | metadata 精确 `{related_task_id:<TaskID>}` | B0-P 的当前 related Task、同 Project、非自身、完整 unresolved 图与无环证明 |
| 无引用 `waiting_for_human` | metadata 精确 `{}` | B0-P 的真实 Blocker/Task 写入、权限与不变量；本卡不产生 Inbox projection |
| 带外域引用的 `waiting_for_human` | 不支持非空 metadata；reference 字段仍按未知字段拒绝，不丢弃后降为 `{}` | 引用 owner 冻结真正 typed union、权责/查询/Inbox 去重；不在此预定义 `reference_type/reference_id` schema |
| `waiting_for_meeting_approval` | 识别 type，完整 Create/payload 返回 `DEPENDENCY_UNBOUND` | D24/D19 的 Meeting/Approval reference 结构与真实来源；现有 MeetingID 不填补该缺口 |
| `technical` | 同上 | D22/D23 的 code/source/cause 允许来源、safe summary 及真实内部权限 |
| `user_cancelled_execution` | 同上 | D22/D23 的执行归属、用户停止证明、cancelled_by/reason 结构与停止组合；复用现有 ExecutionID |

未知字符串、大小写变体及空 type 为 `INVALID_ARGUMENT`。已知但未绑定的三类不能映射为 waiting_for_human、返回空 metadata 成功或进入历史 payload。未来需要带引用的人类待办时，调用方不得请求本卡的无引用分支来伪装能力满足；运行组合缺该能力仍为 `DEPENDENCY_UNBOUND`。本卡不冻结那些尚未知的 reference 字段，仅对当前 `{}` 分支维持严格闭集。

## 3. 冻结 Go 类型与方法

下列类型属于 `work/contract`，`foundation`、`fmt`、`slog` 分别指既有 Foundation 与标准库。`TaskBlockerIdentity` 只作唯一 ID marker，为后继完整 `TaskBlocker` record 保留名字；不再定义第二个 Blocker marker 或把 TaskID 转成 BlockerID。

```go
type TaskBlockerIdentity struct{}
type TaskBlockerID = foundation.ID[TaskBlockerIdentity]

type TaskBlockerType string

const (
    TaskBlockerRelyOn TaskBlockerType = "rely_on"
    TaskBlockerWaitingForHuman TaskBlockerType = "waiting_for_human"
    TaskBlockerWaitingForMeetingApproval TaskBlockerType = "waiting_for_meeting_approval"
    TaskBlockerTechnical TaskBlockerType = "technical"
    TaskBlockerUserCancelledExecution TaskBlockerType = "user_cancelled_execution"

    MaxTaskBlockerTypeBytes = 64
    MaxTaskBlockerMetadataBytes = 1 << 10
    MaxTaskBlockerDescriptionBytes = 1024
    MaxTaskBlockerResolutionCommentBytes = 1024
    MaxTaskBlockerCreateBytes = 8 << 10
    MaxTaskBlockerPayloadBytes = MaxTaskHistoryPayloadBytes
)

type TaskBlockerRelyOnMetadata struct {
    RelatedTaskID TaskID `json:"related_task_id"`
}
type TaskBlockerWaitingForHumanMetadata struct{}

type TaskBlockerMetadata struct {
    RelyOn *TaskBlockerRelyOnMetadata
    WaitingForHuman *TaskBlockerWaitingForHumanMetadata
}

type TaskBlockerCreate struct {
    BlockerID TaskBlockerID `json:"blocker_id"`
    Type TaskBlockerType `json:"type"`
    Description string `json:"description"`
    Metadata TaskBlockerMetadata `json:"-"`
}

type TaskBlockerAddedPayload struct {
    BlockerID TaskBlockerID `json:"blocker_id"`
    BlockerType TaskBlockerType `json:"blocker_type"`
}
type TaskBlockerResolvedPayload struct {
    BlockerID TaskBlockerID `json:"blocker_id"`
    BlockerType TaskBlockerType `json:"blocker_type"`
    ResolutionComment *string `json:"resolution_comment"`
}

func (TaskBlockerType) Validate() error
func (TaskBlockerType) MarshalJSON() ([]byte, error)
func (*TaskBlockerType) UnmarshalJSON([]byte) error
func (TaskBlockerType) Format(fmt.State, rune)
func (TaskBlockerType) LogValue() slog.Value

func (TaskBlockerMetadata) ValidateFor(TaskBlockerType) error
func (TaskBlockerMetadata) Clone() TaskBlockerMetadata
func (TaskBlockerMetadata) Format(fmt.State, rune)
func (TaskBlockerMetadata) LogValue() slog.Value

func DecodeTaskBlockerCreate([]byte) (TaskBlockerCreate, error)
func DecodeTaskBlockerAddedPayload([]byte) (TaskBlockerAddedPayload, error)
func DecodeTaskBlockerResolvedPayload([]byte) (TaskBlockerResolvedPayload, error)
```

以下五个 concrete struct 各自完整提供相同形式的方法，`T` 逐一替换为表中真实类型；不是额外的泛型 API 或 interface：

| T | 必须提供的精确方法形式 |
| --- | --- |
| `TaskBlockerRelyOnMetadata` | `(T) Validate() error`、`(T) Clone() T`、`(T) MarshalJSON() ([]byte,error)`、`(*T) UnmarshalJSON([]byte) error`、`(T) Format(fmt.State,rune)`、`(T) LogValue() slog.Value` |
| `TaskBlockerWaitingForHumanMetadata` | 同上，空 concrete 值的 Validate 成功；pointer 是否存在由 union 校验 |
| `TaskBlockerCreate` | 同上 |
| `TaskBlockerAddedPayload` | 同上 |
| `TaskBlockerResolvedPayload` | 同上 |

`TaskBlockerMetadata` 是由外层 Type 选择的 Go union，不单独提供 JSON codec，也不得作为公共 decoder 目标。对已支持的 type，必须恰有对应的一个非 nil branch，另一个必须 nil；零分支、多分支或错分支全部拒绝。Create codec 将对应 concrete branch 放在唯一 wire `metadata` 字段，wire 中不出现 `RelyOn/WaitingForHuman` 或第二个 discriminator。对另外三种已知 type，ValidateFor 返回 `DEPENDENCY_UNBOUND`，不把任何现有 branch 解释成它们的 metadata。

公开字段/成功值不含 `json.RawMessage`、`map[string]any`、`any`、通用 reference 字符串或后验解释的 metadata。decoder 可在私有局部暂存 raw 字段以选择严格 concrete 分支，随后立即丢弃；这不是对未冻结类型的成功表示，不能保存、输出或以 fallback 形式传给调用者。

## 4. 字段、presence 与文本

| 值 | wire 闭集与规则 |
| --- | --- |
| Create | `blocker_id,type,description,metadata` 四字段全部 required、非 null。无 task_id/project_id/actor/version/cause/status/resolved 字段；所属 Task/Project 来自后继可信命令上下文 |
| rely_on metadata | 唯一 required 非 null `related_task_id`，使用现有 TaskID 的非零 canonical 小写 UUIDv7；不接受其他 key |
| waiting_for_human metadata | required object `{}`；`null`、省略、数组以及任何 key 均不合法 |
| AddedPayload | 精确 required 非 null `blocker_id,blocker_type`；不含 description/metadata/related_task_id 或完整 Blocker 快照 |
| ResolvedPayload | 精确 required `blocker_id,blocker_type,resolution_comment`；只有 resolution_comment 可 null，省略拒绝 |

所有 BlockerID 沿 Foundation typed UUIDv7，拒绝零值、数值 JSON、UUID 非 canonical 大小写/版本/variant。Create 不生成 ID：caller 预生成稳定 ID 并跨重试保留。ID shape 验证不证明该 Blocker 存在或未被使用。

description 为 `0..1024` UTF-8 bytes，允许空串；字段必须存在。字符规则沿 Work description：允许 TAB/LF/CR，拒绝其他 Unicode Cc 与非法 UTF-8；不 trim、NFC 或改写空白，保留原字节。此处只收紧独立 Blocker description 的编码上限，不增加架构未规定的“描述必须非空”规则，不复用现有 32768-byte 上限。

非 nil resolution_comment 为 `1..1024` UTF-8 bytes、至少一个非 Unicode 空白字符；允许 TAB/LF/CR，拒绝其他 Cc 和非法 UTF-8，保留原字节。nil 表示没有 resolution comment，空串或纯空白不等同 nil。该小 payload 支持架构允许的独立 resolve comment，但本卡没有独立 resolve 命令；当前 TaskTransfer 只有总 review comment，后继 T0b/T1 生成每个 ResolvedPayload 时必须令 ResolutionComment=nil，不能复制总 comment 或 invent 逐项输入。

新增 metadata/type/description 不能在原 Blocker 上编辑；“resolve 原对象，再 add 新对象”的持久语义由 B0-P 实现，本卡没有 Update DTO。added/resolved payload 只表达小历史内容，不具有 actor、occurred_at、Task version 或写入权，这些仍由后继事件封套与同 Tx producer 保证。

## 5. 严格 codec、Clone 与错误顺序

所有 standalone concrete metadata、Create、payload 与 type codec 都检查自己实际收到的完整 raw cap，包括外空白；MarshalJSON 检查完整编码输出。复用既有严格 helper，拒绝重复 key、未知 key、大小写替代、非法 UTF-8、孤立 surrogate、尾随值、非对象/null、required 缺失、非法 null 和 scalar 形状。合法 Unicode escape 先还原语义；被 escape 的 key 仍参与同一重复/闭集判断。

metadata raw cap 同样适用于 Create 中 `metadata` 值的完整 JSON 词法范围（从其第一个 token 到最后一个 token，包含对象内部空白）；字段冒号后的外围空白计入整个 Create cap。Create decoder 必须先保留并检查该 raw 范围，再进 concrete codec，不能先 `json.Unmarshal` 成值或 trim 后宣称覆盖了原始 metadata 大小。标准 `json.Unmarshal` 会裁最外层空白；三个 Decode 函数直接调用自有 UnmarshalJSON，不能经标准入口绕过完整 raw cap。

全部指针 receiver 在 nil 时返回安全 `INVALID_ARGUMENT/NotStarted`，不得 panic；正常 receiver 只有整个输入通过后才一次替换，任何失败均保留原值。三个 Decode 函数发生错误时返回对应零值及错误，不能返回部分分支或旧对象。Clone 复制所有 union 指针及 ResolutionComment 指针；字符串/typed ID 按值复制，不共享可变 metadata。无可变全局 cache；调用者并发修改同一个输入指针仍属数据竞争，不声称 Clone 同步了外部写入。

纯边界的错误阶段固定：

1. raw cap、严格 JSON/对象形状、外层闭集、required/null 与 scalar 基础形状先检查；Create 的 metadata 必须是 cap 内非 null object，即使 type 待绑定也一样。
2. 按 BlockerID、Type（payload 用 BlockerType）、description 或非 nil resolution_comment 检查公共值。未知 type 为 `INVALID_ARGUMENT`。Go Validate 从此阶段开始，不代替原始 JSON 检查。
3. 已知但未绑定的三种 type 返回 `DEPENDENCY_UNBOUND`；不尝试验证其未冻结的内部字段语义，也不创建任何成功值。该拒绝优先于现有 Go union 分支匹配；但不会压过前两阶段的 malformed envelope/公共字段。
4. 已支持 type 才验证 concrete metadata 字段和 union 匹配；错误为 `INVALID_ARGUMENT`。无引用 waiting_for_human 中的 reference key 在此作为 unknown 拒绝，不能先剥除字段成功。

本卡所有返回错误都是既有 `foundation.Fault`，`CommitState=NotStarted`；只用当前 `INVALID_ARGUMENT`、`DEPENDENCY_UNBOUND`，Known/Safe 必须保真，不修改 [Foundation Fault](../../../internal/central/foundation/fault.go)。字段错误只含固定安全路径/机器码，不能包含 offending value、description/comment/metadata、跨 Project ID 或原始 cause。public Validate 的字段投影固定为 `/blocker_id:INVALID_BLOCKER_ID`、`/type:INVALID_BLOCKER_TYPE`（payload 为 `/blocker_type`）、`/description:INVALID_BLOCKER_DESCRIPTION`、`/metadata:INVALID_BLOCKER_METADATA`、`/resolution_comment:INVALID_RESOLUTION_COMMENT`；concrete rely_on 使用 `/related_task_id:INVALID_RELATED_TASK_ID`。raw 编码/required/null/unknown 错误沿既有 helper 的固定投影；不承诺同阶段多个 malformed key 的遍历次序。

`BLOCKER_NOT_FOUND`、`BLOCKER_ALREADY_RESOLVED`、`TASK_DEPENDENCY_CYCLE`、`LAST_BLOCKER_REQUIRES_TASK_TRANSITION` 及其他真实引用业务错误的唯一最终映射由 B0-P 冻结并补齐 Foundation Known/Safe；本纯结果不创建会被 Safe 降为 INTERNAL_ERROR 的私有 Code，也不借纯 shape 函数假装已执行存在性/图检查。

## 6. 上限与最大 escaping 证明

| 对象 | 完整 raw / MarshalJSON 上限 | 内容上限 |
| --- | --- | --- |
| TaskBlockerType | 64 bytes | 五个固定值；最长 `waiting_for_meeting_approval` 的 canonical JSON 30 bytes |
| 每个 concrete metadata | 1KiB | rely_on canonical JSON 58 bytes；waiting_for_human 为2 bytes |
| TaskBlockerCreate | 8KiB | description≤1024 UTF-8 bytes，恰一个已支持 metadata 分支 |
| Added/ResolvedPayload | 8KiB（既有 MaxTaskHistoryPayloadBytes） | Added 无正文；Resolved 的可选 comment≤1024 UTF-8 bytes |

按默认 `encoding/json` 的 HTML escaping，每个合法输入 byte 的输出最多6倍，`<`、`>`、`&` 的重复文本实际达到此上界。description 最坏输出为6144 bytes；固定 ID、key、type、引号与 metadata 合计：rely_on Create 为157 bytes、waiting_for_human Create 为111 bytes，所以最大分别为 **6301 / 6255 bytes**，都低于8192。两个 metadata 最大 canonical 输出58/2也低于1024。

Added 的两个 type 最大编码为78/88 bytes。Resolved 最坏 comment 同为6144 bytes，固定部分为102/112 bytes，所以最大为 **6246 / 6256 bytes**；null comment 更小。这些算式绑定上述字段与 canonical UUID 长度，后继若加字段或外域 metadata 必须重新证明，不能只复用短 ASCII 测例。

因此 transition §11 对每个 Blocker 请求项≤8KiB 的预算在这两个分支成立；保守32项总额仍≤262144 bytes，没有提高完整 request/history cap。它不证明 B0-P 的 before/after 最小事实或完整4MiB plan 已满足预算；那些结构尚未定义，后继须在其真实类型上证明，而不能把本卡的 Create 大小当完整持久快照大小。

raw cap 正边界可用合法空白填充达到64/1024/8192，cap+1必须拒绝；内容上限应独立测试1024/1025 UTF-8 bytes，以及恰限的最坏 escaping、多字节字符、TAB/LF/CR与受禁 Cc。合法最大值必须可 Marshal、Decode、Clone，不允许先宣称合法再因同一对象输出 cap 失败。

## 7. 安全投影、旧 schema 与后继接缝

所有新 struct/enum 的直接 Format、LogValue 固定为 `work_task_transition`，沿 transition §11；不输出 ID、type、正文、metadata 或原 cause。业务 JSON 仍保存合法文本，不能称其为脱敏日志。任意 enclosing struct/slice/map 的 fmt/slog 回退可能绕过内层方法，调用方仍须显式固定标记或批准字段，不外推为所有容器自动隐文。

新增 Blocker payload 不扩充旧 [TaskEvent/TaskEventActor](../../../internal/central/work/contract/task_events.go) decoder、旧 TaskEventType 或 Human schema，不改 TaskMutation/TaskChanged。旧 decoder 必须继续拒绝 blocker_added/blocker_resolved、新 payload 字段以及 Agent/System actor；本结果不提前注册新 TaskTransitionEvent/envelope。T0b 只消费本卡已支持的两类；不得据 Type 识别了五个枚举宣称五类 metadata 已完成。

B0-P 仍须真实同 Store/Tx/current authority、Task preimage、Blocker存在/归属/重复resolve、terminal门禁、批次原子性、最后 Blocker保护及图事实。图在 Schedule EX 下取完整 unresolved rely_on 边，按本批 resolve/add 后结果判环；保留历史每 Task≤4096，unresolved 每 Task≤256、Project≤262144，容量/错误/回滚须真实验收。相关 Task 只有 done 自动满足，cancelled 不满足；resolved边不参加当前图。这里引用已有业务门槛，不定义“默认无 Blocker”的 adapter。

T1/T2/T3 仍须 Agent当前事实、真实 Actor/Tool授权、occupancy/pending、claim/逻辑位置映射与停止组合。新增 typed ID、metadata Validate、payload Clone 或完整 codec 都不能证明这些运行事实；transition §13的未满足接缝不因本卡接受而消失。

## 8. 实施与独立验收矩阵

下表是后续两源码结果的准确顶层 selector；本次只做工程规格自查。作者完成后冻结两个源，独立验证者不得参与本 API/实现决策，其 oracle 直接来自本卡与已接受业务来源，不能调用被测 Validate 来生成预期结果。

| top selector | 必须实际证明 |
| --- | --- |
| `TestTaskBlockerIdentityAndType` | 唯一 Foundation typed marker、零/非canonical ID拒绝；五个枚举精确识别、unknown/case/number/null拒绝；Type成功不等于三类Create/payload已绑定 |
| `TestTaskBlockerMetadataUnion` | rely_on唯一必填ID、waiting仅{}；zero/multiple/wrong branch拒绝，reference/未知键不丢弃；三类已知待绑定返回DependencyUnbound；不出现公开raw/map/any/fallback |
| `TestTaskBlockerCreateCodecAndText` | 四required字段完整缺/null/重复/大小写/unknown矩阵，嵌套同样严格；description空串/1024/1025、多字节、UTF-8与Cc规则、字节保留；来源ID/actor/version/status等字段不能注入 |
| `TestTaskBlockerHistoryPayloads` | Added精确二字段、Resolved精确三字段且comment required-nullable；有界非空comment/空白/受禁字符；无description/metadata快照；两种已支持type正例及三类待绑定否定；独立comment值不代表已有resolve服务 |
| `TestTaskBlockerRawCapsAndAtomicDecode` | 三层64/1024/8192 raw恰限/cap+1与外空白；整体Create尚未超限但嵌套metadata超限仍拒；最坏escaping精确最大输出；坏UTF-8/surrogate/尾值/escaped重复键；nil receiver、失败receiver保留、Decode失败零值；malformed+unbound错误优先 |
| `TestTaskBlockerCloneAndSafeLog` | 两union branch与comment指针深复制/保持nil，不修改原值；直接fmt各常用verb与slog固定标记；Fault Known/Safe/NotStarted无正文；独立只读输入并行Validate/Marshal/Decode/Clone无共享状态 |
| `TestTaskBlockerLegacyIsolation` | 旧TaskEventType/TaskEvent/Human actor、TaskMutation/TaskChanged继续拒绝新type/字段/多历史；旧正例继续有效；无Foundation/identity/旧schema diff、无事件注册/DB/service/完整Transfer能力 |

后续实施使用仓库指定 Go 1.27.1、任务独占 GOCACHE/GOTMPDIR，`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off`；最小作者命令为 `go test -p=2 -count=1 ./internal/central/work/contract`、同包 `go test -race -p=2 -count=1`、`go vet -p=2`。先实际发现上述七个 selector，独立验证至少用受控公开 API 输入复跑三类unbound/union混入/嵌套cap/失败atomic/旧schema等否定的 pure/race；所有命令确认实际 Wait/退出后记录结果，UnknownProcess 不推PASS。无必要PG、浏览器或联网依赖。

本次 SPEC 自查仅本地链接/anchor、字段/API/错误及大小算式一致性、Markdown格式和限定文件范围；不把静态Python大小演算当Go codec实测，不把作者自查当独立SPEC接受。交付暂停本文写入后由主线程保存并安排独审；源码、产品与真实集成仍未执行。
