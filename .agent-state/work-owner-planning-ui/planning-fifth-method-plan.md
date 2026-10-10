# Planning05 六次原排序消费方法计划

仅计划，未实施、未运行。技术基线为 `68d146f9`；原 binary 为 `output/ai/work-owner-planning-ui/build/account-recovery11.test`（SHA256 `a272a4bd0e60a8a4ecd882b39c19970a4e9d72f0ba63309eda7c15e56ff67f5d`），私有 dist 保持。root 只授权本文件持久化及独立描述级审查，未授权类级源码实现或下一真实窗口。

## 原事实及限度

原 Planning04 安全材料为 `.agent-state/work-owner-planning-ui/planning-fourth-failure.json`，原 log 为 `/tmp/wui-op4/ui-c5136d57364c467c.log`，原 evidence 为 `output/ai/work-owner-planning-ui/planning-04-evidence/TestAccountProjectWorkPlanningWebStructureAndTasks`。session16621/outer283424，原45秒总界止于第二次 Milestone `move(true)` 的 `originalBody→Response.finished`（helpers305；spec142/164/441）；Go70.80秒、outer159.052秒，actual exit1。全部原 Wait/join、七资源/private/runtime/desc/TCP双尾和inputs不变已闭合，STOP0，窗口已释放。完整九类写、最终seen/schema/client/complete及Go持久后验未到，整轮FAIL。

- 原015第一次 before-peer：418B/version3、正常 requestfinished 和唯一 finished/null，原 start receipt/material/EOF/CL/物理尾及首explicit双observer零pending联合已越过。只接受本次正常分支的有限事实，不宣称failed分支或整Planning动态通过。
- 原018第二次 move-to-tail：200/418B/version4，原 afterEach 44,486.963ms 快照记录 aborted、无requestfinished/finished返回/observer rejection；44,549.638ms原同Request在page/context关闭后追加operation-rejected事件，不能回填首快照。首观察已结束，第二次 native/public 消费没有采样；后继019 GET/version4不能补原消费。
- 原planning03 FAIL及其缺失EOF/事件/public材料仍未知，不借本轮改写原因。Sprint/Task排序尚未执行，不能预断它们也会failed；扩大至固定六次原排序的依据是共同生产调用与同一观察边界，而非猜测未来失败。

## 固定源码共性

以下均引用 `68d146f9` 原文件，不改产品接口或契约。

1. `tests/account-captcha-web/e2e/project-work-planning.spec.ts` 的 `realOrdering` 对三个kind均执行原列表 `[peer,selected]` → `move(false)` → 原列表 `[selected,peer]` → `move(true)` → 原列表 `[peer,selected]`。Planning已有三次调用、六个排序写请求；其它create/update及冲突刺激不属于新增集合。
   同文件 `go:45` 使用 `page.goto(destination)`，Milestone和Sprint的两槽之后各有一次原go，实际替换整个document。不能假定一对浏览器observer存活到第六槽；原native initScript只负责新文档安装，publication必须在新文档ready后重新安装。
2. `web/src/composables/useProjectWorkPlanning.ts:1045` 的 `saveReorder` 统一经 `execute:775`；真实请求如下表。成功后 await `refreshAfterReceipt`，原排序框才关闭。
3. `web/src/composables/useSession.ts:3960` 的 `start` 每次捕获typed command、创建独立 WorkIntent 和新key，再走 `performWork:3819`。后者保同原intent/action/revision/context，执行正式typed API；原 `runAuthorized` 的 actual finally 释放owner后，发布并返回同一当次receipt。不能拿前一次receipt或后继GET替代。
4. `web/src/api/work-planning.ts` 的 `reorderMilestone/reorderSprint/reorderTask` 均正式校验请求和typed响应；`parseWorkPlanningReceipt:732` 区分Structure与Task，`matchFields`校验target/expected_version及Sprint parent。Task没有Structure的command/event_id字段。
5. `tests/account/project_work_planning_web_test.go:93` 的持久后验保留全部九类命令/十二次成功写、唯一key、同体receipt、typed History/Outbox/current版本4、实际读取、外部Task冲突、精确delta和Session Activity。方法计划不能改这些门。

| 固定顺序 | domain / command | 原request精确形状 |
| --- | --- | --- |
| 1 Milestone before | structure / work.milestone.reorder | `{before_id: peer}` |
| 2 Milestone tail | structure / work.milestone.reorder | `{}` |
| 3 Sprint before | structure / work.sprint.reorder | `{milestone_id: seedMilestone, before_id: peer}` |
| 4 Sprint tail | structure / work.sprint.reorder | `{milestone_id: seedMilestone}` |
| 5 Task before | task / work.task.reorder | `{before_id: peer}` |
| 6 Task tail | task / work.task.reorder | `{}` |

共同外层为 `{expected_version, request}`。tail是缺省before_id，不能替换为null；Sprint parent不能遗漏。peer来自原fixture及原列表断言，target/version来自按钮前当前选择与正式当前版本，不能从所观察请求反推预期。

## 唯一闭集与逐槽绑定

只允许 exact `TestAccountProjectWorkPlanningWebStructureAndTasks` 显式启用一个固定 `planning-reorders` policy，按上表顺序恰好六槽。不是任意route/command/callback配置；默认Recovery、独立Recovery、其它作者case不选新集合。

每个原按钮点击前，新建该槽独立记录并冻结Project、kind、before/tail、target、expectedVersion、peer和必要parent。首start和首nonGET原Request即占位；错材料也占位，错序、重复、遗漏、第二mutation使槽和整方法单调失败。上一槽原动作关闭前不可arm下一槽，不得失效后择取后续正确请求。六槽不复用旧firstStart状态、旧Promise、旧receipt、旧Request或XID；不把作者Recovery replay/raw-header ledger整体借入。

浏览器包装仍直接返回原 `start` 的同一Promise对象。同步捕获精确typed实参和entry identity/context/旧receipt，再沿同调用捕获唯一原native fetch。Node锁定同PW Request；原body/key/CSRF/URL.origin与PW actual材料在私有闭包比对，不能写出这些值、不能生成/补写请求头。浏览器自动Origin不要求出现在init.headers，而须PW实际Origin等于native URL.origin及当前location.origin。每个原Request actual头至多一次，在启动前登记同一owned tails；六个实际key在私有内存要求互异，拒绝跨槽旧intent材料。

原Promise settle时逐槽要求：返回对象就是此刻 `progress.receipt`，且不是entry旧receipt；sameidentity/current context、confirmed/committed、精确domain/command/project/target、changed=true与version=expected+1。仅保存安全匹配布尔和公开ID，不输出receipt业务字段。

- Milestone/Sprint：严格对应Structure typed对象，另一对象字段为null、event_id为UUIDv7；Sprint milestone_id绑定冻结parent。
- Task：严格Task typed对象、backlog/unassigned，task_event_id及恰一event_ids；sprint_id和milestone_id与原当前选择、原列表已知的seedSprint/seedMilestone绑定，不能借receipt自报parent或错误套用Structure receipt形状，原group及持久后验保持。
- 旧receipt值相同但对象不同、void fulfillment、错域/command/version/parent、跨槽返回均拒绝。正式API decoder和原same-body/schema/client验证保持。

## 原终态与六槽最终联合

每槽observe和body桥共享这个原Request已登记的唯一terminal Promise；正常requestfinished分支共享一次原Response.finished/null。只有该槽恰一次aborted且零finished可进入待证明消费，不能先启动无必达finished再把它遗弃，也不能凭fulfilled/EOF/后继GET独自接受。

每次原confirmed及排序框关闭后，仅封闭该槽的新增捕获和材料比较；原材料操作必须实际join。继续原下一次open和严格列表顺序断言。封槽不是observer退休，也不作为全槽PASS。保留原page.goto导航及全部刺激，按三个真实document分别管理现有native/public两observer，不创建第三观察层：

1. Milestone文档：两槽及原末次列表 `[peer,selected]` 断言/关闭排序完成后、原 `go(seedMilestone)` 之前，调用既有 `diagnostic.flush()`，取得该文档双observer第一次explicit/pending0/end及原Node采样join；未齐即FAIL，不能等下一文档补尾。
2. Sprint文档：执行原go并ready后显式 `installPublication()`，native由既有initScript在这个新文档安装。只arm固定第3/4槽；两槽及原末次列表断言/关闭排序后、原 `go(seedSprint)` 之前，同样flush本document原首explicit/end并join。
3. Task文档：执行原go并ready后显式重新安装publication，只arm固定第5/6槽。两槽及原末次列表断言/关闭排序后调用最终 `diagnostic.finish()`，取得第三文档双observer原首explicit/end并join。后续原阻塞列表和冲突流程沿原finished门，不为其添加新例外。

Node持续保存同一固定六槽序列及三个原文档记录；新document不能清空既有槽、重置全局顺序或复用旧槽成功标记。每槽额外绑定原document_id与该文档的native/public call坐标，防止新文档重置的call_id/sequence冒充前文档。实际key、Request、XID跨六槽保持唯一；receipt对象身份在各自原文档/原Promise settle时核验，不能从最后文档结果或后继GET补早先对象。首文档缺失或任一文档非explicit/非0pending，整个六槽联合都拒绝；后文档完整不能替前文档。

最后对六个独立Request逐一要求：同原XID、method/path/status、同体schema/实际decoder、所属原文档唯一native/public call、真实EOF/有效CL、原reader/cancel/release/outer尾、实际Session finally和当次receipt发布，以及该原文档两observer首explicit/pending0/end、Node对所有原文档实际join、各文档退出时not_busy。最终数量必须恰六且分属原三个文档各两槽，不能以其中一个槽或文档替另一个。后继合法owner暂时busy不否定此前已完成原调用，但原文档退出及最终零pending/不busy仍为硬门。

create/update/普通列表/detail/冲突刺激维持原finished门；只改变原六个排序请求的明确消费证明，不增加GET、Lookup、replay、IPC、fixture分支或业务写。失败/finally/page/context关闭或超期即封闭全部新准入；verify入口先seal所有header/比较准入，再join截至该边界原操作，解码后再次核pending0/errors0，防止Promise.all快照漏后加任务。任何late settle不能升级已失败/提前退休的槽或整体；timeout不是joined，负控最终受控settle并实际join。

## 范围及验证边界

拟改仍是作者树现四TS `project-work-planning.{helpers,native,publication,spec}.ts` 与既有两CJS `expected-incomplete-controls.cjs`、`ordinary-consumer-owner-controls.cjs`，不新增通用observer、监督器或产品/public Session接口。产品、Go、fixture、driver/supervisor、binary、dist、21项正式HTTP能力均不变。原45s/expect5s/Go120s与6m/root540+60/TCP75、七exact资源/私有目录/desc/所有actualWait和TCP双尾保持。

新增最小方法控直接运行实际helper/observer和真实Session/API：六槽正常与failed完整原消费；每kind精确请求形状、before与缺省、Sprint parent、Task receipt结构与已知parent；错误首调用/首请求、错序/重复/漏槽、跨槽旧Promise/key/receipt/XID；共享terminal及finished一次/failed零；held reader/outer/header/比较、提前退休非0/关闭/超期/晚settle/verify中新准入拒绝。加入三个原文档各两槽的flush/重新安装/最终join正控，以及漏首文档end、非explicit退休、新文档重复call_id冒前文档、未重装public的拒绝控。正常其它写和无policy路径不得进入新增集合。未settle负控事后必须受控settle并实际join，无后台未处理Promise。

当前只做计划审，不运行或重复旧控制。后续实现获root授权后执行必要新增控、format/strictTS/exact单Planning发现；未变R13 ledger/decoder/Recovery AST、Project方法及已接受控制按差异复用。实际diff须再由未参与者窄审、root保存，再另授一次fresh原完整case。任何方法控、局部接缝、旧PASS均不替代Planning完整业务及原Go后验；禁止自动重试。

计划审历史：cleanup发现首稿遗漏原go/page.goto文档更换，原“同一对observer到第六槽才首explicit”的接线不可行。按root仅计划返修授权，现明确每真实文档两槽、原末列表后/goto前flush，新文档ready后重装public，Node统一六槽并分别join三个原文档。首稿描述错误保留此记录；未因此改技术或执行新控制/资源，待cleanup仅就返修计划窄复审。
