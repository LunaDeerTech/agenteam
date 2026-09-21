# agenteam
我现在想开发一个agenteam项目，该项目的目的是：以项目为单位，通过组建ai agent开发团队，并提供相应的协作工具实现多agent协同工作推进项目的开发、交付、迭代。
下面我将阐述本项目预计提供的设计功能：

## 核心定位与设计原则

agenteam 不是另一个 Agent Loop 或单纯的多 Agent 聊天工具，而是构建在成熟 Agent harness 之上的项目级 AI 协作控制面。harness 负责单个 Agent 的运行时循环、工具调用和上下文管理；agenteam 负责项目状态、任务调度、跨 Agent 协作、人类决策、远程 Runner、知识沉淀和执行审计。

默认工作路径是：

```text
Issue → Scheduler 调度 → Agent Loop 执行 → 产物/证据 → 状态流转
```

会议不是所有任务的默认工作方式，而是在人类在场的前提下，用于探索、分歧处理、方案选择和授权执行的可选协作空间。Agent 之间的常规协作优先通过 Issue、plan、comments、知识库和结构化决策记录完成。

系统中的“讨论内容”“会议摘要”“用户决策”“执行请求”和“实际项目变更”必须分开建模，不能因为会议产生了摘要，就认为项目已经产生了执行结果。

## 系统模型（大概，初步草稿）

```
agenteam
├── personal-page
│   └── personal-projects
├── projects
│   ├── meetings
│   ├── kanban-board
│   ├── knowledge-base
│   ├── explore
│   └── settings
│       ├── general
│       │   ├── basic
│       │   │   ├── name
│       │   │   ├── description
│       │   │   └── AGENTS.md
│       │   └── git-repo
│       │       ├── address
│       │       └── ssh-key
│       ├── member
│       ├── agent
│       │   ├── basic
│       │   │   ├── name
│       │   │   ├── tag-color
│       │   │   ├── use-model
│       │   │   ├── description
│       │   │   ├── sys-prompt
│       │   │   └── inject-AGENTS.md
│       │   ├── capabilities
│       │   │   ├── allowed-tools
│       │   │   └── runner-mount-points
│       │   │       ├── name
│       │   │       ├── runner
│       │   │       ├── path
│       │   │       └── description
│       │   └── memory
│       │       └── > REFER: Hindsight
│       └── model
│           └── > SAME: agenteam/model-providers
└── sys-settings
    ├── minio
    ├── db-status
    │   ├── db
    │   └── vec-db
    ├── accounts
    │   ├── sys(1)
    │   └── users(n)
    ├── model-providers
    │   └── provider
    │       ├── name
    │       ├── base-url
    │       ├── api-type
    │       ├── key
    │       └── models
    │           ├── model-id
    │           ├── type (chat, img-gen, embedding...)
    │           ├── context-length
    │           ├── max-output
    │           ├── input-type
    │           ├── reasoning-level
    │           └── properties-override
    ├── workspace-runners
    │   └── runner
    │       ├── name
    │       ├── description
    │       ├── tags
    │       ├── headless
    │       ├── root-path
    │       └── mount-points
    └── agent-presets
        └── > SAME PARTLY: projects/settings/agent, but not include runner-mount-points & memory
```

## 模型管理：
允许接入多个 provider 的多个模型;
系统需要至少配置一个chat、一个embedding、一个reranker才能满足运行
模型管理分为系统级别和项目级别两种，所有项目都可以使用系统级模型，也可以自己配置自己的模型；
在需要选择模型的地方（比如 agent 的配置），两种级别的模型分为两类列出；

## runner：
本项目算是一种分布式系统，系统主体所在的服务器只承担项目的管理、调度以及模型的调用，实际代码的存储位置、调试运行位置等是通过 runner 执行的；
也就是说模型在需要修改代码或者运行命令的时候，需要通过 runner 来操作；
runner的部署采用单命令一键部署，由系统分发二进制。在 runner 管理页面点击添加 runner 后系统需要在后台生成一个短期密钥，然后拼接到一个安装命令中。在目标服务器运行这个命令即可从系统拉取二进制并且完成自动的安装、配置、启动、连接主系统等步骤。runner的安装部署机制可以参考项目： https://github.com/LunaDeerTech/mcpc
runner 的配置是系统级别的，项目里无法配置自定义runner。
runner 需要配置一个 root-path，作为 runner 在设备上的根目录。后续的挂载点都是以项目为单位挂载到 runner 的 root-path 下的子目录，例如项目 A 写了一个挂载点 src，那么在设备上的实际路径就是 root-path/A/src。
runner 的 root-path 需要保证在设备上有读写权限，并且不能是系统敏感目录，同时要避免路径穿透。
headless 是一个bool型配置，用于配置该runner所在的设备是否具有桌面环境。
runner 的description、headless 在 agent 启动时会被注入到上下文，用于表明运行环境。

## Agent管理：
agent 的基础配置包含：名称、主题色tag-color、使用的模型、描述、系统提示词、是否注入项目AGENTS.md到系统提示词。其中主题色用于在用户界面展示时给 agent 的名字着色，其不参与任何其他含义。
agent 的能力配置包含：允许调用的工具、skills、挂载点。挂载点需要配置名称、所属runner、路径、描述，这些信息都会被注入到上下文。允许调用的工具则应该按照工具的来源分类展示，我们的整个系统本身就提供一些内置的工具，比如项目 basic 更新（description、AGENTS.md）、agent、meeting 等等，可以说系统本身就提供了很多工具，允许 agent 操作其所在的项目。
agent 记忆，这个模块直接参考有名的 Hindsight，可以直接把他的设计搬过来；
系统也可以配置一些 agent-presets，需要配置的内容与项目级别的 agent 一样，不过不需要配置 runner-mount-points 也没有 memory 页面。系统级别的 agent 可以相当于一种模板，在项目创建自己的 agent 的时候可以直接导入系统级 agent 的一些配置，注意不是引用。

### memory：
agent 的 memory 为项目里每个 agent 自己的记忆系统，通过 memory 沉淀项目相关的知识、经验和问题，便于 agent 在后续工作中调用这些记忆来辅助决策和执行。memory 的设计可以参考 [Hindsight](https://github.com/vectorize-io/hindsight) 的核心架构；如果复用其代码，需要遵守其许可证并记录借鉴内容。更稳妥的方式是理解其设计后，结合 agenteam 的权限和项目模型自行实现。

## Meetings

### 定位

meeting 是用户与一个或多个 Agent 共同参与的协作空间。它不是所有 Issue 的默认执行方式，也不是要求每次讨论都必须产生代码或项目变更的自动化流水线。

会议主要用于：

- 用户想了解某个问题、听取多个 Agent 的独立观点；
- Agent 之间存在分歧，需要比较方案或补充证据；
- Issue 被阻塞，需要用户做决策、提供信息或批准下一步动作；
- 涉及架构、安全、产品取舍等高风险问题，需要人类在场；
- 用户主动要求多个 Agent 共同讨论某个主题。

会议可以没有项目层面的执行产出。用户通过会议获得信息和判断，本身就是有效结果。是否执行某个动作、是否修改项目状态，由用户在交流过程中主动决定，或由用户批准 Agent 提出的执行建议。

### 会议参与与权限

每个会议至少需要一位人类用户参与，禁止纯 Agent 会议。用户可以创建会议，选择参与的 Agent，并配置参与顺序和讨论主题。

会议中 Agent 的有效权限是在其原有能力配置之上再次收紧：

- 默认只能调用读性质的工具；
- 如果需要调用写性质的工具，必须向用户发起结构化的执行请求并获得批准；
- 用户的批准只适用于明确的动作或批准范围，不能让 Agent 绕过原有工具权限；
- 会议中的决策请求和执行请求应当在界面上与普通发言分开显示。

Agent 也可以主动请求会议，但不能直接启动会议。Agent 需要先提交会议提案或发言，说明会议目的、关联的 Issue、需要用户解决的问题、拟邀请的 Agent 以及预期的用户动作。系统将关联 Issue 置为 `blocked`，并记录 `waiting_for_human` 或 `waiting_for_meeting_approval` 阻塞原因，等待用户批准、拒绝或暂缓。只有用户批准后，会议才进入正式进行状态。

### 会议层与 Agent Loop 层

为了避免把完整的工具调用历史和无意义的思维过程持续注入上下文，meeting 与 Agent Loop 分为两层：

- meeting 层：保存用户和 Agent 面向会议的发言、结构化请求、引用、附件以及会议摘要；
- Agent Loop 层：每次 Agent 发言时临时启动的传统 harness 运行时，负责读取上下文、调用工具并生成本次发言。

当轮到某个 Agent 发言时，系统将会议系统提示词、参与者信息、会议主题、相关 Issue 信息以及 meeting 层的必要历史注入新的 Agent Loop。Agent Loop 结束后，只将本次面向用户的发言写入 meeting 层。下一次轮到该 Agent 发言时，重新启动一个独立的 Agent Loop，不复用上一轮的完整运行上下文。

Agent 的完整工具调用记录和运行过程应在界面中默认折叠展示，便于审查但不作为后续会议上下文的默认内容。会议摘要每轮发言结束后自动更新，摘要只用于帮助用户和 Agent 快速了解会议进展，不代表用户已经作出决策，也不代表项目已经产生执行结果。

### 会议中的请求与实际产出

会议内容和项目变更必须分开建模。至少应区分：

- 普通会议发言；
- 会议摘要；
- 结构化决策请求；
- Agent 提出的执行建议；
- 用户批准的执行请求；
- 实际发生的 Issue、代码、文档或配置变更。

Agent 可以在交流过程中提出建议执行某项动作，用户也可以主动要求 Agent 执行。只有通过用户批准并实际调用工具后，才产生项目层面的变更。会议结束本身不自动改变 Issue 状态，也不自动创建任务或提交代码。

会议需要支持引用其他会议、Issue、文档和 Agent，并支持图片、文档、代码片段等附件。会议也应当记录用户批准、拒绝和暂缓的决策事件，便于后续 Agent 理解上下文和审计。

## issue:
issue 是一个项目的最小 agent 任务单元。
issue 的属性至少有：id（项目里唯一）、title、state（backlog、todo、in-progress、in-review、blocked、cancelled、done）、priority（low、medium、high、critical）、type（feature、bug、task、spike、chore）、description、assignee、plan、events、depends_on_issue_ids、blockers。
plan为该issue的执行计划以及每条计划的完成情况，仅供agent参考，agent也可以自己修改这个plan；
events 为该 Issue 的统一时间线，采用类似 GitHub Issue timeline 的事件模型。它不仅记录 Agent 和人类用户留下的评论、关键内容和证据，也记录 Issue 的状态流转和关键变更历史。事件类型至少可以包括：comment、state_changed、assignee_changed、dependency_added、dependency_removed、blocker_added、blocker_resolved、execution_started、execution_finished 等。用户界面统一按照时间顺序展示这些 IssueEvent，不再把“评论”和“状态变更历史”拆成两套互不关联的数据。
Agent Loop 的完整运行日志不作为 Issue 的属性保存。每次执行都创建独立的 Execution，完整 runtime log 归属于对应的 Execution；Issue 只通过关联的 Execution 和 IssueEvent 展示执行历史与关键结果。
backlog, done, cancelled 这三种状态不需要 assignee，其他状态必须要有 assignee；

`blocked` 表示 Issue 当前不能继续执行，阻塞原因必须可追踪。初步原因包括：

- `rely_on`：依赖的前置 Issue 尚未完成；
- `waiting_for_human`：等待用户提供信息、作出决策或批准动作；
- `waiting_for_meeting_approval`：Agent 已提交会议提案，等待用户批准会议开始；
- `technical`：Runner、模型、外部服务或其他技术问题导致暂时无法继续。

一个 Issue 可能同时存在多个 blocker，因此不建议只保存一个字符串形式的原因。至少需要记录 blocker 类型、相关 Issue 或会议 ID、说明、创建时间和解除时间。`depends_on_issue_ids` 用于记录前置 Issue；当所有必需的前置 Issue 均为 `done` 后，`rely_on` blocker 才能解除。如果还存在其他 blocker，则不能继续流转。

依赖关系默认采用 AND 语义，需要在创建或修改依赖关系时检查循环依赖。前置 Issue 被 `cancelled` 时是否连带取消当前 Issue，或者转为等待用户决策，需要由项目策略或用户明确决定。

agent 每次都是从 issue 启动的，并在执行完成后流转 issue 的状态作为本轮的结束，每一轮都是一个独立的 Agent Loop，同时对应一个独立的 Execution。Issue 的业务信息会被注入到 agent 的上下文；历史 Execution 的 runtime log 默认不注入，只在需要审查或明确引用时加载。Agent 可以通过受控工具更新 issue 的状态、assignee、plan，并通过 comment-issue 写入 comment 类型的 IssueEvent；状态、负责人、依赖、blocker 和执行状态等关键变化由系统自动写入对应类型的 IssueEvent。服务端必须校验状态流转是否合法、是否属于当前 Execution，以及是否满足依赖和权限条件。Agent Loop 的完整 runtime log 由系统写入对应的 Execution，不由 Agent 自己更新。

## kanban-board：
kanban提供本系统的项目核心管理能力。kanban里的最小单元是 issue，若干个issue组成 sprint，若干个 sprint 组成 milestone，一个项目则由若干个 milestone 构成。
kanban 有一个 Execution Scheduler 按可配置的 tick 间隔（默认 30 秒）周期性运行。Scheduler 负责依赖解除、任务抢占和 Agent Loop 的启动，但不负责替代 Agent 或用户作出业务决策。

每次 tick 至少执行以下逻辑：

1. 检查 `blocked` 且 blocker 为 `rely_on` 的 Issue。如果所有必需的前置 Issue 已完成，则解除该 blocker；如果没有其他 blocker，将 Issue 改为 `todo`。`waiting_for_human`、`waiting_for_meeting_approval` 和 `technical` 等 blocker 不由 Scheduler 自动解除。
2. 从满足执行条件且已经分配 assignee 的 `todo` Issue 中 claim 任务，并以原子方式将其从 `todo` 改为 `in-progress`，创建对应的 Execution 和租约，然后启动该 assignee 对应 Agent 的一次 Agent Loop。Scheduler 不负责为 Issue 选择或分配 Agent。必须保证同一个 Issue 同时最多只有一个有效 Execution。
3. 检查正在执行的任务、超时租约和启动失败记录，按配置执行重试、恢复或转为 `blocked(technical)`。Scheduler 不应因为 Agent 尚未完成就重复启动新的 Agent Loop。

`in-progress` 的状态流转通常由 Agent Loop 在完成本轮工作后写入 `in-review`、`done` 或 `blocked`。`in-review` 是否自动触发独立的审查 Agent Loop，需要单独配置，不应与普通执行任务混为一谈。

Scheduler 的管理界面提供暂停/启动按钮。暂停 Scheduler 只停止新的自动调度，不应中断已经运行的 Agent Loop，除非用户明确要求停止运行。

## knowledge-base：
knowledge-base是项目的知识库，用于存放项目的一些关键文档（如产品需求文档，产品设计文档、技术架构文档之类的东西）。需要对进入knowledge- base的文档做向量化，便于agent搜索、插件。
为了便于文档的持续更新，每个文档需要创建一个固定的id，后续更新文档直接根据id覆盖原文档的内容。

## explore 页面：
该页面为一个项目的总览页面，提供一个issue总览的字页面，通过侧边栏树状结构按照 milestone → sprint → issue 的层级关系展示所有的issue，并提供搜索、筛选、排序等功能。用户可以直接在该页面对issue进行操作，如创建、编辑、删除、流转状态等。

## 系统内置提供的工具列表（初步，按来源分类）

所有这些工具都采用内置mcp的方式提供，agent可以调用这些工具来操作项目的各个模块。所有工具都需要在agent的能力配置中被允许才能使用。
工具可以从功能上分类，同时也要按照“读写”来分类。在配置权限的界面要能够想一种设计，既能够按照功能分类也能够按照读写分类。
以下仅是我想的初步的有的一些工具，并非决定。

### 可配置权限的：
项目基础信息（description、AGENTS.md）相关： read-project-info, update-project-info
agent相关： list-agents, create-agent, update-agent, delete-agent
issue 相关：list-issues, create-issue, update-issue, comment-issue
issue 依赖相关：add-issue-dependency, remove-issue-dependency, list-issue-dependencies
knowledge-base 相关：list-docs, create-doc, update-doc, query-doc
kanban 相关：start-scheduler, pause-scheduler
milestone 相关：
sprint 相关：
meeting 相关：list-meetings, request-meeting, add-agent-to-meeting, remove-agent-from-meeting, update-meeting, comment-meeting, request-decision, request-execution-approval

其中，用户或系统界面的 `create-meeting` 用于创建会议；`request-meeting` 仅用于 Agent 提交会议提案，不能绕过用户审批直接创建并启动会议。批准、拒绝会议以及批准执行请求属于用户操作。`request-execution-approval` 只负责向用户说明拟执行的动作、范围和风险，实际写操作仍需经过系统权限校验。

### 内置无法配置权限的：
memory（仅可访问 agent 自己）： retain, recall, reflect

## Agent Loop （Agent 运行时）：
Agent Loop 是传统 harness 意义上的 Agent 运行时，负责上下文组装、模型调用、工具调用、工具结果处理、错误处理和本轮输出。在 agenteam 中，Agent Loop 不是项目状态机，也不是会议本身，而是被项目调度和会议按需调用的执行单元。

在 Issue 执行场景中，通常一次 Agent Loop 只处理一个 Issue 的一轮工作：Scheduler 先将符合条件的 `todo` Issue 原子地改为 `in-progress`，再启动 Agent Loop；Agent 完成本轮工作后更新 Issue 的合法状态、plan 和 comments，运行记录由系统自动追加到 logs。下一轮执行需要重新启动 Agent Loop。

在会议场景中，每次 Agent 发言也启动一个独立的 Agent Loop，但该 Loop 的目标只是生成本次会议发言、提出决策请求或提出执行建议。会议层只保留面向用户的内容和必要的结构化记录，不复用上一轮完整的工具调用历史。

如果 Agent 在执行 Issue 时需要人类介入，应通过 `request-meeting` 提交提案，并将 Issue 置为相应的 `blocked` 状态等待用户处理；不能通过反复启动 Agent Loop 来等待会议审批。

agenteam 是在传统 harness 的基础上额外包装的一层 AI 项目协作层，通过 meeting、kanban、memory、knowledge-base、runner 和审计等模块扩展 Agent 的协作、决策、执行和持续学习能力。它的核心差异不是重新发明单轮 Agent Loop，而是让 Agent 能够在持久化项目状态和人类控制下长期协作。

Agent Loop 模块本身不一定需要从零开发。可以借鉴或集成成熟的开源 harness 框架，再通过适配层接入 agenteam 的模型、工具、Runner、权限和运行记录。需要重点保留清晰的边界：harness 负责运行时循环，agenteam 负责项目级状态和协作。以下开源 harness 可供参考：

- [pi](https://github.com/earendil-works/pi)
- [opencode](https://github.com/anomalyco/opencode)

可以根据实际需要选择直接集成、封装适配或参考其核心逻辑自行实现。使用第三方代码时，需要单独记录借鉴内容、许可证和后续同步策略。由于 agenteam 自己定义了项目级协作模型，不必直接复用这些 harness 的多 Agent 编排方式，但仍应评估其运行时、会话持久化、权限、工具注册和错误恢复能力。

## 技术架构
前端：vue3 + 自定义组件
后端（后端+runner）：go
数据库：postgresql + pgvector
文件存储：minio
缓存：redis
部署：前后端一体二进制 + docker-compose 编排
