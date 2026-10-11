# Execution RoundInput 的 PG 方法

## 正式交付与原件保全

本片完整开发历史已合入 main `bf7c882a6ef02b726d1648903110467fa9c839e8`，root 原推送工具 60964 实际退出 0。最终技术组合为 `44ab6534`，最终文档 `f1fe5484` 可从 main 恢复。basic01 原 FAIL、basic02 分段补验及 compile01/native01 原 PASS 均保留，有限验收范围见[本卡](../../docs/development/work-items/d22-execution-round-input.md)。

原 `output/ai/execution-round-input/` 已用同文件系统 `renameat2(RENAME_NOREPLACE)` 无覆盖迁至 main `output/ai/retired-20261011/execution-round-input/`：81 个文件、95,510,955 字节，迁前后全部路径、SHA256、size、inode、device、mode 及目录身份相同。完整 87,745,702 字节 binary、所有输入、FAIL/PASS、原 Wait 和薄记录器均在。原 `/tmp/eri01/pg-8e344109435c413a85b030440352f6b4` 的 log/request/owned 三份小原件以 exclusive create 另存 `retention/external/eri01/`，共 6,923 字节，SHA/size 核合，原 `/tmp` 未改或删除。实际证明见新根的 `retention/{manifest-before.json,manifest-after.json,external-copies.json,source-history.json,result.json,tool-terminal.json}`。下文路径保留原运行含义，恢复时按上述根映射，不改写旧原件。

四轮实际输入已逐 Git blob 核合：basic01、basic02 各 730 个仓库内文件分别对应 `67cde809`、`71b13d5c`；compile01/native01 各 1,884 个对应 `44ab6534`。四个对应 wrapper 的原 SHA 也全部一致；这些历史及最终任务提交均为上述正式 main 的祖先，未发现未提交 FAIL 源码缺口，无需重造补丁。固定 Go/MinIO、SkillHTTP 热 cache、SkillsMilestoneResume 只读 modules 不搬移，不声称在 Git 或本输出内。

原执行者已亲收全部工具 Wait；native01 的 30712 实际 0、七资源十四次 absence、所有退出双尾及输入门闭合。定向原 PID 的被动检查未见保留，不代替全系统进程可见性。NextRoundProjection 的基础作者、PG 作者及其七个实际方法/PG 文件均确认无旧 68 绝对运行路径依赖，后继使用自身树源码及既有受保护外部依赖。保全后旧树正常状态及 ignored 清单均为空。保全文档已随 main `c34ba3b5` 推送，原工具 50299 实际退出 0；root 核定最终任务 tip `f1fe5484` 为 main 祖先后，普通 worktree remove 与 branch -d 均实际退出 0（工具 chunk `9dc6b3`）。fresh 远端精确查询工具 37217 已实际退出 0 且结果为空，本任务分支从未推送，故没有远端删除；本次只增加树与本地分支。旧树和本地分支清理完成，累计为 33 个 WorkTree、34 个本地分支、63 个远端引用，legacy 147、显式 keep 1 和全部 STOP 保持。完整历史、原 FAIL 和上述 retired 路径不变；恢复不再进入已删除旧树。

NextRoundProjection 没有新增迁移，仍使用原 00066–00068；它不依赖旧树路径不等于其 PG 已通过。

## 原 PG 方法

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

本次 `profile01` 原 Python PID 775973 实际 Wait 0（0.405s），1 正例及 7 反例符合预期；五个 Python 方法文件语法通过、前后输入相同，薄记录器与 67 原文件逐字一致。该离线阶段没有运行 Go 编译、精确 list 或真实 PG；后继 compile01/native01 已实际通过并获有限独立接受，详见本卡。

## 独立编译与原生运行准备

以下命令只有在 root 分配对应资源窗、全部实际输入冻结后执行；输出目录必须不存在。

```sh
python3 -B .agent-state/execution-round-input/run-pg-checks.py compile --output output/ai/execution-round-input/compile01
python3 -B .agent-state/execution-round-input/run-pg-checks.py prepare --compile output/ai/execution-round-input/compile01 --output output/ai/execution-round-input/native01 --namespace /tmp/eri01
```

`compile` 仅原 `-race -tags=integration -c -p=1` 与精确唯一 top 的 list，实际 `Popen.wait` 后保存结果。`prepare` 只复核成功编译/list、binary 与原闭包，生成 fresh namespace 配方，不启动 native、检查 TCP 或创建服务。`record-native.py` 逐字复用 67 的薄记录器：原输入复核后启动唯一 supervisor 并实际 Wait，不拥有清理、资源判据或附加重试。

此处保留原准备方法；实际 compile01 和独立授窗的 native01 已通过，后继复现仍须 root 另授窗口，不从方法准备推定新的运行通过。原 67 的失败与修后验证记录不在本目录改写。
