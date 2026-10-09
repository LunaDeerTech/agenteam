# Knowledge 树命令 HTTP 实现有限独审

有限接受，无 must-fix。输入为 `/workspace/agenteam-knowledge-tree-http` 的 `f1a1bd45`：commandhttp 四产品源（逐字沿 3a063234）、四相邻 pure、专用 OpenAPI/check-schema 与已接受的 SPEC。作者后续 tests/knowledge 未纳本结论；没有编辑作者产品，也没有运行 PG/native/socket。复用作者实际 10 top/86 sub race、Schema33 与 vet；不把作者首编译失败或空间不足未启动改写为通过。

## 实际链路

公开构造只收真实 Knowledge Service/Account HTTPBoundary。五路先原 CheckRequest/RequireHuman，再路由、方法、ID/query/body；Origin/Session/CSRF 不被 405 绕过。Actor 与 Project 原样进入正式 Service；当前 Owner/初始化/归档/Deleting、原命令锁/活 Tx 与权限复核属于真实 B02，而非 HTTP 缓存或测试替身。Handler 不依赖未交付 read adapter、SQL、迁移或生产 root。

16 KiB 原 DecodeJSON 完整读取；全层重复 decoded key、未知字段、原 UTF-8 和本层 surrogate 检查，nullable parent 的 presence、正式 key/版本/title/token形状闭集保持。原 RequestID 只作 transport。title-only Update 保 source=nil、ReplaceSource=false；Lookup 正式 Update/Move/DeleteDigest 绑定 User/Project/原 key/原意图，不绑定 Session 或新 RequestID，不自行执行/重试。Delete 只 Parse token，当前权限和历史 completed replay 先于验签/时效的次序仍由 B02 持有。

Document 先正式 Validate（包括隐藏 ObjectID、AgentRun creator 同 Project），再显式十二字段；preview 先完整 Validate/同 Project/根/子树摘要，再唯一 Human material 投影。Lookup 严格 receipt union、command/target/title/version/changed/parent/delete IDs。完整 JSON 超 5 MiB 或坏投影不发布前缀；mutation 已成功后失败 abort，preview/Lookup 无本次 mutation 才可报告安全依赖错误。原 Fault/CommitState/Unknown 交真实 Account projector，不暴露 cause、对象 ID 或 key。标准 Schema 的五 POST、闭集请求/响应/receipt 与正式 common 标量匹配；它不能证明实际数据库枚举完整性或授权。

所有路径原 2s 从 Account 前开始，继承更早 deadline。native capability 不可用则拒绝；原 body Close、服务同步返回与取消 callback 实际 join 保持，短写/flush/Close/过期失败不复用连接。先完整编码并实际 Close，再输出；成功清 deadline 必须晚于原 callback 退休。底层 I/O 方法能否实际返回及原生网络时限仍须真实测试。

## 独立实际控制

命令：`python3 -B .agent-state/knowledge-tree-http-review/run-risk.py`。固定 Go1.27.1/local/off/readonly/-p1、原 Runner 独占 cache；同进程 UTC2026-10-09T23:48:17.728837Z，free5,771,194,368B。`25660 / e29185 → 6cc25b` actual exit0，race 两 top/九 sub，1.040s；未丢失终态。

- 实际 adapter 加正式 digest 的 private boundary/service port 控制：捕获原 actor/project，只调用一次 Lookup；同 User 新 Session 摘要相同，换 User/title/expected 摘要不同。它不代真实 Session/Owner/PG 当前权限。
- 实际 safe projection：合法 AgentRun 显式字段，foreign creator、隐藏 ObjectID 无效与 service 成功后原 Close error 都零 body abort，不把已知成功伪报未提交。
- 原 requestIO.finish：真实 context.AfterFunc 的原 SetWriteDeadline 被 held；原 body Close 返回后，通过原 goroutine 在 finish 的 channel receive 确认正等 callback，而非 sleep 或第二次 SetDeadline 阻塞。原 callback 返回其错误后 finish 实际退出、保留取消/错误、Close 恰一次且不清 deadline。

只读 overlay 五个冻结输入（四产品及 handler_test helper）；其余测试排除，无 TestMain/服务进程/监听器。runtime.Stack 仅内存判别等待位置，不保存栈或材料。必要风险控见 risk_test.go/run-risk.py。

## 未验范围

仍需作者计划的原生 2s 慢 I/O/断开/keepalive 与真实 Account/B02/PG 当前权限、两锁序、同 key/旧 token 重放、Commit Unknown 与完整资源尾。本次不代这些门，不新增通用平台或复测整个 B02，也不宣称 D12/root/UI 已交付。
