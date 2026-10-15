# 当前状态：继续完整原 M3

用户已授权继续 Knowledge 检索与引用、Memory、领域工具、MCP、Meeting 及必要 Runner 扩展。M4 已撤出；M3 完成后再评估其余缺口。M3 尚未完成，main 只同步已接受文档，当前共同产品组合仍在功能分支验证。目标与完整出口见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)，实际独审范围见 [复核报告](../docs/development/work-items/m3-core-independent-review.md)，派工见 [任务台账](../docs/development/agent-team/tasks.md)。

root 负责全部 Git；integration 负责共享 App/Execution 接线、共同契约与唯一资源调度。每个内容路径只有一个 writer，跨树只按已冻结的准确文件与共享 hunk 组合。暂停 owner 的源码和证据保留，旧通过不自动成为新共同 HEAD 的通过。

## 工作树索引

共 15 个 M3 工作树加 main，16 树均保留。新 Realtime/Artifact 任务在 common 的登记叶文件实施，未新增工作树。

| 分支 | 工作树 | 负责人 / 范围 |
| --- | --- | --- |
| `main` | `/workspace/agenteam` | root：正式交付与状态镜像；M3 产品未交付 |
| `ai/m3-integration` | `/workspace/agenteam-m3-integration` | integration：共同 App/Execution/Audit/Realtime 契约与组合验证；其他 owner 仅写登记借域 |
| `ai/m3-knowledge` | `/workspace/agenteam-m3-knowledge` | Knowledge：检索、文件、hybrid；139清理真实门已过，原固定来源保留 |
| `ai/m3-memory` | `/workspace/agenteam-m3-memory` | Memory：正常与限定冷恢复的固定源码、证据；owner 按需续派 |
| `ai/m3-mcp` | `/workspace/agenteam-m3-mcp` | MCP：OAuth 已全域移交本 owner，正常浏览器已过；142冷恢复与 stranded 收尾 |
| `ai/m3-meeting` | `/workspace/agenteam-m3-meeting` | Meeting：已通过 Summary、并行/队列/插话的固定来源 |
| `ai/m3-model` | `/workspace/agenteam-m3-model` | Model：已通过 Model/Secret/StoppedResolution 固定来源；新实现转 Runner 树 |
| `ai/m3-retrieval-selection` | `/workspace/agenteam-m3-retrieval-selection` | 已收束选型来源：D13 lexical/dense/RRF 比较 |
| `ai/m3-knowledge-validation` | `/workspace/agenteam-m3-knowledge-validation` | Knowledge：固定早期 App 验证输入 |
| `ai/m3-meeting-ui` | `/workspace/agenteam-m3-meeting-ui` | Meeting 固定 UI 产物；Decision/Approval 已过，Retry/Metadata/替换后继仍待 |
| `ai/m3-skills` | `/workspace/agenteam-m3-skills` | Skills：目录/分配、Unknown、published cold 与 R1 定向修复；成熟组已导 common |
| `ai/m3-meeting-participants` | `/workspace/agenteam-m3-meeting-participants` | Meeting：引用/参与者、137删除、141Decision、Reply 与 Realtime 领域提供方 |
| `ai/m3-retrieval-runtime` | `/workspace/agenteam-m3-retrieval-runtime` | 已收束组件来源：pg_search SQL/过滤/readiness；正式组合转 Knowledge |
| `ai/m3-runner-skills` | `/workspace/agenteam-m3-runner-skills` | Model owner：prepare 正常整链已过；Unknown/维护/冷退休继续 |
| `ai/m3-meeting-tools` | `/workspace/agenteam-m3-meeting-tools` | Tools：Proposal/Inbox HTTP/Web 整链已过；共同 Meeting/Inbox 前端唯一写者 |
| `ai/m3-meeting-cold` | `/workspace/agenteam-m3-meeting-cold` | Meeting：固定1–134 Summary source冷恢复；观察SQL修后实际尚待 |

## 已接受范围与剩余出口

- Knowledge 的自动索引、四格式 HTTP/浏览器、PDF 与 Agent 引用及退出已过；固定 schema125 的 Knowledge/Memory 同 App、同库 hybrid 整链已过。139 populated 升级与 fresh139 清理现分别通过：正式 pin 释放、source pair 原子删除、两次成功 COMMIT 后由 Store 边界返回 Unknown 的原只读确认、四域写入差值为零、Model/其他项目保持及 App Join。旧失败已确定为 harness 把事务统计绝对值当本次写入；只修同事务差值观察，产品与预算未变。
- Memory 的跨 Execution 工具、隔离/删除/profile 与 Owner Web、限定真实停止恢复已过；Model 三态停止解析与相应 Unknown 保真修复已独审。剩余物理故障补集不由这些正常门代证。
- Skills 安装、Owner/Agent 分配与后继读取、catalog、两类实际 Unknown 已按各自输入接受。published/running 窗口 cold 已过真实 SIGKILL/Wait→新 guard/同 E/Attempt→原 canonical 保持、一次安装上传、后继模型消费与正常 Join；Stop/lookup 交错的 R1 已定向回归及独审关闭。不含 reserved/missing cold、物理新 U2 或外部安装。
- Runner prepare 正常整链已过真实 Object GET/Runner 包、原 run-command Exit0、三轮模型消费、持久 Transfer/lease 退休及 App/Runner 正常 Join。旧轮业务会话间歇断开仍未定因；维护与 cold 新提供方继续验证，不复跑未变正常链。当前 M3 不新建 Skill 多版本发布系统，也不宣跨更新旧 revision 已验。
- MCP 的控制面、凭据、Tool/Resource/ResourceLink、媒体来源、modes、刷新及 OAuth Owner 浏览器完整链已过。OAuth issuer 使用 Node TLS relay，未宣 Chromium 直接访问 issuer CA。stranded 已到真实旧进程停止和新 guard，但新进程结果观察失败；142冷恢复尚未实际。新 Secret/Project/MCP 私有恢复门的普通通过不代数据库和 cold；独审两处必修已固定，受影响验证尚待。
- Meeting 的两 Agent/Summary、固定 snapshot/retry、并行/队列/插话及原审批/Decision浏览器已过。四工具五调用→canonical Inbox→Owner审阅/lookup/rebuild/恢复→后继模型消费与退出的提案 HTTP 整链已过。提案 Web 整链也已通过 Owner 两次审核、后继九回复、App Join 与首样 guard/数据库退出；捕获的 App/PW 进程与资源清退，未捕获 Chromium 各子 PID 的限制保留。137删除、141Decision Inbox、Meeting上Memory与Summary cold实际仍待。未定义的 update/comment 不猜实现。

## 共同组合与下一门

common 已按各 owner 完整 manifest 组合成熟 Skills/Runner、MCP、Knowledge、Memory、Meeting/Inbox 与前端。Shared App 已接原 prepare authority/dispatcher、同一 Realtime Hub、Meeting/Decision/Agent 提交通知及实际 HTTP Gateway；首共同 App 编译、Meeting/Meeting-Memory/prepare 配置与四个精确入口检查已过；受影响前端五业务 spec 共43例、两 codec spec 共23例及完整类型检查已过，未扩称全部前端测试或共同实际通过。单树旧门不外推本次共同组合，未来 Artifact 上传与142恢复未作为已完成输入混入。

正式 PostgreSQL 连续1–140的空库/升级/重复/健康及140真实约束门已过。141 Decision 的独立空库、真实140存量升级/重复、旧提案字段与xmin保持、严格source/context/事件约束现已通过；1–141均已应用，不可回写。142分配给MCP冷恢复，source与数据库测试binary已准备，实际尚待；143未分配。先用固定新 App 做原137删除，再按迁移前置推进Decision/Memory等门；失败只修实际首断点。

有限 Realtime 的 typed contract、真实受控 WebSocket Gateway 普通/race及有限独审、Execution 公开投影及 Meeting 私有授权/known-commit通知定向门已过，App与前端已进入组合。其范围仅真实状态/waiting/Tool摘要/已提交公开文本、ack/snapshot/resync；不含 token 增量、私有 reasoning 或完整 D24/D25 流式验收。User upload 的正式 Artifact 路径尚在实施，不能用既有 Artifact 引用读取冒充上传已接。

## 资源与恢复

最多两组主构建；唯一 native fixture 不与 Go/Node/image 构建并起。每个 grant 是单命令或有限 batch，原 Wait 与 exact cleanup 后归还，旧 grant 不顺接。native 前 /workspace 与 /tmp 均须满足2.5GiB，使用独立 nonce/DB/目录及正式 helper 的早 owned record，不连接 dev PG/MinIO。源码冻结仅约束实际消费者输入，静态整理无需因无关资源 hold 停止。

固定 Go1.27.1、已验证 MinIO、Playwright1.56.1 与批准的 system Chromium151复用；默认 proxy/TLS保持。cache 清理只沿 root 每轮有界授权，逐文件身份与年龄核验；旧 App 测试 binary 只有 owner 确认替代已编译/配置通过且无后继引用才能精确回收。目录、日志、源码、工具、当前产物与原 Runner 保留。系统受保护进程的不可读观测限制如实记录，不操作其资源。

## 上一任务：M2 已正式交付，任务收尾完成

M2 已正式交付并完成 Git 收尾，完整接受与历史限制见 [M2卡](../docs/development/work-items/m2-task-collaboration.md)及[独立报告](../docs/development/work-items/m2-independent-review.md)。本页不重复旧 M2 验收流水。
