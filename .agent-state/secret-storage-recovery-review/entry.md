# D04 recovery 新入口独立窄审

`22c9d85f..b2c87c4f` 四工具增量有限离线接受，无 must-fix。五业务源已静审的结论复用；这次不重复旧 SQL/维护/Go 矩阵，不认新 recovery PG 已运行。

- driver 只批准两个 literal：`^TestSecretVariableStorageSQL(CommitUnknown|NonceUnknown)$` 与 `^TestSecretVariableStorageSQL(MaintenanceUnknown|Concurrency)$`；错误单 top、宽 regex、尾 slash/混组在 fresh disk/mkdir/资源之前拒绝。
- supervisor 绑定独立 recovery driver/candidate，新增五业务源和实际两个原 proxy 输入，旧 helpers、production、SQL闭包沿旧代码。原 reviewed candidate/driver 与 recovery 不能混配。新完整实际 RUN/PASS 节点为5或8，缺失、重复、额外、FAIL/SKIP均拒；主流程结束时重采输入集合与字节。
- 逆去本增量后原 driver 全文逐字、supervisor 全文 AST 不变；旧 core3/maintenance1 原入口保留，未扩 root 或通用 regex。Go6m、driver105+15、supervisor123+3、TCP75与两资源责任不变。

本人实际执行作者控制（cwd `/workspace/agenteam-secret-variable-storage`）：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/task-planning-recovery/secret-storage-recovery-entry-controls.py --artifacts
```

`66038/3206cc` actual0，68控；包括实际 frozen binary 两组 `-test.list`、实际 driver 错 selector 的 pre-stat 拒绝。14个 main 情形的进程/proc/TCP是显式 doubles，不启动 PG/Docker/socket；旧50控不重复。

独立补例（cwd `/workspace/agenteam-skills`）：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/secret-storage-recovery-review/entry.py
```

`34901/987857` actual0，8控；复用此前本人 actual-main 框架，只换两新组与其固定输入调用，不改作者源码。每组正向、driver原exit2保2、非法UTF8、最终输入失读；各原Wait123、两轮desc/TCP、input与terminal均实际经过受控main。原进程/OS边界是替身，真实输入文件与log parser/关闭流程在执行，不能称PG资源已退役。

作者95272编译出的28,121,484B候选与15,394,827B driver仅列出/负配置，未业务执行；原742/831不覆盖。新两组整体耗时、SQL/实际Unknown/竞争及proxy wg和所有资源尾仍待 fresh grant。本人无live进程/资源，`entry.py` 与本文冻结供root保存；无需为了此小接缝新增通用harness。
