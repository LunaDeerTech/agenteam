# recovery08 Project refresh 诊断与限定补齐

以下原诊断事实保持；root随后授权单Project根GET的限定完成接缝，现已实施并完成作者离线控制、待Runner独审。产品、Go、dist、预算和资源监督均未改；08原FAIL与环境中断尾缺口保持。原证据在`recovery-eighth-failure.json`及ignored `recovery-08-evidence/TestAccountProjectWorkPlanningWebOriginalRecovery/`。

原日志把第一阻塞点定位到spec707调用`refreshProject`，内部spec64已核原响应200后等待`Response.finished()`；其`response.json()`未执行，后继归档DOM/Lookup/原意图重放和最终Work联合验证未到。当前Work native/PW闭集只观测Work路径，publication只绑定Work facade；该Project根GET不在闭集，故不能使用其它Work行的EOF/CL/typed结果为它作证。末文档end未见也不能靠最后pending快照升级。

恢复后确有69份上游response sidecar；其中`response-066.json`指向一份Project GET200、原body SHA匹配、lifecycle=archived。它只证上游原体存在，不证明它与spec64卡住的PW Request唯一绑定，更不证明浏览器EOF、PW failed原因、生产typed/publication或实际owner尾。没有把该上游body直接拿来替代原浏览器消费。

实际生产链：`ProjectWorkPlanningView`点击→`useProjectWorkPlanning.refreshProject`→`workspace.readCurrent`→`auth.projects.get`→`projectRead`→`runAuthorized(identity, work, undefined, 'project-read')`→真实Project Owner API→`accountTransport`。Project API独立校id/owner/lifecycle/version等严格字段，正常Promise经原reader cancel/release、outer body cancel和Session actual.finally释放owner后才fulfill；password命令早resolve不可达，timer/abandon/leave只能拒绝。Workspace另有自己的generation/readGeneration/current-identity校验、accept和canonicalize尾，因此Session fulfillment本身不等于Workspace已发布。

本人`node .agent-state/work-owner-planning-ui/project-refresh-consumer-controls.cjs`实际94231/219968 exit0：9项actual-source控制、0unhandled，Vite仅内存构建真实Session+ProjectAPI/transport、jsdom与原Node Response/ReadableStream，未服务器/PG/browser。正常archived结果严格解析且fulfillment时busy=false；reader/outer取消Promise各被持住时仍pending/busy，另一次facade调用拒busy且原GET计数1；abandon、原30s timer、identity leave可早拒但尾未返仍busy，放尾后不能变成功；错owner/target与坏JSON仍拒。此为作者纯桥接证据，不是08消费阳性，也未证明Workspace发布或新的完成方法可直接采用。

实施只补 recovery 原三次 `GET /api/v1/projects/{id}`。复用现有 native/public 两观察器，原 Session get 和实际 Vue 根提供的唯一 Workspace.readCurrent 均返回原 Promise；正常 Session fulfill 在 actual.finally 之后，Workspace 另须原 Promise 完成、同 identity/Project/generation、本次 readGeneration+1、accept 的严格 typed archived结果与 canonicalize 后 URL。已加载私有 dist 的 Symbol marker经AST核唯一；使用原Vue根provide的公开port，不创建另一Workspace，也不要求生产代码暴露私有owner。原source没有导出该Symbol，因此没有靠猜minified export接线。

原 Request 恰一次正常 requestfinished 仍调用原finished并要求null；恰一次实际ERR_ABORTED且无finished才不调用已知可能无必达的finished。两分支最终都要求唯一XID/document/native/public绑定、真实EOF/identity CL等长、无早cancel/abort、原reader/outer尾已返、两observer期限内explicit首次退休及pending0/end/hooks恢复、sampler joined。原事件闭环在flush/finish/close/原45s期限结束；晚pageclose不造完成。仅记录待核验候选，三域最后真正finish后才联合原sidecar SHA/正式Project schema/实际typedAPI字节与owner/version、恰三不同Project，随后才能进入原complete/Work verify/Go后验。

冻结技术范围：`tests/account-captcha-web/e2e/project-work-planning.{native,publication,helpers,spec}.ts`、`.agent-state/work-owner-http/root_chain_driver.py`；后者仅将 `web/src/api/project-owner.ts` 与 `api/openapi/project-owner.json` 加到既有输入闭包。四原消费/schema/ledger函数AST逐字03f9228e，root adapter逆删两文件即逐字03f；产品/Go/binary14/私有dist不变，原Work/Blocker分支仍仅自身既定路由，四cut/held及全部预算不扩。

新增可恢复控制：

```sh
node .agent-state/work-owner-planning-ui/project-refresh-completion-controls.cjs
node .agent-state/work-owner-planning-ui/ordinary-consumer-owner-controls.cjs
node .agent-state/work-owner-planning-ui/native-diagnostic-controls.cjs
node web/node_modules/typescript/bin/tsc --strict --target ES2022 --module ESNext --moduleResolution Bundler --skipLibCheck --noEmit --lib ES2022,DOM,DOM.Iterable --typeRoots web/node_modules/@types tests/account-captcha-web/e2e/project-work-planning.spec.ts
```

cwd本树；使用既有锁定Node依赖与Python jsonschema/referencing，不下载、不启动socket/browser/PG。新增55控46030/fa6c07实际0/0unhandled：实际生产Vue mount/Session/API/Workspace、held reader/outer/canonicalize、错误/失效generation/identity/timer/abandon、原Promise保留；真实Node事件适配代码（PW/page为明确替身）的失败不调finished、正常返回实际join、关闭/期限/重复/错Request/晚事件拒绝；原sidecar通过正式schema/实际API，错owner/target/extra/hash/source/重复XID拒绝；三份最终联合证明以及错字节/owner/version/重复Project拒绝。locked PW1.56.1实际transform序列化与原dist marker核验在同控制中。先前27/47/49控均实际通过，最终55覆盖其新增范围，没有把它们当多组独立结果。

旧消费者116控13067/389233及native41控99922/ab92ab实际0/0unhandled（因共享观察接线有变而定向回归）；strictTS82068/0b4c55实际0；原四函数AST/root两输入逆投影/Python语法/diffcheck d2c455实际0。原9控94231/219968未改范围复用，不重复跑。普通log均在ignored implementation目录。上述只有作者离线证明，不是实际PW网络或PG/SQL结果；独审前不称新输入accepted，实际轮须root fresh grant，08原FAIL及环境尾缺口不回填。
