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
| `ai/m3-mcp` | `/workspace/agenteam-m3-mcp` | MCP：OAuth 已全域移交本 owner，正常浏览器已过；143/Published 已接受；Startup/Human 最后两窗口 |
| `ai/m3-meeting` | `/workspace/agenteam-m3-meeting` | Meeting：已通过 Summary、并行/队列/插话的固定来源 |
| `ai/m3-model` | `/workspace/agenteam-m3-model` | Model：已通过 Model/Secret/StoppedResolution 固定来源；新实现转 Runner 树 |
| `ai/m3-retrieval-selection` | `/workspace/agenteam-m3-retrieval-selection` | 已收束选型来源：D13 lexical/dense/RRF 比较 |
| `ai/m3-knowledge-validation` | `/workspace/agenteam-m3-knowledge-validation` | Knowledge：固定早期 App 验证输入 |
| `ai/m3-meeting-ui` | `/workspace/agenteam-m3-meeting-ui` | Meeting 固定 UI 产物；Decision/Approval、Retry/Regenerate/history 实际已过；Metadata 修后待真实复验 |
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
- Runner prepare 正常整链与原 App/Runner Join 已过，旧业务会话间歇断开仍未定因。冷门早期 Issue 重复授权读取已用同一活 callback 的严格单次 immutable input 借阅消除一层重复，普通门及独立安全复核通过；没有放宽原权限或预算。随后实际已完成脚本与 Transfer 退休，但小包在 Object 的64KiB integrity 预读阶段即到 EOF，测试持有 Reader 指针并不代表活 lease。仅 cold fixture 改为合法大包并在 ready 前核同 object/process 的真实活 reader 后，新72601已通过完整目标屏障、旧 child SIGKILL/Wait和新 guard，原 GET 完成退休、lease归零、Tool诚实 failed/unknown。整 top 仍失败：Execution 两处无条件 Unknown 拦截阻止已正式发布的 canonical error result 进入后继 Model；现只让 exact skill.prepare/rev1 完整终态候选经过原 Join、正式 Discover/fulltuple 校验与 opaque WithResult 同事务消费，真 CommitUnknown/Lookup error 仍拒。新六项定向普通与 core 独立安全复核通过，真实后继消费与 App Join 待修后 cold；未改 M2 查询 helper、迁移或预算。
- MCP 控制面、正文 Tool/Resource/ResourceLink、凭据与 OAuth Owner 浏览器正常链已过，issuer 使用 Node TLS relay，不宣 Chromium 直接信任。OriginalDelete 的真实停止、新 guard、同原 Delete 收敛及 App Join 已获有限独审；stranded 的 exact materialization 清理与 MCP owner Join 也已接受，末新 child 强制退出的限制保留。Published 旧轮未到目标屏障，sole 摘要已改为正式 Canonical 原字节 SHA256；另核出该窗口 lease 尚未 Acquire，现新增受限 Secret observation planner，使原 private stopped/fulltuple/完整锁/Validate/Observe 可以正式观察原请求的未建 lease，旧 DiscoverUsage 拒绝不变。该接缝定向六门与独审通过。Startup 的 grant 错填 RequestID 已修；前向143 Audit 闭集含版本 int64 上界，真实142升级/历史Human保持、正反约束、fresh/repeat/health与独审已过，1–143不可回写。Human65995已到真实停止、新 guard、原 refresh/outer lease 退休及父级 tuple/stopped 断言，末 graceful child Wait1；同锁已提交 canonical lease_released 事实现用于避免再次释放已合法删除的原 lease，绝不由 absence 推成功，定向三门与独审通过。最终 App 已合这些修复并完成配置/八入口检查；70701 Published 作者整链 PASS6.47s/Wait0，真实 publication 提交屏障、旧进程停止/new guard、正式未Acquire lease观察、原指针CAS/claim、同Session stopped及原App graceful Join全部执行，8资源ID/3目录/3PID已清退，core独立实际复核接受；已发布凭据/版本保持、SDK调用零增、原command pending而lookup stopped的边界保留。Startup 与 Human 仍各自待闭合，旧失败不回填。
- Meeting 的两 Agent/Summary、固定 snapshot/retry、并行/队列/插话及原审批/Decision浏览器已过。四工具五调用→canonical Inbox→Owner审阅/lookup/rebuild/恢复→后继模型消费与退出的提案 HTTP 整链已过。提案 Web 整链也已通过 Owner 两次审核、后继九回复、App Join 与首样 guard/数据库退出；捕获的 App/PW 进程与资源清退，未捕获 Chromium 各子 PID 的限制保留。137删除实际依次遇遗漏路由、取消标志约束及 Project Audit 锁预算不足，三个首因均已窄修，修后完整实际已通过：原 Owner 动作/lookup 完成、held contribution 取消/未派发贡献零启动、四次模型与 lease 退休、同事务 completed Audit/Deleted/receipt、外域三 E 保持、迟到 Send 拒绝及 App/Guard/数据库退出。141Decision Inbox 的取消漏收已定位为可选 typed 零 ID 编码与 SQL 空串省略条件不符；窄修经普通门及原七回复实际复验通过。answer/skip 由原同一 E 继续并完成 Turn，cancel 使 Decision/E/Turn 收敛取消、Inbox resolved；6条 canonical 事件与6条 Outbox 同事务，3条 terminal 事实与原 Decision 同事务，重建不重开。原 App/Guard/数据库退出齐，Model owner 有限独审接受。Meeting 上 Memory 已通过同 E retain/recall/reflect、后继公开回复与 Summary、11次模型退休及完整 App/Guard/数据库退出；原非法 Agent 名称仅为已修 harness 失败。Summary prepared 已提交、Resolve 前的唯一冷窗口已通过旧 child SIGKILL/Wait、新 guard 的原 absence 收敛/诚实失败、零新模型或 Summary、Owner 原取消与新 App Join，独审接受该范围；不泛称全部 Summary cold。旧取消超时事实保留，未定义的 update/comment 不猜实现。

Meeting 旧 Replacement 浏览器与有限独审已通过 Retry、Regenerate、history、deferred Summary、后续正常 Turn 及原退出；不冒本轮逐字比较全部 key/body/context。Metadata 同一8秒截止的 DOM 采样修复已通过7600ms完成与8300ms未完成的正反例，真实复验已越首 Turn、参与者/typed refs保存、第二 Turn、重入/排序/unpin。43275将新首点定位为重载后编辑弹窗 Escape 关闭时焦点未回原按钮；本地原 useLayer/浏览器及组件回归已复现触发按钮被禁用后焦点落 body，现只修 Metadata 组件显式回退目标，七项组件用例与完整类型检查通过。完整 typed refs/Metadata 用户链及末 Go/App Join 仍待，原预算和焦点门保持。

## 共同组合与下一门

common 已按各 owner 完整 manifest 组合成熟 Skills/Runner、MCP、Knowledge、Memory、Meeting/Inbox 与前端。Shared App 已接原 prepare authority/dispatcher、同一 Realtime Hub、Meeting/Decision/Agent 提交通知及实际 HTTP Gateway；新共同 App 已包含删除主锁修复、Decision 可选 scope 修复、MCP 恢复生命周期修复、Artifact 两入口和 Composer 新旅程，编译、Meeting/Meeting-Memory/MCP 配置及九个精确入口检查已过；受影响前端五业务 spec 共43例、两 codec spec 共23例及完整类型检查已过，未扩称全部前端测试或共同实际通过。单树旧门不外推本次共同组合，Artifact 默认 App/metadata 已按其原固定输入实际通过；新组合的 Decision 整链已通过，MCP 142 恢复仍按各独立窗口接受。

正式 PostgreSQL 连续1–143已实际应用不可回写。141 Decision 的真实升级、旧行/xmin、严格source/context/事件约束与143 MCP Startup Audit 的历史Human保持、技术Actor正反/版本上界、fresh/repeat/health已通过并获各自有限核证；144尚未分配。迁移通过不替代 MCP 三个恢复用户链。固定共同 App 的137删除修复、Decision取消、同E Memory及有限浏览器接受保持，按实际首断点继续剩余门。

有限 Realtime 的 typed contract、真实受控 WebSocket Gateway 普通/race及有限独审、Execution 公开投影及 Meeting 私有授权/known-commit通知定向门已过，App与前端已进入组合。其范围仅真实状态/waiting/Tool摘要/已提交公开文本、ack/snapshot/resync；不含 token 增量、私有 reasoning 或完整 D24/D25 流式验收。User upload 已沿正式 Artifact 路径实现并通过普通边界门；独审所见并发 first-attempt 缺口已在原 reserve 锁内窄修，真实 Objects 的晚到 body/已有 live attempt 两门已通过，独审并发 finding 已关闭。默认 App 上传和 point metadata 两个实际 top 已完整通过。上传覆盖正式五阶段、原 Unknown 确认与权限边界；原 sole 错误要求未使用的第二项目账本已按实际范围修正，App/Artifact Join 与零活跃 lease 门保留并通过。metadata 沿当前 Owner 与稳定 Artifact ID 返回正式元数据；两 top 与全部原 fixture 资源退出齐。共同 Reply 受影响三 spec 42例、Realtime 状态三 spec 23例与各自完整类型门已通过；Inline Composer 的32例与完整类型检查及有限独审已过；附件 API/Session/UI 新24例及完整类型检查已过，固定私有 Vite 和 Playwright 单例已准备。有限双 topic 浏览器现已完整通过：同一 E waiting→Owner answer→已提交 Tool 结果/公开文本与 Timeline Summary，两订阅的严格帧/投影核验、三次模型/lease 退休、页面离开后的真实 socket close，以及原 App/assembly/guard/数据库退出全部到达。先前 context.close 被动观察和默认 mode 与测试 intent 不符的两次失败保留；没有改产品预算或用终态轮询代替 WebSocket。Composer 首实际暴露选择范围被 caret 事件覆盖，窄修通过三项受影响用例和类型检查；后继计划丢响应的 Request 观察也按同一请求真实 finished/failed 修正。最新原预算实际已完整通过浏览器旅程、删除前持久 checkpoint、请求一致性与 Delete canonical postimage、五次 Model/lease 退休及 App Joined/guard stopped/数据库连接零。资源 ID 与目录全清，捕获的 App/PW 退出；两个原 Chromium 子 PID 仍为 PID1 zombie，不称全后代不存在。作者整链与 core_review 独立实际复核均接受，旧两轮失败保留；仅两个计划502允许 Request failed，其他11个200均 finished，不外推数据库物理 CommitUnknown 或全部 scope 竞争。

原 Reply 独立 SQL 门已实际通过同 key 换目标拒绝、跨 Meeting 拒绝无副作用及历史消息重用/replay不重复，原 fixture 完整清退；明确使用真实 PG 与 Project/Agent doubles，不扩大为 Executor/Model 新实际。core_review 已独立接受该 Reply PG 范围。Inbox 混合 cursor SQL 也已实际 PASS2.50s/Wait0，混合分页、partial-provider 隔离及晚事件不重开均通过，原资源全部清退；明确 Source/Project doubles，core_review 已独立接受该 SQL 补集。

接下来的必达出口沿原卡收敛：Meeting 旧 Metadata UI；MCP 原 startup 隔离及 Human Refresh 外层 lease/pointer 冷收尾；Runner 必要 cold；最后完成共同接线受影响组合。已接受旧链复用，不扩新矩阵。update-meeting/comment-meeting 只有正式工具目录中的名称，尚无可写对象或命令语义；保持未定义、未注册、未接受，原产品范围问题继续待答，不自行新增或删除能力，不阻其他明确链路。

## 资源与恢复

最多两组主构建；唯一 native fixture 不与 Go/Node/image 构建并起。每个 grant 是单命令或有限 batch，原 Wait 与 exact cleanup 后归还，旧 grant 不顺接。native 前 /workspace 与 /tmp 均须满足2.5GiB，使用独立 nonce/DB/目录及正式 helper 的早 owned record；记录路径必须在启动前不存在，由 helper 独占创建，不预建空 JSON。不连接 dev PG/MinIO。源码冻结仅约束实际消费者输入，静态整理无需因无关资源 hold 停止。

固定 Go1.27.1、已验证 MinIO、Playwright1.56.1 与批准的 system Chromium151复用；默认 proxy/TLS保持。cache 清理只沿 root 每轮有界授权，逐文件身份与年龄核验；旧 App 测试 binary 只有 owner 确认替代已编译/配置通过且无后继引用才能精确回收。目录、日志、源码、工具、当前产物与原 Runner 保留。系统受保护进程的不可读观测限制如实记录，不操作其资源。

## 上一任务：M2 已正式交付，任务收尾完成

M2 已正式交付并完成 Git 收尾，完整接受与历史限制见 [M2卡](../docs/development/work-items/m2-task-collaboration.md)及[独立报告](../docs/development/work-items/m2-independent-review.md)。本页不重复旧 M2 验收流水。
