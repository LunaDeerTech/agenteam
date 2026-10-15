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
| `ai/m3-meeting-cold` | `/workspace/agenteam-m3-meeting-cold` | Meeting：固定1–134 Summary source来源；prepared/Resolve前冷窗口已由共同候选实际通过 |

## 已接受范围与剩余出口

- Knowledge 的自动索引、四格式 HTTP/浏览器、PDF 与 Agent 引用及退出已过；固定 schema125 的 Knowledge/Memory 同 App、同库 hybrid 整链已过。139 populated 升级与 fresh139 清理现分别通过：正式 pin 释放、source pair 原子删除、两次成功 COMMIT 后由 Store 边界返回 Unknown 的原只读确认、四域写入差值为零、Model/其他项目保持及 App Join。旧失败已确定为 harness 把事务统计绝对值当本次写入；只修同事务差值观察，产品与预算未变。
- Memory 的跨 Execution 工具、隔离/删除/profile 与 Owner Web、限定真实停止恢复已过；Model 三态停止解析与相应 Unknown 保真修复已独审。剩余物理故障补集不由这些正常门代证。
- Skills 安装、Owner/Agent 分配与后继读取、catalog、两类实际 Unknown 已按各自输入接受。published/running 窗口 cold 已过真实 SIGKILL/Wait→新 guard/同 E/Attempt→原 canonical 保持、一次安装上传、后继模型消费与正常 Join；Stop/lookup 交错的 R1 已定向回归及独审关闭。不含 reserved/missing cold、物理新 U2 或外部安装。
- Runner prepare 正常整链已过真实 Object GET/Runner 包、原 run-command Exit0、三轮模型消费、持久 Transfer/lease 退休及 App/Runner 正常 Join。旧轮业务会话间歇断开仍未定因；维护与 cold 新提供方的普通门已过；首 cold 实际未到目标 terminal-reader 屏障，未执行目标停止/重启，新 child 闭集诊断实际只观察到原 Skill/Mount/Object 七个事务均提交，Transfer 仍 issued/活 lease，脚本尚未出现；尚未到目标 reader 屏障，继续定位原阻点，不复跑未变正常链。当前 M3 不新建 Skill 多版本发布系统，也不宣跨更新旧 revision 已验。
- MCP 的控制面、凭据、Tool/Resource/ResourceLink、媒体来源、modes、刷新及 OAuth Owner 浏览器完整链已过。OAuth issuer 使用 Node TLS relay，未宣 Chromium 直接访问 issuer CA。stranded 旧轮新进程结果观察失败保留；1–142 正式数据库空库/重复/升级/健康门已过，142不可回写；原 Delete cold 旧轮未收敛后，静态核出 App 将初始化上下文传给长期恢复 worker 的确切缺陷；现只解除启动预算，由原 owner 继续 Stop/Drain/Join，定向门及独审已过。新实际 OriginalDelete 已通过原 exchange/claimed 提交、真实停止、新 guard、同原 Delete 收敛及 App Join，并获有限独审。stranded 也已通过真实停止、新 guard 与 exact materialization 清理、MCP owner Join、Unknown Tool 零重派发；新子进程最后强制退出，不宣整 App graceful Join。Published 窗口尚未开始，seed App 的合并 guard/数据库退出观察失败已窄化事实观察；原 startup 隔离及 Human Refresh 外层 lease/pointer 冷收尾仍待，不以 startup 代全部调用者。
- Meeting 的两 Agent/Summary、固定 snapshot/retry、并行/队列/插话及原审批/Decision浏览器已过。四工具五调用→canonical Inbox→Owner审阅/lookup/rebuild/恢复→后继模型消费与退出的提案 HTTP 整链已过。提案 Web 整链也已通过 Owner 两次审核、后继九回复、App Join 与首样 guard/数据库退出；捕获的 App/PW 进程与资源清退，未捕获 Chromium 各子 PID 的限制保留。137删除实际依次遇遗漏路由、取消标志约束及 Project Audit 锁预算不足，三个首因均已窄修，修后完整实际已通过：原 Owner 动作/lookup 完成、held contribution 取消/未派发贡献零启动、四次模型与 lease 退休、同事务 completed Audit/Deleted/receipt、外域三 E 保持、迟到 Send 拒绝及 App/Guard/数据库退出。141Decision Inbox 新诊断已确认 answer/skip 完成 E、Turn 与 Inbox；cancel 的 E/Turn 已取消但 Decision 仍 pending。首因是可选 typed 零 ID 被编码为全零 UUID，而 SQL 用空串表示省略；两处查询共用的参数编码已修，普通与实际复验待。Meeting 上 Memory 已通过同 E retain/recall/reflect、后继公开回复与 Summary、11次模型退休及完整 App/Guard/数据库退出；原非法 Agent 名称仅为已修 harness 失败。Summary prepared 已提交、Resolve 前的唯一冷窗口已通过旧 child SIGKILL/Wait、新 guard 的原 absence 收敛/诚实失败、零新模型或 Summary、Owner 原取消与新 App Join，独审接受该范围；不泛称全部 Summary cold。Decision 整 top 仍未接受。未定义的 update/comment 不猜实现。

## 共同组合与下一门

common 已按各 owner 完整 manifest 组合成熟 Skills/Runner、MCP、Knowledge、Memory、Meeting/Inbox 与前端。Shared App 已接原 prepare authority/dispatcher、同一 Realtime Hub、Meeting/Decision/Agent 提交通知及实际 HTTP Gateway；新共同 App 已包含删除主锁修复、Decision 诊断、MCP 恢复生命周期修复及 Artifact 两个实际入口，编译、Meeting/Meeting-Memory/MCP 配置及七个精确入口检查已过；受影响前端五业务 spec 共43例、两 codec spec 共23例及完整类型检查已过，未扩称全部前端测试或共同实际通过。单树旧门不外推本次共同组合，Artifact 默认 App/metadata 已按该固定输入实际通过，142恢复仍待独立实际。

正式 PostgreSQL 连续1–140的空库/升级/重复/健康及140真实约束门已过。141 Decision 的独立空库、真实140存量升级/重复、旧提案字段与xmin保持、严格source/context/事件约束现已通过；1–141均已应用，不可回写。142的正式数据库门随后通过，全1–142已实际应用不可回写；143未分配。固定新 App 的137删除修复已完整通过，继续 Decision取消修复并轮转已就绪的MCP/Artifact独立实际门；Memory同E首链证据复用；失败只修实际首断点。

有限 Realtime 的 typed contract、真实受控 WebSocket Gateway 普通/race及有限独审、Execution 公开投影及 Meeting 私有授权/known-commit通知定向门已过，App与前端已进入组合。其范围仅真实状态/waiting/Tool摘要/已提交公开文本、ack/snapshot/resync；不含 token 增量、私有 reasoning 或完整 D24/D25 流式验收。User upload 已沿正式 Artifact 路径实现并通过普通边界门；独审所见并发 first-attempt 缺口已在原 reserve 锁内窄修，真实 Objects 的晚到 body/已有 live attempt 两门已通过，独审并发 finding 已关闭。默认 App 上传和 point metadata 两个实际 top 已完整通过。上传覆盖正式五阶段、原 Unknown 确认与权限边界；原 sole 错误要求未使用的第二项目账本已按实际范围修正，App/Artifact Join 与零活跃 lease 门保留并通过。metadata 沿当前 Owner 与稳定 Artifact ID 返回正式元数据；两 top 与全部原 fixture 资源退出齐。共同 Reply 受影响三 spec 42例、Realtime 状态三 spec 23例与各自完整类型门已通过；Inline Composer 的32例与完整类型检查及有限独审已过，真实默认 Enter/跨 scope 与附件浏览器仍待；有限双 topic 浏览器仍待。

接下来的必达出口沿原卡收敛：Meeting 剩余控制、Decision 取消、有限双 topic Realtime，以及 Composer 的正式来源与附件；MCP Published 窗口、原 startup 隔离及 Human Refresh 外层 lease/pointer 冷收尾；Runner 必要 cold；Artifact 附件浏览器与独审收口；最后完成共同接线受影响组合。已接受旧链复用，不扩新矩阵。Agent update/comment 未获规则回复，保持未注册，是否阻塞完整原 M3 由 root 按原契约裁定。

## 资源与恢复

最多两组主构建；唯一 native fixture 不与 Go/Node/image 构建并起。每个 grant 是单命令或有限 batch，原 Wait 与 exact cleanup 后归还，旧 grant 不顺接。native 前 /workspace 与 /tmp 均须满足2.5GiB，使用独立 nonce/DB/目录及正式 helper 的早 owned record，不连接 dev PG/MinIO。源码冻结仅约束实际消费者输入，静态整理无需因无关资源 hold 停止。

固定 Go1.27.1、已验证 MinIO、Playwright1.56.1 与批准的 system Chromium151复用；默认 proxy/TLS保持。cache 清理只沿 root 每轮有界授权，逐文件身份与年龄核验；旧 App 测试 binary 只有 owner 确认替代已编译/配置通过且无后继引用才能精确回收。目录、日志、源码、工具、当前产物与原 Runner 保留。系统受保护进程的不可读观测限制如实记录，不操作其资源。

## 上一任务：M2 已正式交付，任务收尾完成

M2 已正式交付并完成 Git 收尾，完整接受与历史限制见 [M2卡](../docs/development/work-items/m2-task-collaboration.md)及[独立报告](../docs/development/work-items/m2-independent-review.md)。本页不重复旧 M2 验收流水。
