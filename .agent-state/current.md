# Secret Variable Owner 独立读取补集

- 树 `/workspace/agenteam-secret-owner-independent`，分支 `ai/secret-owner-independent`，root由固定作者 `482c5ae5` 建立。Skills是唯一新增测试作者；Git与构建/cache/真实PG窗口均由root协调。生产、旧测试、迁移、正式卡只读，零迁移。
- 目标仅补六能力库尚无真实证据的目录续页与当前Owner/稳定writer身份分离。原作者Migration9、read6、Atomic/Concurrency10、Recovery5按各版本全尾PASS可复用；read01完整FAIL保留。本补集不重跑原矩阵，不把作者30节点称本人独立执行。
- 唯一新源 `tests/projectvariable/secret_independent_test.go`；精确top `^TestSecretVariableOwnerIndependentReadIsolation$`，直接两子 `real_keyset_and_generation` / `current_owner_receipt_and_cursor`，预期父子共3个RUN/PASS。
- 第一子正式producer创建C/A/B三Secret及一个普通变量，按固定A/B/C预期实际Limit1续页、同User真实新Session且换limit续页，核无重漏/跨type。正式metadata no-op只增两receipt、replay无增量、旧cursor仍有效；真实rename在三行count不变时改顺序、旧token须CursorStale/零候选，fresh页与两域版本精确匹配。
- 第二子先取得A实际receipt/cursor，复用原old/new User EX与Project EX、当前两端Session检查后的Owner SQL事实（不声称OwnerTransfer API）。B当前Get/List及自己的续页正向；在B首次写入前核B使用A旧token CursorInvalid/旧receipt及原写重放IdempotencyKeyReused、A旧scope NotFound，零候选与两域/Activity快照不变。随后B新key真实更新原映射、自己的Lookup正向，A仍拒；全程canary仅按既有安全检查，不打印材料或持久快照。
- 复用 `newSecretOwnerFixture` 的实际Account/D10/D04/Project/Audit/Outbox与原Service/Store清理；Project创建仍使用披露的persistent Skills fixture。新增助手全部限该单文件，只读业务/Session事实，不插canonical/receipt/private witness。无并发框架、后台任务、HTTP/F1/默认root或完整生命周期证明；原2资源PG-only方法拟复用，唯一入口增量尚未实施。
- 当前只完成源码与固定Go1.27.1的gofmt、Python闭集/无Skip/无Parallel/无新进程或SQL写入/空白检查；未运行Go编译/test/vet、未创建candidate、未PG/socket，无cache租约或在途命令。源码两路径先freeze交root保存及未参与者方法审；后继构建须另获授权、复用热cache并same-process fresh>=5368709120B，实际PG另等fresh grant，不自动执行。
