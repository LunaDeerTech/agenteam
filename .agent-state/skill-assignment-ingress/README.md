# Skill Assignment ingress 的 PG 方法

本片沿用原 [PG supervisor](../task-planning-recovery/pg_only_supervisor.py) 与 [root chain driver](../work-owner-http/root_chain_driver.py)，只新增 metadata 字典项。精确 selector 为 `^TestSkillAssignmentIngress$`，目标包 `tests/projectvariable`，必须实际执行一个 top 和两个 sub：

- `TestSkillAssignmentIngress/active-assignment-and-current-replay`
- `TestSkillAssignmentIngress/rollback-commit-loss-and-no-active-history`

`TARGETS` 由原 `METADATA_INPUTS` 推导，无需新增分派函数。编译阶段按实际 PG package 的依赖收集输入，运行阶段沿原 `metadata_inputs` 收集二进制、完整同包 fixture、实际源码和迁移等输入；上述显式 profile 文件不是完整依赖闭包，不能用它们代替实际 compile/list 与输入绑定。PG fixture 已通过 compile03/list 与 native02 整轮，唯一主审已给有限 wholePASS 接受；原 compile01 和 native01 FAIL 保留。首链只到真实 Launch created / Model0，不包含完整 Preparing/capture 真 PG、下一轮或默认生产绑定。

保持原 PG 540 秒主体加 60 秒清理预算、Go 6 分钟 timeout、七项精确资源及真实 Wait、private/runtime/desc 双尾、75 秒自动 HOST_TCP baseline delta 连续两次为空及输入不变门。不得用业务 PASS 代替外层实际退出，不变更基线、过滤连接或自动重试；既有 STOP 与默认生产未绑定保持。

离线控制只读取固定基线与当前两份 Python harness，不启动 Go、PG、Runner 或其他服务：

```sh
python3 -B .agent-state/skill-assignment-ingress/profile-controls.py
```

该控制剥除唯一新增 selector 后比较两份完整 AST，并逐项保持旧 `METADATA_INPUTS`、`TARGETS` 和 `METADATA_GROUPS`；随后直接调用原 `metadata_results`，验证一个正常结果和七个拒绝结果：缺 sub RUN、FAIL、非零实际 Wait、额外 case、重复 Wait、错误 Wait selector、末尾整体 FAIL。实际命令、进程 Wait、限定输入与原输出保存在忽略目录 `output/ai/skill-assignment-ingress/checks/profile01/`，不将离线构造日志称为真实 PG。

本次 `profile01` 实际进程 Wait 0（PID 752037，0.454 秒）：1 正例接受、7 反例拒绝，两份旧 AST/有效映射保持，3 项方法输入前后逐项相同，窄 profile 已限定接受。PG 编译与首次运行结果另列如下，不以该离线控制代替。

产品基础按原结果分段接受：basic01 工具 82378 / Go 746836 实际 exit 1（145.503s），32 top / 141 sub PASS，但 Mount 测试 double 缺 Query 编译失败，vet 未执行；修后 basic02 工具 39722 实际 exit 0，Go 751539 的 Mount 1 top / 9 sub（4.657s）与 Go 751635 的原 11 包 vet（75.608s）通过。每次 918 个实际 Go 输入前后相等，跨次仅 Mount 测试差额，合计 33 top / 150 sub，不是单轮全绿。原 FAIL 完整保留。两项 Skill decoder 静态 must-fix 的修后 16 子例包含在已通过的 basic01 中，没有虚构修前动态失败。capture 控制只承受控 SQL/历史兼容范围，不据此宣称完整 Preparing/capture 真实 PG 已通过。

## PG 编译与原生运行

`compile01` 原工具 17832 实际 exit 1，Go 757854 Wait 1 / 59.695s，1868 输入前后相同。新 PG fixture 的两处类型接线错误为 `SecretVariable.ID` 不存在及 `sc.Skill` 未定义；未进入 list、未生成 binary。`20fee0c4` 仅改为 `SecretVariable.Fields().ID`、正式 `pc.Skill` alias 并加入 import。修后 `compile02` 原工具 29133 实际 exit 0：Go 759908 Wait 0 / 22.383s，list 760191 Wait 0 / 1.072s，唯一 `TestSkillAssignmentIngress`。1868 输入在编译前、编译后及 list 后相同，跨轮仅该 PG 文件变化；87,037,290 字节二进制与原输入绑定见 `compile02/binary.json`。原 compile01 FAIL 不回填。

`native01` 使用固定修后输入，运行闭包为 1869 项（1868 编译输入加实际 binary），包含 82 个同包 Go 文件及全部迁移。原工具 53013 亲收实际 exit 1，supervisor 761393 Wait 1 / 118.399s，原日志 terminal 1 / 116.946s。一个 top、两个 sub 均 FAIL：总计 26.04s，两个 sub 分别 11.82s、14.21s。第一 sub 在原 `AssignSkill` 返回后检查错误时报 `DEPENDENCY_UNBOUND`，尚未进入成功 receipt 的断言；第二 sub 在预期 final callback 真回滚的复合断言处同报 `DEPENDENCY_UNBOUND`。不能据复合日志拆出每个内部布尔值，也不能计 assignment、回滚后验或 Unknown 恢复通过。

Go 763094 与 driver 761416 的原 Wait 均为 1；七个相同资源 ID 的十四次 absence、desc/private/runtime 双尾、自动 HOST_TCP delta 两次 empty 与 1869 输入不变门均齐。整轮因业务失败仍为 FAIL；资源/cache 窗口已释放，没有改门或重试。原件在 `output/ai/skill-assignment-ingress/{compile01,compile02,native01}/`；本轮原日志为 `/tmp/sai01/pg-df4ccc67dd4a4b4fb39d3b3918c72f35.log`，实际工具终态分别保存在各目录的 `tool-terminal.json`。

独立审查静态定位了产品接线缺口：Project 的 `Discover` / `ValidateInTx` producer 闭集遗漏 Skill。原产品作者在 `6151c756` 窄修该 gate，保留精确当前授权与 typed fact 检查；该代码归因不回填 native01 没有记录的内部阶段。basic03 原工具 24439 实际 0：Go 769447 的新增 Project 2 top / 19 sub race 通过（13.154s），Go 769721 的 Project 与 contract 两包 vet 通过（3.747s）；456 个实际输入含 00067，各阶段前后相同。它补充此前 33 top / 150 sub 和 11 包 vet，不重写前轮失败。

compile03 原工具 88871 实际 0，Go 770353 Wait 0 / 33.698s，list 770888 Wait 0 / 1.098s，唯一 top 不变。1870 输入在编译前后及 list 后相同；相对 compile02 只新增 Project authority 与其测试、修改原 events.go，其余不变。实际 binary 为 87,056,060 字节，身份见 `compile03/binary.json`；native02 的 1871 项闭包只追加该 binary，仍含 82 个同包 Go 文件及全部迁移。

native02 原工具 91341 亲收实际 exit 0，supervisor 772115 Wait 0 / 114.126s，原日志 terminal 0 / 113.205s。一个 top、两个 sub 全 PASS：23.85s（12.12s / 11.73s）；Go 773688、driver 772116 原 Wait 均为 0。七个相同资源 ID 的十四次 absence、desc/private/runtime 双尾、原 75 秒门内自动 HOST_TCP delta 两次 empty、1871 输入不变及 metadata 实际 case/Wait 门全部齐。原日志 `/tmp/sai02/pg-c18b9e6796504edd9e6faa56f814c970.log` 与 `native02/{outer.log,terminal.json,tool-terminal.json}` 均保留，资源窗口已释放。

唯一主审已亲核原件并接受本片有限 wholePASS：正式 Owner Assign / Remove、四引用 owner 同 Tx 版本维护、真实 typed Audit/Outbox、queued 与历史路由、final 真回滚，以及实际 Committed 后受控丢回执、只 Lookup 原事实。真实链止于 created / Model0，使用显式领域装配和 Project 持久 test Skills receipt；不代表生产 Project initializer、默认绑定、完整 Preparing/capture、00068/DetermineRoundInput、下一 Model 或 Transcript 应用。basic01、compile01、native01 的实际 FAIL 完整保留，已有 STOP 不变。
