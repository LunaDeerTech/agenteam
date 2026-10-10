# Secret Owner：00030 与四组 PG 入口窄独审

固定作者 `1ab291970c2b6e8ee65a2bdef2d35c8a92df8472`，仅审 00030、既有 `pg_only_driver.go` / `pg_only_supervisor.py` 的新增入口及 `pg-entry-controls.py`；相邻新 migration 测试只核正式 Migrator/目标约束方法。`026e18` 核七相关技术路径等于该提交。无 must-fix，有限接受为后续真实执行输入；没有启动 PG/socket 或真实测试子进程、执行 Go/candidate/driver，也没有 SQL 或业务通过结论。

- 00030 前三行替换为正式 tx/Up/说明，其余正文逐字等于已审 eaf209f5 draft。树内只有一个 00030，编号写权沿 root 明确分配；不以 26–29 文件连续推断它们已正式交付。原 payload/history/同 Tx 延迟 FK/Audit guards 无新语义变化。
- migration 方法从正式 Migrator 空库、重复和 00029 存量升级进入；普通 canonical/commands/history/generation/Audit/events 前像比较只排除新增两列，并核原 receipt 重放。五个 CHECK 刺激要求精确 SQLSTATE/constraint，变量更新先满足旧 immutable version/updated 条件；history 先证一行更新成功，再显式切 IMMEDIATE 要求精确 deferred FK 拒绝并回滚。它们是待实跑方法，不能由静核声称 PostgreSQL 已接受 DDL。
- 新入口仅四精确组，完整 RUN/PASS 节点分别 6/10/5/9，包含父 top 与嵌套迁移子节点。该域错 selector/root 模式拒绝；driver 除这两新增组合外原单 top 门保留。固定 artifact pair、实际 package 旧 helpers/原 COMMIT proxy、生产依赖和嵌入密码表/迁移都在输入闭包；前后还核文件集合、正规文件与非 symlink。旧默认、PG2 资源、105+15 / Go6m / 123+3 / TCP75、原 Wait/退休及失败清理路径保持。
- 本人 `b28e20` 实际重取作者 182 纯控，唯一运行适配为临时输出目录重定向本人 ignored output；`--artifacts` 未用。实际执行旧 driver 全文逆差异、supervisor 全 AST 逆差异、独立本地 import 闭包、四组真实 main 的明确 Child/proc/TCP/artifact 替身与异常日志/输入/selector 控制。
- 持久独立控制为 `entry-controls.py`；cwd 本树执行 `PYTHONDONTWRITEBYTECODE=1 python3 -B .agent-state/secret-variable-owner-review/entry-controls.py`，`f1b478` actual0/78 控。覆盖 DDL 身份、同数量错 RUN/重复 PASS、错嵌套父、额外 FAIL/SKIP、读错误安全投影、Wait 后新源或 symlink、driver 非零/Wait 缺失/owned 后代/TCP 不清，以及失败后仍到双 descendant/TCP 判定/input/terminal。进程、proc、TCP 和二进制均明确受控，绝不当真实退休证明。

复用前轮六测试方法静审及作者固定产物 compile/list 作为各自有限事实；本轮不重复产品全库、旧测试矩阵，也不把新方法或编译结果冒成真实验收。原失败与未验证边界保留；正式 SQL 升级、每组业务及原资源完整尾待 fresh grant。
