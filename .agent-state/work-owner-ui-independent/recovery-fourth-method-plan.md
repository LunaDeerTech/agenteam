# Recovery04 唯一窄方法计划（仅计划，未实施/未运行）

基线：独立树 `/workspace/agenteam-work-ui-independent`；技术 `e194a772`，Recovery03 原失败三记录已由 root 保存 `5d409e72`。Go 候选仍为原 `account-independent-race.test` / `1784cf4744303fb613ec2dab0d4773797ad8d6c3b867fc9131d3d9c0fc36a0a7`，私有 dist 来自已验复制的 `65409abc`。本文件只保存源码闭包分析和待审方案；root 未授权实施或下一实际轮。作者 Work 正在其独占树实现 planning，审本计划不交其源写权。

## 原03证据与结论限度

- 原45秒预算超时于独立 spec:254 的 `seen.verify(3)`；helper:1003 先等 `Promise.all(tails)`，其后:1004 才输出 `observerErrors` 期望0实3。首 afterEach 在 observer 相对44.047865秒、page未close，原记录中 `observer_rejected_at` 全为null。旧共享方法没有保存这三个 late catch 各自的后继事件，不能倒填为最初三次reject。
- 原安全快照与当前选择源码可确认三条没有声明cut、也不在既有 ordinary 闭集的响应：seq4 blocker Project 的 Tasks列表 GET；seq30 task Project 的 Sprints列表 GET；seq66 structure Project 的历史 Milestone PATCH重放。均是原Request/status200/XID、`requestfailed=aborted`、无requestfinished/response.finished返回；当前 `observe` 对它们走普通 `await r.finished()`，这些Promise仍在原tails中。数字恰为3不是late catch逐条归属的替代证据。
- 两列表原native都有EOF、Content-Length与915/354字节相等、reader/outer cancel与release实际尾、无abort，但 `bound_public_call=false`；public只包detail/Lookup/retry，未包真实list方法。原PATCH有441字节及相同完整native尾、call9绑定实际retryOriginal且正常typed返回，但原策略仅认可not_observed入口，原记录 `replay_from_not_observed=false` / `replay_receipt_published=false`，独立case也未安装历史Milestone入口。
- 原三文档双observer首explicit退出时均pending0，Node sample实际join。三域循环和三Project刷新谓词已返回；complete与Go独立持久后验未到，wholeFAIL保持。以上是方法覆盖缺口，不是产品失败因果，也不能凭原native EOF或后继GET补PASS。不得把作者R13或本轮新GET用于补原03。

## 只新增三个固定入口

只能由 exact `TestIndependentProjectWorkPlanningWebRecovery` 显式启用，下表之外全部沿原判据。三个policy为闭集字面量，不能接受任意callback、route、方法名或查询模板。

| policy | 安装/arm时点 | 唯一原调用和请求 |
| --- | --- | --- |
| `independent-blocker-tree-tasks` | blocker Project 空Explore完成、publication已安装，Sprint row真实可见后，点击该Sprint的展开按钮之前 | 正式 `auth.workPlanning.listTasks(blockerProject,{sprint_id:seedSprint,state:'backlog',assignee_agent_id:null,limit:50})`；同Project `/tasks` 的首GET；无body、无cursor/text/type/priority/额外query或重复querykey |
| `independent-task-tree-sprints` | task Project 空Explore完成、publication已安装，点击该Milestone展开按钮之前 | 正式 `auth.workPlanning.listSprints(taskProject,{milestone_id:seedMilestone,limit:50})`；同Project `/sprints` 的首GET；无body、无cursor/其它query或重复querykey |
| `independent-history-milestone` | structure归档后原第二次“查证原命令”成功、显示原历史版本之后，唯一“按原请求重放”点击之前 | 原已声明lost-milestone-update意图的同Project/target/expected_version/key/CSRF/Origin/rawbody PATCH；必须绑定此前这一原Lookup发布的历史receipt及原retryOriginal Promise |

不是“四次所有tree list”策略：blocker的Sprints列表、task的Tasks列表、三个初始Milestones列表仍要求原finished；不新增GET、重试、replay、IPC或fixture分支。Authority、作者Recovery/Planning、其它Project/域/列表/写操作默认行为均保持。

## 两个列表的真实消费者绑定

1. 基线来自原已加载fixture IDs、原empty Explore与现tree状态，不能从被观察请求反推预期。arm前目标child row不存在；不导航、不造成功页、不调用额外API。严格冻结Project、parent与固定query；默认filters由现源规定为空，任何实际非空过滤或分页请求拒绝该入口。
2. 浏览器端仅在明确policy期间包装对应正式Session list方法。同步保存实参副本、原identity/context与操作身份后调用原函数，包装器直接返回原Promise对象，不能声明async或另造resolved Promise。首错误公共调用仍占位并失效，不能等待更晚正确调用；同一入口第二调用/第二原fetch也使整入口失效。
3. 捕获这一公共调用的唯一原native fetch：GET、同origin、精确path及完整query集合、body不存在；typed实参、native URL/query和同一PW Request三方一致。查询材料仅内存，安全报告只保存固定policy、公开ID和匹配布尔，不能输出Cookie、headers、原query串或业务值。
4. Session原Promise必须真实正常返回严格typed page，不能void、错页、错Project/parent、重复ID或旧结果；保正式API实际decoder。仅已有固定seed child出现在其原typed items且原page有界/无next_cursor时成立；不用另一GET或响应侧car猜公共返回。真实UI从原不存在child到出现同seed row，并继续用这一真实row展开/选择。保同identity/current context和首操作结束后发表这一真实结果的证据，不用单独“列表里有ID”替代Promise归属。
5. 只向现ordinary账本加入已捕获原Request的这两个闭集入口；它们不算declared cut，不增加expectedIncomplete=3。requestfinished正常路径仍必须原Response.finished/null；恰一次aborted且零finished的路径只记待证明消费，最终完整原reader/EOF/CL/cancel/release、真实Session正常返回、UI消费者与两observer首explicit零pending、Node实际join齐后才可通过。无法证明任一分项即FAIL。

## 历史Milestone原重放

1. 使用独立case原lost-milestone-update的原Request对象和不可变意图；在归档后的原Lookup到达时保存这个实际Lookup Request和公共调用，不新增Lookup。arm冻结最新已完成且同意图的原Lookup；错误最新候选占位，不得退回更旧有效条目，等待中出现后继mutation/Lookup使原arm失效。
2. 核真实Structure返回语义：`result.domain==='structure'`、`result.value.state==='committed'`；原Session此时发布的 `progress.receipt.domain==='structure'` 且 `progress.receipt.value===result.value.result`。不能套用Task的`status/receipt`字段。完整历史receipt只暂存内存，不写出内容。
3. 在retryOriginal同步调用之前保存该published receipt对象与历史值、sameidentity/context及confirmed/committed/canReplay。包装原函数返回同一原Promise；原fetch/PW按首mutation锁定，错path/target/key/version/body/CSRF/Origin、第二mutation或额外request立即失效，不择后。
4. PW原original/replay所需actual headers每对象至多读一次，先进入现有tails再启动；不改原provisional事实。原响应XID、raw头完成可任意先后，但只能关联原Request。原native fetch的实际key/CSRF与URL origin及body，在私有closure中对照同PW actual材料；不得生成、补写请求头或公开材料。封闭后不启动新操作，未join不接受，晚结果只能拒绝。
5. retry正常结果必须是该次当前 `progress.receipt` 同一对象，领域/command/Project/target正确，完整历史receipt值与冻结Lookup发布值相等；当前对象已后继修改，不能用当前GET/当前版本替历史receipt。原UI历史版本、当前内容区分、归档前后完整facts恒等和Go唯一原key/operation/History/Outbox/receipt后验全部保留。
6. 仅上述已锁原PATCH可进入待证明消费；同原status/XID、schema和actual API decoder、原native全物理尾、Session实际owner释放与首explicit双observer零pending/实际join必须同时成立。原正常finished/null亦保留。不得泛化至历史Task/Blocker、not_observed、其它PATCH或作者case。

## 操作尾和封闭

- 原observe与需要消费终态的桥共享同Request唯一事件终态Promise，正常finished如有两个调用方则共享唯一原Response.finished操作；不能先启动finished再在failed分支遗弃。所有新raw-header/瞬时材料比较Promise在调用前登记进同一原tails，立即挂rejection sink，不创建第二监督器。
- 每policy首错误占位、单调invalid。case失败/finally/page/context关闭或超期即封闭新准入；首explicit退休保存原pending数，之后即使实际settle也不改成通过。verify入口关闭新header/比较操作、join截至该边界全部实际操作，解码末再核pending0/错误0，禁止Promise.all数组快照漏后加入操作。
- 复用现每Project flush与最终finish；在真实目标操作和后继原UI链完成之前不得为了pending0提前结束。双observer首explicit要求不变，不用timer或page close充实际join。没有新的timeout、等待窗口或失败重试。
- 原45s/PW expect5s/Go120s/Go6m/root540+60/TCP75与七资源/actualWait/input/desc/private/runtime/TCP双尾全保；原三个cut、三Project同体刷新、业务断言、完整Go后验均不减少。

## 精确源码闭包与验证输入

当前拟改范围仅本独立树，下列五技术源须root另授写权，不能从作者树整体复制helper/ledger：

1. `tests/account-captcha-web/e2e/project-work-planning-independent.spec.ts`：只Recovery上述三个arm及其真实UI完成断言；Authority字节不改。
2. `tests/account-captcha-web/e2e/project-work-planning.helpers.ts`：三个closed-policy原Request/终态/tail接线；默认选择及sourceRun闭集保持。
3. `tests/account-captcha-web/e2e/project-work-planning.native.ts`：只有这三个policy的精确query/material投影与原消费合取，旧默认路线保持。
4. `tests/account-captcha-web/e2e/project-work-planning.publication.ts`：两个实际list facade与Structure历史Lookup/retry原Promise的闭集绑定、退出和安全投影。
5. `.agent-state/work-owner-ui-independent/recovery-consumer-controls.cjs`：新增独立方法控；只复用固定依赖/真实Session/API/已有安装fixture，不复制作者断言冒独验。

产品、public Session接口、Go、fixture、driver/supervisor、dist、node锁和原source-binding-controls不改，不重编Go。独立断言是本case的真实刺激/原SQL后验；既有作者R13 PASS仅说明其原输入，本case不借它接受新list或历史Milestone。新的方法控要求直接执行拟修改的真实helper/observer及实际Session/API，不以复刻predicate当oracle。

必要最小控制：两个list正常finished与failed+完整原消费；同Project错parent/query/重复querykey/额外cursor/filter/body、错误首调用/后续正确不得补、void/错typed/旧UI/旧Promise/identity切换拒绝；Structure确切state/result字段、同receipt对象及历史值、错key/CSRF/Origin/rawbody/version/target/重复XID/错误最新Lookup/第二mutation拒绝；原normal与failed共享terminal双消费者、held reader/outer/headers/比较操作、首explicit非0、封闭后晚settle及verify中新准入均拒绝。未resolve负控随后受控settle并实际join，不遗留后台Promise。unknown policy和默认Recovery/Planning/Authority不进入新增集合须有明确负控。

方法源稳定后仅format/strictTS/exact独立Recovery发现及上述必要控制，复用未变sourceRun10控/Go与既有原范围证据；随后交未参与实现者审实际diff。真实Recovery04只有root新fresh窗口可执行，一次原top，不能以控制/list/旧PASS代动态或自动重跑。

审查状态：本计划待Work作者有限只读方法审；作者不得写独立case或把静审当独验。尚无实施、控制结果或新实际运行。
