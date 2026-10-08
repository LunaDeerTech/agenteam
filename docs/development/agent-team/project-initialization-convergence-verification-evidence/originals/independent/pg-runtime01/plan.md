# 独立初始化收敛 PG A/B — runtime01 仅准备

本提案不授权执行。私有五项离线已实际通过，见 pg-offline01/summary01.json（283b8ff78a994775368d8a8ec1b2c7a9786c5e8eaa63a6dbcf685eb6acf08b30）；原 pg-probes01 的“未格式/未编译”是保留的历史准备状态，不代表当前状态。当前未执行本 source gate、真实 PG 或测试体，未安装五源或私有源到仓库。

## 固定范围与最小输入

- author candidate03 五源固定，不改生产。复用已窄审 PG v02 driver 62895c8d30f5f35fbec65521ad0b145c74730d368f8e6bcd57102cb9a4ea0b5e、closure a2e64479ba3217a73d17e4305520a08f3f04191fe3caf51734607d93f11ce9c1，父 full20 race / dynamic / CGO0 来源不重跑。
- 私有 actual race 图 377 包/2363 files/1 generated、extra0，无自定义 TestMain/UI 消费。与父图只有 2 个实际文件增量：私有测试源与新生成的 Project 测试 main；所有交集 hash 相同。actual-union-delta.json 绑定父 full20/dynamic 和私有实际图，不复制全源清单。
- runtime closure 3384 files / 440 sets，8-map overlay（5 candidate03＋2 UI 删除＋1 private）。仅 tests/project 的有效文件集合扩一源；无 UI 内容/JS/schema/runtime-read 新增，无 Object 停止探针。
- driver.py 仅替换 GROUPS 与 runtime_environment 内 overlay 路径。其余全部函数/类 AST 同作者 v02，包括 main/源码门禁、top watchdog、direct/adopted wait、精确资源回收、补充 TCP retirement。ROOT 自动落自有目录。source-only wrapper 仅 BASE 换自有目录，其余原 45s 生命周期逻辑不变。

## 两轮精确执行

| 次序 | label | group | top |
|---|---|---|---|
| A | independent-a01 | independent-a | TestIndependentInitializationConvergenceTransactionBoundary |
| B | independent-b01 | independent-b | TestIndependentInitializationConvergenceCanonicalFacts |

A 的 3 个代表：真实其他 Store live Tx 交原 Authority 且 caller marker 必须 rollback；SH 缺 EX 的忽略错误 poison；原事务中一次合法检查后仅取消第二调用 childctx，原未取消 caller marker 仍因 gate poison rollback。B 的 4 个代表：四状态各一项可提交的不一致事实、拒绝零变化、修回合法后通过；completed 当前改名/version9 与原历史初始 snapshot 区分。

明确依赖作者新合法 SQL fixture、真实 Store/migrator、同 Store Account 构造依赖及正式 registeredService；独立的是调用顺序/破坏修复/断言，不声称 helper 独立重写。snapshot 只证明两行和所列副作用计数，不称全数据库字段零变化。此端口不消费 Human Session，不称账户/Skills 创建 E2E。旧 Skills adapter 仅由作者旧 selector 兼容证据承担；本两 top 不调用旧 assemble/newFixture 或 Skills stub。

实际命令保留原 `sh scripts/test-objects.sh -run '^(唯一top)$'`，full20 原 fixture / 7 精确资源，原 -tags=integration -race -count=1 -timeout=6m，GOFLAGS readonly/p1/v/冻结 overlay。top 从 RUN 到 PASS/FAIL（含 Cleanup）120s；fixture cleanup 必须实际 Background Drain 终局，outer watchdog 仍兜底。A 必须 actual0/topPASS/watchdog完整/实际wait/7资源两清/owned与runtime两清/输入同及 TCP 补充完成后才 B；失败先实际终局、停后继，不自动 retry。

每轮新鲜 ≥5GiB（前置与spawn前）、live PID/starttime/daemon、原资源完整 Mount 与空私有 Docker config；本地两个固定 PG digest 只 inspect，不 pull。无需沿用任何旧 daemon 基线。direct/adopted wait 精确记录；非 owned PID1 shim 差集单列未 wait。原 helper 内部生命周期边界不因外层实际 wait 被改称全部 inner join。

TCP 仅继承 v02 host 元数据补充轮询，无 payload/ptrace/非 owned signal，不称完整短连接 trace/tuple 所有权；在业务实际终局后以单调 deadline 限制新读和 sleep，≤75s 是退役观察预算，不延 120s/6m，也不承诺调度器级硬实时。任何仍存 host 差量导致 FAIL 并停止，不清理非任务网络。

## 提交 root 的下一执行前置

source-gate/command.json 是唯一待授权准备命令：固定绝对 Python 执行 45s wrapper（42s 开始收尾），child 仅 driver --check-input-only；3390 输入前后 hash / 440 sets / actual wait / 双 owned。此分支在 Docker/image/resource 查询和应用 spawn 之前返回，当前未执行。commands.json 的两轮真实命令只作提案；execution-freeze.json 尚不存在，proposal-freeze.json 的授权 bool=false。只有 root 明确交独占窗口后才另存授权冻结，不覆盖提案。UI 与作者窗均不能由本提案自行认定已释放。

静态准备脚本首次把 graph.excluded_ui_consumed 的布尔 false 误按空列表断言，发生在 driver/runtime 生成之前。保留 prepare-original.py、原 traceback、首结果与一行修复差量；此为 scratch 脚本静态检查失败，不是 Go/产品/资源失败。修后准备实际0，未执行任何 driver。

三个停止与生产未绑定边界不变：不初始化/放行 Skills/Object runtime，不绑定新创建 HTTP、不修改旧成功 gate，非完整 D08/D10 交付。本轮只准备，最终 PG 结果与 README 后继仍待授权。
