# D11 Work Milestone / Sprint 结构库

当前接受补充（2026-10-08）：本卡全部18路径已按固定版本组合获得root正式接受并交付 `a64fb5e783373255a4b0ae7936e5685f25f458c6`，root已核origin/main一致；见[永久验收及全18补充](../agent-team/d11-work-structure-verification.md)。原失败/U1修复、各轮源版本、独立B外部工具terminal缺口与原监督器Wait/root当前清零证据分别保留；不是完整D11或生产Work。以下SPEC阶段及初始授权叙述保留原时点，当前接受状态以本补充和永久验收为准。

- 修订：rev1＋D1；阶段：完整独立 SPEC 静审及唯一 D1 差量/最终组合已通过，root 已接受。见[永久 SPEC 验证记录](../agent-team/d11-work-structure-spec-verification.md)。这是规格接受，不是产品实现、编译或真实 PG 验收通过。
- 来源锚：root 恢复后确认 `main@0d06fd69`；技术依据另按 §10 的必要文件 SHA 固定，不声称整个 HEAD 或上游模块已经验收。
- 唯一规格 writer：architecture_worker；唯一业务作者：fixture_recovery。root 已移交 #1–17 仅在作者 scratch 形成完整实施候选，尚未授权 main 安装、Go/Node 或资源执行；#18 README 末件未授。产品独审由 root 指定未参与实现的实例；backend_worker 继续 Model Settings UI 两个 Go harness，不共享本卡写域。
- root 仅为本卡预留全局迁移 `db/migrations/00021_work_structure.sql`；当前连续迁移到 00020。预留不是已实施，不修改旧迁移。
- 交付：Human Owner 的真实 Milestone/Sprint 层级、读/分页、创建、title/description 更新、人工重排、版本、幂等/Unknown、typed Outbox 事件及 Project 精确事件门禁。不是完整 CRUD、Task、Sprint lifecycle、D11 整体或生产 Work。

## 1. 依据、已验依赖与范围

按[开发计划 D11](../development-plan.md#d11-work-management-领域)顺序推进“结构 → Task canonical/查询/rank → 状态/reviewer → Blocker → Sprint lifecycle → TaskEvent/context → 综合验收”。既定业务规则来自 [Task Domain §3–5/10–11/17–18](../../architecture/project-work-management/task-domain-model.md)、[Sprint §3–6/16–20](../../architecture/project-work-management/sprint-lifecycle.md)、[Project Owner/归档](../../architecture/project-work-management/README.md)。本卡在这些规则下冻结工程类型/限额/持久化与并发规则，不新增 Milestone lifecycle、时间计划、force/cascade 删除或隐式 Sprint 搬移。

| 直接依赖 | 正式接受依据 | 本卡实际消费与边界 |
| --- | --- | --- |
| D03 Store、活 Tx、锁、迁移 | [D03 已完成](d03-postgresql-foundation.md)及[基础契约](d01-contracts/foundation.md) | 同一个 Store 的 WithinTx/InTx/AcquireAll/RequireHeldLocks；不是新数据库入口 |
| 当前 Human Session、Owner、Project gate | [D08 B01/B02](d08-project-owner.md)：B01 `199554b`，B02 `6319d03`，B02 最终报告 SHA `9ab034850a899ec58a9099e09f5b98f8d1c412bfc9172ed736f32e81c4e4f228` | 真实 RequireOwnerInTx；当前 Session/Owner/initialized/lifecycle 逐次校验；不宣称 D08/D10 全完成 |
| D06 typed Event/同 Tx Outbox | [D06 最终接受](d06-transactional-outbox.md#d06最终采纳与后续绑定)，报告 SHA `3a9d1b7c1df941086d56ce9bcb00411007ba8c3f974c25e099fb9857f8998626` | PrepareAppend/AppendEventInTx、opaque issuer、CurrentAccess/NewFact；不新增 dispatcher/handler |
| 分页及安全标量 | [D01 foundation](d01-contracts/foundation.md)，现有 foundation/cursor | UUIDv7、Version、CommandMeta、canonical-v1、HMAC cursor；签名不是授权 |
| 正向初始化 fixture | D08 B02 明示隔离 initializer 与真实 Project Create/confirmation 已验 | 测试专属持久 Account/Session 输入经真实 Authority 校验，Project 服务＋持久 test-only Skill receipt；不冒正式登录、真实 Skills、创建 HTTP 或 Object runtime |
| Work 事实与 Project 事件准入 | 当前不存在 Work canonical；Project events.go 仅 Project/Model | **本卡新增并真实验收**：Work canonical/command producer 与精确 Project Work gate，不能把现有接口声明视为已绑定 |

D11 整体依赖 D10 的 Agent/Skill 能力仍未齐。本卡只消费已验 Human 子能力；没有 assignee、Agent Tool、Execution、SDK/模型调用依赖。三停止（Object runtime join repair、OpenAI tools 独立验收、SPA concurrent publication）、Jina 与 Image 来源的停止状态不在本卡恢复范围。

以下动作不提供公共命令：Sprint start/complete/rollover/delete，Milestone delete，Sprint 跨 Milestone move，Task create/move/delete，Execution/Dispatch 查询或清理。`Project.current_sprint_id` 与已有 `project/contract.SprintID` 保留，不新建第二种 Sprint 身份、不直接写 Project pointer。Milestone 删除和跨 Milestone 搬移的产品含义未完整冻结，当前排除，不为首卡补造规则。

## 2. 数据、编码与有限排序

### 2.1 标量和 DTO

`ProjectID = identity.ProjectID`，`SprintID = project/contract.SprintID` 为别名；新 `MilestoneID = foundation.ID[Milestone]`、`CommandID = foundation.ID[StructureCommand]`，marker 归 Work contract。全部非零、小写规范 UUIDv7，不接受 UUIDv4、任意字符串、零值或第二套 marker。Version/order_generation 为 1 起始 int64，JSON 为规范十进制字符串；溢出安全拒绝 `INVALID_STATE`，不得回绕。时间为 foundation.Instant/DB UTC 微秒，由服务端数据库生成。

| 字段 | rev1 精确工程限额/规则 |
| --- | --- |
| title | 1–256 Unicode scalar，最多 1024 UTF-8 bytes；必须至少一个非 Unicode 空白字符；拒绝全部 Unicode Cc 控制字符（含 NUL/DEL/C0/C1）；保留其余原字节，不 trim/NFC/大小写折叠；重名合法 |
| description | 0–32768 UTF-8 bytes；可空，允许 TAB/LF/CR，拒绝其它 Unicode Cc 控制字符；原 Markdown/plain text 字节保留 |
| 输入 JSON | 严格字段闭集、单值、拒绝重复 key/大小写替代/未知字段/null 越界/非法 UTF-8/孤立 UTF-16 surrogate；公共命令 request 的原 JSON 最多 256 KiB，纯 Go request 同样逐字段验证 |
| patch | title/description 至少一项显式提供；nil 表示省略，JSON null 拒绝；空 description 是清空，不等同省略 |
| manual_rank | 服务端拥有，32 个小写十六进制 ASCII；表示开区间 `(0, 2^128−1)` 的定长整数；不接受 caller rank、浮点或 locale 排序 |
| 每个排序组容量 | Project 内最多 4096 Milestone；每个 Milestone 最多 4096 Sprint。达到上限的新 create 返回 `RESOURCE_BUSY`＋`GROUP_LIMIT`；历史 replay 不重验容量；不静默截断 |
| page | 复用 PageRequest：默认 50，1–200；limit=0/null 拒绝，cursor 最多既有 8192 bytes；不增加 offset/任意排序/搜索过滤 |

`Milestone` 对外安全读取字段闭集为 `id,project_id,title,description,manual_rank,version,created_at,updated_at`。Milestone version 是本卡新增的工程并发 token；不引入 Milestone lifecycle。创建 version=1，created_at=updated_at=同一数据库时刻；真实字段更新/重排各 +1。其 Sprint 子项创建/变化不改父 Milestone version/updated_at。

`Sprint` 为上述公共字段再加 `milestone_id,started_at,started_by,completed_at,completed_by,state`。四个 lifecycle 字段显式 nullable；state 只作读取投影，不存储状态列。Actor history 引用为 Work 的闭合稳定投影：human `{kind,user_id}`；agent_run `{kind,project_id,agent_id,execution_id}`；service `{kind,service_name,cause_ref,project_id}`，严格遵循 identity 现有枚举/标量且项目一致，无 session/token/授权布尔值。历史投影只表达事实，不授任何角色本卡写权限；本卡 create 一律四字段 null，update/reorder 不接受这些字段。后继 lifecycle 服务才可从已授权 Actor 产生它们。

Sprint 状态按正式规则、同 Tx 当前 ProjectAccess.Project().CurrentSprintID 推导：planned=未 started/未 completed/非 pointer；current=已 started/未 completed/正是 pointer；completed=已 started/已 completed/非 pointer。by 与对应 at 同有同无；创建同样 version=1、created_at=updated_at；所有对象 created_at≤updated_at，存在的生命周期时间满足 created_at≤started_at≤completed_at≤updated_at（忽略缺省 completed_at）。发现不可能组合、外项目 pointer/损坏持久标量或成对事实缺失，安全 `INTERNAL_ERROR`，不得默认 planned 或当不存在。读取必须验证当前 pointer 对应同 Project 的 canonical Sprint 及 current invariant（非 null 时的一次有界精确读取）；没有无界扫描要求。

只有 planned/current Sprint 能接收本卡新字段更新/业务重排；completed 返回 `INVALID_STATE`＋`COMPLETED_SPRINT_IMMUTABLE`，包括试图以“值未变”绕过。已成功原命令的历史 replay 例外按 §4，返回原结果，不重新修改 completed 内容。

### 2.2 rank 与 generation

排序组固定：Milestone 为 `project_id`；Sprint 为 `(project_id,milestone_id)`。DB 显式 `COLLATE "C"`，稳定序为 `(manual_rank ASC,id ASC)`；本组 rank unique 是并发约束，ID 次排序仍保留。create 只追加组尾，不开放 caller 指定首 rank。

重排只接受一个可选 `before_id`：缺省表示放到组尾；JSON 显式 null 拒绝；有值表示放在同组该对象之前。先从当前序列移除 target，再插入；before 不能是 target，不能跨组/项目、不能是不存在对象。不提供 after、rank、任意完整 ID 列表或跨组 move。合法语法但不可见/不在组的 target/anchor 统一 `NOT_FOUND`，避免枚举；自身 anchor 为 `INVALID_ARGUMENT`。按最终锁定事务中的最新兄弟位置解释 before，不接受陈旧 rank。

从左右邻居整数 L/R（首尾使用未存储的 0/M，M=2^128−1）取 `floor((L+R)/2)`；若没有严格中间值，先仅在当前组按原 `(rank,id)` 顺序将 n 个既有对象重编号为 `floor(i*M/(n+1))，i=1..n`，再按原 target/before **身份**重算最终邻居及新 rank。使用确定的整数算法（可用标准库 math/big），不引入外部 rank 包/随机重试。

纯 rebalance 保留实际相对顺序、所有业务 version/updated_at、生命周期及 parent；不产生业务事件或成功业务命令。它可以重编号 completed Sprint 的存储 rank，但不能改变 completed 的相对逻辑位置或业务字段；这是存储维护，不开放对 completed 的重排命令。无独立后台/HTTP/rebalance 公共 API，仅必要时在本次合法 mutation 的同 Tx 内执行。任一后续失败须一并回滚，不能先提交维护再声称命令全回滚。

每组有独立 `order_generation`，逻辑初值 1。成功 create、真实 reorder 或仅 rank token 重整使排序/成员/游标位置变化时，最终 Tx 对该组 **恰 +1**；一次命令包含 rebalance＋reorder 仍只 +1。title/description 更新、no-op、历史 replay 不推进。真实 reorder target 恰 +1 version/updated_at，其余对象即使重编号也不增 version/updated_at。若 remove/insert 后逻辑顺序未变，no-op：不改任何 rank/generation/version/time，不为 no-op 强行 rebalance。

### 2.3 持久表与迁移

00021 只建 `agenteam_work` 的五张表与必要索引/约束：`milestones`、`sprints`、`milestone_order_groups`、`sprint_order_groups`、`structure_commands`。所有对象/命令 UUID PK；对象含 project_id，Sprint 以 `(project_id,milestone_id)` FK 指向 Work Milestone 的同项目 unique key，禁止孤立或跨 Project 父子；同组 rank unique 使用可延迟到事务末尾的约束以支持原子 rebalance。父子删除使用 RESTRICT/NO ACTION，无 CASCADE。

Milestone group 以 project_id 为 PK；首次 create 在持锁事务物化初始组并推进到 2。此前完全空 Project 的逻辑 generation=1，list 不为读取写行；若组行缺失但 canonical 已非空是损坏。Sprint group 在创建 Milestone 的同一最终 Tx 物化，初值 1；key 为 `(project_id,milestone_id)`，FK 指向本域父行。组 generation 不是父对象业务 version。

Work 表不直读/改 Account、Project、Outbox 私表；Project 归属/初始化/门禁由真实 Authority 提供，不把 DB 外键当授权。跨域清理通过正式 participant，不能配置级联删除 Work。Work 内局部约束包括正版本/代数、title/description 的 bytes 边界、rank 格式与界限、parent 唯一性、生命周期成对字段和时间关系，以及同 Project 至多一条 `started_at IS NOT NULL AND completed_at IS NULL` 的 partial unique 约束；pointer 一致性仍由真实 ProjectAccess＋Work 读取验证，索引不代替跨域事实。纯 DTO 验证补足 Unicode/Actor 闭集。无 Task 表、membership registry、空占用表或虚构通用 outbox_authority 表。

`structure_commands` 的唯一身份为 `(project_id,command_name,idempotency_key)`，另存不可变 command_id、actor_user_id、semantic_digest、原类型化命令、安全 planning 数据、plan_revision、state=`planned|completed`、created_at、committed_at/receipt。事件 Header/typed payload/目标 postimage/最终逻辑 position 属 planned 数据；event_id 若存在全域 unique。completed 的 receipt/语义/事件身份不可变；planned 不是已接受的业务结果。存储须严格区分 nullable 字段、限制规划/回执 JSON 的闭集与 bytes（planning ≤1 MiB，原命令与 receipt 各 ≤256 KiB，包含规范编码转义后的大小）；不得持久化 Session/CSRF/任意诊断错误。计划存 group 的前 generation 与目标/邻居身份，不能复制 4096 行 rank 向量；最终在固定 generation 下用相同算法重算整组。命令原文/description 受 Project 数据生命周期保护，不日志输出、不自动 TTL，不保留已删 Project 正文副本。

命令行约束：plan_revision≥1；planned 必须有完整 plan/event_id，receipt 与 committed_at 为空；completed 必须有已验证 receipt/committed_at。真实变化的 completed 保留最终不可变 plan/header/payload/event_id 供历史 producer 验证；no-op completed 的 plan/event_id 为空、receipt.Changed=false。除同义 planned→新 revision 或 planned→completed 以外不覆盖命令事实，成功 no-op 可直接插入 completed；不能提交有业务行/事件而命令仍 planned 的最终事务。可用 DB CHECK 的状态/空值/对象类型关系由迁移落实，typed payload/receipt 与语义的关系由真实 repository/producer 校验。

fresh migration、已有 00020 populated 库升级、重复 migrate、事务失败无部分 schema/业务变化均须实际 PG 验证。00001–00020/迁移 embed/source 实现只读；若现有嵌入模式不能自动包含 00021，先报具体缺口，不擅自扩路径。

## 3. 精确公共边界、分页与同 Tx placement

新增 `work/contract` 固定以下值/接口；具体私有 helper 可落在授权源内，不机械拆包。c 指 work/contract，pc 指 project/contract，f 指 foundation，i 指 identity。

```text
// 六个命令名恰为这六项。
CommandName = "work.milestone.create" | "work.milestone.update" |
              "work.milestone.reorder" | "work.sprint.create" |
              "work.sprint.update" | "work.sprint.reorder"
CreateMilestoneRequest { MilestoneID; Title string; Description string }
CreateSprintRequest { SprintID; MilestoneID; Title string; Description string }
UpdateFields { Title *string; Description *string }
ReorderMilestoneRequest { BeforeID *MilestoneID }
ReorderSprintRequest { MilestoneID; BeforeID *SprintID }
StructureMutation { Command CommandName; Changed bool;
                    Milestone *Milestone; Sprint *Sprint; EventID *event.EventID }
CommandLookupRequest { ProjectID; Command CommandName;
                       Key f.IdempotencyKey; Semantic f.Digest }
CommandLookup { State: "committed" | "in_progress" | "not_observed";
                Result *StructureMutation }

CreateMilestone(context.Context, i.Actor, f.CommandMeta, ProjectID,
                CreateMilestoneRequest) (StructureMutation, error)
UpdateMilestone(context.Context, i.Actor, f.CommandMeta, ProjectID,
                MilestoneID, UpdateFields) (StructureMutation, error)
ReorderMilestone(context.Context, i.Actor, f.CommandMeta, ProjectID,
                 MilestoneID, ReorderMilestoneRequest) (StructureMutation, error)
CreateSprint(context.Context, i.Actor, f.CommandMeta, ProjectID,
             CreateSprintRequest) (StructureMutation, error)
UpdateSprint(context.Context, i.Actor, f.CommandMeta, ProjectID,
             SprintID, UpdateFields) (StructureMutation, error)
ReorderSprint(context.Context, i.Actor, f.CommandMeta, ProjectID,
              SprintID, ReorderSprintRequest) (StructureMutation, error)
LookupCommand(context.Context, i.Actor, CommandLookupRequest) (CommandLookup, error)

GetMilestone(context.Context, i.Actor, ProjectID, MilestoneID) (Milestone, error)
ListMilestones(context.Context, i.Actor, ProjectID,
               f.PageRequest) (f.Page[Milestone], error)
GetSprint(context.Context, i.Actor, ProjectID, SprintID) (Sprint, error)
ListSprints(context.Context, i.Actor, ProjectID, MilestoneID,
            f.PageRequest) (f.Page[Sprint], error)

// 后继 Task 的真实结构读取口；不证明 Task membership/占用或写权限。
ReadPlacementInTx(context.Context, f.Tx, i.Actor, ProjectID,
                  SprintID) (Placement, error)
Placement { Milestone Milestone; Sprint Sprint }
```

上表为精确类型/方法签名的规格表示，不是可直接编译的 Go 源。所有字段编码为对应 snake_case；ID 字段用同名 typed ID，Meta/版本沿 foundation。Create 的 target ID、Sprint 的 MilestoneID 与 title 必填，description 仅省略时规范为 `""`，显式 null 拒绝；ReorderSprint 的 MilestoneID 必填，其余 presence 按 §2。contract 分别提供 Commands（六命令＋Lookup）和 Reader（四读＋Placement）接口；不合并成可接受任意 operation 字符串的执行器。

所有 request/response 逐键安全序列化并校验 union：Mutation 恰有对应种类对象；create Changed=true/EventID非空；真实 update/reorder 同样 true/非空；no-op false/null。Lookup 仅 committed 有 Result；其余 Result=null。历史 result 是原成功快照，可能已不是当前 version/state/rank；不能拿它冒充最新读取。回执等值指严格 typed result 的 canonical JSON bytes，不是 SQL JSONB 原始键序或含传输 RequestID 的 HTTP envelope。一般 fmt/slog 对包含正文、key、semantic、plan/receipt 的复合对象输出固定安全摘要，显式安全领域 DTO 编码另行调用；隐式错误不可带 SQL、driver message、原命令正文。

使用 `f.CommandMeta`，create 的 ExpectedVersion 必须 nil，其余必须非 nil/合法，且仅指目标对象，不是父 Project/Milestone version。Create target ID 必须由调用方在首次意图前生成并贯穿重试；不能在 Unknown 后生成新目标，不能按 title 去重。每个命令提供一个同签名参数的 pure `...Digest(actor,meta,project,...request)`，用于精确 Lookup；它只做类型/语义摘要，不认证用户。Lookup 不接受任意 namespace/owner 列表、不自行补目标，不把猜中的 digest 当权限。

库构造固定为 `work.NewAuthority(store Store, projects pc.ProjectAuthority)`、`work.NewReader(store Store, authority *Authority, cursors cursor.Keyring)`、`work.New(store Store, deps Dependencies)`；返回值均 `(对应指针,error)`。Dependencies 恰为本域 Authority、Events oc.Appender、WorkEvents c.WorkEvents、Activity `TouchActivityInTx(ctx,tx,actor) error`。Authority 是本卡 oc.ProducerAuthority；Reader 实现四个读和 Placement；Service 实现六命令/Lookup，另提供 Stop()/Drain(ctx) 对本实例在途调用真实取消/等待，无后台恢复 worker。构造只验证绑定/类型/自己持有的 Store identity，不访问 DB、不声称能透视 pc.ProjectAuthority 内部 Store；异 Store 由每次同 Tx 调用的真实 Authority 拒绝。

Store 复用已有结构：postgres.SQLExecutor＋InTx、WithinTx、AcquireAll、RequireHeldLocks（签名与 Project Store 一致）。不新增 DB 驱动、全局接口或可传入任意 SQL 的外部 API。Reader 不依赖 Events/Activity；命令缺任一必要端口（含 typed nil）返回 `DEPENDENCY_UNBOUND`，不能降级“只写表不发事件”。当前生产 root 不创建/暴露 Work 服务；测试组合必须注入已验真实 Account/Project Authority、Outbox Appender，单纯成功 stub 不构成验收。

### 3.1 每页当前授权和稳定 cursor

所有普通读自行开短只读事务，当前 Human/Project Read gate 后只查本域；无 Touch、命令行、事件或补组写入。ListSprints 必须精确验证父 Milestone 属于已授权 Project，不存在/外项目为 NOT_FOUND；合法空组才返回 `items:[]`。Get 不用标题或 Project 路由作为身份。

cursor 使用现有 Keyring，Binding.Scope=`identity.InProject(project)`；QueryDigest 为 canonical-v1 的 `{format:1,kind:"work.milestones"|"work.sprints",project_id,owner_user_id,milestone_id:null|ID}`；不含 Session、limit、title/正文。Order 固定 `manual_rank:asc,id:asc`。Position 恰为 `[Text(rank),UUID(id)]`，OrderGeneration 必须非空/正整数。先当前授权和父验证，再验证签名/闭集/type/scope/query/order；代数不同 `CURSOR_STALE`，其余 token 错误 `CURSOR_INVALID`。失效 Session 不因坏 cursor 改为泄漏历史页。

每页同事务读取 generation 与 `(rank,id) > cursorPosition` 的最多 limit+1 行；多一行仅决定 next_cursor，不能作为额外 item。最后页无 next_cursor；允许续页改变 limit。内容 title/description 可在页间变化，不承诺全程快照；结构/物理 rank 变化令旧 cursor stale，调用方重读首屏。key rotation 沿既有 kid，不自发弱签名/无签名模式。只读锁使同页不会混合两个代数。

### 3.2 同 Tx 与锁 union

一律先验证本 Store 的活 Tx，再 RequireHeldLocks 验证所需完整绑定；内部 InTx 口不补锁、不内开 Tx、不 commit、不发外部网络或 Provider I/O。EX 满足 SH；不能临时 SH→EX 升级。所有动态 AppendPlan 锁与本域锁先 Normalize/去重取较强模式，再**首次一次 AcquireAll 完整取得**：Command0 → User/Outbox registry1 → Project2 → RankGroup/ProjectSchedule3 → SprintAggregate5 → event record6。既有 NormalizeLocks 在去重前即拒绝原输入长度 >512；本卡一次一个 target/至多一个事件，锁集合为常数大小，不为 rebalance 的每个 sibling 增加锁（Project EX 已保护整组）。拼接包含重复项的输入也须满足该上限，不截断或只核去重后数量。

| 操作 | 本域最小锁，最终发事件还须并入 AppendPlan.Locks |
| --- | --- |
| 六个命令的准备/最终事务 | 原 CommandIdentity EX；User EX（最终成功 Touch）；Project EX，明确以粗粒度结构 guard 换取无 MilestoneAggregate 扩口 |
| Create/Reorder Milestone | 上述＋RankGroup EX，key=`work.milestone:<project UUID>` |
| Create/Reorder Sprint | 上述＋RankGroup EX，key=`work.sprint:<project UUID>:<milestone UUID>`＋目标 SprintAggregate EX |
| Update Sprint | 通用命令锁＋目标 SprintAggregate EX；只写 title/description，不需读出的旧 rank/parent |
| Update Milestone | 通用命令锁；不虚造 MilestoneAggregate |
| Get / List | User SH、Project SH；GetSprint 加目标 SprintAggregate SH；List 加对应 RankGroup SH；无 command/Activity 锁 |
| LookupCommand | 原 CommandIdentity EX、User SH、Project SH；可确定原 writer 已退出，不只做无锁 SELECT |
| ReadPlacementInTx | caller 已持 User SH、Project SH、ProjectSchedule EX、目标 SprintAggregate SH；先完整验 Store/Tx/locks，再真实 RequireOwnerInTx(Read) 和 Work 父子读取 |

Project EX 的加强只属于本结构实现策略，不改 foundation 全局枚举或 D01 普通 mutation 的最低锁约定。后继所有 Work 结构/lifecycle 及 Task placement writer 必须遵守 §7 的共同 gate；若改细粒度，另审全依赖和 phantom 保护，不能移除 guard 后沿用本验收。

ReadPlacementInTx 只返回当前真实 Milestone/Sprint 和完整状态，不提供可保存授权 token，返回后不得逃逸 Tx 当后继授权。调用方要写 Task 必须在同 Tx 另完成 Mutate gate、planned/current 与真正 Task/occupancy 检查；completed 返回读取事实，不据此许可修改。缺 Authority 或无效/异 Store/已结束 Tx、缺 ProjectSchedule/Sprint 锁明确失败；不能返回默认空 Placement。

## 4. 命令、错误次序、幂等与 Unknown

### 4.1 完整语义与有序门禁

CommandIdentity 固定 `namespace="project", owner_ids=[project_id], command=六个完整 work.* 名, key=meta.IdempotencyKey`。摘要使用版本化 `work-structure-command-v1`＋canonical-v1，包含 command、project、目标 ID、CreateSprint/ReorderSprint 的 Milestone ID、稳定 Human user_id、expected_version 的 presence/value、所有 request 字段及 patch presence、before 的规范语义（请求省略 tail 在摘要中展开为 null，入站显式 null 仍拒绝）。排除 RequestID、SessionID、CSRF、超时、生成 event/command ID、服务端 rank/generation/time。body 无任意 trim；有序对象引用不做集合排序。不同 Project 或不同 command 下同 key 为独立身份，同 scope/command 下换 target/actor/字段/expected/before 为异义。

对合法 Actor/输入下，按此顺序，不搬用 Model lifecycle/Secret 专有规则：

1. 入口 ctx/纯语法检查：未构造 Actor 为 UNAUTHENTICATED；合法 AgentRun 为 DEPENDENCY_UNBOUND；Service 为 FORBIDDEN；非法 DTO/meta/ID 为 INVALID_ARGUMENT，不查询数据。
2. 首次完整锁 union，真实当前 Session、Project Owner/initialized/Read gate。当前 Session 撤销/过期优先；不属 Owner 或无 Project 为 NOT_FOUND（沿已验 Authority）；未初始化、deleting 为 PROJECT_NOT_ACTIVE。System admin 不旁路。archiving/archived 可以读历史。
3. 在原 identity 串行下读取命令。若 actor_user_id 不同，NOT_FOUND；同 key 异义为 IDEMPOTENCY_KEY_REUSED；已 completed 同义返回原 receipt，先于 expected_version、当前 completed、容量和新写依赖，不 Touch/不 append。Session 更新不改稳定 user 摘要，但须使用新有效 Session 重新授权，旧 Session 不可 replay。
4. 没有 completed 才 RequireOwnerInTx(Mutate)：仅 active。planned 不是成功、不享有归档写例外。再查 target/父及其项目归属（不可见为 NOT_FOUND）、create ID 占用（RESOURCE_BUSY/TARGET_OCCUPIED）、expected_version（VERSION_CONFLICT/STALE_VERSION）、Sprint completed（INVALID_STATE/COMPLETED_SPRINT_IMMUTABLE），依次拒绝。
5. 检查 anchor/同组、容量、rank 与所有局部 invariant；合法 no-op 仍需完成前面所有新写检查。最后生成/验证完整结果与必要事件/Activity 计划，原子提交。

Update 仅改显式字段且至少一个值真实变化才 +1/version/time/event；无变化持久一个 successful no-op receipt，首次同样 TouchActivityInTx。Reorder 用最新身份顺序按 §2.2；create 已占用同 target 的不同 key 不当作 replay，不自动改生成 ID。错误均为现有 foundation.Code＋安全 FieldError（如 `/expected_version:STALE_VERSION`、`/sprint_id:COMPLETED_SPRINT_IMMUTABLE`、对应 `/milestone_id` 或 `/sprint_id` 的 `TARGET_OCCUPIED`、对应 `/project_id` 或 `/milestone_id` 的 `GROUP_LIMIT`），不扩 foundation closed Code。禁止输出 body、key、Session、SQL 或外项目实体详情。

Create 的 caller target ID 若已被任一 Project 的同类 canonical 对象占用，统一安全 `RESOURCE_BUSY/TARGET_OCCUPIED`，不透露对方 Project/正文、不重绑。不同 Project 并发使用同 target ID 时，数据库 PK 是最终防线；只能在确定失败/回滚后按本域精确已知约束映射此错误，不能靠 SQL 文本猜测或在 poisoned Tx 内继续写。其它本不应发生的 FK/rank/canonical 约束冲突安全失败，不隐式改 parent/rank 再提交。

明确未提交的业务失败不保存 successful receipt，不固定失败结果；可以原 key/同语义再走当前检查。planned 行可以保留原身份/原输入用于重试，但不赋予继续写入资格，不吸收异义请求为新意图。

### 4.2 准备与原子终局

Outbox PrepareAppend 会在事务外调用 producer Discover 并添加 registry/event 锁；因此采用明确两阶段，不在持业务 Tx 时调用它来补低序锁：

1. 准备短事务：取得本域完整 locks，执行 §4.1 当前检查，读取真实 Work canonical/组 generation，持久 `planned` 原输入/期望前事实/拟 postimage/typed event Header+payload。此事务**不改 canonical、generation、Activity、Outbox**。no-op 可直接在一个同义锁事务保存 completed receipt＋Activity，不创建假事件。
2. 事务外从固定计划 Restore typed Event，调用真实 Events.PrepareAppend。Work producer 只为匹配的持久计划发 opaque dependencies；Project gate 独立做闭集绑定。缺失/失败不假装事件成功，planned 不等于业务通过。
3. 最终事务：AcquireAll 本域＋AppendPlan 完整 union；重新当前 Read、identity/semantic/receipt、新写 gate、目标 expected、parent、生命周期及计划适用性。旧 Session/Actor/Project/issuer/plan_revision/event 替换不沿用计划。以当前 locked facts 再确认实际 changed/position；canonical、version/time、必要纯 rebalance、组 generation、Outbox、成功 receipt 和 Activity 在此**同一个物理 Tx**提交。Activity 使用真实 Account port，同 Tx 验 User EX；失败全部回滚，历史 replay/Lookup/普通读不 Touch。
4. 最终必须匹配被准备的 plan_revision/header/payload；如果 rank 维护、兄弟结构变化或其它非 target-version 事实令计划过期，释放并结束整个未提交 Tx，再按原命令语义重新规划。最多 3 个规划轮次，耗尽 RESOURCE_BUSY；不更新 expected_version、target、parent 或 before 身份，不在同 Tx 临时补锁。target version 已改变直接 VERSION_CONFLICT，不把它称内部重规划。

重规划须在原 CommandIdentity EX 下确认仍 planned、无完成 receipt；每次新 plan_revision 替换整个不可变规划与新事件身份，旧 opaque plan 无效。不能在可能仍有 final writer 的情况下根据“看不到 Outbox”自行替换；所有 writer/lookup 共享原 identity 锁。计划更新本身不产生业务 version/Event/Touch，不允许无限计划循环。准备和重规划取数据库时刻作为拟业务 occurred_at/updated_at，必须不早于旧 updated_at；最终 committed_at 是实际最终事务数据库时刻，二者不宣称等同物理 COMMIT 时间。

完成顺序允许先写拟 canonical，再调用 AppendEventInTx 验证 NewFact，最后保存带 event_id 的 receipt；任何失败由同 Tx 全回滚。事件存在但 receipt 未提交、canonical 已改但 receipt/事件失败、只提交 rebalance 均不可能成为成功分支。completed 原事件若已清理不能用 producer 重新生成它：已 completed 仅 CurrentAccess 历史验证，NewFact 必须拒绝。

### 4.3 COMMIT Unknown、取消和原请求恢复

每个准备/最终事务使用原 CommandIdentity 作为 primary cause；attempt_id 只用于诊断，不作新业务 key。WithinTx 的 Unknown 原样保留。禁止用 `context cancelled`、连接关闭、clock 超时、无锁 SELECT 空、planned 存在或“未看到事件”推断完成或回滚。

命令遇 Unknown 时至多进行一次 **只读** 确认：独立、最多 3 秒 context（沿既有 Project writer 串行确认模式，包含在调用/资源总预算），原 identity EX 后重做当前授权/semantic 检查。看到同义 completed 才返回原成功；确认缺行才知该准备尝试未留下记录，可返回明确 NotCommitted；看到 planned 只证明准备事实，仍返回原 COMMIT_UNKNOWN，不能在本次 Unknown 分支执行 final/replan。确认超时、授权变更、读取失败、确认本身 Unknown 均保原 `COMMIT_UNKNOWN/unknown/retry_hint=lookup`，不伪造 not_committed 或成功。

显式 Lookup 使用 caller context，必须串行等待原 writer：completed→原结果；planned→in_progress（只表示未完成的持久准备记录，不证明有后台 worker/live goroutine）；无行→not_observed（只表示本次锁定观察），不创建行、不自动执行。只有保留原请求的显式 Execute 才能继续合法 planned 或确认无结果后的原命令；继续时完整重验当前状态。archived 允许 Lookup/已完成 replay，拒绝 planned final；没有“档案可读即可以补写”的分支。

调用前或 COMMIT 前已确证取消/回滚，传播可 errors.Is 的 ctx.Err/NotCommitted，全部未提交变化消失。已确定 committed 的结果不能因随后 delivery ctx cancellation 改写为未提交；未知仍未知。Service.Stop 拒绝新 admission、取消本实例已登记命令/Lookup；Drain 等这些调用、最多3秒的确认 context、Rows 和真实 Tx 返回，不能以 cancel 通知代替 join，也不关闭注入的共享 Store/Outbox。Reader/Authority 是同步端口，其调用由组合者逐项等待；Service.Drain 不冒称已退休独立 Reader 或其它 Service 实例。无后台自动重放、无租约过期即抢占，也不新增 process-death 服务。

## 5. Typed Work producer 与 Project gate

### 5.1 恰两个事件 schema

生产者固定 `work`。恰 `work.milestone_changed` / aggregate `work.milestone`、`work.sprint_changed` / aggregate `work.sprint`，schema_version=1。Header scope 必为相同 Project、aggregate_id 为目标、aggregate_version 为该次成功后的业务 version、aggregate_sequence 必为空，OccurredAt 对应本次 planned postimage 的 updated_at。两个 `event.DefineEvent[T]` 均在 Outbox.New Seal catalog **之前**注册；与既有 Project/Model schema 并存，不更改其定义。`RegisterWorkEvents(catalog)` 返回 `WorkEvents`，其 NewMilestoneChanged/NewSprintChanged/Restore 严格使用同一 catalog 的 typed schema；不是外部任意 JSON builder。

payload 闭集：

```text
MilestoneChanged {
  command_id: CommandID, actor_user_id: UserID,
  change: created|updated|reordered, changed_fields: ChangedField[],
  position: null | {previous_id: MilestoneID|null, next_id: MilestoneID|null,
                    order_generation: Version}
}
SprintChanged {
  command_id: CommandID, actor_user_id: UserID, milestone_id: MilestoneID,
  change: created|updated|reordered, changed_fields: ChangedField[],
  position: null | {previous_id: SprintID|null, next_id: SprintID|null,
                    order_generation: Version}
}
ChangedField = description | manual_rank | title
```

created 的 changed_fields 恰 `[description,manual_rank,title]`；updated 为实际变动的 title/description 按字节排序、非空且无重复，position=null；reordered 恰 `[manual_rank]`。created/reordered position 非空，两个邻居以最终逻辑顺序给出、边界显式 null，不可等 target/彼此；group generation 为成功后值。payload 不含标题/description、业务 key/digest、Session、SQL、raw rank 或整 canonical 对象。Header 提供目标/版本，position 与 changed_fields 表达历史变化，不靠消费方拿当前对象猜当时改变。普通结构事件不是 TaskEvent/Platform Audit，不新增 Work Audit producer。

### 5.2 Work Authority 必须校验真实事实

`DiscoverAppend(ctx,actor,summary)` 用 EventID 定位本域持久命令/plan；只作 key/锁 discovery，不成为当前授权。summary 必须精确等于原 typed canonical Header＋payload digest，命令类型/target/project/actor user/plan_revision 及变化种类一致。Work 的私有 PlanIssuer 生成 dependencies；binding 至少包含目的 `work.structure.append-v1`、实际 ActorDetails（含当前 Session）、完整 summary、command identity/ID、plan_revision、规定两阶段和规范 locks。opaque 不含正文/原 key，可存受限 command_id/plan_revision；Matches 以实例 issuer＋digest 为准，不以 JSON shape 或 caller 造的依赖替代。

`ValidateAppendInTx(...,stage)` 先本 Store 活 Tx/完整持锁、issuer/binding/opaque/locks 精确匹配，再当前 Project Read gate、原 command user/semantic/事件匹配。CurrentAccess 对 completed 可验证其不可变历史 receipt/typed event，不要求目标仍停在历史 version。NewFact 只允许 active 的 Mutate gate、该 exact planned 命令，以及本 Tx 中真实 canonical postimage/target version/parent/title-description实际结果/组 generation/最终邻居与计划逐项一致；不能仅比较 event header、调用方 postimage 或命令“存在”。本域同时核状态与原前事实/changed_fields一致、no-op 不得伪 emit。事件签名成立但无命令、错 row/阶段、跨 Project/Actor、另一 Authority issuer 或仅存 raw payload 均拒绝。

所有 producer 验证读写沿 caller Tx；绝不在 Validate 里 Acquire/补锁或开内层 Tx。真实 Outbox Appender 再校验自己 sameStore/plan/current Session/local gate。Event plan 是一次实际 Session 的授权依赖，换新 Session 只能从同一稳定原命令重新 Prepare；不能把跨 Session 业务幂等误解为复用旧 AppendPlan。

### 5.3 Project-owned 精确扩口

新 `project/work_event_authority.go`，仅 `project/events.go` 两个 Discover/ValidateInTx 分派入口窄增 producer=`work` 分支。闭集恰 §5.1 两个匹配的 type/aggregate/schema、ProjectScope、同 project、Human、合法 aggregate version、无 aggregate sequence、AppendProject。其它 Work type/schema/aggregate/action 不在本卡授权；未知 producer/动作保持 DEPENDENCY_UNBOUND，伪造已绑定身份/issuer等沿 FORBIDDEN/INVALID_ARGUMENT 安全分类拒绝，不把 switch 改为任意 producer 通行。

Project gate 的 issuer 为该 Project Authority 实例自有 projectIssuer，purpose=`project.work-structure.append-v1`；binding 固定完整 ActorDetails/Event Summary/Project/两阶段 `[CurrentAccess,NewFact]` 与规范 User SH＋Project SH 锁。Discover 只为 CurrentAccess 生成计划；Validate 两阶段复用同计划，先 same Store/活 Tx/完整持锁与 exact issuer/binding/opaque/locks，CurrentAccess 调 RequireOwnerInTx(Read)，NewFact 调 Mutate。它只证明 Project gate，Work command/canonical 另由 producer 证明，不读取 Work 私表。

保留 Project/Model/Lifecycle 原分支和错误门禁；新代码可 import Work contract 常量但不 import Work 实现，防依赖环。Outbox、identity、foundation、Audit 公共接口均不需要改；若实现发现不能使用现有签名，先交具体原因和精确路径给 root，不自行增契约。

## 6. 精确产品写域与共享交接

下表为已接受 SPEC 的 **18 路径闭集（17 技术＋README 末件）**。root 已移交 fixture_recovery 的当前范围仅为 #1–17 的 scratch 完整实施候选；main 安装、Go/Node/资源和 #18 README 仍未授。规格作者没有产品写权；原候选 18 路径保持，未扩大接口或文件范围。

| # | 路径 | 唯一业务作者/责任 |
| --- | --- | --- |
| 1 | `internal/central/work/contract/structure.go` | fixture_recovery：DTO、pure validation/digest/接口 |
| 2 | `internal/central/work/contract/structure_test.go` | 同上：类型/错误/编码/摘要 |
| 3 | `internal/central/work/contract/events.go` | 同上：两个 typed schema |
| 4 | `internal/central/work/contract/events_test.go` | 同上：payload/闭集/安全编码 |
| 5 | `internal/central/work/service.go` | 同上：构造、依赖、调用 Stop/Drain |
| 6 | `internal/central/work/structure.go` | 同上：六命令/Lookup/Unknown |
| 7 | `internal/central/work/rank.go` | 同上：rank/重排/内部 rebalance |
| 8 | `internal/central/work/reader.go` | 同上：四读/placement/cursor |
| 9 | `internal/central/work/repository.go` | 同上：本域持久化、Store、闭集错误辅助 |
| 10 | `internal/central/work/events.go` | 同上：真实 Work producer |
| 11 | `internal/central/work/structure_test.go` | 同上：rank/plan/停止/公共边界 pure tests；可通过公开 Project gate 做必要无资源组合 |
| 12 | `internal/central/project/work_event_authority.go` | 同上：新精确 Work gate |
| 13 | `internal/central/project/events.go` | 同上：仅两个 dispatcher 分支，原 Project/Model/Lifecycle 保持 |
| 14 | `db/migrations/00021_work_structure.sql` | 同上：root 唯一预留迁移，五表/局部约束 |
| 15 | `tests/work/fixture_test.go` | 同上：integration 标签，真实 Authority/PG/Outbox 组合及有界真实 PG commit proxy；私有辅助不另改共享 helper |
| 16 | `tests/work/structure_test.go` | 同上：migration/persistence/authority/atomicity integration |
| 17 | `tests/work/structure_concurrency_test.go` | 同上：并发/rank/Unknown integration |
| 18 | `docs/development/backend/README.md` | 技术接受后 root 单独移交末件 |

没有 HTTP/OpenAPI/前端/生产 root、App、Work Audit、全局锁枚举、公共脚本、go.mod/sum、旧测试/helper、Project pointer writer 的隐含授权。新增 probe 可在独验 scratch 与单虚拟 test target overlay 中经 root 单授，不能直接落未授权仓库路径。

Project events.go、全局迁移嵌入输入/最终 Go 图、Go/cache、数据库 fixture 和 README 均是共享点。Model Settings UI 的 active Go/PG/browser 窗内不得安装本卡 #12–14 或改变它们依赖的输入。可以在互不冲突的 scratch 准备；安装顺序、最小 fingerprint 差量重绑和所有资源窗由 root 唯一调度。两个任务不能把目录不同当作 Go/cache/迁移独立。

## 7. 后继真实绑定责任

| 后继能力 | 事实 owner / 正式前置 / 必验边界 |
| --- | --- |
| Task placement | D11 Task canonical 消费真实 ReadPlacementInTx，同 caller Tx/Project gate/ProjectSchedule＋Sprint 锁；另做 Mutate 与 completed 拒绝；不能持久化 caller 提供的 Milestone 关系，不能将读口称 Task 已接通 |
| Sprint membership / DeleteSprint | 同 Work Task canonical 是 membership Source of Truth；同 Tx 从指定 Sprint 查真实 Task，不能信客户端 task_ids/count、缓存或未实现模块为空。首次 planned 且 no Task 的删除正向验收必须在 Task 卡/后继窄卡完成，与并发 Task create/move 共享 ProjectSchedule；current/completed 禁止删除 |
| WorkOccupancy / PendingDispatchReader | [D01 domain-lifecycle](d01-contracts/domain-lifecycle.md#work-命令与占用端口)已定义；D22/D23 负责真实 Execution/Dispatch adapter 与竞争验收。缺 adapter/typed nil/读失败一律 Unbound/Unavailable，不是 empty；waiting/取消未 join 仍占用，Task 删除必须核历史而非仅当前空闲 |
| Sprint lifecycle / current pointer | D11 后继由 Work 核真实 Task/moves/TaskEvent/占用，Project-owned 同 Tx pointer writer 另冻结；不由 Work 直接改 Project 私表。不因本卡空 Sprint 可创建而提前称 start/complete/rollover 通过 |
| Work Project 清理 participant | D08/D11 后继负责整个 Work 域的停止、Inspect、Cleanup、命令正文和引用清理；目前未注册。两结构表/五表清理不能自称 Work 全清，不造 no-op participant 或 empty Inspect 让 Project 删除通过 |
| 对外/生产组合 | D15 Tool、D25 投影/查询消费、D26/D27 HTTP/UI、root/App readiness 各有后继卡；本卡只在真实隔离库组合验证，不开放生产服务、不改 ready503 |

本卡不为了列齐未来功能预造 Delete/occupancy 等全套函数。若调用方现在要求这些能力，集成缺口明确 DEPENDENCY_UNBOUND，不能加返回空集合的实现。也不复用 `agenteam_project.work_claims`（仅 creation/lifecycle claim）或 Object/Artifact project_work 当 Task canonical。

## 8. 验收矩阵与实际执行边界

实施者读 [Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)，验证者读 [verification 技能](../../../.agents/skills/agenteam-verification/SKILL.md)；角色/升级/证据规则复用[团队流程](../agent-team/README.md)。本卡列的是将来门槛，当前未运行编译、测试、PG 或资源。

### 8.1 pure/STATIC 与离线

必须覆盖：全部 DTO 精确边界/非法 UTF-8 与 surrogate/版本0和溢出/UUIDv4/patch presence/响应 union/安全 fmt；六摘要的字段变化、新 Request/新 Session 相同语义、raw 正文区别、target/Project/actor 隔离；固定 rank midpoint/组界限/rebalance 保序/no-op/completed 维护边界；cursor shape 与 generation；typed catalog/两个 payload 的替代拒绝；plan 私有 issuer/拷贝/新旧 Session/各 bound 字段；nil/typed-nil/foreign Tx/缺锁/反向锁/内部补锁拒绝和 Stop/Drain 真实终局。

受 root 单授离线窗口时，先对 #1–13/#15–17 的确定 Go 文件 gofmt；`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`，Go/cache 唯一 owner。适用命令：`go test -race -count=1 ./internal/central/work/...`；`go vet ./internal/central/work/... ./internal/central/project`；仅编译 `go test -race -tags=integration -c ./tests/work -o <task-private-binary>`；以及下列精确 Project pure 回归。每个命令外层 hard deadline≤45s，输出/child tree 实际 wait/join，不能以 timeout wrapper 退出推断内部退出。不调用全库脚本/网络/自动安装；超过预算先封失败交 root，不悄悄增时或拆掉必需场景。

Project pure 精确 selector：

```text
^(TestOwnerAuthorityOrdersLockSessionAndFacts|TestForeignOwnerAndUninitializedRowsAreNotGrants|TestMissingOrdinaryProjectDoesNotExposeTombstone|TestEventFactUsesTypedCanonicalPayload|TestAppendPlanBindsCurrentSession|TestModelProjectEventUsesSameCompletePlanForReadAndMutate|TestModelProjectEventPlanClosesActorSummaryIssuerAndPurpose)$
```

它们保护唯一旧写源两处分派的 Owner/Project/Model 原行为；不是 Work 真实 producer 验收。须核实际发现名/执行数，no-tests/Skip 不计 PASS。需要精确 `-list` 时另按已编译 binary、generated testmain/init/TestMain 闭包确认可安全发现后 root 单授，不能把编译误称 test body 已过。

### 8.2 六个新真实 PG top

所有 top 使用 `//go:build integration`，不并行 `t.Parallel`。名称固定如下，子例可组织但不得遗漏矩阵。一次执行单 top，以便单窗口的输入/失败/退役可归因。

| top selector | 必须真实到达的验收 |
| --- | --- |
| `^TestWorkStructureMigration$` | fresh 00021、populated00020→21、重复/失败回滚；局部同项目 FK/rank unique/生命周期约束；原表/数据保持，无运行时建业务 schema |
| `^TestWorkStructurePersistenceAndPaging$` | 真实 Human Authority＋Project Create/隔离初始化＋六命令；两 Project/两 Milestone/多 Sprint 层级；重名可存；copy-safe receipt；至少 205 项跨默认/改变 limit 翻页，无过滤截断；HMAC/kid/Project/父/owner/generation 替换；空合法组与缺依赖不同 |
| `^TestWorkStructureAuthorityAndReplay$` | revoked/expired/foreign/admin/AgentRun/Service、未初始化/archiving/archived/deleting；当前授权先于 receipt；同义 replay 先于 stale/容量/completed；cross Actor/Project/command/key/target；新 Session replay；planned 归档拒绝；当前/完成 lifecycle 由明确 test-only 同 Tx 一致事实提供，不声称 lifecycle 命令通过 |
| `^TestWorkStructureAtomicityAndProducer$` | mutation/version/generation/command receipt/Outbox/Activity 一起成功或回滚；真实 producer 两阶段/伪造 Header/payload/ID/plan/issuer/Session/同 Store/Tx/locks；无 canonical/只 raw event 拒绝；原事件重复 CurrentAccess 与不可补写 NewFact；错误/取消/Rows 终局；无第二事件、无 Audit 假事实 |
| `^TestWorkStructureConcurrencyAndRank$` | 同 key 两 Service 实例实际 Command lock 竞争；不同 key 同 target version 一胜；不同父组/同父尾部并发无孤立与重复 rank；真实 rebalance 触发（受控合法紧邻存储 ranks）保全部旁观对象业务 version/time，completed 稳定；旧cursor stale；原 Update 与 reorder/rebalance 竞争不写回旧 rank/parent；3轮重规划上限和旧 plan 拒绝 |
| `^TestWorkStructureCommitUnknown$` | 准备 COMMIT 与 final COMMIT 分别经 owned loopback PG 协议代理：实际旧 writer 未终局时 Lookup 等待/超时保持 Unknown；release 后真实 commit/rollback 分支、同义新 Session恢复；确认错误/取消不变伪成功；提交前取消回滚、已确定提交后 deliverycancel 不撤销成功；无自动重执行/新 key/重复 Event/Touch；完整子协程/连接/Tx join |

正向 fixture 的用户/Session 可按已验 B02 专属持久输入构造，再由真实 Account Authority 验证；不得用返回 true 的 Session/Owner stub。Project 必须走真实 Create/confirmation；隔离 Skill receipt 是显式 test-only schema 中的真实持久事实，不是 D10 成功声明。普通初始化链不调用 Object Runtime/ProcessGuard；fixtureProcess.ConfirmStopped 应继续 fail-closed ResourceBusy，不能假造进程死亡。编译闭包可能含纯 Object keyring，不等于运行或重开停工任务。

需要 current/completed/撤权等未来/安全状态前提时，仅 fixture 使用精确对象的真实 DB 事务和规定 gate/调度锁植入一致事实，记录入口与不足；业务待测调用始终经过真实 Authority/Work。不得把 test-only seed 说成已验 Start/Complete/Login。代理必须实际转发/hold PostgreSQL COMMIT frame，不能用 fake Store 的 Unknown 返回值代替；控制握手和 waiter 应可观测 exact key/mode/Tx，不能只 sleep 猜竞争。

### 8.3 必须保留的旧 PG 回归与独立不同构造

受影响的旧能力精确回归各自单轮，不跑含停工任务的全套脚本：

- `./tests/project`：`^TestProjectB02OwnerPortRequiresCallerTransactionLocks$`、`^TestProjectB02AuditEventReceiptTouchAtomicityAndNoOp$`、`^TestProjectB02ReceiptRequiresCurrentSessionAndReplaysAcrossRenewal$`。保护原 Project gate、原 producer/原子性和当前身份/历史结果顺序。
- `./tests/outbox`：`^TestOutboxAtomicAppendCurrentAuthorityAndReplay$`、`^TestOutboxMissingLocksPoisonAndMappingChangeRollsBack$`。保护共享 Outbox 双阶段/完整锁与历史投影。其 fixture 的受控 producer 不替代本卡新 Work 真实 producer；未知提交的真实门槛由新 top 达成。

原结果若确实绑定未变源/依赖且适用可由独验明确复用，不能把不同 migration 图/唯一 Project dispatcher 改动默认视为无影响。独验至少另用 **两个不同构造** 的私有 PG probe（root 另授 overlay/资源），不复制作者 helper/断言冒独立性：

1. A：先开启真实 Account 撤权或 Project 归档事务并持精确 gate，让另 Service 的 prepared/final/历史 lookup 成为可见 waiter；提交撤权后必须拒绝，反顺序已开始短 Tx 可先提交。核四类 durable 事实零额外变化，fresh Session/另一 Project 隔离；不以 stub authority 或只换测试名代替。
2. B：原 final writer 在实际 COMMIT frame 上被隔离；另一连接尝试原 identity lookup 与另 key 的 rank 操作，证明未在旧 writer 未终局时生成第二计划/事件。分别 commit/rollback 后验证原回执 bytes、当前 sibling order/generation、旁观业务版本及 no-op/重放无重复；至少再用不同密集 rank 几何触发一次自动 rebalance，防只验证作者选的 midpoint 向量。

两个代表之外，独验完整静审全部18路径/授权与恢复矩阵，对作者实际原件逐项结论。作者自测、独立 probe 和最终组合证据分别记录；原失败/未达分支不回写为通过。只有整卡门槛通过后才移交 README #18，最后独核其与真实范围一致；不抢先标记 D11 已完成。

### 8.4 资源和时间基准

实际命令/参数/ENV/依赖闭包由实施/独验在本卡下准备并封 STOP，root 授唯一窗口后才执行；本规格没有启动资源。保原基准：离线每命令≤45s；每新/旧 PG top **120s 含 setup/body/cleanup**；Go package hard timeout≤6m；启动前 fresh 可用磁盘≥5GiB；owned/resource cleanup 后的补充 host TCP delta 尾部两次清空观察总限≤75s（不证明完整短连接轨迹或 tuple ownership）；原七资源 ID 管理上限/退役方法适用。PG-only 不为凑七个 ID 启动 MinIO/browser/Node，实际需要的容器/network/目录/端口/代理/child 列精确有限清单；若沿原七 ID fixture，就七项全部入两次不存在证据；新增或超出需 root 明授，不自动扩大。

输入固定到实际最小 Go 闭包/迁移/工具与 helper，按源变化做差量，不机械沿用旧903/486等数量，不复制大图。绑定 Go local toolchain、race 与 build tags；浏览器/Node/web/dist/schema 不属于本库输入。安装 #12–14 后按当前闭包重新绑定离线 binary与后续各轮，不能拿安装前编译结论覆盖新迁移。

finally 先停止 admission/取消，实际 join Work/PG proxy 的 handler/serve/forward goroutine、store Rows/Tx，完整关闭监听/owned连接；wrapper 对 direct/adopted/watchdog 各记录实际 wait，不把 status观察或 SIGKILL发送当 wait。核 owned runtime清单及 declared resource IDs/容器/network/端口的两次退役与输入前后同；daemon/PID1 shim 非 owned 另列，不 wait、不声称全机清零。cleanup 失败即本轮失败/STOP，保 raw/metadata，不串到下一轮或自动重试；若失败无新证据交 root 处理。资源窗内不 Git/换共享输入/开其它 Go/cache/业务资源。

## 9. 接受条件、未绑定清单与升级

当前：root 已接受完整独立 SPEC（rev1 全文＋唯一 D1 修订的最终组合），并将 #1–17 仅 scratch 实施移交 fixture_recovery；00021 仅预留。main 安装、共享 Project/migration 窗、Go/Node/真实资源和具体执行计划/selector/input freeze 仍须 root 单独移交并与 Model Settings 活动输入分离；#18 README 未授。尚无产品实现或动态验收通过结论。

完成时必须能证明：真实层级/版本/rank/分页；当前权限→receipt→新写优先序；sameStore/liveTx/完整锁；跨 actor/project 幂等/Unknown；typed producer＋Project closed gate＋canonical facts/receipt/Event/Activity 原子性；必要旧兼容和不同构造独验；所有 owned 实际退役；README末件准确。不以“接口有了”“库可编译”“prepared PASS”或一个 top 通过替代整卡。

Task membership、Sprint 删除正向、Milestone 删除、跨 Milestone move、Task/TaskEvent/context、Execution/Dispatch、Sprint lifecycle/pointer writer、Work lifecycle participant、Tool/HTTP/UI/App/root 与生产 ready 均仍未绑定/未提供，后继责任见 §7。若发现 Task 事实是当前非破坏性结构命令的实际必要条件，应以具体触发/接口/来源报 root，不能注入 empty 绕过。需要公共新锁类型、跨域直接 SQL、新 producer/Audit/HTTP、额外产品字段/limit或本闭集外路径，先停止受影响部分列精确差量，不能因本卡“结构”名称获得宽泛域写权。

## 10. 最小静态输入与 SPEC 封存

本卡采用下列必要共享源码指纹及声明签名，配套来源清单/作者静态自查由规格交接 freeze 固定。它不是完整 Go dependency graph、不是当前全源通过证据。历史正式接受依据在 §1；更早有界候选 `28034095f87c68dff09a88fc37e5ce7e10e7817cac07e24259617c9715876c68` 仅为方向来源，其未填工程项由本 rev1 明确冻结。

| 必要共享输入 | bytes | SHA-256 | 签名/影响 |
| --- | --- | --- | --- |
| `internal/central/project/authority.go` | 9818 | `a9e7d5768a18c23abf9a1cf86a396bbe6901d348c70b16c48b32fa559fa95321` | RequireOwnerInTx/current：当前 Session、Owner、Read/Mutate |
| `internal/central/project/contract/lifecycle.go` | 29744 | `5b74f0a959bbcffde811b11d207f9ed6928bded9d51c965772d34763520ddea6` | CheckOwnerGate/CheckOwnerActorKind/ProjectAuthority |
| `internal/central/project/contract/types.go` | 12125 | `7465815c5da4e1bf24b6958d1aa1c93959ffece4256883bd343199c3ed9cfbf1` | 既有 SprintID、ProjectRef.CurrentSprintID |
| `internal/central/project/events.go` | 12341 | `4f97a16b6b9176f58c5ce05d22253664d602556a066a7c2ecec5f0995398d52d` | 唯一旧产品写域；Project/Model/Lifecycle 精确分派 |
| `internal/central/project/external_event_authority.go` | 3243 | `d7d43050ca99e20dd221564ad3a3a12c018f9dd0505e6ad68e287e8cd353a519` | 已验 Model gate 绑定模式；不更改 |
| `internal/central/project/repository.go` | 16349 | `f89d5b582b79cb31434d70c03be43a99ac1b66261591850d141263c2af44ed55` | Store 五个方法及安全错误 |
| `internal/central/account/session.go` | 4862 | `4df72697b0e66dd372be859099e7d813960d36402ca540c2a01576ff68adb0e5` | TouchActivityInTx：User EX、同 Tx、当前 Session |
| `internal/central/foundation/id.go` | 3120 | `40c6dce3cbaa3f8bcb6171fcd0ffd5b4af2d495d7a0b440607ce346fc2196e54` | UUIDv7/CommandMeta |
| `internal/central/foundation/fault.go` | 5460 | `94a339976fb8f1e1173256aa840dd1273644cafd37dc6a3e96e33d1cb8ff09de` | closed Code/安全 Fault |
| `internal/central/foundation/lock.go` | 5196 | `5f328d35a0bf313f589654c9e52b5b6b0b9f8f3f3b8502e9d1f612b588220384` | 全局锁枚举、RankGroup、SprintAggregate |
| `internal/central/postgres/transaction.go` | 9811 | `aae85b4fcf7411e31dfedd7e125c4d0aa6119b41d7071ee1787e088c14cc83f3` | 同 Store/活 Tx/EX满足SH/缺锁poison |
| `internal/central/outbox/contract/append.go` | 2344 | `4367c1e4a02fa3062da31d6d0305b5b74ed704cc434b3f0c53509e170d8be93f` | Appender/opaque AppendPlan |
| `internal/central/outbox/contract/authority.go` | 11951 | `1c78ac9f0e8c9ff996e9b9048f65d1f8d7bc005989d1021b2e5e13c2f721f5a0` | ProducerAuthority/ProjectAuthority/Stages/NormalizeLocks512 |
| `internal/central/outbox/append.go` | 8131 | `39242c8c5c1b4596c8e11bbff8f494d66b7b838abcf76667b27f872fde2c3706` | PrepareAppend完整锁与Append两阶段实际顺序 |
| `internal/central/outbox/service.go` | 8014 | `3fdfd1d2565a30984b9c912bd2e5a1a128c4d0496128005b245f194e0690e507` | producer映射与catalog Seal |
| `internal/central/event/contract/event.go` | 5820 | `0f1934a31b04706bead116552775c0b4e824dd6f3fc593f3a32808f3e22436ea` | typed Event/Header/Summary |
| `internal/central/event/contract/catalog.go` | 6395 | `55a5b24a36eec5409385a72f9be342d568eb8ca4651921bed4b25a13b2eb8742` | DefineEvent与sealed catalog |
| `internal/central/cursor/cursor.go` | 6023 | `c9f13d1cc4f40966cf545d0ea6dcc0f5c5ffc821982e54f3e680e1a604150e5f` | Scope/digest/order/position/generation签名 |
| `internal/central/cursor/canonical.go` | 3484 | `1c3a9cab07b385301f27126f4794bf4439e1a97022a3d630013b598d3a7a5836` | canonical-v1 |
| `db/migrations/embed.go` | 146 | `ec8dfa638b97c1456b5d04e536b319c5d7d4f8faea755c194796ea69be1ad578` | 全局SQL嵌入自动包含新编号 |

实际复用签名：`pc.ProjectAuthority.RequireOwnerInTx(context.Context, foundation.Tx, identity.Actor, ProjectID, identity.AccessIntent) (ProjectAccess,error)`；`oc.ProducerAuthority.DiscoverAppend(...event.Summary)`/`ValidateAppendInTx(...,Dependencies,Stage)`；`oc.Appender.PrepareAppend(...event.Event)`/`AppendEventInTx(...foundation.Tx,...AppendPlan)`；Project-owned `Discover(ProjectRequest)`/`ValidateInTx(...ProjectRequest,Dependencies)`；Account `TouchActivityInTx(context.Context,foundation.Tx,identity.Actor) error`。Store/Lock/Cursor 精确使用 §3 的现有接口，不预授改动这些只读源。

SPEC 作者仅做文档结构/闭集/本地链接/字段与权限优先序/来源 SHA 自查，无 Go/Node/PG/网络/Git/产品执行。完整 rev1 独审原 FAIL `fdd314b3`、唯一 D1 差量及最终组合 PASS `febcf667` 均保留；root 已接受 rev1＋D1，产品验收与执行授权保持上述后继边界。原候选/失败/来源字节原样保存，行政末件封存后 STOP。
