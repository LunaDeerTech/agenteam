# D15/D18/D22 单 run-command Operation

状态：2026-10-12 显式 Central run-command 接线及首次真实链已随完整历史 main `4ebde5a7` 交付并推送；原工作树及本地/远端分支现均已实际清理。基础定向 race、vet、PG 编译/list 和 native01 均实际通过；唯一 principal 已完成 native01 whole PASS 的有限独立验收。当前仍为显式构造入口，不开放生产默认绑定。

正式依据：[Model/Tool](d01-contracts/model-tool.md)、[Runner execution-runtime](../../architecture/runner/execution-runtime.md)、[Control 协议](../../architecture/runner/control-protocol.md)及[已交 write-file Operation](d15-runner-write-file-operation.md)。本片只把真实持久 ToolCall 接到一次 command Operation，并保存完整可信调用报告和双流；不消费 Transcript、不启动下一 Model。

## 授权与事务

真实 Execution owner 同 Store、live Tx、完整有序锁借出原保护参数与 current Agent/Tool witness。独立 admission 只在原同步 witness 内核当前 approval_policy=allow 及 ProjectVariable owner 的当前有效环境确实为空；default/auto、非空环境和读取失败不被伪装为空。首次创建、Resolve 与最终携原 RID 的 Claim 使用此门。allow 只跳过审批判断，不跳当前权限、资源、能力和固定规则；配置不是 grant。审批等待和 Secret 环境注入为后续片，普通 Mount 配置不另加 Admin 审批。

created 原提交已知成功且实际返回后才 Resolve；Prepare 只生成稳定 RID；Attempt 保存该 RID 并等待其原提交真实返回，才允许最终 Claim。Claim 的同 Tx 当前授权也实际返回后才进入 WSS，数据库锁不围住网络调用。读、写、命令 authority/ticket/binding 保持独立闭集。

原 Dispatcher 真正等待 session/RID/OP、每流序号与完整 UTF-8 字节数、原 frame/terminal 或连接退役后，Tool 才执行最终事务。`command_results.body_data` 保存原 `RunCommandResult` 报告（最多32KiB），stdout/stderr 各自最多64KiB，独立长度及 SHA；完整 Operation/Attempt/Execution/Project/Agent/RID 绑定与 retired 在同 Tx。迁移70由 Tool contract owner 单写；`report_available` 必须来自可信报告，不接受调用者开关。

wire success 仅表示完整调用报告。exit0/signal0、无取消或 completion code 且 output_complete 才是 Tool success；exit 非零记 `failure/command_exit_nonzero`，自发信号记 `failure/command_signaled`，取消、超时和内部 I/O 按原 flags/code 保留完整报告。SQL failure 映为 phase=failed/outcome=error；取消保持 cancelled；unknown 使用原 failed/outcome=unknown、Attempt unknown。未知或损坏传输不保存伪完整报告。声明的截断和未证全进程树退出不自行改写真实事实。

DB Unknown 持有原 owner/身份/候选，仅 `LookupRunCommand` 观察原最终记录；零重发、不重最终写、无读回推成功。缺行或 running 不证明 rollback。只有原调用实际返回与精确最终候选同时成立才释放最终 Unknown owner。完成后的历史读取仍核原 identity 和真实 current 权限，但不再要求后改的 approval/environment dispatch admission；这些变化不抹已发生报告。Stop/Drain 等待实际调用和读者返回，不把取消当退出。

## 写域与验证边界

recovery_lead 负责 Tool runtime 七个 run_command 产品文件、必要控与本卡；skills_milestone_review 负责 Tool contract/Registry/迁移70，variables_review 负责 ProjectVariable/Execution/Loop，skill_package_review 负责 Mount/Central Runner，独立方法作者负责 PG、方法与恢复 README。Git 及真实执行窗口由 root 统筹，runtime_recovery 为唯一 principal。

基础 `basic01` 精确通过 41 top / 207 sub（34 新 top、7 个受影响旧 top），10 包 vet 实际退出 0，905 项输入前后一致；`compile01` race 编译及唯一 `TestRunCommandOperation` list 实际退出 0。离线方法控制 1 正 7 反通过，未将它计作真实业务运行。方法、输入及原进程记录见[本片恢复说明](../../../.agent-state/runner-command-operation/README.md)。

真实 PG 入口 `TestRunCommandOperation` 的 `committed-attempt-command-and-lookup`、`environment-added-before-claim-no-dispatch` 两场景通过，合计 31.86s。第一场景在原 Attempt 真实 COMMIT 后持住其返回，已持久 RID 期间仍零 wire；放行后真实 argv 子进程 exit23，原认证 WSS 返回完整双流和调用报告，Tool 如实保存 `failure/command_exit_nonzero`（SQL `failed/error`），不是因 wire completed 而判为命令成功。完整报告、两流及原 Attempt 退休同 Tx；最终真实 COMMIT 后受控遮蔽回执形成 Unknown，后续仅 Lookup 原候选，Model 和 wire 各一次。第二场景在 Claim 前由普通 Owner 正式新增值为 `""` 的环境变量；空字符串仍是有效 entry，实际 current environment 门拒绝发送，原请求零 wire、无完整报告。

native01 原工具 `29984` 由执行作者亲收 actual 0；supervisor `894697` actual 0 / 124.193s，原 driver `894704` 与 Go `896357` Wait 0。原同 7 个资源两次 absence 共 14 项，desc/private/runtime 双尾、75s 原自动 HOST_TCP delta 两次 empty，以及 1979 项运行输入前后不变均齐，whole PASS。保留全部原日志、调用与实际终态；没有改变预算、TCP 基线或退出门。

此真实链使用正式 Owner/Registry/Agent/Execution/ProjectVariable/Mount 及 Runner 登记、认证 WSS、Manager 和本地 command owner；Model JSONCaller 仍为受控端口，Project 仍沿用原 test Skills receipt。PG 的 Unknown 是实际 COMMIT 后受控回执丢失，不宣称物理数据库结果未知，也不证明真实 tools HTTP 或完整模型工具循环。

本片不开放 production initializer/default executor/完整 registry，不实现真实 tools HTTP、Approval owner、SecretUsage 注入/masking、下一轮 Model、Transcript 消费、Execution 终态退休或 ManagedProcess；全部原 STOP 不变。

## 主线恢复与清理

受测源码 `3ce4e7c4` 与最终文档提交 `1065b7eb` 均已为main祖先；两份最终文档之外没有改变受测技术输入，合入的44路径与候选逐blob一致。原76文件/96,100,547B/13目录及四份tmp副本已无覆盖保全，20输入图的1,979历史版本main可逐字恢复；原Wait与证据来源限制保持。旧树及本地分支使用普通remove/-d，远端fresh最终tip后精确lease删除，成功空查询与main/ai prune齐；详见[恢复说明](../../../.agent-state/runner-command-operation/README.md)。后继Write结果消费为独立在制片，不回填本片未涵盖的Transcript或下一Model能力。
