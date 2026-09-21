# agenteam
我现在想开发一个agenteam项目，该项目的目的是：以项目为单位，通过组建ai agent开发团队，并提供相应的协作工具实现多agent协同工作推进项目的开发、交付、迭代。
下面我将阐述本项目预计提供的设计功能：

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
    │           ├── type (chat, img-gen, embedidng...)
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
    │       └── mount-points
    └── agent-presets
        └── > SAME PARTLY: projects/settings/agent, but not include runner-mount-points & memory
```

## 模型管理：
允许接入多个 provider 的多个模型;
系统需要至少配置一个chat、一个embedding、一个reranker才能满足运行
模型管理属分为系统级别和项目级别两种，所有项目都可以使用系统级模型，也可以自己配置自己的模型；
在需要选择模型的地方（比如 agent 的配置），两种级别的模型分为两类列出；

## runner：
本项目算是一种分布式系统，系统主体所在的服务器只承担项目的管理、调度以及模型的调用，实际代码的存储位置、调试运行位置等是通过 runner 执行的；
也就是说模型在需要修改代码或者运行命令的时候，需要通过 runner 来操作；
runner的部署采用单命令一键部署，由系统分发二进制。在 runner 管理页面点击添加 runner 后系统需要在后台生成一个短期密钥，然后拼接到一个安装命令中。在目标服务器运行这个命令即可从系统拉取二进制并且完成自动的安装、配置、启动、连接主系统等步骤。runner的安装部署机制可以参考项目： /Users/deer/mcpc
runner 的配置是系统级别的，项目里无法配置自定义runner。
headless 是一个bool型配置，用于配置该runner所在的设备是否具有桌面环境。
runner 的description、headless 在 agent 启动时会被注入到上下文，用于表明运行环境。

## Agent管理：
agent 的基础配置包含：名称、主题色tag-color、使用的模型、描述、系统提示词、是否注入项目AGENTS.md到系统提示词。其中主题色用于在用户界面展示时给 agent 的名字着色，其不参与任何其他含义。
agent 的能力配置包含：允许调用的工具、skills、挂载点。挂载点需要配置名称、所属runner、路径、描述，这些信息都会被注入到上下文。允许调用的工具则应该按照工具的来源分类展示，我们的整个系统本身就提供一些内置的工具，比如项目 basic 更新（description、AGENTS.md），agent、meetings等等，可以说系统本身就提供了很多工具，允许agent操作其所在的项目。
agent 记忆，这个模块直接参考有名的 Hindsight，可以直接把他的设计搬过来；
系统也可以配置一些 agent-presets，需要配置的内容与项目界别的 agent 一样，不过不需要配置 runner-mount-points 也没有 memory 页面。系统级别的 agent 可以相当于一种模板，在项目创建自己的 agent 的时候可以直接导入系统级 agent 的一些配置，注意不是引用。

### memory：
agent 的 memory 为项目里每个 agent 自己的记忆系统，通过memory沉淀项目相关的知识、经验、坑等内容，便于 agent 在后续的工作中可以直接调用这些记忆来辅助决策、执行等。memory 的设计可以直接原样参考 [Hindsight](https://github.com/vectorize-io/hindsight)，他也是 MIT 架构的可以直接借鉴或者拿来用，不过我建议还是应该参考他的核心架构、设计我们自己写一遍我们的。

## Mettings:
用于用户与一个或多个Agent 对话、聊天。与传统的ai聊天不同的是，用户可以创建会议，然后添加需要参与会议的 agent，并安排agent 的发言顺序，也就是说不仅仅是用户与一个ai交流，而是允许多个 ai 同时参与交流，形成一个按序发言的会议讨论形式。会议中所有的agent默认只能调用“读”性质的工具，如果需要调用“写”性质的工具则需要用户批准，不论是读还是写都只是在agent原有的工具权限配置基础上做的额外权限控制。
为了避免会议过程中的长篇大论浪费token以及无意义的思维链、工具调用历史被注入上下文，我们的meeting架构肯定也要和传统的ai聊天不同。传统的聊天就是纯粹的多轮对话或者多次 Agent Loop，调用ai聊天接口，我们则需要分层处理。整体上分为两层：meeting层、Agent Loop层，其中Agent Loop层就是传统意义上的agent harness架构，meeting层则是展示给用户以及其他agent看的信息来源。
具体来说：meeting 层只包含用户以及各 agent 的“发言内容”，当轮到某个agent发言的时候，其需要把 meeting 层的系统提示词注入到 agent 的系统提示词，参会者的信息、会议主题、meeting层的发言历史等信息注入到 prompt，然后启动 Agent Loop，将最终的结果作为该 agent 的发言内容发送到 meetings。为了避免结果长篇大论，系统提示词语需要要求agent最终采用精简的回答内容，限制字数。完成后下一次轮到这个agent发言的时候需要重新按照上述的流程启动一次 Agent Loop，不应该服用上一轮 Agent Loop。这样才能避免无意义的agent上下文膨胀。
不过界面上仍然应该提供一个默认折叠的思考过程，用于展示 Agent Loop 运行的完整记录。
可能会涉及到一些决策、以及权限的请求，因此需要提供工具用于agent向用户发起结构化的决策请求，供用户可以直接在前端上点击选择选项或者是直接以文本回答。会议在所有agent完成回复后认为一轮结束，每一轮发言结束都会自动更新会议的主题（摘要）。
除了用户可以发起会议之外，agent自身也具备主动发起会议的能力，用于要求相关用户介入做决策或者讨论一些问题。会议的发起需要至少一位人类用户参与，禁止纯agent 的会议。
会议需要具备引用的能力，需要支持引用：其他会议、issue、文档、agent。需要支持文件（图片、文档、代码片段等）作为附件。

## issue:
issue 是一个项目的最小 agent 任务单元。
issue 的属性至少有：id（项目里唯一）、title、state（backlog、todo、in-progress、in-review、blocked、cancelled、done）、priority（low、medium、high、critical）、type（feature、bug、task、spike、chore）、description、assignee、plan、comments、logs
plan为该issue的执行计划以及每条计划的完成情况，仅供agent参考，agent也可以自己修改这个plan；
comments 为agent在处理这个issue的过程中留下的关键内容、证据等信息。人类用户也可以在此处留言。comments系统整体上应该采用github那样的issuecomment形式，也就是说不仅仅是记录展示留言，还应当展示项目的流转、变更历史；
其中logs为所有经手过这个issue 的agent每次的执行完整记录，每次一条；
backlog, done, cancelled 这三种状态不需要 assignee，其他状态必须要有 assignee；
agent 每次都是从 issue 启动的，并在执行完成后流转 issue 的状态作为本轮的结束，每一轮都是一个独立的 Agent Loop。除了 logs 之外，issue的其他信息都会被注入到agent的上下文。因此在设计issue的agent system prompt时，需要要求agent在最后结束时更新issue的状态、assignee、plan、comments等信息。logs会在每轮结束后自动追加到issue的logs中，不是由agent自己更新的。

## kanban-board：
kanban提供本系统的项目核心管理能力。kanban里的最小单元是 issue，若干个issue组成 sprint，若干个 sprint 组成 milestone，一个项目则由若干个 milestone 构成。
kanban 有一个 Execution Scheduler 按可配置的 tick 间隔（默认 30 秒）周期性运行，按 `blocked → todo → in_progress → in_review` 顺序遍历 Issue，启动 agent loop。 kanban 的管理界面提供一个暂停/启动按钮用于控制 Execution Scheduler的启停。

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
knowledge-base 相关：list-docs, create-doc, update-doc, query-doc
kanban 相关：start-scheduler, pause-scheduler
milestone 相关：
sprint 相关：
meeting 相关：list-meetings, create-meeting, add-agent-to-meeting, remove-agent-from-meeting, update-meeting, comment-meeting, request-decision

### 内置无法配置权限的：
memory（仅可访问 agent 自己）： retain, recall, reflect

## Agent Loop （Agent 运行时）：
此处的 agent loop 其实才是传统的 harness 架构，在我们的 agenteam 系统里一般来说，agent loop 只会执行一次，这个一次指的是只会执行一次 Agent Loop，直到该 agent 完成本轮的任务（比如完成一个 issue 的处理），然后流转 issue 的状态，结束本轮。下一轮的执行需要重新启动 Agent Loop。
也就是说我们的系统是在传统 harness 的基础上额外包装了一层 ai 协作层，通过 meeting、kanban、memory、knowledge-base 等模块来扩展实现 agent 的学习、协作、决策、执行等能力。相比于传统的 harness multi agent 架构理论上能够让agent更好地协作、学习、决策、执行。
不过这个 Agent Loop 模块我认为我们不应该开发，因为工作量比较大难以把握。因此我认为可以直接把成熟的开源的 harness 框架借鉴过来或者集成过来。我在这里列出一些开源的 harness 框架，供参考：
- [pi](https://github.com/earendil-works/pi)
- [opencode](https://github.com/anomalyco/opencode)
我个人倾向于不是直接原样集成他们的代码或者是修改他们的代码拿过来，而是参考他们的核心逻辑、关键架构等技术，我们自己写。这些开源 harness 都是 MIT 协议，可以任意拿他们的东西来用，不过为了尊重作者我们还是因该专门有个文档来记录借鉴了哪些东西。当然了，因为我们有自己的多agent架构了，所以完全不需要考虑他们的多agent模块。

## 技术架构
前端：vue3 + 自定义组件
后端（后端+runner）：go
数据库：postgresql + pgvector
文件存储：minio
缓存：redis
部署：前后端一体二进制 + docker-compose 编排