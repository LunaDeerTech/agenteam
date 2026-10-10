# Runner Default 新 main 准备的限定独审

2026-10-10，Skills agent 未参与本增量实现。结论：**有限接受，无本轮确认的 must-fix**。这里只接受测试观察修正与既验入口/TCP诊断移植；不证明新 main 的 Default 真实业务通过，原 14016 FAIL 不回填。

## 冻结输入

受审树 `/workspace/agenteam-runner-control-delivery`，基线 `85832bd9353478b1ff945c714955a2344e30c8e3`，作者冻结后 root 已保存 `b1630855`。限定四技术路径：

- `tests/process/runner_control_test.go`
- `.agent-state/work-owner-http/root_chain_driver.py`
- `.agent-state/task-planning-recovery/pg_only_supervisor.py`
- `.agent-state/runner-control-delivery/preparation-controls.py`

作为既有依赖只读 `tests/process/runner_failure_test.go`；该文件与基线逐字相同，不重新独审 C 的完整方法或生产实现。作者新候选编译/list 结果单独保留，本次未重新编译。

## 实际核对与控制

`f820cb` 实际 diff：删除旧仅计 handler 的 TLS proxy，Default 唯一 constructor 改用既审 `newRunnerFailureTransport`，三个原 `retired` 调用改为 `controlRetired`；去掉不再使用的 imports，其余业务完整投影回基线。新复用实例初始 held=false，Default 未调用 hold/换址；没有伪造响应或跳过原 cmd/SQL/身份与退出断言。

`bf3267` 静核原 C 依赖：`controlRetired` 仍为原 3 秒严格期限；同两端原流、两次实际 `io.Copy` 返回、实际 Close 返回及 half-close 尾共同参与退出条件，handler 归零本身不再冒充 owner 退役。该观察不修改原 C 方法，也不将取消或强退等同 Read 已 joined。

本人命令：

```sh
cd /workspace/agenteam-runner-control-delivery
PYTHONDONTWRITEBYTECODE=1 python3 -B .agent-state/runner-control-delivery/preparation-controls.py
```

实际 `e8a67e` exit0：18 个既已接受 TCP 纯控制在实际移植源上通过；实际 configuration 1 正/6 负；7 个 observer 格各观察 14 次精确资源身份，含错/缺 top、缺 Wait、残资源与私有目录。控制明确使用内存/文件与资源替身，临时目录实际退出删除，未启动 main、业务子进程、PG/Docker/socket 或访问真实 `/proc`。

两工具仅增加 `^TestRunnerControlDefaultProcesses$` 闭集映射及原 TCP evidence 增量。移除这些增量后全文回到基线；7 个 TCP helper 与作者已审 `ed709367` 逐字相同。旧 configuration 与 input_paths 不变，原 root7/Go6m/540+60+3、PG-only123+3、TCP75、actual Wait/reap/private/runtime/desc/input 尾不变。TCP观察仍只在原100ms间隔内使用有界采样；owner证据不豁免原差集失败，不把末次重读代替此前两个连续空样本。

本轮没有新增通用框架、没有修改作者四源或旧 C/生产、没有重复完整旧业务矩阵。新 Default 是否能在实际原预算内完成，仍由后继 fresh grant 真实窗口证明；本记录不把候选 compile/list 或这些替身控制称为动态验收。
