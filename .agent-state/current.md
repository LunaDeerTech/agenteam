# 当前状态：继续原定 M3

2026-10-14 用户决定继续完整原 M3：Knowledge 检索与引用、Memory、领域工具、MCP、Meeting 及所需 Runner 扩展。**M4 撤出当前里程碑；M3 完成后重新评估剩余缺口，不自动进入交付阶段。** 目标见[开发计划](../docs/development/development-plan.md#当前开发目标与里程碑)，实际出口见[M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)。M3 尚未完成，main 不接收未验收产品 WIP。

当前有 15 个 M3 工作树及 main，共 16 树。每个内容路径只有一个 writer；root 负责所有 Git，integration 负责共享 App/Execution 接线及唯一资源调度。固定验证树和暂停 owner 的源码保留，不把建树或编译计为功能通过。

| 分支 | 工作树 | owner / 当前范围 |
| --- | --- | --- |
| `ai/m3-knowledge` | `/workspace/agenteam-m3-knowledge` | `m3_knowledge`：Knowledge、共享 retrieval/diagnostics、同库 hybrid 与固定镜像 fixture |
| `ai/m3-memory` | `/workspace/agenteam-m3-memory` | `m3_memory_tools`：Memory 正常链与限定冷恢复已过；共享消费者由原 owner 提供 |
| `ai/m3-mcp` | `/workspace/agenteam-m3-mcp` | `m3_mcp` 暂停：媒体/source/modes 已过；OAuth 浏览器与 stranded 实际门待续派，OAuth 核心由 integration 继续 |
| `ai/m3-meeting` | `/workspace/agenteam-m3-meeting` | `m3_meeting`：Summary 固定 snapshot/retry 整链已过，继续队列/并行/finalize 等控制 |
| `ai/m3-integration` | `/workspace/agenteam-m3-integration` | `m3_integration`：共享契约、App/Execution/OAuth、迁移组合与资源调度；独审由 core_review 独立执行 |
| `ai/m3-model` | `/workspace/agenteam-m3-model` | `m3_model`：固定 Model/Secret/StoppedResolution 证据保存；新实现转 Runner 树 |
| `ai/m3-retrieval-selection` | `/workspace/agenteam-m3-retrieval-selection` | 原选型 owner 已收束：固定 D13 词法/dense/RRF 比较证据 |
| `ai/m3-knowledge-validation` | `/workspace/agenteam-m3-knowledge-validation` | `m3_knowledge`：已冻结 App 首链验证输入 |
| `ai/m3-meeting-ui` | `/workspace/agenteam-m3-meeting-ui` | `m3_meeting`：已过 Decision/Approval；Retry/Regenerate 与 typed refs 两新旅程待实际验证 |
| `ai/m3-skills` | `/workspace/agenteam-m3-skills` | `m3_memory_tools`：目录/分配/撤权失败出口及两类 Unknown 实际恢复；独审新 U1/U2 交接窄修 |
| `ai/m3-meeting-participants` | `/workspace/agenteam-m3-meeting-participants` | `m3_meeting`：参与者/真实引用与 137 删除意图；purge 尚未完成 |
| `ai/m3-retrieval-runtime` | `/workspace/agenteam-m3-retrieval-runtime` | 原选型 owner 已收束：固定 pg_search SQL/过滤/readiness 组件证据，组合转 Knowledge |
| `ai/m3-runner-skills` | `/workspace/agenteam-m3-runner-skills` | `m3_model`：prepare 本地与 Central 定向门已过，原 Transfer/mount/session/App 已静态接齐；正常首链待验 |
| `ai/m3-meeting-tools` | `/workspace/agenteam-m3-meeting-tools` | `m3_meeting_tools`：四个已定义工具、Proposal/Inbox/Owner HTTP 与 App 已静态接齐；首真实链待验；update/comment 定义待用户异步回复 |
| `ai/m3-meeting-cold` | `/workspace/agenteam-m3-meeting-cold` | `m3_meeting`：固定完整 1–134 与准备锁修复，供 Summary source 冷恢复；不混孤立 137 |

Knowledge 的真实外部模型引用、自动索引更新/profile 切换、四格式 HTTP 与完整浏览器上传/逐字下载/PDF 替换/键盘链及后继 Agent 页码引用已通过，全部 App/Guard/fixture 退出。PDF 查看器 ready 早于首绘制的边界另由无 PG 组件截图验证可见正文。固定 pg_search 正式镜像已实际构建并核包/hash/许可、HTTPS/CA 与 immutable ID；SQL/两域过滤/bootstrap/readiness 组件已过，真实 Knowledge/Memory 同库 App hybrid 尚待。

Memory retain/跨 E recall/只读 reflect、隔离/删除、profile 切换与 Owner 浏览器已通过。原已退休窗口、authorized 及 Resolve 已提交但 Begin 未入场窗口已真实 kill/restart 后保原调用/租约身份、零重发、canonical 失败被原 Agent 消费并完整退出。Model StoppedResolution 三态已独立实际通过；Memory 三处原写 Unknown 被观察错误覆盖的窄修已复核关闭，物理 Unknown 未覆盖范围不外推。

Skills 安装/Owner 分配/下一轮正文、Agent self-assignment、同项目目录与另一目标分配、跨项目及 caller 正文拒绝已通过。Owner 撤 allowlist 后原 checkpoint 已能以确定拒绝结束原 E，零目录成功/分配并完整退出。最终 Tool 事务两物理 Unknown 分支已过；Skill 发布提交分支只读确认后首次完成原 Tool 结果，回滚分支保 Unknown/未 Joined 后真实停止，两轮分别接受，旧整 top 失败不回填。独审新 U1→U2 写错误身份交接缺陷已有窄修/普通门，仍由独审确认，不扩大原正常链接受。

MCP 控制面、凭据 HTTP/浏览器、Agent Tool/Resource/ResourceLink 已通过。四媒体 canonical Object/字节/MIME 与后继轮持久、text Model 诚实 Unsupported、租约和 owner 退出已过；测试 consumer 在原 Model 私有上下文使用正式 SourceResolver/Object Read/Close 与四类拒绝已过，不表示生产媒体 profile 支持。OAuth 正常 domain/Owner HTTP/S256/原 lookup 已过；新增真实到期刷新只发一次、Secret CAS、原 startup caller 与外层请求退休、重查零刷新及完整 App/资源退出也通过。OAuth 浏览器、flow/exchange/refresh 冷恢复与 stranded 实际门仍待。

Meeting 已过两 Agent/Summary、Decision/Approval/单贡献取消浏览器及 Summary 七回复固定 snapshot/retry、新 generation 换合法模型、sent 取消和完整退出。新的 Retry/Regenerate 与 typed refs App/dist 已就绪，首次浏览器停在测试脚本 expect 导入，修复仅脚本、不冒产品通过。四领域工具/Proposal/真实 Inbox 及 App 装配已保存，首九回复真实链尚待；删除仍须原执行 owner/lease 的正式退休观察与本域 purge。

正式 PostgreSQL 17.8 连续 1–135 空库/重复/升级/健康已过，已应用迁移不可回写。133 已前向修复原 127 CHECK 缺陷并实际验证坏存量原子拒绝。136 工具/Inbox、137 删除意图、138 exact prepare Unknown 仍须各自完整调用方与连续 Source 门；迁移通过不冒业务出口。独立审查范围见[报告](../docs/development/work-items/m3-core-independent-review.md)，新增高风险权限/恢复组合仍要独验。

最多两组主构建；唯一 native fixture 不与 Go/Node/image 构建并起，原 Wait 和 exact cleanup 后才归还。固定 Go1.27.1、MinIO、锁定 Playwright/Chromium 与已过产物复用。只用任务独立 nonce/DB/目录，不连接 dev PG/MinIO；默认 proxy/TLS 保持。源码仅在目标 owner 实际 idle 后同步，旧失败不回填，资源清理不计产品进度。

## 上一任务：M2 已正式交付，任务收尾完成

2026-10-14 用户已明确恢复并要求完成第二里程碑。M2 约定业务、控制与恢复门已取得实际通过。最终验证源为 `e110faeca0ef96d89d0f30c154c19ed582d89a46`，App 编译原 session87516 Wait0。最终独立审查已正式接受 M2 约定范围、无 must-fix；root 已将产品与正式文档整合并推送到 main。

**main 推送与本机 M2 Git 收尾均已完成。** 原远端认证阻碍已解除，root 确认原 push session36678 Wait0，并经远端查询确认。六个 M2 工作树、本地分支、远端分支及本机跟踪引用均已清理；本机只保留 `/workspace/agenteam` 主工作树。fetch 配置仅有 main 精确条目，本轮不存在 M2 专用 fetchspec。当前无活动 M2 任务或待清理对象；M3 活动分支见本页当前摘要。

## 最终真实链与退出

固定 e110 的 `live-final.FZ9v5n` 原 session3646 **Wait0，143.46秒**，保默认 proxy、NO_PROXY、CA/TLS；实际 route 为 trusted_proxy=true、tls_verification=true。正常 Scheduler 始终 enabled，M2 专属正式 `relaunch_skip_count=40` 为 eligible visit 次数，旧 M1 入口仍240；不承诺固定秒数。总300秒、单attempt60秒、MaxOutput4096、项目全部work/review/retry sent阈值12不扩大。

- 同一 Task 两个真实 work Execution，各3个 Model sent、零retry，实际 Runner 读取输入并分别发布新的 Artifact；第二次遵循 Human 退回意见保留统计字段并增加 verified=true。
- 两个真实 reviewer Execution 各2个 Model sent、零retry，各自只用原 read-artifact 读取精确 worker Ref/完整正文，原 Operation、retired Attempt、Tool result、来源与后继消费逐一配对。reviewer 只确认内容，Human 唯一决定状态；不称自主审核决定。
- 真实浏览器完成6阶段、2次Human写入、3次Artifact读取：读首成果→退回todo→同Task新E修订→读新成果→接受done→刷新仍可读。18条同请求联合完整响应证明闭合，其中17 requestfinished、1 ERR_ABORTED仍同时满足原生EOF/零cancel、公有consumer fulfilled、Go完整字节/Close/handler返回；不把该终态改写为requestfinished。首/次E完整projection保真、历史和旧成果保持均通过。
- 原 Runtime Drain、Model/Tool/Execution/Scheduler/planning/assembly/producers全部Join，原App/root实际返回且err=nil。Node659356原Wait成功，Runner658917原Wait0，test658791原Wait0；原Guard真实Flock取释后，原ProcessClaim stopped=true、同ownedDB app连接0，首次观察即满足（1次、8ms）。两秒只读尾观察不写数据或终止连接，旧M1 helper不变。
- 最终精确6个Docker资源、3个nonce目录、3个已记录PID、2个modules链接全部absent；`cleanup-verification.json`、`binary-provenance.json`已写。App SHA256 `63be49fa544a529fd8cec67b3068927a31880a6c255675c4822cb061fe20c67a`；Runner复用30844编译原件，其相关源码至e110未变。

## 已通过的其他门及复用范围

| 范围 | 原证据与边界 |
| --- | --- |
| 迁移与组合 | 原61435完整迁移source解析含00099及App编译Wait0；后继组合App编译均按相应输入记录，最新87516 Wait0。00094不回写，00099仅六处UUID/text配对修正。 |
| Approval | `approval.CgLQmg` /90958 Wait0：同E/Operation/fingerprint批准一次Attempt/report/副作用，拒绝零Attempt/report/副作用且00098合法后继，同事务receipt/Outbox及重放/lookup通过。 |
| 持久取消与非法配对 | `cancellation.KID4fc` /22112 Wait0：Execution取消保护Task blocked/blocker，与整Task cancelled分开验证；实际运行child/group退出、同Tx receipt、重放/lookup。非法approval result配对被真实PG拒绝并回滚，合法拒绝后继通过。 |
| waiting取消与重启 | `pending-waiting.iaPxiA` /91741整轮FAIL中的waiting-cancel PASS：同进程真实owner复用、原Guard、零Attempt，旧SIGKILL/Wait→新Process不复活、晚approve拒绝。不能称跨进程竞争；该轮另两失败保留。 |
| waiting冷恢复 | `remaining.B8leZr` /93835 Wait0：原SIGKILL/Wait后新断连、新Process/newGuard与严格新增authenticated Runner连接，才批准原E/Operation；一次副作用及原Join。 |
| 已完成/无结果冷恢复 | `recovery.yqtgeM` /54107整轮FAIL中两子例PASS：完成Tool真实持久结果后旧kill/Wait、新Process只Lookup同op/原字节/一次副作用；无结果路径四次原读取、不重发，未解owner最后kill/Wait，不冒充已完成恢复。 |
| pending Unknown取消 | 93835 Wait0：真实COMMIT响应丢失→Store Unknown→原COMMIT独立读Created；原Owner取消同Tx，原Visitor LookupFound不重发，1Launch/1E/1attempt。精确原technical custody在全部owner Join后关闭、live0。仅Found，不扩NotFound/cold矩阵。 |
| Artifact来源拒绝 | 93835 Wait0：真实A AgentRun携B完整Ref沿原Core/Adapter Project gate返回FORBIDDEN；不给B正文/地址，Artifact/Object业务与读取审计快照不变，原owners Join。正常失败Operation/Attempt允许；不称当前撤权或全部领域授权矩阵。 |
| Core与生命周期风险 | 严格closed-five实际Core Registry路径保Ordinary/current/source/tuple/锁门；原4470定向通过，独立4061 Registry/Runtime race28结果通过。retained/waiting独立90524 race通过，修退休20列及旧fixture后Execution全包28089通过。 |
| Human HTTP/browser | `task-human.tEUTa0` /75611 Wait0；browser accept为68867混合FAIL中的PASS，return深拷贝修后81046 Wait0。这些受控链在审核阶段暂停Scheduler，范围单列；正常Scheduler/外部模型由最终3646证明。 |

独立审查、阶段范围与正式合同沿 [M2卡](../docs/development/work-items/m2-task-collaboration.md)、[M2独立报告](../docs/development/work-items/m2-independent-review.md)、[Approval卡](../docs/development/work-items/m2-tool-approval.md)。backend/artifact.md、backend/README.md与审批卡已按实际绑定/取消语义窄更新；OpenAPI形状和默认Runtime disabled边界保持。

## 历史失败与未覆盖边界

早期 Start Core门、00094配对回滚、retained/waiting生命周期、Core Builtin Ordinary误拒、browser浅拷贝投影等失败均保留原证据；相应修复与后继通过不回填原轮。完整归档沿 [证据manifest](../output/ai/m2-delivery-evidence/manifest.json)。

- `live.YFB7ig` /29440 Wait1：work/Human链到达但review failed；旧closed code不足，后继只修已证实的required tools与禁tools提示矛盾，不猜原精确原因。
- `live.Y0PFsa` /8836 Wait1：首work/review实际通过，浏览器首work详情502；后继未复现，原具体transport/observer原因仍未知。
- `live-diagnostic.o3gaO6` /63389 Wait1/300.06秒：两work/首review8Model成功，M1的240 eligible visits使第二review未及预算派发。已保存原PG一次remaining48/version204只读事实；92仅现场聊天，不补造已保存序列。
- `live-review40.eDsq8e` /11992 Wait1：完整业务与前置Join通过，最后ProcessClaim stopped/PG连接gone合并二布尔之一false，原日志未区分。最终3646首次即两者true，不能倒推11992哪个false或宣称已证明其具体原因。

M2不外推自主reviewer决定、所有权限负例、跨进程owner竞争、NotFound/cold Unknown全矩阵、默认Runtime已开启、通用signed download/Runner transfer来源绑定、M3/M4或其他旧STOP解除。

## 交付与资源

所有Go源码冻结；无活动Go/native/Runner/browser，两个Go槽及唯一native窗口均已释放。固定Go `/workspace/toolchains/go1.27.1/bin/go`，`GOMAXPROCS=2 -p=2`；共享缓存、源码构建MinIO与锁定Node依赖均在worktree外并保留。临时review modules链接及frontend依赖链接已清退，外置共享依赖目标保持。

最终脱敏证据已精确保全到 `/workspace/agenteam/output/ai/m2-delivery-evidence`，192份原件及manifest仅在主树本地保全、未纳入Git，包含必要的最早失败、后继验证和最终成功原件；现行文件集合、统计与来源以 [manifest](../output/ai/m2-delivery-evidence/manifest.json) 为准。清理前原来源→review staging→main目标逐文件SHA256/大小和精确路径集合全部一致，无额外文件或符号链接；独立审查已亲核全部原件与主树副本、最终二进制来源和真实退场。只保原native日志、owned资源/清理、来源hash、完整proof引用的公开业务响应/观察与必要race日志；排除KEY、private Session/enrollment/material、raw provider正文、二进制、cache。manifest记录技术与独立验收通过、产品main远端交付及Git收尾完成，不表示原件已远端上传；准确内容与文件hash以manifest为准。编译产物与dist可按锁定源码重建，不是不可再生证据。

本轮 M2 无待执行事项；后续按用户目标从已确认 main 派发新任务，不恢复已清理的旧任务分支。全部 Git 由 root 负责，执行者不执行 Git 写操作。
