# 会议页面布局

> 上层：[项目工作台](project-workspace.md)。业务契约：[Meeting 架构](../../architecture/meeting/README.md)。

## 1. 页面职责与区域示意

左侧为会议会话列表；右侧随准备 / 已开始状态变化，复用同一消息输入框。

```text
双顶部导航
┌──────────────────┬─────────────────────────────────────────┐
│ 新会议 / 筛选    │ 新会议准备：内容区中央                   │
│ 会话列表         │    ┌────────────────────────────────┐   │
│ 标题 / 更新时间  │    │ 输入消息与结构化资源节点       │   │
│                  │    │ +  并行/串行     编排Agent 发送 │   │
│                  │    └────────────────────────────────┘   │
└──────────────────┴─────────────────────────────────────────┘
已开始会议：
┌──────────────────┬──────────────────────────┬──────────────┐
│ 会话列表         │ 顶部：标题               │ 悬浮详情卡片 │
│                  │ 消息 Timeline            │ Participants │
│                  │                          │ Summary      │
│                  │                          │ References   │
│                  │ ┌──────────────────────┐ │ 可折叠       │
│                  │ │ 输入消息             │ │ 独立滚动     │
│                  │ │ +  并行/串行    发送 │ │              │
│                  │ └──────────────────────┘ │              │
└──────────────────┴──────────────────────────┴──────────────┘
```

## 2. 新会议中央输入框

准备区没有标题、讨论主题或独立参会表单，只展示中央输入框。底部左侧依次为加号、发言模式；右侧依次为参会 Agent 编排、发送。新会议默认并行，用户可改为串行。

加号打开悬浮分组列表：

| 分类 | 控件 | 插入内容 |
| --- | --- | --- |
| 添加 | 附件上传 | file node，上传后使用授权 Artifact identity |
| 知识库 | 选择知识文档 | knowledge node |
| 任务 | 选择 Task | task node |
| Agent | 选择 Agent | mention node |

选择器仅显示当前项目可访问对象。加号负责插入结构化信息，不自动 Pin；文本、链接及 @ / # 交互遵循既有 inline content。具体字段及服务端校验见[引用与内联内容](../../architecture/meeting/meeting-references-inline-content.md)。

编排按钮打开 Agent 多选及顺序调整浮层，候选显示 name、tag-color、description 和真实 busy 来源。Agent 插入菜单只列已编排名单，编排负责参会成员及默认顺序，mention 按架构参与本轮目标选择，不自动加入未参会 Agent。并行模式仍保留编排顺序，供展示与之后串行使用。

发送首条消息启动会议，需有效正文 / 资源内容及至少一个参会 Agent；新会议 Participant identity 转换和创建幂等遵循架构。发送中防重复提交；失败保留正文、资源节点、名单、顺序与模式。

## 3. 已开始会话与详情卡片

会话顶部只显示标题。标题生成前使用“新会议”占位；首轮 finalize 自动生成一次，后续不随 Summary 更新改写，不提供创建标题表单。

输入框移至消息区底部；右侧只保留发送，左侧加号与发言模式仍在。参会管理从右上角卡片进入。已有会话沿用上次选择的模式，每次发送固化本轮模式，不改变正在运行或已创建的 Turn。

详情卡片默认展开，可折叠；桌面保留阅读空间，避免盖住消息，卡片内部独立滚动。

| 分区 | 展示与入口 |
| --- | --- |
| Participants | 当前参会用户 / Agent，Agent 名称与身份摘要；调整成员及默认顺序 |
| Summary | goals（含主题与目标）、decisions、unresolved、facts；生成中 / 更新失败反馈 |
| References | Tasks、Knowledge、Links、Files；Pin / Unpin、排序、打开及 unavailable / deleted 提示 |

消息 Timeline 的 Agent Execution 行默认折叠，展开才加载真实运行详情；busy、跳过、retry、regenerate、DecisionRequest 和 Approval 卡片保留现有交互。Summary 与标题不是业务状态来源。

## 4. 会话侧栏、默认选择与导航

侧栏显示标题与更新时间，提供筛选、新会议、归档 / 恢复及删除入口；标题生成后同步更新侧栏。proposed 分组展示提案内容、来源、建议参与者及批准 / 拒绝；不是人工填写的会话主题表单。

普通会议入口或“新会议”清除会话选择，展示中央输入框，不恢复之前会话。点击列表或指定会话链接打开对应内容。会话切换有未发送输入时提示继续编辑或放弃，不自动发送。

## 5. 加载及异常

列表、Timeline、详情卡片及展开的执行视图分别加载。无 Agent 提示到项目设置创建，禁用新会议发送；无历史会话仍显示中央输入框。上传失败不形成无效 file node；移除参会 Agent 使草稿 mention 失效时提示修正。

首轮标题 / Summary 尚未提交时保留占位并显示更新状态；失败遵循 finalize 重试，不伪装为已完成。发送或审批失败显示原因，已删除引用显示不可用身份。Realtime 断线提示恢复并重新读取权威快照。

## 6. 窄屏与可访问性

会话列表改覆盖侧栏；新会议输入框仍位于内容区中央，控件保持左右分组并允许换行。已开始输入框在底部保持可达；详情卡片改可展开覆盖面板，关闭后回到消息区并恢复触发焦点。菜单、编排浮层及资源选择器支持键盘和关闭操作；长代码块局部滚动。

## 7. 相关架构

[Meeting Domain Model](../../architecture/meeting/meeting-domain-model.md)、[Turn Runtime](../../architecture/meeting/meeting-turn-runtime.md)、[Context & Summary](../../architecture/meeting/meeting-context-summary.md)、[Timeline & Realtime](../../architecture/meeting/meeting-timeline-realtime.md)、[References & Inline Content](../../architecture/meeting/meeting-references-inline-content.md)。
