# AT-0005：审计决定归位与文档验收

修订：2（主线程整合计划；S03 补核出站策略适用边界）。基线：`main / 2f9ba63`，保留本项已有七份未提交文档。依据为[决策记录](d00-audit-decisions.md)、[Skills 设计提案](d00-skills-design-proposal.md)和[Harness 调研](d00-tool-call-harness-research.md)。P01–P25、C01–C08 已完成逐项决定或既定规则核对；本卡同步正式文档，不开展 D01 或产品实现。

## 目标与边界

将已确认规则放回其拥有者专题，修正受影响的图、例子、前端交互和开发门槛，避免读者必须依赖讨论记录才能理解当前架构。讨论记录保留被覆盖选择的历史，但以最终决定为准。D00 报告保留审计时的发现与证据，另加处置映射，不重写历史验证结果。

本项不冻结所有未来字段、SQL、库版本或参数；D01 和各责任模块仍须按开发计划提供具体规格、实现与真实验收。不得将环境已安装版本、静态调研、文档一致性检查写成协议/集成已通过。全局搜索另立产品设计工作项，Dashboard 保留容器；用户已移出当前范围的备份要求不得重新加入。

不改产品代码、锁文件、CSS tokens、历史草稿或角色配置；不安装依赖、不联网调研、不运行无关产品测试、不部署、不推送。子 agent 不做 Git 写操作、不切换分支、不创建下级 agent。最多一个活动 `verification_worker`，显式使用 `gpt-6-astra / max`。主线程负责台账、决策状态、审计处置表、开发计划整合、本卡、最终审查与本地提交。

## 串行任务卡与覆盖

每卡只授权表内文件；目录范围仅包括其中现有 Markdown，新增文件须在表中点名。同组文档不必全部修改，保留不受影响规则。跨组引用缺口在交付中交接，后卡统一。每卡必须读取 AGENTS、验证角色 skill、本卡、相关决策原文及实际 diff，再执行。

| 卡 | 决定覆盖 | 授权写入范围 | 完成条件 |
| --- | --- | --- | --- |
| S01 基础、账号与项目 | P01–P05、P24、C08 的项目删除/Audit | `docs/development/repository-structure.md`；新增 `docs/architecture/platform-infrastructure/foundation-contracts.md`；`docs/architecture/platform-infrastructure/{README.md,authentication/,deployment-runtime.md,outbound-network-policy.md}`；`docs/architecture/security-governance/audit.md`；`docs/architecture/project-work-management/README.md` | 基础惯例、环境主密钥/DB 迁移元数据、管理员出站策略、认证默认值/配置/验证码/SMTP、用户名路由和项目归档/永久删除归位；清除相关旧政策 |
| S02 模型、Agent、Skills、Knowledge/Memory | P06–P11、C05 的变量图 | `docs/architecture/platform-infrastructure/model-system/{README.md,model-configuration.md,model-resolution.md}`；`docs/architecture/platform-infrastructure/model-token-usage.md`；`docs/architecture/agent-management.md`；新增 `docs/architecture/agent-skills.md`；`docs/architecture/project-work-management/{project-environment-variables.md,task-domain-model.md}`；`docs/architecture/knowledge-memory/`；`docs/architecture/platform-infrastructure/object-storage.md` | 模板推荐、项目级技能及按 Agent 分配/安装/下一轮生效、排序维护、目录不留结构历史、检索管理员配置、记忆冲突重算归位；技能正文/文件路径清楚 |
| S03 Runner、工具与治理 | P12–P17、C07 | `docs/architecture/runner/`；`docs/architecture/tool-system/`；`docs/architecture/security-governance/`（保留 S01 Audit）；`docs/architecture/mcp-integration/`；`docs/architecture/platform-infrastructure/outbound-network-policy.md` 仅澄清基础设施连接与业务可配置目标的适用边界 | Linux/macOS+Bash、桌面矩阵、Owner Session Tunnel、工具可选超时/无总期限、人工等待不自动过期、技能工具与真实后端、Attempt 独立持久化、MCP 原有边界一致 |
| S04 执行、调度、会议与实时 | P18–P22、C01–C04、C08 的事件/Dispatch | `docs/architecture/agent-executor/`；`docs/architecture/agent-loop/`；`docs/architecture/scheduler/`；`docs/architecture/meeting/`；`docs/architecture/platform-infrastructure/{model-system/chat-model-runtime.md,model-token-usage.md,realtime.md,human-inbox.md,internal-domain-events.md}` | 单一模型重试 owner、四项 Meeting 来源、快照实际更新水位/节流间隙、同幂等键不同语义拒绝、未知 Launch 继续原流程核对、历史 regenerate 等下轮摘要、Meeting 删除边界和不自动清理一致 |
| S05 索引、前端设计与开发门槛 | P01–P25、C01–C08 引用整合，重点 P23/P25/C06 | `docs/architecture/README.md`；`docs/frontend-design/README.md`、`architecture-follow-ups.md`、`layouts/`；只读复核主线程整合的 `docs/development/development-plan.md` | 所有新增架构有入口，前端设计对应新政策，D01/责任模块职责与验收承接清楚，已确认 tokens 不改，状态不冒充实现 |
| V01 全量独立验收 | 全部 P/C | 只读当前工作项全部 diff 和 Markdown 引用 | 核对最终决定逐项覆盖、冲突/图/例子无遗漏，链接/fragment、UTF-8/LF/whitespace 通过；明确工程未验收边界 |

## 必须保留的跨卡约束

- P01 同一幂等键不同语义输入拒绝，覆盖旧 Launch 的宽松表述；契约按职责组织、事务经正式端口，避免循环引用和万能共享包。
- 项目归档自动停止、只读且可恢复；永久删除自动停止后清理，无恢复功能。保留历史审计所需最小追溯和禁止自动 TTL 与显式领域删除的区别，不将永久删除变成全平台日志抹除。
- Add Skills 不可从系统删除，但与 `install-skill` 均默认启用、可按 Agent 禁用；`assign-skill` 普通可选。安装仅入项目库，正文只读自身获配技能。下一模型轮次更新技能 binding，不热换 startup snapshot/工具快照；移除不编排正在运行的执行。
- 无平台统一 ToolOperation 或 Execution 强制期限；工具按需公开可选超时。人工审批一直等待明确处理，停止/删除引起的显式取消不叫超时。自动审批模型请求仍可有有限 timeout。
- Scheduler 未知 Launch 沿现有串行流程按同一 Dispatch/key 核对，不引入专门恢复平台或强制人工兜底；Execution 终态不决定 Task 业务结果。
- Runtime item `seq` 仅排序，Execution 更新进度独立；快照水位对应实际包含内容并覆盖订阅前已发未 flush 间隙。保持流式、批量快照与 resync，不新增持久 delta 日志或逐 token 写库。
- 模型请求自动重试由 Model System 统一拥有；Loop 的语义新调用、Tool retry 和用户 retry 分开。SDK 真实 attempts 与用量可见，不盲拼 partial streams、不重放工具副作用；渐进单请求 timeout/watchdog/取消规则保留。
- Meeting Trigger 四项引用由 Meeting 领域校验；历史回复重新生成等下一正常 Turn finalize 更新摘要，摘要幂等必须含实际输入版本，不能只看末条 ID。Meeting 归档与 Project 归档不同，删除只停止本会议活动。

## 每卡执行、检查与交付

1. 用只读 Git 核对 `main`、HEAD、改动列表；保留已完成卡和用户改动。分批读取相关决定和源文，避免输出截断造成遗漏。
2. 修改拥有者正文及同文件图/示例/首期范围；不只在末尾加一段修正规则并留下相反正文。引用其他专题而不复制另一套完整规则。
3. 检索受影响词和入链；授权范围外的问题交接到后卡。遇真实未定产品选择、未授权新增文件/范围或不可调和决定，停止受影响步骤并报告主线程；工程细节留责任模块而不伪造决定。
4. 执行 `git diff --check`，检查写入文件 UTF-8、LF、末尾换行、尾空格、相对链接与标题 fragment；主线程最终扩大为所有正式文档及当前交付入链。纯文档不运行 Go/npm/浏览器/数据库测试。
5. 报告实际改动文件、逐项覆盖、实际命令/结果、未改理由、跨卡待办与未验证范围；确认无后台命令/剩余写入。主线程审查后才能下发下一卡。

## 进度与验收证据

状态：S01–S05、V01 及主线程终审均已完成；随 AT-0005 创建本地提交。本卡交付仅为文档。

- C04 收尾及 C05–C08 顺序只读核对通过；无新增产品问题，源文归位由本卡完成。
- S01 已完成并通过主线程 diff 审查：10 份文件，100 条本地链接/fragment，UTF-8/LF/末尾换行/尾空格与 `git diff --check` 通过。复核补齐共享挂载和 Runner 清理未知边界。未运行产品测试。
- 主线程已将 D01–D28 的责任和验收门槛整合到开发计划；29 个有序标题/依赖表条目一致、50 条直接依赖无前向引用；审计处置表覆盖唯一的 25 个 P 项与 8 个 C 项。
- S02 已完成：15 文件/125 条本地链接及格式、`git diff --check` 通过；主线程审查全部 diff 和 Skills 正文，通过小修明确先查找再编写、Workspace 责任和 Memory 冲突范围/事务图。
- S03 已完成并经主线程逐组 diff 审查：20 文件/158 条本地链接与 fragment、格式及 `git diff --check` 通过；工具可选 timeout、审批持久等待、真实桌面矩阵与 Owner Session Tunnel 已归位。出站 §3 明确 Deployment 基础连接与运行时业务目标边界，不以环境变量来源放宽模型/MCP/SMTP 等目标策略。
- S04 已完成并经主线程逐组 diff 审查：24 文件/229 条本地链接与 fragment、格式、743 个 fence 配对及 `git diff --check` 通过。复核补齐 Task 无变更不写伪事件、旧 Summary 不丢弃替换消息及 Meeting 删除 Audit；未运行产品测试。
- S05 已完成并经主线程审查 13 文件 diff；144 条本地链接与 fragment、14 个 fence 配对、格式及 `git diff --check` 通过。开发计划只读复核 29 项/50 条直接依赖无前向引用，76 条链接通过，无必须返修项。
- V01 已通过：新的 verification_worker（gpt-6-astra / max）独立审查 81 份 tracked diff 和 6 份新文档，无需修复的决定遗漏或跨文档冲突。117 文件/1208 条本地链接与 fragment、87 份改动文档格式、1789 对 fence 均通过；输出截断范围已小段补读，未以截断内容冒充完整覆盖。
- 最终命令与范围：`git status --short`、`git diff`、`git diff --check`；临时 Python 文档检查器遍历 docs Markdown 及根 AGENTS/README，验证本地目标/标题 fragment，并检查 87 份改动的 UTF-8/LF/末尾换行/尾空格；结构检查器核对 29 项顺序/依赖及 33 项映射。主线程收尾复跑 `python3 /tmp/agenteam-doc-check.py` 与 `python3 /tmp/agenteam-structure-check.py`，结果均通过；临时检查器不作为产品代码交付。
- 所有执行/验证子 agent 及后台命令均已停止。仅提交本项 87 份 Markdown，通过台账/本卡 Git 历史定位本地 Conventional Commit，不推送。文档归位验收不等于 D01 契约或 D02–D28 实现验收；未运行 Go/npm/浏览器/数据库/协议或故障恢复测试，也未验证外部链接和图的实际渲染。下一正式开发项仍为 D01。
