# D12 Knowledge 文档移动 UI

历史候选基线为 main `1d41132a`，保存于 `0646b6bf`。产品与必要基础控已获独审接受，真实默认 App 两个业务场景及三张截图已有限接受；native04及唯一后继native05均因原宿主TCP差额尾不为空而wholeFAIL。第五次复验额度已用尽并停止重试，整片未完成、未合入主线。

## 范围与既有合同

消费 [树命令 HTTP](d12-knowledge-owner-tree-http.md) 与 [Knowledge 布局](../../frontend-design/layouts/knowledge-base.md)，提供当前 Owner 在既有 initialized active Project 中移动文档到根层或同项目 active 父文档。默认 App 已将真实 Knowledge Service 接到树命令 handler；不新增后端、删除、Source/Jina、Object 或生产初始化能力。

`POST /api/v1/projects/{project_id}/knowledge/documents/{document_id}/move` 必须完整提交 `expected_parent_id` 和 `target_parent_id`，值为规范 ID 或显式 null（根层），不提交 expected_version。200 为 `{document,changed}`。Move 本身不改变内容版本；并发正文/改名产生的实际版本不能被旧 UI 观察误拒。

原 Lookup 为 `POST .../knowledge/documents/commands/lookup`，body `{command:"move",document_id,request:原完整对象}`，同原 Idempotency-Key；服务重算 MoveDigest。历史回执仅证明原命令，不能当作当前位置或当前授权。

## 页面与状态

- 父文档选择器独立读取目录，不更改正在查看的文档；显式根层，核当前目标及完整祖先，拒自身、后代和跨 Project。后端仍重新校验。
- 打开/明确采纳时绑定原 parent。冲突保留目标选择，先重新读取，再由用户明确采用当前位置，不静默替换 expected parent。
- 完整响应与 DTO 校验完成才发布。提交未知保留原 project/document/body/key，查询和显式原样重放；不自动重试、不自动生成新 key。归档保历史查证入口，禁止新移动/重放。
- 确认移动后独立当前 GET/祖先/正文读取，刷新来源和目标父目录；历史回执不直接覆盖当前观察。
- 沿唯一 Session transport/owner、实际取消收尾、身份/Project/读取代际守卫和 rename/text 未保存确认。checking 不将原未知意图误当新身份清除。

## 实施与验收

专属 API、controller、dialog、client/Session、ProjectKnowledgeView 和原读取 owner 刷新已接通，App/router 未改。针对性协议、Session取消收尾、页面未保存确认、归档与当前403/404拒绝清理控制通过；原失败及修后证据见 [当前记录](../../../.agent-state/current.md)。作者实施与自测不代独立验收。

复用已交 text create 与 title-search 的默认 App/真实读取 fixture、原树命令 PG 权限/防环/重放证据。native04 的 `TestKnowledgeMoveWeb/{move-normal,move-lost}` 实际业务均PASS：normal两次Move完成root→parent→root，23请求全finished；lost一次Move在原业务提交后真实截断，保持失败终态，再以原完整意图及同key Lookup确认，独立读取当前位置/祖先/正文/目录，17请求16finished、未重发Move。独审逐条核40份同RID原native/public consumer/Go联证和三张浅深色、桌面/窄屏图片，有限接受已到。原件位于 `output/ai/knowledge-move-ui/native04/`。

原outer21715实际退出1；自有资源/实际Wait/私有目录与输入尾齐，但原75s `tcp()-baseline` 门残1条ESTABLISHED，wholeFAIL不能改写为通过。容量线程已join，159个1s样本无错误，仅证明采样最小可用空间约2.58GB，不表示连续峰值。01目录失败、02明确ENOSPC和03定位/原TCP失败均保留；后继TCP归属/运行环境条件由root协调，不盲重跑、不豁免原门。真实浏览器未扩大至撤权竞争、分页、防环矩阵或所有未知结果，生产初始化及Source/Jina等既有STOP不变。

在另一真实链Secrets02按原门wholePASS并释放资源后，root只批准一次固定输入复验。native05沿原2456输入、74b4 binary、dist-01及两sub，技术与04无差额，未重编或改门。首次准备工具10730因低于5GiB实际失败并保留；root保全原件、清理已交冗余checkout后，原40896补核实际0，启动前可用6,830,522,368B。原件在 `output/ai/knowledge-move-ui/native05/`。

05两业务sub实际PASS/75.37s并获有限独验：normal23请求仅22finished，1个children GET ERR_ABORTED保原终态且完整消费联证，完成两次Move；lost17请求16finished，保原真实cut、同key完整意图Lookup与独立当前读取，未重发Move。独审亲核40份联合证明与3PNG。原两Node、Go、driver、8个Chrome实际Wait均0，七资源14次absence和private/runtime/desc双尾齐；但原75s TCP差额尾仍残4行（2 TIME_WAIT、2 ESTABLISHED），因此原outer41218实际退出1/189.800s、supervisor404792实际wait1，inputs_unchanged=True，整轮wholeFAIL。容量线程实际join、187样本0错误，最小3,164,753,920B仅为采样最小，不能称连续峰值。完整原终态后已释放窗口；此前全部FAIL保持，不以业务或自有资源通过代替整轮验收，不继续循环重跑。

## 当前主线兼容恢复

经主线程明确授权，在最新主线基础的新工作树恢复上述保存实现，旧保护树与 01–05 失败原件不改。移入尚未交付的 Move API、Session 原意图、父选择/dialog、当前读刷新与真实浏览器场景；主线已包含的 rename 定位修复直接复用。

与已交 Knowledge Delete、Rename、Text 和 ArtifactCreation 合并：页面操作及路由保留各自未保存确认、当前读取与 dispose；Session 保留双方的未决原意图互斥，Move 发布进度后再次核当前 Session，拒绝同步身份退休后的迟到返回。Go setup 沿相邻 Title/Delete fixture 使用真实默认 Project/Skill/Object 创建，去除过时的默认未绑定断言及测试专用 initializer；不修改 Move/Unknown/账本/原退出业务断言。

本次仅准备实现和有限验证。新运行器使用精确 top/两个 sub、两个不同 Node PID 的原 Wait，以及自有容器、网络、私有目录、运行目录和后代进程退出硬门；没有将历史全宿主 TCP 差额失败改为通过。运行范围和判据须独审接受，由主线程另行明确放行新集成窗口；本段不声称已有新的浏览器通过结果。
