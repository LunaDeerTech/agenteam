# Knowledge 首读失败的有界诊断计划

本文件只提出方法补充，未实现、未运行第二次真实链。原 `read-author-01` 整体 FAIL 及全部实际退出事实保留在 [read-first-failure.json](read-first-failure.json)，随 `10ae1b81` 保存。生产源、四 GET、fixture、selector、成功判据和预算保持原样。

## 已知事实与未知首因

冻结源码为 `bd970b97` 的四个技术文件。原安全记录是 `stage=actual-consumption`、`page_closed=true`、`observer_report=false`；12份 GET200 完整反代元数据及两布局截图存在，不能替代客户端消费、发布及首轮显式退役证明。原 Node/Go/driver/outer 均失败，资源和输入尾完整结束。

原代码坐标：

- `tests/account-captcha-web/e2e/knowledge-owner-read.spec.ts:179` 的一个 stage 包含按钮 enabled、`observer.idle()`、`observer.finish()` 三个 await（180、183、184）。仅凭该 stage 无法区分最初未完成的门。
- 同文件286–303的 catch 先再次 await `finish()`，随后才写 first-failure。原首错的页面状态与 pending 因此可能被后续关闭覆盖；现有 `page_closed=true` 不能证明页面先关闭导致原失败。
- `knowledge-owner-read.native.ts:748` 的 Node 原响应尾顺序是一次 `headerValue("x-request-id")` 后一次 `Response.finished()`；793–803的 `finish` 先 join 这些尾，再在同一 page.evaluate 内同步调用两个已有 observer 的 finish/seal，然后分别 await。
- `.agent-state/knowledge-owner-ui/native-controls.cjs` 已有67项覆盖真实客户端/Session与 browser-side 两观察器，但 PW 行为半边为受控行；它没有执行上述完整 Node `observeKnowledge` 编排。

因此，目前只能确认诊断覆盖缺口。原45s内到底是按钮、idle、某个 PW header/finished 尾、page.evaluate，还是其后断言先失败，仍未知。不把短正文、浏览器事件差异或产品行为当成已证实根因。

## 拟补充的记录顺序

后继若获实施授权，只修改原 spec、native 和 native-controls 三个技术路径；本轮只写此计划。Go fixture/config、页面生产源、shared driver/supervisor和依赖锁均不改。

1. 在原 await 前同步更新固定 phase 枚举，并在该 await 正常返回后标记已完成。三个外层阶段分别为 `completion-button`、`observer-idle`、`observer-finish`；finish 内再记录 `pw-tails`、`page-observers`、`report-write`。其后联合证明与 Schema 使用独立固定阶段。阶段只定位原控制流，不新增成功分支。
2. catch 入口在任何 await 前同步保存不可变 `first_failure`：原 phase/已完成门、input hash、Node 单调序号、当时已知 page/context closed 标志、原 PW 请求安全状态和两个已有 observer 的最后安全采样。明确每份采样的取得序号与阶段；不存在或已陈旧就记 `not_observed`，不能将最后一次零 pending 当作当前值。随后才执行原 finish/cleanup。
3. 原 PW listener 和现有响应尾就地记录同一个 Request 对象的事件顺序。每行只含既有 sequence、闭集 GET path/query、XID、status、requestfinished/requestfailed、header await 已进入/实际返回/拒绝、finished await 已进入/实际返回/null或error、尾是否实际退出。header和finished仍各调用一次，原 receiver、Promise、顺序与异常路径不改；不读取 request headers、Cookie、CSRF、正文或 Error.message。
4. 使用已有 `idle()` 的原 page.evaluate 对两个已有 observer 的 snapshot 做安全投影，随原布尔结果一起返回并在 Node 留下最后样本；不额外开启 browser轮询或第三观察器。投影仅允许 retired/reason/pending/first-pending/failed/overflow、行计数、原 reader/read/cancel/release/outer 计数以及原 public fulfilled/rejected/settled/current/not_busy/bound 标志。禁止复制 typed 对象、document文本、身份材料或 snapshot 未列明字段；public pending 不用 native pending 推算。
5. 首快照以后，只向同一诊断对象的 `tail_events` 追加固定枚举、序号和安全计数，不修改首快照及首失败结论。同步 page/context close listener 也只更新原状态并追加。复用已有256上限控制有界事件数；溢出只记录固定 overflow 并拒绝成功，不能删首快照或静默视为完整。诊断写入失败亦保持失败并继续原实际退出，不新增后台写任务。
6. success report仍由原完整 finish 产生。诊断事件、文件存在、截图、零 pending样本或晚到 fulfilled 都不能替代严格联合判据。最终没有原完整 report或有观测错误，整 case仍 FAIL。

本计划不改变 finish 的当前等待与 seal 顺序，也不以提前 seal 掩盖 PW 原尾缺失。如果新增诊断证明该顺序需要修复，再独立提出最小实际差异。catch仍等待原资源尾，但首失败已先保存；不新增竞速超时、替代 promise、GET、clone/tee、body重读或成功豁免。

## 实际 Node 编排的必要离线控制

复用原 `native-controls.cjs`，直接调用生产测试文件导出的 `observeKnowledge`，用受控 Page/Context 事件发射器与 PW Response 实现控制这些原 await。受控 Page.evaluate 可执行两个现有 observer 的真实函数；资产定位沿同冻结 dist 的只读 AST，不改资产、不建第二 Session。PW/浏览器部分仍明确是离线受控输入，不冒真实 browser。

- 正常：同一 Request 的 header返回、requestfinished、finished返回null，双 observer 原尾实际完成；原 finish 结果和严格 joint predicate仍通过，新增诊断不改变返回数据/计数。
- 持有 header：在原响应尾确实进入后保存 phase/pending，finished尚未调用；首失败先落盘。之后释放原 header 再 join，不借迟到完成改写首快照或整轮失败。
- 持有 finished：header已实际返回、finished已进入但未返回，与 requestfinished事件是否到来分别控制；证明“事件发生”不能代替原 finished Promise。释放只补 tail，不升级首失败。
- page/context关闭与 evaluate拒绝：原 Request 坐标与关闭前后顺序保留，首快照不可被catch后的close覆盖；双 observer最后样本缺失或陈旧时保持明确未知。
- 原 browser-side reader/public尾 held、first-pending非零或timer退休：保留已有拒绝门。没有新的 snapshot/诊断字段能够将旧FAIL翻成PASS，也不让 void、错XID、坏长度或后继busy通过。
- 输出安全与有界性：用受控 sentinel 字段证明 headers、Error.message、typed/body不落诊断；事件cap、首快照深拷贝、后续append及0 unhandled均实际断言。所有受控 promise由控制自身释放和join，不留下后台尾。

这些控制只补原未覆盖的 Node编排与记录顺序；不重复166项页面矩阵，不改已验客户端或dist。实施后的最小离线验证预计为受影响方法控、严格TS、限定格式与diff检查；无Go源/资产变化则不重编Go或重建dist。

## 下一步边界

由独立审查先核本计划是否足以区分原三个 await及延迟catch写盘，又保持全部原成功门。计划接受后仍须明确实施范围，再冻结实际差异供审查和保存。新的真实尝试仅能在 root 另授唯一窗口后进行，仍为原1 Go top /0 sub、1 PW case、retry0、PW45s、Go top120s含cleanup、Go6m、root540+60+3、TCP75及原七资源/actual Wait/输入初尾闭包。原首跑 FAIL不会被后继结果改写。
