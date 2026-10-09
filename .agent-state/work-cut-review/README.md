# Work 四条预声明截断方法独立复核

2026-10-09，Skills。只接受 Work 固定 `152eb964` 相对 `ae101b00` 的四条预声明截断观察方法；未修改 Work 产品或 helper。普通 native 诊断当时仍 WIP，不在本结论内。原 recovery05 `63642` 整体 FAIL 不变，不是新真实 UI 或整包验收。

依据 D11 Owner planning UI 卡 §8.2 与先前独立方法核定：固定 Playwright 1.56.1 的 `_onRequestFailed` 不结束 `Response._finishedPromise`，四条故意截断流本来不可能取得完整 EOF。因此仅 `unforwarded-milestone-update`、`lost-milestone-update`、`lost-task-update`、`lost-blocker-add` 不创建无必达的 finished 操作，以同一原 Request 在 page-close 前的真实失败事件结束本地观察。该事件必须联合唯一产品 owner 实际尾后可用的公开 Lookup、Go 原 Write/Flush/Hijack/Close、零转发或真实 completed SQL、精确原历史/唯一事实与资源收尾，不能独自宣告业务通过。普通五条 aborted 的原证据不足不变；held-read 原方法不扩大。

静核固定 helper diff：原预声明、精确材料与 Request identity、cap4、503 固定 headers 保留；新增成功事件反证，failed 后成功也累计错误；page-close 结束本地事件等待但拒绝验收，晚 failed 不能补为成功。普通响应仍唯一原 finished，held-read 仍原 finished/取消路径及晚拒绝门槛。originalBody/schema/decoder、Go 注入/SQL/Lookup/实际 Wait/资源预算未改。

离线 `53260` **actual exit 0，63 控制，0 unhandled**。`controls.cjs` 从固定 Git 版本读取实际 helper 和作者已有 57 控制，追加本实例六个控制：四种截断各在失败尾已完成后注入成功事件，仍必须拒绝、finished 调用为 0；未声明普通失败即使 finished 返回 null 仍拒绝且只调用 1 次；held-read 实际授权取消后，原 finished 晚拒绝仍失败且只调用 1 次。实际 PW 类方法控制同时验证固定 1.56.1 的 failure/finished 语义。所有声明、transport 事件、内存 evidence 和 schema 外部调用是控制输入；不证明真实网络/客户端发布/SQL，不启动 PG、浏览器或 socket。

在 `/workspace/agenteam-skills` 执行：

```sh
node .agent-state/work-cut-review/controls.cjs
```

探针对 Work 树只读，固定源码仅在内存加载，结果写入 Skills 忽略目录 `output/ai/skills/work-cut-review/independent-controls.json`。原作者 `69562` 57 控制、`29474` strict TS 的范围仍保留；此结论不使普通 native WIP 获得接受。待作者冻结 native 诊断接线后，另核其仅观察、不改变普通 finished/消费/发布门槛及实际观察尾。

## 后续 native 整包独审

2026-10-09，对固定 Work `d3322e3e` 相对 `152eb964` 的五个技术路径有限接受，无 mustfix：`tests/account-captcha-web/e2e/project-work-planning.{helpers,spec,native,publication}.ts` 与 `.agent-state/work-owner-planning-ui/native-diagnostic-controls.cjs`。依据作者冻结的 `recovery-native-diagnostic-proposal.md`、D11 卡 §8.2 和上述四截断方法；未修改 Work 源码。此处是诊断实现独审，不是实际浏览器消费／发布／Go／SQL 或整包 PASS，原 recovery05 整体 FAIL 不变。

静核原 fetch、Response、body、reader 与公开 facade 原 Promise；只观察原操作，不额外 read/cancel/clone/tee 或业务 HTTP。native 与原 PW Request 必须由固定 method/path/status 和全局唯一 XID 绑定，public call 还需同 document、当前 identity、单一候选及 Node 侧二次唯一校验。缺失／冲突保持 unbound。初始 auto-read 可能先于 public installer，因此不补造公开调用证据。DOM 仅记录入口状态及 fulfillment 后观察时点，不是发布因果证明。闭集投影、数量／字节上界、私密原值不持久化和单 flight 保持。

`dd8eb0` actual exit 0 的 TypeScript AST 检查确认：`originalBody`、`decodeOriginal`、`schemaProgram` 与方法基线相同；spec 内全部 198 个 `expect`、`ipc`、`complete`、`seen.verify` 调用及顺序相同。普通 finished／同原 body／schema／client 门槛未改，没有 fallback。导航原 `go()` 使用 `page.goto` 后 `ready`，新增 flush 是实际 document 边界观察。

必要探针 `native-controls.cjs` 在读取实际冻结文件前以本地 `git diff --exit-code d3322e3e` 验证 native/publication/作者 controls 三个输入；两个浏览器 installer 仍经作者实际锁定 Playwright 1.56.1 transform 后函数序列化再进 VM，Node sampler 从实际 TS 提取。完整命令：

```sh
# cwd: /workspace/agenteam-skills
node .agent-state/work-cut-review/native-controls.cjs
```

最终 `16351`／`280b89` **actual exit 0，41 控制，0 unhandled**（作者 33 ＋本人 8）。独立增量验证：

- 原 reader.cancel 与 stream.cancel 各保持同一个原 Promise、一次调用、同一 rejection error；取消不变 EOF，原私密错误不持久化。
- 同步 read throw 保留原异常、无虚构 settle/EOF；退休后捕获的 cancel wrapper 仍调用原操作，不能升级已结束快照。
- end evaluate 拒绝可以表示该观察 Promise 已返回，但不成为 end snapshot。固定 PW 实际 `Page.prototype.evaluate` 是 async，内层同步 throw 变成同一错误的 Promise rejection；没有为不存在的同步 PW API 改造入口。
- finish 已置 stopped、实际 end evaluate 仍 held 且原 listeners 尚在时，同原 Request 的 late response/failed/finished 以及新 Request 均不能改 XID、事件或请求集合；最终保持 unbound、CL-EOF false，移除 listeners 后晚事件也不改保存文件。
- 使用真实 Node `ReadableStreamDefaultReader`（read 为继承方法），`Object.preventExtensions` 后经实际 installer 的原 getReader 仍只调用一次、返回同 reader，原 read 方法和两次数据／EOF 返回正常。defineProperty 被 safe 边界拦住，诊断明确 observer_failed、read_calls0、read_donefalse，没有将产品成功变成诊断异常。

`71581` 曾完成前五个增量的 38 控制，`86592` 完成加入 PW／晚事件后的 40 控制，均 actual0；`96237` actual1 是本人新增控制引用注入环境外 `root` 变量导致 ReferenceError，改为固定路径后再验，不是产品反例，原失败保留。作者 66614／33 控制、96324 strict TS、67870 exact list 的已有范围不扩大。

退休证据必须分开解释：`retired` 仅说明观察停更，wrapper 所有权冲突仍由 `observer_failed` 明确报告，不能只读 retired 推出所有 hooks 恢复成功；`pending_observations` 不等于原生产分支已 join。Node `sample_joined` 仅说明该 evaluate 观察分支已返回，`end_snapshot_observed` 与各 document `source:end` 单独记录；缺 end 或 late callback 不升级。flush/finish 先对已有 pending 最多等 250ms，若已返回再对 end evaluate 最多等 250ms，是两个串行上界，不声称整个 finish ≤250ms。原 PW45／expect5／Go120／Go6m／root540+60+3+75 未扩。全过程只离线 JS／实际库方法／静态源，没有 PG、浏览器、socket 或网络；新的真实执行仍需 root fresh grant。

## 普通完成方法的限定判定

2026-10-09，只读审查 Work `5a49197a` 保存的 `recovery-ordinary-completion-proposal.md`，技术仍 `d3322e3e`。有限接受其闭集方法可实施性；尚未实现或接受新 gate，未补运行 owner 动态控制，recovery06／05 原整体 FAIL 不变。正式 D11 Work Owner planning UI 卡 §5（128行）要求唯一实际 Cookie owner 保留到 fetch/body/cancel；§6（156行）要求 fatal UTF-8、完整 EOF、严格解析及两层 cancel/release 实际返回；§8.2（199行）要求同原 method/path/query/identity/status/body 的 schema/client 与 Go/SQL 后验。205/207行当前 ordinary-finished-only 是必须明确修订的方法约束，不是作者已有绕过许可。

实际 `useSession.ts:1563–1609` 的 Work `runAuthorized` 顺序为 await 原 API、current 重验、catch、finally 释放自身 owner、`actual.then(resolveVisible,rejectVisible)`；readWork 3871及checkOriginal 3985均传 command=undefined，password 的提前成功分支不可达，timer／abandon只会提前拒绝。`client.ts:439–478` 保原 reader cancel 的同一 Promise／release，1619再 await outer body cancel；正常 facade fulfillment 因此具备原 actual 尾后的源码桥，单独 busy=false或visible拒绝无此含义。`project_work_planning_web_fixture_test.go:330–386` 先完整读／Close并用同 raw 写sidecar、同 raw重建原转发body，detail GET与Lookup不走截断分支；`decodeOriginal` 原请求投影及schema gate仍必需，不能只用CL等长替代同体证据。固定PW1.56.1 `browserContext.js:174–192` 再核 failed仅发事件、finished事件才resolve原 `_finishedPromise`；Model19正常finished及不同Session race不是本方法先例。

可实施范围只为 recovery 预先确定、HTTP200／无query、已安装原public观察的detail GET与Structure／Task Lookup。必须联合同一原Request/Response与唯一XID/document/method/path/status、真实EOF先于任何取消／拒绝／abort、identity编码合法CL等字节、两层原cancel成功及release、原typed正常fulfillment及当前identity、实际文档end和全部观察尾、原sidecar/schema/client及全部UI/Go/SQL/resource后验。四声明cut、held-read、原originalBody三调用、list/query/mutation/Problem/未知路由保持旧方法。成功finished反证、重复／错身份／仅pageclose／缺public或end均拒绝；未返回分支不能丢给race后冒join。原06首Structure Lookup无public、末document无end，既有六fulfillment尚无该新方法实际控制，不能回填。

发现并已交作者的必要观察补口：当前 native353–365与publication273/395/418的expiry及显式finish共用同一retire，只有retired／source:end不能区分先到期再取快照。新实现须保闭集首次退休原因、退休当时pending与实际期限；即使timer尚未调度但期限已到，也不能由finish记为主动完成。晚finish或晚callback不得将已到期／当时未就绪升级为可接受end；owned hooks还原失败仍必须拒绝。新门槛使用证据前还须真实Session＋实际WorkAPI/transport控制EOF后分别阻塞reader及outer cancel、visible早拒绝／身份改变／晚尾；不能以mock facade正常resolve替代。作者冻结技术后另行独审；本轮没有代码改动、PG、browser、socket或新动态PASS。

## 普通完成方法技术整包限定接受

Work冻结 `5a49197a..0ad6e5b6`，仅四TS/helper-native-publication-spec、三CJS控制及必要正式方法说明。未改 Work 作者树，未参与实现；有限离线接受，无mustfix，不回填 recovery06/05 FAIL，不证明新browser/PG/Go/SQL/七资源实际结果。

本人重新运行作者实际源码98控（44531）与native41控（13536）、helper80控（4497）全部actual0/0unhandled。新增可恢复控制 [ordinary-controls.cjs](ordinary-controls.cjs)，以locked transform的原作者actual Session+Work API+transport fixture注入四个独立风险检查：Milestone/Structure各自reader/outer stream取消尾held时，第二次真实facade调用必须由原Session拒绝busy，fetch计数仍1；原可见Promise仍pending、原owner仍held，只有释放该原尾后原Promise正常typed fulfillment，第二次被拒的调用无native绑定、不能成为完成证据。67453最终102控actual0/0unhandled（原98＋4）；不是102组浏览器或网络用例。

```sh
# cwd /workspace/agenteam-skills；wrapper只向本树忽略output写可重建结果
node .agent-state/work-cut-review/ordinary-controls.cjs owner
node .agent-state/work-cut-review/ordinary-controls.cjs helper
# cwd /workspace/agenteam-work-ui；只读源、无文件输出
node .agent-state/work-owner-planning-ui/native-diagnostic-controls.cjs
```

首8cd8b0把native脚本从Skills cwd启动，因固定相对路径找不到TypeScript而setupFAIL，未执行控制；仅纠正cwd后13536通过，原失败保留。独立6b8451实际TypeScript AST核 `originalBody`、`decodeOriginal`、`schemaProgram`、四截断ledger逐字等于5a49197a；spec里实际5处originalBody调用同原参数字节不变（先前方法描述称“三调用”，本次以实际五处为准），产品web/src/internal/Go sidecar无diff。复用作者strictTS48083与exact list46979，不重复无输入变化的检查。

接受的因果边界：recovery原Request起点选定的无query200详情GET或Structure/TaskLookup，actual failed/closed/finished先判；known failed从未调用不必达finished。正常finished仍实际await原finished为null，其它端点、声明cut、held-read及originalBody保持原路径。备选证据联合唯一Request/XID/document/public调用、EOF早于cancel、identity编码CL实际等长、两cancel成功及release、原typed正常fulfillment及同当前identity、真实Session actual.catch.finally在visible正常成功前释放的源码桥、两观察器explicit首次退休/pending-at-retirement0/未过期限/实际hooks还原、Node实际end/evaluate返回和原body SHA/schema/client。首次expiry或pending不能由晚finish升级；晚counterevent在原decode后再查，缺end或pageclose不接受。显式retired只表示停观察，仍须observer_failedfalse与这些实际尾。

预算仍两个串行最多250ms观察等待，全部在原PW45/expect5/Go120/Go6m/root540+60+3/TCP75内；未增budget、未白名单忽略failed、未第二次HTTP补证、未抓原body或私有owner。生成报告是闭集安全字段，本方法不依据落盘报告反读授权。真实下一轮仍由root统一freshgrant。

## Blocker Lookup 单端点增量限定接受

只审 Work `24ffd0ba..ff12b21a` 五技术路径：helper/native/publication 三TS与expected-incomplete/ordinary-consumer-owner 两CJS。有限离线接受，无mustfix；原recovery07整体FAIL及完整资源尾不回填，未运行新browser/PG/socket/网络。Work作者树全程只读，原整包102/80/41未变范围复用，不重复全矩阵。

新增范围严格为无query、HTTP200、UUIDv7 Project/Task的POST `/api/v1/projects/{project}/tasks/{task}/blocker-commands/lookup`。原Request起点闭集、knownfailed不创建finished、全部native/public/Node end/真实退休联合条件不变；额外要求path Task与public target相等。公开观察仅原`checkOriginal`的blocker三状态，实际Session分支仍`runAuthorized(original.identity, work, undefined, 'work-lookup')`，经原API/transport及按原命令严格receipt解析、owner finally和current-intent复核后才正常fulfill。没有更换facade/owner桥；mutate、错误method/query/path和其它端点保持原finished。

必要探针 [blocker-controls.cjs](blocker-controls.cjs) 只在内存筛选作者新增18个Blocker控制，再增加两项使用实际Session/API/transport的Blocker Resolve控制，生产源码与实际locked PW1.56.1 installer照常加载。ded763 actual0/20控/0unhandled：Resolve原outer cancel held时可见Promise仍pending、原owner busy、第二实际facade无新HTTP；只在放开原尾后正常committed才成为证据。若Resolve返回未resolved的原receipt则严格解析拒绝，即使实际EOF和取消已完成仍不能认完成。另c95b1a actual0/6差异helper控制：新增精确POST正向knownfailed不调用finished，GET/query/extra/非法Task/其它blockers路径均拒且保原finished调用。控制过滤只减少重复旧检查，不替换任何待测方法。

```sh
# cwd /workspace/agenteam-work-ui；输出仅Skills忽略目录或stdout
node /workspace/agenteam-skills/.agent-state/work-cut-review/blocker-controls.cjs owner
node /workspace/agenteam-skills/.agent-state/work-cut-review/blocker-controls.cjs helper
```

068d5f实际AST核originalBody/decodeOriginal/schemaProgram逐字24ffd，五技术源与ff12b21a零diff，产品web/src/internal、go.mod/go.sum和spec无增量。作者strictTS81593、格式34490与native62015/41的未变范围复用；这不是新真实UI/Go/SQL/资源组接受，下一实际轮仍需root fresh grant。
