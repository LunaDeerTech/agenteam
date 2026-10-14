# 当前状态：继续原定 M3

2026-10-14 用户明确调整优先级：继续原定 M3 的知识检索与引用、Memory、领域工具、MCP、Meeting 和相关 Runner 扩展；**M4 暂时废弃为当前里程碑，M3 完成后重新评估剩余缺口并继续完善，不自动进入交付阶段。** 本次文档调整基于 main `56d0d0b0`，当前目标和依赖见[开发计划](../docs/development/development-plan.md#当前开发目标与里程碑)，派工与实际分支见[任务台账](../docs/development/agent-team/tasks.md#当前目标与工作包状态)。

M3 按共同基线 `d9356e09` 建立六个功能工作树，后续另建选型、固定验证、Meeting UI 与 Skills 准备工作树，共十个工作树。每个路径均只有一个内容 writer；root 负责全部 Git、契约同步与最终集成。

| 分支 | 工作树 | owner / 范围 |
| --- | --- | --- |
| `ai/m3-knowledge` | `/workspace/agenteam-m3-knowledge` | `m3_knowledge`：Knowledge 索引检索、共享 retrieval 与引用 |
| `ai/m3-memory` | `/workspace/agenteam-m3-memory` | `m3_memory_tools`：Memory generation、长期 snapshot 保留与真实统一 Tool 链，已回本树继续 |
| `ai/m3-mcp` | `/workspace/agenteam-m3-mcp` | `m3_mcp`：MCP Connection、发现、Tool/Resource；已恢复实现，准备 Owner Connection 与 Agent/Resource 真实 App 链 |
| `ai/m3-meeting` | `/workspace/agenteam-m3-meeting` | `m3_meeting`：Session/Turn/Contribution、provider、Runtime 与执行控制 |
| `ai/m3-integration` | `/workspace/agenteam-m3-integration` | `m3_integration`：App/Tool/Executor/身份共享契约、组合 harness |
| `ai/m3-model` | `/workspace/agenteam-m3-model` | `m3_model`：Model Embeddings、structured Runtime 与 serving retention；限定真实退休及 OAuth Secret 写入/Unknown 门已过，完成媒体小组后转 Skills |
| `ai/m3-retrieval-selection` | `/workspace/agenteam-m3-retrieval-selection` | `m3_retrieval_selection`：D13 固定词法/dense/RRF 比较已保存并停写；基线 `a1ce6f1b` |
| `ai/m3-knowledge-validation` | `/workspace/agenteam-m3-knowledge-validation` | `m3_knowledge`：固定 App 首链验证，运行期间源码不漂移 |
| `ai/m3-meeting-ui` | `/workspace/agenteam-m3-meeting-ui` | `m3_memory_tools`：Meeting HTTP/UI 已阶段保存；实际 Owner 浏览器链已过，Agent busy 范围单列 |
| `ai/m3-skills` | `/workspace/agenteam-m3-skills` | 后继 `m3_model`：准备中，尚未写入；动态 Skills 原工具入口与相关 Runner 接缝 |

当前为实现、公共接线与限定真实验证阶段，整链和 M3 未验收。Knowledge 的实际 canonical Object→两次真实 Embedding→4096 维 serving generation 与原 App/Guard 退出已通过，Scheduler→query-doc/read-doc→后继引用正在固定候选验证。Memory 的真实 PG current revision/shared RRF/跨 Execution 命名空间过滤已过，generation 与长期 snapshot 保留现接 App，真实工具和模型组合待验证。MCP 官方 SDK 的真实 TLS 协议四子项已过，App 已接统一审批执行、Resource capture、Secret lease 退休与 HTTP；文本 Resource canonical 读取投影已作纯测待真实 Object 验证；实际 Agent、OAuth 连接协议及媒体完整链仍未完成。Meeting 实际 Owner 浏览器完成列表/新建/详情/发送/原 lookup/刷新，限定于 Agent busy；原首轮工具选择已按 captured Meeting 改为 auto（Task 仍 required），两 Agent 与普通文本 Summary 的首真实 App 链已通过；恰好 3 次受控模型 HTTP、3 个 Model/Secret 原 owner 退休及原 App 退出，完整控制/Decision 仍待。

Model 同一 v2 snapshot 的真实 64/64/8 共 136 个 Embedding、4096 维及原 Usage/Secret/Join 收尾已过；长期 serving retention 的真实受控调用、两个 generation 独立引用、填充数据升级和两个真实 CommitUnknown 分支均有限通过，仍须 Knowledge/Memory 实际业务接入。OAuth Secret 的 create/update/delete、CAS、回滚与两个真实 CommitUnknown 分支已在独立受控 fixture 通过，未外推 MCP OAuth 协议。retention 独审已保存；后继 Secret/Audit 窄审由 `m3_core_review` 维护[独立报告](../docs/development/work-items/m3-core-independent-review.md)。

D13 固定词法与融合比较及先前离线独审已保存。用户关于默认 dense 或继续原 hybrid 范围的决定尚未到达，架构和默认选择保持原合同；显式 `native_pg_fts` 仅用于限定首链。动态 Skills 的 list/read/install 已安排后继 writer，PDF 有界解析及本阶段相关 Runner 接口仍是 M3 待完成包，不将全 D22 欠账无界并入。

公共依赖锁文件由 integration 唯一持有，其他树只读。迁移按 root 编号在候选树连续组合；main 不接收未验收产品 WIP。每树 `.agent-state/current.md` 保存可恢复片段，本索引不复制执行流水。最多两组重构建/独立 PG fixture，由 integration 调度；固定 Go1.27.1、MinIO 和共享锁定依赖复用，浏览器已恢复锁定 Playwright 1.56.1/Chromium1194，入口 owner 按限定候选继续真实组合验证。现有 dev PG/MinIO 不作任务测试库，默认代理与 TLS 校验保持。旧 STOP 只按原风险范围保留。

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
