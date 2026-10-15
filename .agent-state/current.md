# 当前状态：M3 明确范围已接受，主线接续与完善评估

本轮已明确的 Knowledge 检索与引用、Memory、领域工具、MCP、Meeting 及必要 Runner 扩展，连同共同接线、实际用户链和相应独立核验，均已闭合。本文件作为 `main` 的接续索引，M3 源码与必要可恢复材料已完整原子合入并推送 `main`；root 继续负责逐对象收尾，已完成与保留状态如下。M4 继续撤出当前里程碑，下一轮先按真实缺口评估完善，不自动进入生产发布或 E01，不宣完整工具目录或全产品 ready。范围与限制见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。

root 负责全部 Git；integration 负责共同内容和限定冲突核对。各路径保留唯一 writer。独立分支旧 current 中的待验描述不再作为本轮接续任务，历史失败与未覆盖范围按正式卡保留，不回填为通过。

## 工作树与收尾索引

当前实际为12个M3工作树加main，共13树。main已正式推送；knowledge、knowledge-validation、meeting-tools三棵物理工作树和对应本地/远端分支、本机跟踪引用均已精确清退，fetchspec无这三项残留。其余工作树与ref按下方材料/依赖原因保留。root只在作者/后台退出、内容与材料可恢复、共享依赖不再使用后逐对象处理，不能只凭git clean或单链PASS删除。

| 分支 | 工作树 | 负责人 / 范围 |
| --- | --- | --- |
| `main` | `/workspace/agenteam` | root：正式接续、原子 Git 交付与后续评估 |
| `ai/m3-integration` | `/workspace/agenteam-m3-integration` | integration：主线交付后的协调索引与材料收尾，保留此树 |
| `ai/m3-memory` | `/workspace/agenteam-m3-memory` | Memory：正常与限定冷恢复的固定源码、证据；owner 按需续派 |
| `ai/m3-mcp` | `/workspace/agenteam-m3-mcp` | MCP：当前明确核心窗口、143与共同接线已获限定接受 |
| `ai/m3-meeting` | `/workspace/agenteam-m3-meeting` | Meeting：已通过 Summary、并行/队列/插话的固定来源 |
| `ai/m3-model` | `/workspace/agenteam-m3-model` | Model：已通过 Model/Secret/StoppedResolution 固定来源；新实现转 Runner 树 |
| `ai/m3-retrieval-selection` | `/workspace/agenteam-m3-retrieval-selection` | 已收束选型来源：D13 lexical/dense/RRF 比较 |
| `ai/m3-meeting-ui` | `/workspace/agenteam-m3-meeting-ui` | Meeting 固定 UI：Decision/Approval、Retry/Regenerate/history与最终Metadata完整实际已接受 |
| `ai/m3-skills` | `/workspace/agenteam-m3-skills` | Skills：目录/分配、Unknown、published cold 与 R1 定向修复；成熟组已导 common |
| `ai/m3-meeting-participants` | `/workspace/agenteam-m3-meeting-participants` | Meeting：引用/参与者、137删除、141Decision、Reply 与 Realtime 领域提供方 |
| `ai/m3-retrieval-runtime` | `/workspace/agenteam-m3-retrieval-runtime` | 已收束组件来源：pg_search SQL/过滤/readiness；正式组合转 Knowledge |
| `ai/m3-runner-skills` | `/workspace/agenteam-m3-runner-skills` | Model owner：prepare正常链与共同cold完整后继消费/Join已接受 |
| `ai/m3-meeting-cold` | `/workspace/agenteam-m3-meeting-cold` | Meeting：固定1–134 Summary source来源；prepared/Resolve前冷窗口已由共同候选实际通过 |

## 本轮已接受出口

- Knowledge：自动索引、四格式HTTP/浏览器、PDF与Agent引用、同App同库Knowledge/Memory hybrid，以及139 populated升级和fresh清理。原成功COMMIT后的Unknown只读确认、四域零新写与原App退出按实际范围接受。
- Memory/Skills/Model：跨Execution retain/recall/reflect、Owner入口和限定真实停止恢复；动态Skills安装/分配/catalog/后继读取、published窗口及原Unknown身份保真。Model三态停止解析与原call/borrower退休证据复用；不扩大到所有物理故障或全部安装窗口。
- Runner：正常prepare/原run-command与原Join已过。最终共同race App的18568原cold FULLPASS35.47s，真实活reader/terminal GET屏障、SIGKILL/新Guard、原tuple退休/readerWorks0、strictUnknown后继Model/同E成功及App/Runner正常Join完整执行，core独立接受。旧失败保留；未单独实证warm Issue物理组合，旧间歇会话断开原因仍未确定。
- MCP：控制面、正文Tool/Resource/ResourceLink、Owner/OAuth浏览器与原OriginalDelete/stranded限定窗口均保持。最终Published70701、Startup53497、Human63771三窗口完整退出并获独审；Human只原disabled隔离窗口，stranded末强杀不冒整App graceful Join。迁移143及其升级/fresh/repeat/health/版本上界独审已闭。详见[MCP卡](../docs/development/work-items/m3-mcp.md)。
- Meeting/Inbox：顺序/并行/队列/插话、Decision/Approval/取消、固定Summary/retry、新generation、删除/Audit、Memory工具、Proposal审阅与恢复、原Reply和mixed-cursor SQL补集均按各自范围接受。有限双topic Realtime、Composer来源/附件/Pin/删除、Retry/Regenerate/history和最终Metadata15342完整旅程均实际通过并独核。Metadata含3Turn/5E、历史引用冻结、0正文读取、8Model/lease及App Join；日志database_gone只表示原App数据库连接归零，fixture删除由精确ID/目录独核另证。详见[Meeting卡](../docs/development/work-items/m3-meeting.md)。

## 共同候选与真实退出

最终共同源码完整纳入Runner/MCP专属叶及App/Router/Runtime/Audit共享接线；四个共享hunk独审、Runtime/Project八项普通、一次race App编译、同件五配置/四路由/二十入口均通过。该App直接完成18568原cold完整出口；共同Web最终类型及Vite均通过并由ui_review核证，不以编译结果外推未跑用户链。

最终共同App为 `/tmp/m3-common-integrated-race.horkxrsy/app.test`（SHA256 `4d53084cb3799346a5521a64433835aea1beeb8e9e44f2331983461cd6e1630b`）。完整共同Web为 `/tmp/agenteam-m3-common-final-web-ready.7jvu8q9_/dist`，来源a20580ae。Metadata15342实际使用旧已编96226与仅替换最终Metadata组件的 `/tmp/agenteam-m3-metadata-final-web-ready.b00ftc9d/dist`，不是整个共同Web的浏览器验收；两产物及原manifest各有明确来源。

所有本轮命令、App/Runner/Node/observer与fixture已收原Wait/清退。Metadata34个捕获PID中32个absent，两个同start_ticks的Chromium为PPID1/Z，不称全PID不存在；Composer及其它原浏览器的各自观测限制仍保留。连续迁移1–143已实际应用不可回写，144未分配。

## 后续范围与材料

本轮已明确M3无剩余必达测试门。后续先评估完整Lifecycle/全平台Runner、全面旧页体验、生产SPA/部署恢复、完整D24/D25 token流与其它原产品缺口，不自动新增M4。update-meeting/comment-meeting仍只有长期目录名字，当前调用闭集和本卡具体出口未依赖未定义的可写对象/命令语义，因此不纳本轮完成门；仍未定义、未注册、未接受，原产品问题保留，不以用户沉默作决定。

本轮已完成knowledge、knowledge-validation、meeting-tools的最小收尾，必要生产与验收源码均已核对正式保全；Tools原Proposal请求终止观察脚本已精确补入主线并复用其原实际。Knowledge旧 `index_worker.go.wip` 已获原作者确认废弃，两个本树依赖软链也按精确路径移除；共享依赖实体与所有/tmp证据不动。root逐树普通remove、精确本地/远端ref及跟踪引用删除均完成，最终远端查询确认三ref不存在。首次裸Git远端删除的认证失败，已用既有gh credential helper与当前认证环境的单命令配置解决，未更改持久凭据配置、登录或权限范围，不再列为阻塞。

integration保留本协调索引。memory的 `web/node_modules` 实体仍被MCP/旧UI软链引用；Model的37个原日志与7个owned记录继续保树/ref，4个旧binary可再生。唯一公开向量恢复输入已按原正式路径纳入main的 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) manifest、result与vectors.json.gz，来源784c6468；raw JSON是可恢复副本。Meeting四树、两retrieval、skills、runner及mcp的独有原证据/材料去向尚未完整确认，保留各树与ref，后续逐项处理，不扩大本轮交付。

正式schema测试动态依赖的 `.agent-state/knowledge-content-http/schema-controls.py` 与唯一PDF paint复现源码已随原子交付保留。共享 `/tmp/agenteam-m3-inbox-node-i2kmjb_g/node_modules-overlay`、正式Node/Go/MinIO工具及当前产物有跨树依赖，不随工作树盲删。可再生缓存/重复日志不要求永久归档；未知归属或关键来源先保留，最终由root更新实际收尾状态。
