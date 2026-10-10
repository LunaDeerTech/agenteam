# Knowledge 命令 PG / native 方法限定独审

2026-10-10，审查者 Skills；作者树 `/workspace/agenteam-knowledge-tree-http`，基线 `d504a5ac`，范围为 `tests/knowledge/owner_tree_commands{,_fixture,_authority,_transactions,_unknown}_test.go` 与 `internal/central/knowledge/commandhttp/native_test.go`。仅静读冻结源码和原失败事实，没有启动 Go、PG、socket 或复跑已接受的产品 / 纯控 / 入口矩阵。

结论：两处测试判据缺口已由作者窄修，有限接受修后的方法，没有剩余已确认 must-fix。此结论不是修后动态 PASS。

- PG 使用真实 Account 登录 / 登出、HTTP boundary、B02 Service、Object、Audit 和 Outbox；Project 初始化 / Owner / phase 为明示上游事实注入。事务包装沿原 Store、原 live Tx、原 AcquireAll；锁证据包含原 backend、database、完整 advisory key / mode / granted 与 `pg_blocking_pids`，竞争方实际返回再回收。两种 COMMIT 故障使用原完整 frame proxy，核原 Unknown / AttemptID、独立连接原事实与释放后的 writer join，不用 controlled Store 假造 Unknown。
- Unknown 末子的 `002f0824` 时间前置遵循正式 TouchActivity throttle：原 User EX 与 RequireCurrentSession 前后，将 issued/activity 同移两分钟，保 token / expiry / CSRF / CHECK。原 PG01 在复合断言失败时未采各分项，修后不能反推原实际值。
- Delete 原来只核返回长度两项，不能证明它们确是 root 与 child。作者新增 UUID 字符串升序的完整期望 slice，再 `reflect.DeepEqual`；正式 `DeleteResult.Validate` / `compareIDs` 同样要求严格升序。foreign / duplicate / missing / 逆序均拒，原 tombstone、outside、replay 和 Lookup 断言不变。
- native 使用真实 listener / conn / net/http Serve / 原 Body / 原 Write，controlled Account / domain 边界明确。慢读要求原预算下界、零业务调用、原 Close 与 EOF/reset；keepalive 验证下一请求 deadline 清除；取消要求原领域调用实际返回与 handler / conn / Serve 尾。peer-disconnect 使用完整 TCP Close，没有实际 half-close 场景，不扩大声称。
- native 背压原来只要求非空错误与上界，可能接受早发普通错误。作者改为 `errors.As(net.Error)` 且 `Timeout()`，实际 elapsed 至少原 requestBudget 的 3/4、仍保原上界 +800ms；原刺激、writer 透传与资源尾不变。该四行修复已静态复核。

原 PG01 `29190/fa6ad6` whole FAIL 与完整资源尾保留：Authority 3、Mutations 3、Transactions 4、Unknown 前两子实际通过，末子原复合断言失败；不据本审升级。后继修复仅 Mutations / Unknown 两 top 六子使用新候选，未变 Authority / Transactions 结果可复用。旧 native01 从未执行，native02 含新 Timeout 判据，仍待唯一真实窗口。没有为本静态方法审增加控制框架。

## 原动态结果收口与最小独立补集

2026-10-10，最终技术 `3976b984`；本人只读原日志核对 `d642b2` actual0，没有运行业务、Go 或 socket。上段“仍待”是静审时点，现后继结果如下：

- native02 `52400/0b3bce` outer0，67.850s，三个父/六子 RUN、PASS 恰各一次；原 child1422975、driver1422968 Wait0，runtime/private/desc 双尾、TCP 两次空差量与 input unchanged 齐。日志 `/tmp/ktc-native-01/pg-87731d1f665a4a1cbb33325eb9548141.log`。实际背压 Timeout 和原预算下界经过；断开仍是 TCP 全 Close，不称 half-close。
- PG03 `9527/b8dcb4` outer0，117.055s，Mutations/Unknown 两父六子全部通过；原 Go1417828、driver1416054 Wait0，七个精确资源各两次 absent，private/runtime/desc/TCP/input 尾齐。日志 `/tmp/ktc-pg-02/pg-2cd352f2538145f7bbcd89f675642d9a.log`。
- 原 PG01 日志 `/tmp/ktc-pg-01/pg-477a2456070142ecb5f6ade631696bee.log` 的未变 Authority 三子与 Transactions 四子及父节点逐项通过；原 Unknown 末子及父 top 失败、whole FAIL 不回填。固定输入组合现覆盖作者四 top/十三子，不能称当前 HEAD 单次全量通过。

有限接受上述实际方法及结果，没有新增确认的产品 must-fix。Runner `25660/6cc25b` 的实际两 top/九 sub 覆盖 actual adapter/正式 digest、坏隐藏字段/提交后投影与原取消 callback held/join；B02 已正式交付的真实事务、正文和 tree/reference 独立补集继续复用。没有必要重跑作者 Unknown、锁序或 native 矩阵。

正式 HTTP 交付前建议只补一个独立 PG 情景：**当前合法的新 Owner 不能取得旧 Owner 的命令回执**。正式卡 §2 要求 User/原意图绑定，§3 要求每次当前授权；`commands.go:LookupCommand` 是当前 `readScope` 成功后才读取原命令并比较持久 `row.user`/digest。作者两锁序实测 Owner 漂移后旧 A 的 mutation 被拒，但未让已经获权的 B 到达此旧回执分支；Runner 的不同 User 控制止于 controlled Service 捕获摘要，不能代持久回执组合。

最小一 top/一个连续场景即可：真实 A rename 后原 key Lookup 正向，改变原 title 的 Lookup 必须拒绝；在同正式锁/原 Tx 中完成已披露的上游 Owner A→B fixture 事实；B 用新 key 真实 rename/Lookup 正向证明当前权限，再用 A 的原 key/原意图 Lookup 必须 `IdempotencyKeyReused` 且不发布 receipt；A 的旧 Session 仍按当前权限拒绝。核原 A command/receipt 不变、拒绝段无新增领域事实，并保原资源/调用尾。具体刺激与断言由未参与 HTTP 实现者独立编写，不手种 command/receipt，不引入新并发或故障框架，不把上游 Owner fixture 称生产转移 API。

该补集尚未实现或执行；不是已知漏洞，也不新增 root、UI、正文下载或整个 D12 完成结论。无须复跑旧十三子来完成它。
