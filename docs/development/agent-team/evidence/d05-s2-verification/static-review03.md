# D05 S2 review03 独立差量静审

结论：固定两生产、一单测差量闭合了所报 startup admission 静态 trace；当前实际私有调用链未见新的硬阻断。尚未执行任何 Go 或真实 fixture，不能据此称 Runtime 失败已动态修复或 S2 通过。

## 固定输入和原失败

`delta.json` 的三组 before SHA 均匹配独立持有的 review02 28 文件；after SHA 均匹配本目录固定源码。差量只给 operation 保存不可变 initializing 位、让 child 按真实父身份继承该位，以及新增一个单测顶层，没有改变数据库、对象权限、checkpoint、预算或旧测试。

已读作者 `compat-runtime-01.log` 的真实失败：`TestObjectRuntimeBusyDoesNotMaskLaterRecoveryFailure` 预期 FORBIDDEN，却得到 DEPENDENCY_UNAVAILABLE；`TestObjectRuntimeRecoveryCleanupKeepsStartupAndWorkerBudget/startup` 在原 13 秒内没有到达真实 cleanup。该轮 objects 41.236s，FAIL。原 `input-compat-runtime-01.json` 的 28 SHA 与 review02 完全一致。本目录保留日志和输入副本，未把首轮红丢弃或改称基础设施失败。

## 实际调用链与并发边界

1. `Runtime.Initialize` 在 runtimeReady=false 时以 admit(true) 建立父 operation；Service.Initialize 复用这个父。原 childOperation 清除 operationKey 后改走 begin(false)，因而 verifier/cleanup 子项在实际工作前被 readiness 拒绝，足以解释硬错误被掩盖及 startup cleanup 未进入。
2. review03 的 initializing 值仅由 Service.admit 创建 operation 时赋值，发布到受 mutex 保护的 registry 前完成，之后不变。childOperation 先核父非空、同 Service、registry 中 active、父 ctx 未取消，再清除 key 进入 admit；不能用普通 context bool 或外域 operation 获得初始化通行。
3. 当前生产只有三个 childOperation 调用：recovery.go 的 verify 与 cleanup，transfer_recovery.go 的 cleanup。两个 recoverProgress 均先 begin，再无条件令 ctx=op.ctx。即调用方把含 private operationKey 的 context.WithoutCancel 传入公开 Recover，也会在这些路径恢复为原父 op.ctx。cleanupIOContext 的 WithoutCancel 只用于 child 建立之后的物理 zero/remove，没有进入 childOperation 的调用链。
4. active 检查释放 mutex 到 admit 的间隙，父 finish 先 cancel，再做 work 收尾和 registry 删除。因此父若退出，admit 的 ctx.Err 检查会拒绝，或刚建立的子 ctx 已继承父取消，不能获得脱离父生命周期的初始化权限。此判断限于上述实际调用链；若未来增加未经 op.ctx 归一的 child 调用，必须重新评估，不能泛化为任意脱离取消 context 都安全。
5. admit 再取得同一 mutex，仍先核 stopped/forced，再按 ordinary parent 的 runtimeReady、调用者取消与原配额拒绝；Force 的收集与 child 插入也受该 mutex 串行。新 child 有独立 cancel 和 own registry 条目，取消 child 不取消父轮次，完成只删除自己。ordinary parent 的 initializing=false 不绕过 readiness。
6. 新单测覆盖初始化父继承及 child 独立 cancel、普通父 readiness、无父/未注册/外域/已结束/取消父/取消 caller/stop/force/quota 的拒绝与无泄漏。它是针对这些分支的纯测试，不能替代真实 startup/worker、实际 I/O 与原预算的验收。

## 后续独立验证

`execution-plan-review03.json` 将既定 23 顶层增为 25：追加原失败的两个完整 Runtime 顶层，保留 startup/worker 两子例、原 13 秒触达窗口和取消后 1 秒 join，不延长预算。原 21 新测试中的 standalone writer 修复不重复单独执行；旧 Source unopened Force 仍保留原 500ms。

review02 的原 writer/Source 修复绿证据和未受影响的代码证据可按三源精确差量保留，但 Runtime 真复验不可用 race/vet 或本静审替代。作者完整旧覆盖、最终 28 文件及其编译依赖固定之后，再对照本目录 `expected-input-review03.json`；此文件只是独立重建的预期输入，并非作者最终冻结声明。

本轮仅运行只读文本命令及 Python 哈希/集合核对，实际结果见 `checks.json`。无 Go、Docker、仓库/作者目录写或 Git 写。现 all-stop，等待作者真实复验和主线程交独占窗口。
