# 主线程统筹的开发团队

本页维护现行团队流程。[任务台账](tasks.md)保存全局产品状态，[开发计划](../development-plan.md)保存长期门槛；[当前分支状态](../../../.agent-state/current.md)保存跨设备接续所需的最小信息。历史验收报告保留当时事实，不以旧派工、哈希或归档步骤驱动新任务。

团队能力的采用依据与未采用的外部做法见[团队设计依据](team-design.md)。

用户与主线程讨论产品目标、可见行为和取舍。主线程负责目标、跨任务边界、一级负责人调度、最终整合与 Git 交付；一级负责人自主将完整子目标拆给二级执行者，组织局部返修、集成和验证。主线程和各级负责人默认并行派发已具备执行条件的工作，二级及更深执行者按收益继续拆分，逐级向直接负责人汇报，主线程不逐文件重做负责人已完成的审查。

主线程在任务开始时建立或接续 `ai/<task>` 工作分支，持续保存中间成果；子 agent 不自行切换分支。依赖已满足、契约稳定且写入与资源不冲突的模块和完整结果任务应尽可能并行。依赖、契约和完整验收门槛见开发计划；单项任务完成不代表模块完成，所有适用门槛仍须完整通过。

## 从产品想法到交付

1. **明确当前产品目标。** 主线程核对已确认规则，只将影响当前范围的未决产品行为、实质性范围变更或必须授权的行动交回用户。相关问题合并并附建议，不重复打开已定决定；工程细节由相应负责人处理。
2. **并行派发完整子目标。** 主线程确定共同契约和跨任务所有权，将可同时推进的工程目标、范围与验收条件交给多个一级负责人。各负责人使用[任务模板](task-template.md)引用正式规则，自主拆分并行子任务、确定文件与共享资源所有权，并选择实现与验证角色。有可用容量和就绪工作就滚动补位，不等待同批最慢任务结束；授权范围内连续推进，不增加逐角色、逐阶段的用户批准。
3. **执行、自测与主动保存。** 执行者完成实现、有意义的自测和必要文档，范围内自行选择局部细节、修正工具使用和排查问题。必要中间源码、harness和probe保存在正式路径或 `.agent-state/<task>/`，按下文触发点自动 checkpoint；未验收或暂时失败也须保存。
4. **逐级验证与整合。** 执行者停止交付范围写入并报告结果，负责人按风险组织独立审查、处理返修并集成子目标。稳定输入已有的通过证据可以复用；负责人向上交付通过、失败、未验证范围和关键证据，主线程核对整体边界与集成结论。
5. **正式交付原子变更。** 验收通过后，主线程把代码、测试及必要文档整理为完整结果交付 `main`，需要时 squash 工作分支的 WIP。保存进度不等于验收，不等待验收才推送工作分支。用户已持续授权后续开发的 checkpoint 和常规源码提交、推送，无需逐次提醒或确认。

主线程在开始、阻塞、进入验收和完成提交时汇报；持续工作期间约每 60 秒给出简短进展。子 agent 逐级报告，直接负责人消化范围内工程阻塞；只有跨子目标冲突、超出授权或需要产品决定的问题上报主线程。必须由用户决定的问题保持待定，沉默不视为选择。

## 角色与技能

十一种角色按任务需要选用；同一角色可承担一级负责人或下级执行者，也可同时创建多个实例。每个实例使用不同的 `task_name`，明确各自目标、文件和资源范围，不设置角色单例或每负责人固定配额。角色定义职责，技能提供专业方法，任务卡决定本次写入权、资源需求和验收范围。

| 角色 | 适用职责 | 项目技能 |
| --- | --- | --- |
| [delivery_lead](../../../.codex/agents/delivery-lead.toml) | 负责跨专业完整子目标：拆分依赖与所有权、处理局部阻塞、集成和组织验证 | [agenteam-subtree-delivery](../../../.agents/skills/agenteam-subtree-delivery/SKILL.md) |
| [architecture_worker](../../../.codex/agents/architecture-worker.toml) | 影响分析、工程规格与契约、验收方案；不补造产品规则 | [agenteam-design](../../../.agents/skills/agenteam-design/SKILL.md) |
| [research_worker](../../../.codex/agents/research-worker.toml) | 默认只读，围绕明确技术未知项有界查证，给出证据、限制与建议 | [agenteam-design](../../../.agents/skills/agenteam-design/SKILL.md) |
| [backend_worker](../../../.codex/agents/backend-worker.toml) | Go、Central、Runner 与共享协议的完整结果实现、自测与必要文档 | [agenteam-go-development](../../../.agents/skills/agenteam-go-development/SKILL.md) |
| [frontend_worker](../../../.codex/agents/frontend-worker.toml) | Vue 页面、组件与样式的完整结果实现、自测与必要文档 | [agenteam-vue-development](../../../.agents/skills/agenteam-vue-development/SKILL.md) |
| [data_worker](../../../.codex/agents/data-worker.toml) | 明确范围的 PostgreSQL 迁移、SQL adapter、事务、锁、幂等与数据恢复 | [agenteam-database](../../../.agents/skills/agenteam-database/SKILL.md)，按需 Go |
| [security_reviewer](../../../.codex/agents/security-reviewer.toml) | 默认只读，评审权限与当前授权、跨 scope、Secret、出站边界及负例证据 | [agenteam-security](../../../.agents/skills/agenteam-security/SKILL.md) + verification |
| [test_worker](../../../.codex/agents/test-worker.toml) | 构建或重建可恢复的 harness、fixture、确定性并发测试和真实浏览器测试 | [agenteam-test-engineering](../../../.agents/skills/agenteam-test-engineering/SKILL.md)，按需 Go / Vue |
| [platform_worker](../../../.codex/agents/platform-worker.toml) | 锁定工具链与依赖恢复、隔离环境、构建发布工具和进程排障 | [agenteam-runtime-debugging](../../../.agents/skills/agenteam-runtime-debugging/SKILL.md)，按需 Go |
| [documentation_worker](../../../.codex/agents/documentation-worker.toml) | 成组或跨文档归位已确认决定，检查引用与一致性 | [agenteam-documentation](../../../.agents/skills/agenteam-documentation/SKILL.md) |
| [verification_worker](../../../.codex/agents/verification-worker.toml) | 测试计划、证据分析、故障归因与独立验证；任务卡授权时补充指定测试 | [agenteam-verification](../../../.agents/skills/agenteam-verification/SKILL.md) |

所有角色接收完整结果目标、正式契约、Git 基线与限定 diff、明确写入/资源范围和当前可用容量；交付实际文件路径、检查结果、失败限制、下一步及可恢复片段。负责人承担设计细化、局部决策、集成和返修工作，消化范围内问题；不靠逐层转发审批维持层级。简单任务由一个执行者完成，范围内返修可复用有效实例，只有职责、独立性或上下文变化才换实例。

数据库、安全、测试和排障技能可由实现者或验证者按需组合，不必再创建对应专家。独立性取决于实际参与：编写 harness 的 `test_worker` 不能独立验收自己的 harness；参与实现决策的负责人不能形成该实现的独立验收结论。`security_reviewer` 默认只读，修复须另行明确文件所有权；`platform_worker` 的身份不授予部署权限。调研结论经负责人采纳进入正式契约，跨子目标契约由主线程协调，文档角色按成组归位的收益使用。

### 按任务选择最小团队

下表给出每项结果所需的起始能力，同类就绪结果可由该角色的多个实例并行完成；“独立验证”可由具备相应技能且未参与实现的实例承担，不要求依次经过所有专家。

| 当前任务 | 起始组合 | 何时增加或调整能力 |
| --- | --- | --- |
| 低影响文档、局部配置 | 一个执行者 + 直接负责人审查 | 成组跨文档归位用 documentation；有实际运行时未知项才调研或排障 |
| Go / Central / Runner 业务变更 | backend + 独立验证 | 先解决协议契约未知项；有可独占迁移或 SQL 子目标时加 data，跨专业集成复杂时用 delivery |
| 事务、迁移、锁与 Unknown 恢复 | backend 或 data + 具备 database 技能的独立验证者 | 真实 PG、独立连接或确定性故障注入需要新增 harness 时加 test；迁移编号仍只有一个所有者 |
| Vue 页面与缺失浏览器 harness | frontend + test；按风险安排独立验证 | 后端契约/fixture 源码需修改时加 backend；确有工具链或隔离环境阻塞时用 platform |
| 权限、凭据、跨 scope 或出站规则 | 对应实现者 + 未参与实现的 security reviewer | 验证者组合 security 和 verification 技能覆盖关键负例；需要新测试基础设施才另配 test |
| 构建、依赖恢复、卡死或性能问题 | 对应所有者组合 runtime-debugging；环境子目标交 platform | 先形成复现与观测事实；SQL 瓶颈按需组合 database，性能结论须有同场景实测对比 |

负责人主动寻找有独立结果、明确边界且能节省时间或提升质量的并行工作；高耦合修改由同一所有者顺序推进，其余就绪工作继续。新团队能力不改变产品停项、生产未绑定状态或模块完成门槛，当前事实仍以台账与目标任务卡为准。

直接负责人在卡片中列出必读技能和规格的相关段落。执行者首次读取仓库规则、当前卡与对应 `SKILL.md`，再按需读取源码和技术参考；同一任务后续只补读变化，不全量继承聊天或反复遍历产品架构。技能建议服从用户决定、仓库规范及任务范围，不授权新增依赖或扩大修改范围。

Vue 测试任务补充 [vue-testing-best-practices](../../../.agents/skills/vue-testing-best-practices/SKILL.md) 的相关参考。真实浏览器与 harness 任务以仓库内的 `agenteam-test-engineering` 为入口，使用 `tests/account-captcha-web/` 锁定的 Playwright 1.56.1；环境已有 `playwright` 技能可补充使用。执行者检查实际依赖、浏览器和资源条件，在授权范围内恢复缺项；缺少外部技能本身不阻止使用锁定 CLI。技能可发现不代表运行条件满足，外部版本、许可证和依赖边界见[技能来源](skill-sources.md)。

## 依赖、所有权与并行

并行是默认调度方式。主线程先找出跨子目标中依赖已满足、契约稳定且无写入/资源冲突的就绪工作，同时派发给相应负责人；各级负责人在自己的范围重复这一过程。收到完成、阻塞或容量变化后立即重算就绪工作并补位，不以整批完成作为下一批的起点。只把真实依赖或资源冲突的部分串行，不让一个阻塞任务冻结整棵任务树。

仓库按用户要求设置项目全局 100 个子 agent 并发值，不设置角色单例或永久子树配额。主线程协调跨树容量，各级负责人根据当前运行时可用名额和 CPU、内存、PostgreSQL 连接、Docker、浏览器等实际承载协商使用、借用与回收；阶段结束后及时报告已释放容量，独立验证需要时及时调整实现并发。不要让等待下级的负责人占满执行容量；无空位时由合适的现有实例推进，待实际释放再补位，不能反复创建撞限。

例如主线程可同时派两个 `backend_worker`，分别负责互不重叠的 Central 与 Runner 子目标，另派一个 `frontend_worker`；某个 backend 负责人有可独占的数据子目标时继续派 `data_worker`，同时完成自己的接口或集成工作。任一子目标结束后，核实资源和运行时名额实际释放，就接入下一个就绪实现或独立验证；这些角色及层数按任务调整，不预留固定比例的空闲席位。

主线程维护跨子目标依赖与所有权，各级负责人维护自己的直接下级任务和局部所有权；使用现有卡片或简短派工消息即可，不重复建立全树任务索引、永久锁表或新增批准环节。并行下发前确认：

- 所需前置能力已经实现并有适用验收证据，共同正式契约稳定。只消费上游已验收子能力的任务可先行，但须明确必要依赖、未完成能力与后续绑定责任，不能把接口声明视为已实现依赖。
- 同一文件同一时间只有一个写入者。负责人可移交文件所有权，父级和其他执行者随后保持只读；台账、卡片和规格也遵循此规则。已登记的其他任务改动属于共同工作区正常输入，不覆盖或回退。
- 公共接口、依赖锁文件、全局迁移编号及应用顺序、生成资产/目录、可写缓存、端口、测试库和 Docker fixture 指定唯一所有者或使用顺序。目录不同不代表独立；仅在环境隔离且容量足够时并发运行，冲突时串行受影响部分。确需独立 worktree 时由主线程按任务必要性创建和整合，仍检查共享数据库、缓存及进程资源，子 agent 不自行执行 Git 操作。
- 只读审查针对停止写入的范围或可定位的冻结修订。默认记录 Git 基线与限定路径的实际 diff，新增文件列入范围；冻结副本结论在整合时核对相关差异。正式集成前停止全部相关写入与后台命令；checkpoint只需暂停所保存文件的写入，仍在运行的检查如实登记，无关任务可继续。
- 需求、契约或所有权变化时，由相应负责人暂停受影响写入、保留改动并修订任务；跨子目标变更交主线程协调，无依赖任务可继续。

执行者遇到缺少产品决定或公共接口、需要未授权依赖或扩大范围、所有权冲突，或反复失败且没有新证据时，只暂停受影响部分，向直接负责人报告事实、已尝试方法和建议。负责人先处理范围内问题，确需跨子目标协调时再逐级上报。

## 按风险验收与证据复用

| 任务风险 | 执行与验收方式 |
| --- | --- |
| 简单、低影响的文档、配置或局部调整 | 一个执行者自查，加直接负责人审查；不新增无关产品测试或固定审批链 |
| 普通业务变更 | 实现者自测，由未参与该实现的实例独立审查；负责人核对边界、集成与证据 |
| 权限、数据、事务、协议、恢复等高风险变更 | 明确关键正常、异常、竞争和恢复场景，必须由未参与实现的实例独立验证相关场景，再由负责人集成 |

作者对结果和自测负责，独立验证不替代自测。验收负责人使用 `gpt-6-astra / ultra`，可按需要继续分派验证任务；独立验收的实际执行者及形成验收结论的负责人均不得参与所验实现。一级负责人组织子目标验证，主线程核对完整模块门槛、跨任务风险与关键结论，不逐文件重复哈希或全文审查。层级调度不缩减既定实现、异常处理、真实依赖和集成验收范围。

验证默认以 **Git 基线提交、限定路径的 diff 和停止写入状态** 标识输入；已有冻结修订也可直接引用。记录实际命令、必要目录/环境、结果与限制，按变更风险核对影响结论的必要依赖。语义与相关依赖未变时复用已有证据，修复后只重跑受影响检查。仅写“通过”或引用旧聊天不构成证据。

不默认计算文件指纹、`sha256`、全树依赖闭包或 manifest，也不重复生成索引。仅发布产物完整性、外部文件身份或具体故障定位确实需要时，对明确范围计算一次哈希，说明用途并复用；后续只有对应输入变化才更新，不能把每轮重复哈希作为验收门槛。产品已有完整性检查与依赖锁文件仍按真实用途维护。

可从 Git、锁文件和命令再生的日志、构建与缓存留在忽略的 `output/ai/` 或工具缓存。无法再生且接续必需的源码、harness、probe、独特失败原件和输入必须主动保存到正式路径或受跟踪的 `.agent-state/<task>/`；不能仅放 scratch 或只记一个失效临时路径。保存必要材料时保留最小内容并脱敏，不默认复制大型日志、依赖树或永久 verification/archive 包。相关命令和自有资源仍须核实实际停止/清零，checkpoint成功不能代替该结论。

纯文档检查内容一致性、Markdown 结构、相对链接/fragment 和 `git diff --check`；配置或技能变更增加对应解析、格式及可用的加载/发现检查。前端按影响检查工程与浏览器行为；后端先核对实际工程，再确定测试命令。没有运行的检查明确标为未执行，不声称产品行为已通过。

交付保持简短：完成结果与实际文件范围、检查及可复用输入、未验证内容或阻塞，以及是否停止写入和后台命令。缺陷交直接负责人安排原文件所有者修复，验证者不越权修改产品实现。

## 运行时配置与实例

[项目配置](../../../.codex/config.toml)与[十一个角色文件](../../../.codex/agents/)统一要求各层实例使用 `gpt-6-astra`、`model_reasoning_effort = "ultra"`，Fast Mode 对应 `service_tier = "priority"`。角色身份按 TOML 的 `name` 字段识别，检查时枚举实际文件。所有层级继承权限和 sandbox，均可在授权范围内按需要继续委派，不自动换模型，不修改全局 trust。

项目按用户要求将 `agents.max_concurrent_threads_per_session` 从 20 提高到 100，表示全树子线程并发配置值，不含主线程。新 CLI 加载项目配置时请求该值，实际可用名额仍受运行时、服务和机器承载限制。`agents.max_depth = 6` 是可调整的递归深度上限，不要求任务拆满六层；该字段只对当前 V1 多代理运行时有效，V2 会忽略它，切换运行时须重新核实支持情况。

创建实例时使用自包含任务卡，显式指定 `gpt-6-astra`、`ultra`；支持 `fork_turns` 的接口设为 `none`。工具未提供自定义角色时，可用普通实例附上角色指令和必读技能；模型不可用则报告阻塞，不降低模型或思考强度。

Fast Mode 的配置目标、CLI 支持与实例实际服务档位须分别核实。`spawn_agent` 接口没有 Fast/service tier 参数时，只能显式指定其支持的模型与思考强度；不能据此声称当前所有实例已启用 Fast。项目配置是否加载还受运行时版本、trust 及会话覆盖影响；配置变更不自动切换当前会话，未实测的加载或继承行为须保留限制。

本机 CLI 0.159.0-alpha.3 的模型目录将 `priority` 标为 Fast、列出 `gpt-6-astra` 支持 `ultra`，`fast_mode` 功能为开启；字段解析已验证角色 `service_tier` 和 V1 `agents.max_depth`。官方文档和加载器支持从 `.codex/agents/*.toml` 按 `name` 自动发现角色，无需维护另一份同名注册表。

仓库提供 Python 3.11+ 的[团队启动工具](../../../scripts/ai-team.py)，要求 POSIX 进程组（Linux、macOS 或 WSL），当前不支持原生 Windows。本轮只在 Linux 实测。从仓库根使用：

```sh
python3 scripts/ai-team.py check
python3 scripts/ai-team.py start --dry-run
python3 scripts/ai-team.py start -- '接续当前任务，先读取 .agent-state/current.md'
```

`check` 和 `start` 可选 `--max-agents N`，仅覆盖本次新 CLI 的子线程并发值，要求正整数且不含主线程；不带该选项时使用仓库的 100，只有仓库字段也缺省时才继承本机或后端默认，默认可能更小。`check` 报告请求值、配置观测值与来源，V2 专用配置或服务限制仍可能决定实际容量。先检查设置来源并结合机器承载选择；例如以下 `32` 表示本次覆盖为较小配置值，不改仓库默认，也不承诺实际可用容量：

```sh
python3 scripts/ai-team.py check --max-agents 32
python3 scripts/ai-team.py start --max-agents 32 --dry-run
```

`check` 做本地格式检查并通过短时 CLI app-server 的 `config/read`、`skills/list` 检查显式有效配置和技能发现，不调用模型。`start` 将项目模型、思考强度、服务档位、agent 参数和按角色 `name` 生成的绝对 `config_file` 路径传给本次 CLI；上游加载器说明同一文件会去重，本轮检查只证明显式角色映射被解析。它不修改 trust、全局配置或权限，`--dry-run` 只显示启动命令。检查进程在自有进程组中运行，父进程退出后仍处理自有管道和子进程，并在有界时间内收尾。显式配置解析与技能发现不能证明角色实例已经执行或每次请求实际使用 Fast；新设备须报告实际检查结果，不能照抄本机结论。

TOML 可解析、技能可发现、显式实例创建成功与 CLI 自动加载生效是不同证据，不要求每次修改逐角色重复演练。运行时线程名额可能与活动任务数不同；只使用实际提供的停止或关闭接口，并核实名额是否释放，不能将任务完成声明当作名额释放。

续派已 STOP 或完成的实例须调用 `followup_task`，并确认接收与实际启动后再记为进行中；`send_message` 只传递消息，不能代替启动。历史初始化证据见[初始化验证记录](verification.md)，不作为当前配置或本次行为的通过证明。

## 自动保存与跨设备恢复

保存与验收分开。主线程在任务开始时建立或接续 `ai/<task>`，准备未被忽略的 `.agent-state/current.md`；工具自动将它纳入首个 checkpoint，无需另做 bootstrap 提交。各级负责人保证必要片段可恢复、交接精确路径并暂停这些文件的写入，主线程批次执行 Git，子 agent 不直接暂存、提交、推送或切换分支。

以下时点主动 checkpoint：每个有意义结果、下级交付、长检查之前、暂停或回用户之前；有未保存改动时，至多5分钟后的下一个安全写入边界立即保存。源码未完成、检查失败或未运行都不阻止保存，状态中照实记录。没有后台 daemon；由执行中的 agent 按触发点调用，不能承诺突然断电前最后字节已同步。

由主线程在仓库根调用[保存工具](../../../scripts/ai-checkpoint.py)，显式列出本次必要文件；工具自动包含 `.agent-state/current.md`：

```sh
python3 scripts/ai-checkpoint.py save --message 'wip: task checkpoint' -- path/to/source .agent-state/task-name/probe.py
```

工具只接受 `ai/` 分支和具体文件，不接收目录、忽略路径、符号链接或无关已暂存改动；主线程先核对并保留其他任务的 index，不用 `git add .` 扫入无关内容。无文件变化时不空提交，仍会尝试 push，便于重试已有本地 checkpoint。正常保存将 WIP commit 推送到 `origin` 同名分支，不 force push。

`.agent-state/current.md` 由指定唯一写者维护目标、状态（进行中/阻塞/完成）、任务分支、必要材料精确路径、完成/未完成、实际测试、失败限制、下一步及依赖恢复方法；它是当前分支的可恢复状态，不追加逐轮流水。台账只在全局产品状态变化时更新，不重复抄录分支记录。目录规则见[状态说明](../../../.agent-state/README.md)。

网络、凭据或 push 失败时保留本地 checkpoint，明确说明“尚未远端保存”，后续安全边界自动重试；未成功 push 不宣称跨设备就绪。非快进冲突先 fetch 并整合双方改动，不 force 或覆盖他人。必要源码已保存且 push 成功，才表示另一设备可获取该 checkpoint；构建产物和依赖按记录重建。

新设备由 agent 先 fetch `origin`，读取 `ai/*` 分支的 `current.md`，选择状态为“进行中”或“阻塞”且匹配用户当前目标的任务，跳过已完成并整合的分支；多个无关活动任务确实无法判断时才合并问一次目标，不能只按最新提交时间替用户选择。全新 clone 的步骤如下，`repository_url` 使用已有仓库地址，`task_branch` 由 agent 按目标与远端状态选定：

```sh
git clone "$repository_url" agenteam
cd agenteam
git fetch origin --prune
git for-each-ref --sort=-committerdate --format='%(refname:short)' refs/remotes/origin/ai/
# 对候选分支读取状态；这里的 task-name 替换为已确定的任务。
task_branch=ai/task-name
git show "origin/$task_branch:.agent-state/current.md"
git switch --track -c "$task_branch" "origin/$task_branch"
cat .agent-state/current.md
```

已有 clone 同样先 fetch 并核当前工作树；干净时接续选中的远端分支，已有本地分支只做正常快进或明确整合。dirty时先在当前任务分支保存必要改动，不覆盖、不丢弃；尚在非 `ai/` 分支时由主线程先建立保留现有改动的任务分支。读取状态后只恢复缺少的必要依赖，环境检查见[恢复指南](recovery-2026-10-08-environment.md)。

## 交接与 Git 交付

新会话读取 [AGENTS.md](../../../AGENTS.md)、选定分支的 `.agent-state/current.md` 和台账目标行，再按需读正式规格与必要前置；不遍历整份计划或历史报告。中途交接前完成安全 checkpoint，明确本地提交、远端保存结果、仍在运行的命令和资源状态，不能用 Git 保存代替退出/清理。

各层子 agent 均不执行 `git add`、`git commit`、`git push`、`git reset`、`git clean`，不切换分支或创建、切换 worktree。工作分支可保存多个未验收 WIP；`main` 只接收正式验收结果，主线程按完整原子变更整理代码、测试及必要文档，必要时 squash，避免把 WIP流水或大型临时日志带回主线。不要再为补哈希、重复报告或回填本提交编号单开 docs 收尾提交。

用户对 checkpoint 与常规源码 commit/push 的持续授权不包括部署、force push或覆盖其他人的工作。最终报告给出完成结果、实际验证与限制、本地提交和远端保存状态；同批文档通过 Git 文件历史定位自身提交，不回填自身哈希。
