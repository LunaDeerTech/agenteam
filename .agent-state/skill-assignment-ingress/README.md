# Skill Assignment ingress 的 PG 方法

本片沿用原 [PG supervisor](../task-planning-recovery/pg_only_supervisor.py) 与 [root chain driver](../work-owner-http/root_chain_driver.py)，只新增 metadata 字典项。精确 selector 为 `^TestSkillAssignmentIngress$`，目标包 `tests/projectvariable`，必须实际执行一个 top 和两个 sub：

- `TestSkillAssignmentIngress/active-assignment-and-current-replay`
- `TestSkillAssignmentIngress/rollback-commit-loss-and-no-active-history`

`TARGETS` 由原 `METADATA_INPUTS` 推导，无需新增分派函数。编译阶段按实际 PG package 的依赖收集输入，运行阶段沿原 `metadata_inputs` 收集二进制、完整同包 fixture、实际源码和迁移等输入；上述显式 profile 文件不是完整依赖闭包，不能用它们代替实际 compile/list 与输入绑定。PG fixture 已实际落真实装配源码，两个方法体继续实施，尚未编译/list/native，本说明不构成业务或资源验收。首链只到真实 Launch created / Model0，不追加完整 Preparing/capture 真 PG。

保持原 PG 540 秒主体加 60 秒清理预算、Go 6 分钟 timeout、七项精确资源及真实 Wait、private/runtime/desc 双尾、75 秒自动 HOST_TCP baseline delta 连续两次为空及输入不变门。不得用业务 PASS 代替外层实际退出，不变更基线、过滤连接或自动重试；既有 STOP 与默认生产未绑定保持。

离线控制只读取固定基线与当前两份 Python harness，不启动 Go、PG、Runner 或其他服务：

```sh
python3 -B .agent-state/skill-assignment-ingress/profile-controls.py
```

该控制剥除唯一新增 selector 后比较两份完整 AST，并逐项保持旧 `METADATA_INPUTS`、`TARGETS` 和 `METADATA_GROUPS`；随后直接调用原 `metadata_results`，验证一个正常结果和七个拒绝结果：缺 sub RUN、FAIL、非零实际 Wait、额外 case、重复 Wait、错误 Wait selector、末尾整体 FAIL。实际命令、进程 Wait、限定输入与原输出保存在忽略目录 `output/ai/skill-assignment-ingress/checks/profile01/`，不将离线构造日志称为真实 PG。

本次 `profile01` 实际进程 Wait 0（PID 752037，0.454 秒）：1 正例接受、7 反例拒绝，两份旧 AST/有效映射保持，3 项方法输入前后逐项相同，窄 profile 已限定接受。未运行 PG 编译、list 或 native。

产品基础按原结果分段接受：basic01 工具 82378 / Go 746836 实际 exit 1（145.503s），32 top / 141 sub PASS，但 Mount 测试 double 缺 Query 编译失败，vet 未执行；修后 basic02 工具 39722 实际 exit 0，Go 751539 的 Mount 1 top / 9 sub（4.657s）与 Go 751635 的原 11 包 vet（75.608s）通过。每次 918 个实际 Go 输入前后相等，跨次仅 Mount 测试差额，合计 33 top / 150 sub，不是单轮全绿。原 FAIL 完整保留。两项 Skill decoder 静态 must-fix 的修后 16 子例包含在已通过的 basic01 中，没有虚构修前动态失败。capture 控制只承受控 SQL/历史兼容范围，不据此宣称真实 PG 已通过。
