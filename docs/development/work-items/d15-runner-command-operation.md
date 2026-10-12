# D15/D18/D22 单 run-command Operation

状态：2026-10-12 在 `/workspace/agenteam-runner-command-operation` 实现显式 Central run-command 接线。当前为源码阶段，尚未运行本片 Go、PG 或 WSS；既有本地 Command 与 WSS 的通过证据不等于本片通过。

正式依据：[Model/Tool](d01-contracts/model-tool.md)、[Runner execution-runtime](../../architecture/runner/execution-runtime.md)、[Control 协议](../../architecture/runner/control-protocol.md)及[已交 write-file Operation](d15-runner-write-file-operation.md)。本片只把真实持久 ToolCall 接到一次 command Operation，并保存完整可信调用报告和双流；不消费 Transcript、不启动下一 Model。

## 授权与事务

真实 Execution owner 同 Store、live Tx、完整有序锁借出原保护参数与 current Agent/Tool witness。独立 admission 只在原同步 witness 内核当前 approval_policy=allow 及 ProjectVariable owner 的当前有效环境确实为空；default/auto、非空环境和读取失败不被伪装为空。首次创建、Resolve 与最终携原 RID 的 Claim 使用此门。allow 只跳过审批判断，不跳当前权限、资源、能力和固定规则；配置不是 grant。审批等待和 Secret 环境注入为后续片，普通 Mount 配置不另加 Admin 审批。

created 原提交已知成功且实际返回后才 Resolve；Prepare 只生成稳定 RID；Attempt 保存该 RID 并等待其原提交真实返回，才允许最终 Claim。Claim 的同 Tx 当前授权也实际返回后才进入 WSS，数据库锁不围住网络调用。读、写、命令 authority/ticket/binding 保持独立闭集。

原 Dispatcher 真正等待 session/RID/OP、每流序号与完整 UTF-8 字节数、原 frame/terminal 或连接退役后，Tool 才执行最终事务。`command_results.body_data` 保存原 `RunCommandResult` 报告（最多32KiB），stdout/stderr 各自最多64KiB，独立长度及 SHA；完整 Operation/Attempt/Execution/Project/Agent/RID 绑定与 retired 在同 Tx。迁移70由 Tool contract owner 单写；`report_available` 必须来自可信报告，不接受调用者开关。

wire success 仅表示完整调用报告。exit0/signal0、无取消或 completion code 且 output_complete 才是 Tool success；exit 非零记 `failure/command_exit_nonzero`，自发信号记 `failure/command_signaled`，取消、超时和内部 I/O 按原 flags/code 保留完整报告。SQL failure 映为 phase=failed/outcome=error；取消保持 cancelled；unknown 使用原 failed/outcome=unknown、Attempt unknown。未知或损坏传输不保存伪完整报告。声明的截断和未证全进程树退出不自行改写真实事实。

DB Unknown 持有原 owner/身份/候选，仅 `LookupRunCommand` 观察原最终记录；零重发、不重最终写、无读回推成功。缺行或 running 不证明 rollback。只有原调用实际返回与精确最终候选同时成立才释放最终 Unknown owner。完成后的历史读取仍核原 identity 和真实 current 权限，但不再要求后改的 approval/environment dispatch admission；这些变化不抹已发生报告。Stop/Drain 等待实际调用和读者返回，不把取消当退出。

## 写域与验证边界

recovery_lead 独占 Tool runtime 七个 run_command 产品文件、必要控与本卡/README；skills_milestone_review 负责 Tool contract/Registry/迁移70，variables_review 负责 ProjectVariable/Execution/Loop，skill_package_review 负责 Mount/Central Runner，独立方法作者负责 PG 与方法。root 负责全部 Git 和真实执行窗口，runtime_recovery 为唯一 principal。

拟 PG 入口 `TestRunCommandOperation` 两场景为 `committed-attempt-command-and-lookup`、`environment-added-before-claim-no-dispatch`：实际 Attempt COMMIT hold-return 期间零 wire，exit23 完整双流与最终真实 COMMIT 丢回执后只 Lookup；Claim 前普通 Owner 新增空字符串环境变量仍是有效 entry，原请求零发送。Model JSONCaller 受控与 Project 原 test Skills receipt 边界保持。源码控与真实 PG 尚未运行，方法和结果见[本片恢复说明](../../../.agent-state/runner-command-operation/README.md)。

本片不开放 production initializer/default executor/完整 registry，不实现真实 tools HTTP、Approval owner、SecretUsage 注入/masking、下一轮 Model、Transcript 消费、Execution 终态退休或 ManagedProcess；全部原 STOP 不变。
