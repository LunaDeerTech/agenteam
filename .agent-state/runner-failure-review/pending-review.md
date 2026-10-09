# Runner B pending 请求体与首子映射独立窄审

结论：对 `/workspace/agenteam-runner-control` 基线 `eea4ced0` 后的六技术文件增量有限接受，无本范围 mustfix。Work 未参与实现；仅执行下面离线控制，无 PG、OS listener、业务进程或真实 B 重试。3175 整体 FAIL 保留，committed 子原限定 PASS 不升级为整轮通过。

范围：`tests/runnercontrol/process_crash_linux_test.go`；`.agent-state/runner-control/{pending-body-controls.py,pending_body_bounds_test.go,pending-selector-controls.py}`；`.agent-state/task-planning-recovery/{pg_only_driver.go,pg_only_supervisor.py}`。原 `pending_request_body_test.go` 作为作者固定 Go 方法红绿依据只读复用。

- pending 分支在原 checkpoint 前读取同一原 POST Body，限制 MaxDeviceBodyBytes+1，要求非空已知 Content-Length、精确长度、无 Read/Close 错误并清理自有 raw。没有转发、backend 调用、额外 HTTP 或人工取消。整个测试逆去这一个 helper/调用后逐字原版，committed 路径、SIGKILL/实际 Wait、原 3s idle、无 token 重启、key/file lock 与持久事实断言不变。
- 独立控制执行真实 source helper、固定 Go HTTP/1.1 Server/TLS 与内存 net.Pipe；原 peer 关闭后，原 request context 与 handler 自然结束，原 Serve 实际返回。另补原 Close 被持住时 helper 尚未返回、放行后分别保留成功或原错误，以及 Read 同时返回完整声明字节与错误仍拒绝。没有用失败清理 abort 制造正向取消证明。
- 唯一新 selector 为 `^TestRunnerControlProcessCrashRecovery$/^pending_persisted_before_backend_admission$`。原 105s ctx 内先以父 top 执行实际 `-test.list` 并实际 Wait，执行仍使用完整子 selector；原 105+15/123+3/Go6m/TCP75 预算不变。同 B 输入闭包包含此次新增 discovery，采集仍支付原余额。旧 single/A/四组/B/root 字节逆投影均保持。
- supervisor 成功只认 RUN/PASS 恰父与目标子；缺失、重复、错兄弟、skip/fail、非法 UTF-8 或不可读均拒绝。新增失败只设 code=1，原资源、Wait、TCP/input 全尾不跳过。作者 25 映射/原 34 闭包与零余额不启动控制复用，不重复整矩阵。

实际独立命令：在 Work 树执行 `python3 .agent-state/runner-failure-review/pending.py`。68346 / 7e07e6 actual0：3份 source 全文逆投影、7个闭集日志控制，Go race 2.077s、4top/2sub。Go discovery 控实际运行旧预编译 candidate `runnercontrol-process-crash-race-2.test` 的父 top listing（PID1217787实际 Wait0），另核预取消与 Start 失败拒绝；这不是用旧 candidate 证明修后业务通过。原六作者技术路径只读，控制前后3实际执行源字节不变。

首90d69f在3逆投影/7日志控后，Go临时文件位于另一 module root 导致 internal import setupFAIL；接着a04787尝试对照旧Work树不存在的 protocol 文件，Python setupFAIL。仅把控制改为目标树虚拟路径的 Go overlay，物理文件仍在 Work ignored output，随后7e07e6通过；两次setupFAIL不当业务结果或偷偷替换作者输入。

工具链固定 Go1.27.1；readonly/offline module cache `/workspace/agenteam/output/ai/model-ui-recovery/go-mod`，只写 Work 原 gocache。脚本可恢复但使用当前精确树/历史Git对象与旧 precompiled discovery 产物，换机需配置这些入口。未编译新的修后 B 候选/driver；它们仍由作者和 root 以最终冻结 source 准备并另排真实首子窗口。
