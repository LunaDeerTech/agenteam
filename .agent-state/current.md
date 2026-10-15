# 当前状态：M3 明确范围已接受，主线接续与完善评估

本轮已明确的 Knowledge 检索与引用、Memory、领域工具、MCP、Meeting 及必要 Runner 扩展，连同共同接线、实际用户链和相应独立核验，均已闭合。本文件作为 `main` 的接续索引，M3 源码与必要可恢复材料已完整原子合入并推送 `main`；root 继续负责逐对象收尾，已完成与保留状态如下。M4 继续撤出当前里程碑，下一轮先按真实缺口评估完善，不自动进入生产发布或 E01，不宣完整工具目录或全产品 ready。范围与限制见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。

root 负责全部 Git；后续协调以 main 的本索引为唯一入口，integration 已完成停写并清退。各路径保留唯一 writer。独立分支旧 current 中的待验描述不再作为本轮接续任务，历史失败与未覆盖范围按正式卡保留，不回填为通过。

## 工作树与收尾索引

当前实际为9个M3工作树加main，共10树。main已正式推送；knowledge、knowledge-validation、meeting-tools，以及本次 integration、meeting、meeting-cold 的物理工作树和对应本地/远端分支、本机跟踪引用均已精确清退，fetchspec无这六项残留。本地冗余 `work` 分支也已普通删除。其余工作树与ref按下方具体材料/依赖原因暂留，材料收拢或依赖解除后继续逐对象收尾，不据此恢复产品开发。

| 分支 | 工作树 | 当前保留原因 |
| --- | --- | --- |
| `main` | `/workspace/agenteam` | root：正式接续、唯一协调索引与后续评估 |
| `ai/m3-memory` | `/workspace/agenteam-m3-memory` | `web/node_modules` 实体仍被 MCP/UI 软链引用；另有两张已审截图与三个冷恢复原记录组待收拢 |
| `ai/m3-mcp` | `/workspace/agenteam-m3-mcp` | 正式报告引用的原记录及三张已审截图待收拢；诊断草稿/patch/probe 已有正式替代 |
| `ai/m3-model` | `/workspace/agenteam-m3-model` | 独有 Model/OAuth/retention/StoppedResolution 复现测试、OAuth result 与原记录尚未收拢；已保存向量不构成留树理由 |
| `ai/m3-retrieval-selection` | `/workspace/agenteam-m3-retrieval-selection` | 冻结候选/fusion harness、实际排名与来源材料尚未收拢 |
| `ai/m3-meeting-ui` | `/workspace/agenteam-m3-meeting-ui` | 六张原已审截图尚未收拢；dist 可再生，依赖软链指向 Memory |
| `ai/m3-skills` | `/workspace/agenteam-m3-skills` | 首链来源记录及正式报告引用的 assignment/catalog/Unknown/published 原记录待收拢 |
| `ai/m3-meeting-participants` | `/workspace/agenteam-m3-meeting-participants` | `tests/meeting/participants_test.go`、`references_test.go` 尚未入 main；与已清 cold 树的两份源码完全相同，当前保留此份 |
| `ai/m3-retrieval-runtime` | `/workspace/agenteam-m3-retrieval-runtime` | `check-bootstrap.py` 与原 bootstrap manifest/结果待收拢；安装/bootstrap SQL 已有 main 的 deploy 正式同内容替代 |
| `ai/m3-runner-skills` | `/workspace/agenteam-m3-runner-skills` | 正常链、首 cold 失败及最终共同 cold 的来源/命令/Wait/退出原记录待收拢 |

远端另有三个旧任务分支，均已核对当前 tip，暂不按已完成分支清理：

- `ai/live-chat-execution-e2e-20261013`：Live E2E 尚未正式通过，独有 `live_chat_execution_app_test.go`、`transport_observation_test.go` 与验收卡未合入 main。卡中的 DNS 问题是旧轮历史，不代表当前接口状态。
- `ai/task-comments-20261013`：独有 API、迁移、服务、`TaskCommentForm.vue` 和测试未在 main，完整任务尚未交付。
- `ai/project-cleanup-validation-20261012`：独有 `run-browser.py` 中的 `APP_LIVE_CHAT_SELECTOR` / `live_chat_environment` 是 Live 卡明确依赖的配套恢复入口，Live 分支自身不含该入口。

目前远端为 main、九个 M3 和上述三个旧任务，共13分支；`work` 无远端分支。本次只核对这些保留原因，不恢复旧任务或重验。

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

本次 integration 的 `output/ai/m3-artifact-user-upload/` 四轮及 `output/ai/m3-core-review/stopped-resolution{01,03}/` 共30份原件、21,179B，已在普通工作树删除前保全到 main 工作树 `output/` 下相同相对路径；逐文件内容、路径集合和 mtime 一致。它们仍属本机忽略证据，未纳入 Git 或宣称远端保存。integration 的 `web/node_modules` 仅删除软链，共享 overlay 目标未动；meeting/meeting-cold 无独有未保存改动，cold 的两份额外 SQL 回归源码由 participants 树保留。三树和对应本地/远端ref均已由 root 清退，远端逐项确认不存在。

其余九树按表中具体材料或依赖暂留，不是未完成的 M3 产品门，也不要求永久保树。唯一公开向量恢复输入已按原正式路径纳入 main 的 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) manifest、result与vectors.json.gz，来源784c6468；Model 的 raw JSON 与已保存 gzip 解压内容逐字节一致，4个旧 binary 可再生。原日志只按仍需引用的最小集合收拢，不因历史输出长期保留全部临时分支。

正式schema测试动态依赖的 `.agent-state/knowledge-content-http/schema-controls.py` 与唯一PDF paint复现源码已随原子交付保留。共享 `/tmp/agenteam-m3-inbox-node-i2kmjb_g/node_modules-overlay`、正式Node/Go/MinIO工具及当前产物有跨树依赖，不随工作树盲删。可再生缓存/重复日志不要求永久归档；未知归属或关键来源先保留，最终由root更新实际收尾状态。
