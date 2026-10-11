# Execution RoundInput 的 PG 方法

本片复用原 [PG supervisor](../task-planning-recovery/pg_only_supervisor.py) 与 [root chain driver](../work-owner-http/root_chain_driver.py)，仅新增 metadata 字典项。精确 selector 为 `^TestExecutionRoundInput$`，目标包为 `tests/projectvariable`，必须实际执行一个 top 和两个 sub：

- `TestExecutionRoundInput/assignment-first-replacement-and-replay`
- `TestExecutionRoundInput/rollback-commit-loss-and-determination-first`

`TARGETS` 继续由原 `METADATA_INPUTS` 推导。显式 profile 文件不是完整依赖闭包；compile/list 和 runtime 均沿原 `metadata_inputs` 收集完整同包 fixture、实际 internal 源码、迁移及原工具依赖，并核对输入前后相同。迁移 `00068_execution_round_inputs.sql` 必须在实际输入内，编译成功后只允许原输入加真实 binary 的运行集合。

首链由独立 PG fixture 复用真实已提交 ToolResult 消费、正式 Owner Skill Remove/Assign 与同事务 ingress，核对 cutoff、replacement、重放、整体回滚与提交丢回执恢复。继承 fixture 的原一次受控 Model 和真实 Runner WSS/I/O 不等于新增 Model 调用；Determine 阶段不得新增 Model 或 Tool dispatch。下一轮请求投影/Model、生产默认与既有 STOP 保持，源码或编译成功不算 PG 业务通过。

原门保持：PG 主体 540 秒加 60 秒清理、Go 6 分钟 timeout、七个精确资源及实际 Wait、private/runtime/desc 双尾、75 秒自动 HOST_TCP baseline delta 连续两次为空、原运行输入不变。不得手工过滤连接、替换 baseline、自动重试或用业务 PASS 代替完整外层退出。

## 离线 profile 控制

```sh
python3 -B .agent-state/execution-round-input/profile-controls.py
```

控制以固定 `67cde809` 为方法基底，剥除唯一新增 selector 后比较两份完整 AST，并保持旧 `METADATA_INPUTS`、`TARGETS`、`METADATA_GROUPS`。直接调用原 `metadata_results` 验证 1 个正常构造结果及 7 个拒绝结果：缺 sub RUN、FAIL、非零实际 Wait、额外 case、重复 Wait、错误 selector 和整体 FAIL。离线控制不启动 Go、PG、Runner 或任何服务；原输出及限定输入前后记录放在忽略目录 `output/ai/execution-round-input/checks/profile01/`，不把构造日志当真实 PG。

本次 `profile01` 原 Python PID 775973 实际 Wait 0（0.405s），1 正例及 7 反例符合预期；五个 Python 方法文件语法通过、前后输入相同，薄记录器与 67 原文件逐字一致。Go 编译、精确 list 和真实 PG 均未运行。

## 独立编译与原生运行准备

以下命令只有在 root 分配对应资源窗、全部实际输入冻结后执行；输出目录必须不存在。

```sh
python3 -B .agent-state/execution-round-input/run-pg-checks.py compile --output output/ai/execution-round-input/compile01
python3 -B .agent-state/execution-round-input/run-pg-checks.py prepare --compile output/ai/execution-round-input/compile01 --output output/ai/execution-round-input/native01 --namespace /tmp/eri01
```

`compile` 仅原 `-race -tags=integration -c -p=1` 与精确唯一 top 的 list，实际 `Popen.wait` 后保存结果。`prepare` 只复核成功编译/list、binary 与原闭包，生成 fresh namespace 配方，不启动 native、检查 TCP 或创建服务。`record-native.py` 逐字复用 67 的薄记录器：原输入复核后启动唯一 supervisor 并实际 Wait，不拥有清理、资源判据或附加重试。

后继真实 native 须另获独占窗口；本次方法准备不宣称 Go 编译、PG 业务或整轮退出通过。原 67 的失败与修后验证记录不在本目录改写。
