# Resolve 拒绝完成观察方法草案

状态：方法 SPEC 已获 Runner7519／87336／59271／51dfc2 限定独审接受；首版实现及后继 PW 异常修正待实现独审与真实验证。root 已授权只对下述六 tuple 实施，原预算不变。新main49546仍FAIL，本方法不能补写该轮的instance、owner finally或end事实。诊断契约修复的Runner69969／6f595c有限接受不等于本实现接受。

## 限定对象

只讨论当前 `authority-and-identity.ts` 六个预声明 `denied` 调用。实现须把声明与实际目标、当前已核Session/User/role、原一次导航及响应Request逐项绑定；不能以URL后缀或任意4xx自动登记。

| 当前场景 | 目标 | 身份依据 | 候选的拒绝语义 |
| --- | --- | --- | --- |
| lifecycle-deleting／pending | 原辅助Project | ownerSession | 409 / PROJECT_NOT_ACTIVE |
| lifecycle-other-owner／admin-owned | 原另一Owner Project | ownerSession | 404 / NOT_FOUND |
| flow217 | main | otherSession | 404 / NOT_FOUND |
| flow224 | main | adminSession | 404 / NOT_FOUND |

元组来自正式Project Authority的当前Owner隐藏与lifecycle Read gate（`authority.go`、`contract/lifecycle.go`）。旧foreign分支允许的403仍只能走原正常finished路径；本候选不扩403。17种Model操作、Session restore、普通Resolve成功、其他Project读取、Work方法均不在范围。

## 产品完成链与现有证据

真实调用为 `auth.projects.resolve → projectRead → runAuthorized`。`useSession.ts` 的正常拒绝先由actual catch归类，再经finally清timer/abandon/owner并写busy=false，最后 `actual.then(resolveVisible,rejectVisible)` 才拒绝原public Promise。另一条expiry／abandon可直接rejectVisible(cancelled)，早于actual尾；不能把任意拒绝当owner释放。

`resolve-rejection-owner-controls.cjs` 已在51209 actual0用实际Session、client、Project API和Vue响应状态跑4格。transport仅将原响应outer body.cancel的返回Promise明确hold；生产函数／owner closure未替换。同步公共busy watcher与原Promise观察记录：404/409均为 busy → cancel-tail-release → released → rejected；abandon与受控30s expiry均为 busy → rejected(cancelled) → cancel-tail-release → released。四格都有完整native EOF，后两格证明EOF及可见拒绝不能替代actual finally。0 unhandled、无浏览器/socket；这是离线受控时序，尚非真实页面的finally证明。

真实49546已有同原Request的409／262B=CL、实际typed拒绝和loading→error DOM采样；缺拒绝瞬间的busy／identity时序，instance旧判据错，最终slot/end与hooks退役未得。因此还没有任何一格满足下述候选。

## 候选要求（全部同时满足）

1. 保留原 response.finished Promise及PW事件观察。候选只能在上述预声明、同一次动作的唯一GET `/api/v1/projects/resolve`上评估：精确origin/query/目标，动作前0、动作后1、一个目标Request、同PW Response/Request、同公开X-Request-ID、native请求计数1。正常finished路径继续原样工作。
2. 原native reader取得严格中断前EOF，identity编码、合法唯一Content-Length、实际bytes严格相等；所有read与reader/stream cancel均settled且无reject，release成功，无signal abort/起始abort、无EOF前cancel/abort。不得clone、增加body reader、构造第二个fetch Promise链或补HTTP。
3. 唯一已加载公共Session singleton的原resolve函数／this／args／原Promise不变。实际公开导出AccountFailure、kind=problem、精确status/code元组、header/Problem XID、正式安全instance `/api/v1`；合法Problem当前的typed/schema约束须在实际输入上闭合，不能仅据构造器instanceof。拒绝观察时与final两次都须匹配预声明User/Session/role/epoch、authenticated且busy=false。取消、expiry、abandon、改身份或旧Promise不得进入候选。
4. 提前安装公共DOM MutationObserver：同目标URL下新的loading发生在该原resolve调用后、原typed rejection前；随后出现精确read-error／unavailable heading，真实layout可见，Providers列表与dialog为0，未离开目标或变化身份。旧heading、已有loading、同值后来重放与jsdom的无layout结果不能冒真实发布。原055–059全部保留并在候选判定后实际执行，尤其两个原Model请求／operation零增量断言。
5. 必须在页面仍活、原45s用例预算内，并在同原headers后最多5s完成最终sample→sampler实际join→native slot end→公共wrapper/MutationObserver/timer恢复→监听退役→final唯一性与计数采样。时间不累加、不等observer45s expiry或pageclose；任一步过时/不稳/缺失即不能接受。现有finally中的迟到end不足，具体接线与late-counter控制必须另审。
6. body消费／typed拒绝／公共发布／Session owner完成要分别记录；PW aborted或原finished仍pending只记原事实，绝不改名finished。候选继续时，原PW观察Promise须被生命周期登记并在实际page/context关闭后join，根进程/Node/七资源/handler/私有目录/desc/TCP/input原尾仍全部必需。

## 实现与验证边界

- 独审上述实际Session拒绝正控与held-cancel＋expiry/abandon负控，及新typed status/code和真实schema输入；不能复用Work fulfilled或restore消费的结论。
- 拒绝瞬间公开busy/identity/role与最终采样之间的精确记录；当前后采样不能升级为此事实。
- 主流程保持原budget，增加与原finished同时观察的有界candidate并证明原Promise/lifecycle join，而非在pageclose后补成功。首版接线已落源码，尚未获得实现独审或实际新组合接受。
- 真实end、hooks退役后晚请求／晚counter、Promise迟settle、身份/导航变化、headers后超5s、slot expiry、错误body/length/media/code/instance/XID、重复请求及旧DOM全部必须拒绝。现有adapter替身与jsdom不能单独证明真实layout或网络终态。
- 完成限定独审后仍需root独占fresh grant及实际新输入窗口；不能只为诊断literal修复盲重跑原普通finished失败。

若上面任何actual owner/publication/end条件无法以同原请求观测，保原FAIL与finished门槛，本草案不授权降级。

首版实际接线在 `authority-and-identity.ts`、`resolve-publication-observer.ts`、`native-client-probe.ts` 与 `resolve-rejection-contract.ts`；只在实际headers事件后设置原5s截止。候选ready后仍执行原055–059，再结束和恢复slot/hooks/监听，最后读取保留的safe counters与当前公共状态，全部条件通过才继续。原PW Promise原样登记；Model spec仅authority传可选beforePublish，在原verify/check/count/dispose后close+join，成功后才原子发布completed。提前PW拒绝或returnedError即使已join也不得发布；已排队拒绝先经同一微任务轮观察，不能借close标记。

离线恢复后实际终态：82172→038b06的生产boundary→正式schema→实际client/controller/workspace/View/PW observer为20+7+19项、0unhandled；78258原adapter16+publication6、82441严格TS均actual0。共享Session受影响回归62629→234461为25+113项actual0。自读PW早异常两红168f53后，82862→5d9840新adapter19（新增提前拒绝／返回Error／当轮已排队拒绝）+publication6实际0。9bb7ed按原native build配置write:false实际重建并与已有privatebundle逐字相等，没有换account/helpers/dist。旧35073终态在环境恢复后不可取得，仍保缺口，不由后继结果回填。jsdom/layout/transport-tail、PW target close与adapter均有明确替身，不能冒真实页面／网络终态。
