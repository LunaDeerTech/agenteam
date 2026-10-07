# Candidate03 #5 native test delta — bounded STATIC PASS

唯一改动为本卡 native test。production03、schema、integration4 和私有 independent probe 不变。复用 candidate02 其余完整 STATIC。

新外层 Write/FlushError wrapper 在实际方法进入时计数，以 defer 记录实际退出；Write requested bytes 是传入完整片段长度，returned bytes 使用原 inner Write 返回的 n。它继续委派给既有 summaryNativeWriter，再委派真实 ResponseWriter / ResponseController.Flush；Timeout 只由原 inner 对 errors.As(net.Error) 且 Timeout() 的实际 I/O 错误确认，不由 context 取消、handler entry 或模拟 timeout 置位。GET 要求真实进入/退出匹配且 asked>returned，再结合 net timeout、first deadline已到和last deadline清零；HEAD要求真实Flush进入/退出匹配及原timeout断言。

仅 blocked_get_write 父期限由120ms改1m，同一100000合法 efforts保留；生产仍在每次请求开始建立原2s，因此这不是把产品期限改成1m。其余案例120ms、terminal3s和整包40s保留；原真实连接、listener、Serve/Cleanup链不变。wrapper原子计数没有新增goroutine/资源；所有读取在实际handler终局后。

首次writeclose失败原始证据已核：actual exit1、9 listener、实际wait、owned PID/TCP含TIME_WAIT双清、输入相同。旧探针只证明未观察到net timeout；它没有Write/Flush阶段计数，trace不含write，不能据此认定编码阶段超时。前8sub通过属于失败组内子证据。新差量未由本审查实际运行；必须重跑精确writeclose并观察真实写入/timeout/尾部证据。

本定点STATIC没有剩余阻断，不代表新的native runtime PASS。独立runtime仅重绑定#5有效hash及candidate03，私有native同包受影响重编/list；未受影响的integration编译和已有结果可复用。所有真实窗口仍待root明确交接。
