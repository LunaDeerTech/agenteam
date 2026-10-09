# recovery08 Project refresh 局部诊断

当前仅诊断，普通完成门、产品、Go、dist、预算和资源监督均未改；08原FAIL与环境中断尾缺口保持。原证据在`recovery-eighth-failure.json`及ignored `recovery-08-evidence/TestAccountProjectWorkPlanningWebOriginalRecovery/`。

原日志把第一阻塞点定位到spec707调用`refreshProject`，内部spec64已核原响应200后等待`Response.finished()`；其`response.json()`未执行，后继归档DOM/Lookup/原意图重放和最终Work联合验证未到。当前Work native/PW闭集只观测Work路径，publication只绑定Work facade；该Project根GET不在闭集，故不能使用其它Work行的EOF/CL/typed结果为它作证。末文档end未见也不能靠最后pending快照升级。

恢复后确有69份上游response sidecar；其中`response-066.json`指向一份Project GET200、原body SHA匹配、lifecycle=archived。它只证上游原体存在，不证明它与spec64卡住的PW Request唯一绑定，更不证明浏览器EOF、PW failed原因、生产typed/publication或实际owner尾。没有把该上游body直接拿来替代原浏览器消费。

实际生产链：`ProjectWorkPlanningView`点击→`useProjectWorkPlanning.refreshProject`→`workspace.readCurrent`→`auth.projects.get`→`projectRead`→`runAuthorized(identity, work, undefined, 'project-read')`→真实Project Owner API→`accountTransport`。Project API独立校id/owner/lifecycle/version等严格字段，正常Promise经原reader cancel/release、outer body cancel和Session actual.finally释放owner后才fulfill；password命令早resolve不可达，timer/abandon/leave只能拒绝。Workspace另有自己的generation/readGeneration/current-identity校验、accept和canonicalize尾，因此Session fulfillment本身不等于Workspace已发布。

本人`node .agent-state/work-owner-planning-ui/project-refresh-consumer-controls.cjs`实际94231/219968 exit0：9项actual-source控制、0unhandled，Vite仅内存构建真实Session+ProjectAPI/transport、jsdom与原Node Response/ReadableStream，未服务器/PG/browser。正常archived结果严格解析且fulfillment时busy=false；reader/outer取消Promise各被持住时仍pending/busy，另一次facade调用拒busy且原GET计数1；abandon、原30s timer、identity leave可早拒但尾未返仍busy，放尾后不能变成功；错owner/target与坏JSON仍拒。此为作者纯桥接证据，不是08消费阳性，也未证明Workspace发布或新的完成方法可直接采用。

最小下一候选范围供root决定：仅该测试既有`GET /api/v1/projects/{id}`，先补同原Request/唯一XID/当前文档的原native消费与`auth.projects.get(id)`公开Promise绑定，安全只记id/owner/lifecycle/version及身份/计数；保原Promise/Response/reader，不clone、不多consume、不新增HTTP。若采用等价完成方法，还须独立证明Workspace本次归档发布、首退休原因/到期时pending/实际end与hooks尾、同原体严格schema/client，以及缺XID/错id或owner/重复请求/早拒/取消未返/晚pageclose/旧DOM等拒例。原PW正常finished路径保持，不能先调用已知无必达finished再假称退休；普通候选没完整证据仍FAIL。该范围尚未实施、未授权新实际轮；不泛化其他Project/Session接口，不改共享产品。
