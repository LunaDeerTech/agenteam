本轮只文件准备，原 e52120ab 计划保留，不改 candidate03 仓库两源。

依据 root/architecture 执行前核对修正原计划两句：

- Go1.27.1 无 listener 直调 ReverseProxy 默认抑制 copy-error panic；overlay 显式把未启动的 &http.Server{} 放入请求 http.ServerContextKey，再要求实际 recover 等于 http.ErrAbortHandler。未启动 Server 或 listener。
- release IPC 没有 target，重复 release 会释放当前 C。token 例明确 release 当前 C，再把 C 的 Write 阻塞；旧 A context、普通 D 请求完成和重复 release 不能让 C 的 joined 提前。没有“旧 release 不影响 C hold”的断言。

保留五个不同故障边界：list release/write、detail cancel/ErrorHandler、A/B/C/D token隔离、真实文件ENOTDIR导致原saveResponse失败、detail Write错误引发实际server式unwind。没有机械双倍同例，也未扩大到业务schema/client验收；不覆盖裸token负例，避免对当前D1无额外意义的重复断言。

原 hold 不提供进入通知，overlay 仅用1ms ticker在2s期限内观察实际受锁 held predicate。它不会依据等待时长推定进入；随后所有 callback返回后的Write/ErrorHandler关卡均由显式channel握手确定。失败前登记cancel/release/unblock和实际goroutine join；2s只是观测报警，异常仍保留ownership等待实际返回，外层30s test/42s执行截止/45s整命令保原失败。

run.py 固定唯一D1 top，不允许别的selector或命令；必须另给root true grant、精确prepared freeze和最终Model两源。当前grant模板false且Model空；不消费仍在返修的Model新pure hash。runner复用旧903路径清单引用，只在未来授权执行时核固定源/工具、补本overlay与最终Model两源并比较before/after；本次未扫903源、没建新图/资源driver。

准备只以Python ast.parse检查runner语法和文件级断言，未执行runner、gofmt、Go编译/测试/list、Node、资源或主机扫描。Go语法/类型/race与五例动态结果均待后授。旧candidate02检查不能外推本overlay或candidate03。
