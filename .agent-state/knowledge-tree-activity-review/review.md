# Knowledge tree HTTP Activity 前置独立窄审

固定输入 `/workspace/agenteam-knowledge-tree-http` 的 `002f082460c8be1b3c3cd51a368c7b64467cc9e7`，相对 `70f135e4` 仅 `tests/knowledge/owner_tree_commands_unknown_test.go` 与 current。未参与这段 HTTP 测试返修；静态有限接受，无确认 must-fix。没有运行 Go、PG 或 socket。

正式 Account `TouchActivityInTx` 只更新至少60秒前的 last_activity。原 fixture 通过真实 Login 创建的新 Session 尚不满足该前提。新增 helper 在原 User EX、同 Store 活 Tx 下先真实 RequireCurrentSession，再仅更新 exact Session/User 的 issued_at 与 last_activity_at，各向前平移2分钟。两字段同移保留00010的 `last_activity_at >= issued_at`；absolute expiry、idle policy、token verifier、CSRF key、身份和业务事实不变。RETURNING 必须确认已到60秒更新门，随后再真实校验当前Session，并要求原 Tx known Committed 后才执行测试。

原 titleFailActivity 始终先调用真实 Account Touch，再返回注入错误；当前修复没有伪造提交、receipt 或 Activity 成功。最终回滚仍要求原 Activity 不变、pending plan/无receipt及 Lookup 无副作用；原重试继续要求 calls=2、Command/Event不新增、Outbox精确+1、Audit不变、Activity严格前进。原复合条件只是拆为三段安全诊断，未删除门槛。前两种原 COMMIT Unknown 源码无变化。

源码依据：`012cab` 限定 Git diff，`db290f` 正式 Session/Touch/迁移约束，`680025` 原真实 Touch wrapper，`1ebc17` 真实 Account 构造；均只读。原29190整体FAIL及末门未打印的值保持未知，不把静态定位补作当时已确认归因或业务PASS。修后候选及真实重验仍需各自实际结果。

## 单 Unknown 入口增量

作者在002f0824之后冻结两个原Python工具及 `selector-controls.py`。唯一新增 `^TestKnowledgeTreeCommandHTTPUnknown$` 复用原Unknown的三子闭集，保留原四top入口和native入口；新目标仍走原根监督路径及安全日志读取处理。独立 `60e6ab` 运行 `PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/knowledge-tree-http/selector-controls.py --unknown-only`，39控制actual0；RUN/PASS缺漏、重复、FAIL/SKIP、错目标/UTF-8、选择器拒绝与实际main四种明确进程/OS替身均到达，保持原Wait540、reap/desc、七ID十四次观察、TCP双尾及input末验。实际资源全部为替身，没有Go/PG/socket或真实子进程。

同一命令另逐项移除本次新增literal、复用组、expected一行并恢复main原单条件，两Python文件全文逐字等于002f0824；native_driver.go逐字未改。旧三工具逆至7fee的现成控制复用通过，不以整块逆投影代替本次增量检查。Go6m/root540+60+3/TCP75、原输入集合和旧分支均保留。入口有限接受，无确认must-fix；不构成三子真实结果或旧失败返填。

## Mutations＋Unknown 封闭补验入口

固定d504a5ac后冻结的两Python工具与 `selector-controls.py`，仅新增 `^TestKnowledgeTreeCommandHTTP(Mutations|Unknown)$`。复用原Mutations、Unknown三子各自闭集及原expected/main全尾；旧四top、单Unknown、native配置及预算不变。本审不重审Skills负责的精确删除集合/native断言修正，不运行候选或补写原PG01失败。

独立 `d92b65` 实际运行 `PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/knowledge-tree-http/selector-controls.py --recheck-only`：61控制actual0，包括原真实main的四种明确进程/OS替身、日志闭集和坏selector拒绝；没有真实子进程/PG/socket。另只逆去本次新literal/复用组/expected/main条件，两Python全文逐字等于d504a5ac；现成旧三工具逆至7fee控制复用。有限入口接受，无must-fix；不构成2父6子真实行为通过。
