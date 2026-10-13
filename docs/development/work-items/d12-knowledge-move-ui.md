# D12 Knowledge 文档移动 UI

当前恢复候选 `73f7fa81` 已通过有限前端检查及真实默认 App 的 Move 正常、丢回执恢复两场景，按 `knowledge-move-owned-v1` 核实本次自有资源关闭；三张真实截图与动态结果已获非作者有限独审接受，随本次变更交付 main。历史 `0646b6bf` 的 native04/05 因全宿主 TCP 差额尾未空而 wholeFAIL，旧失败和未知归属保持，见下方历史记录。

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

## 原保存候选的实施与验收

专属 API、controller、dialog、client/Session、ProjectKnowledgeView 和原读取 owner 刷新已接通，App/router 未改。针对性协议、Session取消收尾、页面未保存确认、归档与当前403/404拒绝清理控制通过；原失败及修后证据见 [当前记录](../../../.agent-state/current.md)。作者实施与自测不代独立验收。

复用已交 text create 与 title-search 的默认 App/真实读取 fixture、原树命令 PG 权限/防环/重放证据。native04 的 `TestKnowledgeMoveWeb/{move-normal,move-lost}` 实际业务均PASS：normal两次Move完成root→parent→root，23请求全finished；lost一次Move在原业务提交后真实截断，保持失败终态，再以原完整意图及同key Lookup确认，独立读取当前位置/祖先/正文/目录，17请求16finished、未重发Move。独审逐条核40份同RID原native/public consumer/Go联证和三张浅深色、桌面/窄屏图片，有限接受已到。原件位于 `output/ai/knowledge-move-ui/native04/`。

原outer21715实际退出1；自有资源/实际Wait/私有目录与输入尾齐，但原75s `tcp()-baseline` 门残1条ESTABLISHED，wholeFAIL不能改写为通过。容量线程已join，159个1s样本无错误，仅证明采样最小可用空间约2.58GB，不表示连续峰值。01目录失败、02明确ENOSPC和03定位/原TCP失败均保留；后继TCP归属/运行环境条件由root协调，不盲重跑、不豁免原门。真实浏览器未扩大至撤权竞争、分页、防环矩阵或所有未知结果，生产初始化及Source/Jina等既有STOP不变。

在另一真实链Secrets02按原门wholePASS并释放资源后，root只批准一次固定输入复验。native05沿原2456输入、74b4 binary、dist-01及两sub，技术与04无差额，未重编或改门。首次准备工具10730因低于5GiB实际失败并保留；root保全原件、清理已交冗余checkout后，原40896补核实际0，启动前可用6,830,522,368B。原件在 `output/ai/knowledge-move-ui/native05/`。

05两业务sub实际PASS/75.37s并获有限独验：normal23请求仅22finished，1个children GET ERR_ABORTED保原终态且完整消费联证，完成两次Move；lost17请求16finished，保原真实cut、同key完整意图Lookup与独立当前读取，未重发Move。独审亲核40份联合证明与3PNG。原两Node、Go、driver、8个Chrome实际Wait均0，七资源14次absence和private/runtime/desc双尾齐；但原75s TCP差额尾仍残4行（2 TIME_WAIT、2 ESTABLISHED），因此原outer41218实际退出1/189.800s、supervisor404792实际wait1，inputs_unchanged=True，整轮wholeFAIL。容量线程实际join、187样本0错误，最小3,164,753,920B仅为采样最小，不能称连续峰值。完整原终态后已释放窗口；此前全部FAIL保持，不以业务或自有资源通过代替整轮验收，不继续循环重跑。

## 当前主线兼容恢复

经主线程明确授权，在最新主线基础的新工作树恢复上述保存实现，旧保护树与 01–05 失败原件不改。移入尚未交付的 Move API、Session 原意图、父选择/dialog、当前读刷新与真实浏览器场景；主线已包含的 rename 定位修复直接复用。

与已交 Knowledge Delete、Rename、Text 和 ArtifactCreation 合并：页面操作及路由保留各自未保存确认、当前读取与 dispose；Session 保留双方的未决原意图互斥，Move 发布进度后再次核当前 Session，拒绝同步身份退休后的迟到返回。Go setup 沿相邻 Title/Delete fixture 使用真实默认 Project/Skill/Object 创建，去除过时的默认未绑定断言及测试专用 initializer；不修改 Move/Unknown/账本/原退出业务断言。

适配及判据经非作者有限源码审查接受后，由主线程明确放行新受控窗口。运行器使用精确 top/两个 sub、两个不同 Node PID 的原 Wait，以及自有容器、网络、私有目录、运行目录和后代进程退出硬门，保留 120 秒/sub、45 秒浏览器与零重试。失败观察仅在原 consumer 退休前保存有界闭集 DOM 与遮蔽输入的截图；不改变成功行为、业务断言或原失败。

恢复候选的[首轮轻验](../../../output/ai/knowledge-move-recovery/light-summary.json)为 8 spec/57 项：Move 37、Delete 12、Rename 3、ArtifactCreation 5，完整类型和生产构建通过。继承用户 `708a8f54` 的安全 UUID/开发登录及 `02e51334` 的公开认证入口后，最终候选 `73f7fa81` 的[受影响补验](../../../output/ai/knowledge-move-recovery/final-merge-supplement.json)为认证四 spec 加 Move Session 共 134 项，完整类型与构建通过；两组范围分别保留，不将重叠用例机械相加。新 Node helper/spec 零诊断、两模式 Playwright list 各 1 项；共享导入已有 5 项类型诊断保留，不称整套 harness 类型通过。App race 编译及精确 list 实际通过。

恢复后的 [web-native01](../../../output/ai/knowledge-move-recovery/web-native01/result.json) 两 sub 在 setup 的 `original concrete domains missing` 失败，Node 尚未启动。原因是 fixture 沿用旧 `*knowledgeWork` 装配断言；仅按当前真实 `*knowledgeCallAssembly` 及其非空 pair 修正两行，保留原失败和业务断言，重新编译后执行新的受控窗口。

[web-native02 原退出记录](../../../output/ai/knowledge-move-recovery/web-native02/post.json)为工具 `24882/fa2b9b` 实际退出 0、60.676 秒，1 top/2 sub 全部通过，两个不同 Node PID 均实际 Wait。normal 执行 2 次 Move/0 Lookup 完成根→父→根，23 请求中 22 finished、1 ERR_ABORTED；lost 执行 1 次 Move，在真实提交后的响应物理截断后仅以原完整双 parent 意图及同 key Lookup 1 次，17 请求中 15 finished、1 ERR_ABORTED、1 ERR_CONTENT_LENGTH_MISMATCH，无重发 Move。原失败网络终态与完整 native/public consumer/Go 同请求证明并存，不改记为 finished。

本轮 fixture、Go、Node 与已知子进程实际退出，3 容器/3 网络及私有目录、runtime 均已清退，受测输入未变，`whole_pass` 与 `owned_cleanup_closed` 为 true。这个结果只覆盖本轮业务和自有资源，没有补证旧全宿主 TCP 差额的归属。三张实际截图经独审亲看：1440 浅色桌面确认目标父文档且目录已嵌套，390 深色 normal 确认回到根层，390 深色 lost 显示目标父与“移动已确认；已读取当前文档与位置”；对话框和主操作可读，未见横向溢出。两 observer 均 retired/pending=0。动态结果与上述三图获有限接受，未扩大到分页、防环矩阵、撤权竞争、全部 Unknown 情形、Source/Jina 或完整 D12。
