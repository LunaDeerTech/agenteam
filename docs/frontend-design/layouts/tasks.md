# 任务页面布局

> 上层：[项目工作台](project-workspace.md)。Kanban 与 Explore 共用 Work Management 数据和操作规则。

## 1. 页面职责与示意

```text
双顶部导航
工具栏：Kanban / Explore     搜索 / 状态 / 优先级 / 类型 / Agent 筛选
Kanban
┌─────────────────┬───────────────────────────────────────┐
│ Milestone 选择  │ Sprint 标题 / 状态 / 操作              │
│ Sprint 列表     │ 七列 Task 看板         Task 详情抽屉 → │
└─────────────────┴───────────────────────────────────────┘
Explore
┌─────────────────┬───────────────────────────────────────┐
│ Milestone       │ 所选 Milestone / Sprint / Task 详情    │
│ └ Sprint        │ 基础信息 / 对象操作 / 关联内容         │
│   └ Task        │                                       │
└─────────────────┴───────────────────────────────────────┘
```

## 2. 默认选择与视图切换

普通任务入口为 Kanban。读取 `Project.current_sprint_id`，选择对应 Sprint 并同步所属 Milestone；Explore 默认展开其祖先并显示 Sprint 详情。没有 Current Sprint 时保留规划结构，内容提示“当前没有正在进行的 Sprint，请选择”，不自动选择 planned 或 completed Sprint。

| 当前选择 | 切至 Kanban | 切至 Explore |
| --- | --- | --- |
| Sprint | 保留 Sprint 并展示看板 | 选中 Sprint 详情 |
| Task | 展示所属 Sprint 并打开 Task 抽屉 | 选中 Task 详情并展开祖先 |
| 只有 Milestone | 同步 Milestone，提示选择 Sprint | 保留 Milestone 详情 |
| 未选择 | 保留待选状态 | 保留待选状态 |

关闭抽屉只移除 Task 选择，保留 Sprint 和看板位置。手动切换 Milestone 时清除原 Sprint / Task，提示选择该 Milestone 的 Sprint；选择 Sprint 不自动启动它。直接对象链接优先于 Current Sprint 默认项。用户已手动选择后 Current Sprint 变化不强制跳转；删除当前对象后显示对象已失效及重新选择入口。

## 3. Kanban 栏目与字段

七列顺序为 `backlog → todo → in_progress → in_review → blocked → done → cancelled`，展示中文名称、数量、Task 卡片。内容区横向滚动，空列保持可见。卡片显示 title、type、priority、assignee Agent、未解决 Blocker 提示；不把执行状态混成 Task state。

Sprint 列表显示 title、推导 lifecycle 和当前标识，按 manual_rank 排序。Task 按领域 priority 顺序及 `sprint + state + priority` 内 manual_rank 排序；过滤不写回排序。

筛选包含 text、state、priority、type、assignee；Milestone / Sprint 来自侧栏选择。工具栏可清除筛选。创建 Task 使用当前 Sprint，初始状态 backlog；不在任意列创建绕过状态机的 Task。卡片点击打开右侧抽屉，不离开看板。

## 4. Explore 与对象详情

树按 Milestone / Sprint 的 manual_rank 展示，展开动作独立于选中；Task 展示标题和 state。Task 节点按领域稳定排序加载，不按客户端任意顺序拼接；大列表按需分页。

| 对象 | 详情字段 | 操作入口 |
| --- | --- | --- |
| Milestone | title、description、id、created_at、updated_at；所属 Sprint 列表 | 创建 / 编辑 Milestone、排序、创建 Sprint |
| Sprint | title、description、Milestone、推导 lifecycle、started_at/by、completed_at/by、version；Task 汇总与列表 | 创建 / 编辑、启动、完成及 rollover、满足契约时删除 |
| Task | 下述共享详情 | 下述统一操作 |

Milestone 没有独立 lifecycle、开始日期或截止日期。Sprint completed 后 membership 冻结；完成 Current Sprint 前检查活动执行和 pending dispatch，未完成 Task 必须选择合法 planned rollover 目标，不自动启动下一 Sprint。

## 5. 共享 Task 详情

Kanban 抽屉与 Explore 主内容复用同一结构，抽屉提供关闭按钮。

| 分区 | 字段 / 内容 |
| --- | --- |
| 标题与属性 | id、title、description、type、priority、state、assignee_agent_id（以 Agent 名称呈现） |
| 规划位置 | milestone_id、sprint_id 对应名称与跳转；位置调整独立入口 |
| Plan | Markdown / text 内容与编辑入口 |
| Blocker / 依赖 | type、原因与该类型 metadata、解决状态；rely_on 显示 related_task_id 目标 |
| Timeline | TaskEvent 按时间呈现，评论与状态事实统一展示，支持较早事件加载 |
| 执行 | 按 trigger reference 查询 AgentExecution，展示状态及运行详情入口；不复制成 TaskEvent |
| 元信息 | version、created_at、updated_at；version 供并发控制，不作为可编辑字段 |

type 固定为 feature / bug / task / spike / chore；priority 固定为 low / medium / high / critical。assignee 仅项目 Agent；todo / in_progress / in_review 必须有合法 assignee。

## 6. 操作与业务限制

普通字段、Plan、位置移动、状态流转、Blocker 操作和评论使用各自领域入口。状态操作只展示当前合法目标，需要评论或 Blocker 时收集对应信息。看板拖拽跨列仍调用状态机；无法执行的动作说明原因并回到原列。同列排序限合法 rank 分组；首版不引入跨 priority 的自由 rank。

Task 删除只允许满足领域条件的纯 backlog 草稿；done / cancelled 冻结业务字段，不提供 reopen。依赖 cancelled Task 不自动视为满足。Blocker 解除与 blocked 恢复遵循领域 reconciliation，不由前端擅自流转。Sprint 删除条件遵循生命周期契约；Milestone 删除尚无正式契约，首版不提供入口。

普通字段保存携带 version；冲突显示服务器最新值和保留的用户输入，让用户决定重新编辑，不静默覆盖。拖拽失败回滚显示。已完成 Sprint 的查看入口保留，编辑能力由领域允许范围决定。

## 7. 加载、空状态与窄屏

规划树、看板、详情分别加载；无 Milestone 提供创建入口，无 Sprint 提供创建入口，无 Task 提供创建入口。筛选无结果显示清除筛选；请求失败就地重试。对象无权限不展示其内容。

窄屏侧栏改覆盖面板；Kanban 局部横向滚动，详情抽屉占满可用内容区；Explore 选中后收起树，可再次打开。关闭详情返回原看板，浏览器返回遵循对象导航记录。

## 8. 相关架构

[Task Domain Model](../../architecture/project-work-management/task-domain-model.md)、[Task State Machine](../../architecture/project-work-management/task-state-machine.md)、[Blocker 与依赖](../../architecture/project-work-management/task-blocker-dependency.md)、[Task Timeline](../../architecture/project-work-management/task-event-timeline.md)、[Sprint Lifecycle](../../architecture/project-work-management/sprint-lifecycle.md)、[Scheduler](../../architecture/scheduler/README.md)。
