# Knowledge 命令 PG / native 方法限定独审

2026-10-10，审查者 Skills；作者树 `/workspace/agenteam-knowledge-tree-http`，基线 `d504a5ac`，范围为 `tests/knowledge/owner_tree_commands{,_fixture,_authority,_transactions,_unknown}_test.go` 与 `internal/central/knowledge/commandhttp/native_test.go`。仅静读冻结源码和原失败事实，没有启动 Go、PG、socket 或复跑已接受的产品 / 纯控 / 入口矩阵。

结论：两处测试判据缺口已由作者窄修，有限接受修后的方法，没有剩余已确认 must-fix。此结论不是修后动态 PASS。

- PG 使用真实 Account 登录 / 登出、HTTP boundary、B02 Service、Object、Audit 和 Outbox；Project 初始化 / Owner / phase 为明示上游事实注入。事务包装沿原 Store、原 live Tx、原 AcquireAll；锁证据包含原 backend、database、完整 advisory key / mode / granted 与 `pg_blocking_pids`，竞争方实际返回再回收。两种 COMMIT 故障使用原完整 frame proxy，核原 Unknown / AttemptID、独立连接原事实与释放后的 writer join，不用 controlled Store 假造 Unknown。
- Unknown 末子的 `002f0824` 时间前置遵循正式 TouchActivity throttle：原 User EX 与 RequireCurrentSession 前后，将 issued/activity 同移两分钟，保 token / expiry / CSRF / CHECK。原 PG01 在复合断言失败时未采各分项，修后不能反推原实际值。
- Delete 原来只核返回长度两项，不能证明它们确是 root 与 child。作者新增 UUID 字符串升序的完整期望 slice，再 `reflect.DeepEqual`；正式 `DeleteResult.Validate` / `compareIDs` 同样要求严格升序。foreign / duplicate / missing / 逆序均拒，原 tombstone、outside、replay 和 Lookup 断言不变。
- native 使用真实 listener / conn / net/http Serve / 原 Body / 原 Write，controlled Account / domain 边界明确。慢读要求原预算下界、零业务调用、原 Close 与 EOF/reset；keepalive 验证下一请求 deadline 清除；取消要求原领域调用实际返回与 handler / conn / Serve 尾。peer-disconnect 使用完整 TCP Close，没有实际 half-close 场景，不扩大声称。
- native 背压原来只要求非空错误与上界，可能接受早发普通错误。作者改为 `errors.As(net.Error)` 且 `Timeout()`，实际 elapsed 至少原 requestBudget 的 3/4、仍保原上界 +800ms；原刺激、writer 透传与资源尾不变。该四行修复已静态复核。

原 PG01 `29190/fa6ad6` whole FAIL 与完整资源尾保留：Authority 3、Mutations 3、Transactions 4、Unknown 前两子实际通过，末子原复合断言失败；不据本审升级。后继修复仅 Mutations / Unknown 两 top 六子使用新候选，未变 Authority / Transactions 结果可复用。旧 native01 从未执行，native02 含新 Timeout 判据，仍待唯一真实窗口。没有为本静态方法审增加控制框架。
