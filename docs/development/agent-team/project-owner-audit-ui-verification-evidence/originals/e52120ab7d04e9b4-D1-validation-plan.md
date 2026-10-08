D1 受控时序验证计划。PLAN ONLY，尚未执行。

目标是对应 held 请求的回调/body/handler 实际结束，不以最终累计相等替代场景时序。后续 root 另授 scratch overlay，调用 candidate03 原 apiHandler 与原 controlResponse，包裹真实 httputil.ReverseProxy；不修改已接受 helper/生产源码。内存 RoundTripper 返回已知安全 JSON/200/原 request，阻塞 ResponseWriter 与 ErrorHandler 用 channel 确定握手，无 listener/PG/MinIO/browser，无 sleep 猜时序。这只证明私有代理完成合同，不声称正式 API/schema/权限/producer 验收。

所有例子分别覆盖精确 list/detail 路径，失败前登记取消、解除阻塞和实际 goroutine join：

1. 实际 hold 消费后 held=1/joined=0；release 后等下游 Write 进入并保持阻塞，证明 ModifyResponse 已返回。此时 joined=0/server_finished=0、原 arm 的 held!=joined gate 仍关闭。放行 Write 并收到实际 apiHandler 返回后，两计数才变1；再读不重复增加。
2. hold 后 ctx.Cancel；等 ReverseProxy ErrorHandler 进入并保持阻塞，ctx.Done/回调 error 不能让 joined 提前变1。放行并实际 join 后才完成。另让已 release 的 Write 返回错误，外层 recover http.ErrAbortHandler，核原 handler unwind 后完成一次。
3. token 隔离：A held 时 B 普通请求完成不能替 A 增 joined；A 实际完成后才 arm C。C 尚未退出时，A 的重复 release、旧 canceled context、D 普通请求完成都不能完成 C。关卡 held/joined 必须为1/0→1/1→2/1→2/2。
4. 在 hold 标记之后让原 saveResponse 的受控 scratch 写入失败，ErrorHandler 未结束时 joined=0，实际返回后为1。证明完成责任早于该失败登记；保留实际错误。
5. 无 token 的裸 controlResponse 请求应被拒绝，非 held 请求不增加 joined。正式受控例子必须用真实 apiHandler，不能人工增计数或伪造完成。原 arm Fatal gate 不在活跃主测试内故意触发；依据其未改 predicate 和阶段计数，后续若需直接验证 Fatal，须另授隔离测试。

每例留同步关卡、原错误、实际调用返回/所有测试 goroutine join。race/编译/执行均需 root 另授，每命令≤45s。真实 authority list_cancel_join/detail_cancel_join、Session/Project/domain late tails 仍待完整fixture资源窗；原 case45/top120含Cleanup/pkg6m/TCP75/资源拓扑不变。

Model 最终两源尚待 STOP 后绑定。candidate02 的离线 PASS 属于旧源，不能作为 candidate03 重新编译或本计划运行结果。
