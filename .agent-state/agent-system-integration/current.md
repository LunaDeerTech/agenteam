# 当前有限组合：新增领域连接准备

- root 已在原 `e028467b` 上按五组来源精确导入64个技术路径，加 cleanup 唯一合成的 `lifecycle_cleanup_test.go` 两行适配，保存为 `bc2bc5ea`。来源为 Human Install `7cf8cd15`、Registry/Builtin `19094bf5`、Tool Runtime `6eb3a62a`、Execution/Agent capture `e9b8fb14`、Skill Agent 安装 `b3e251ad`；不覆盖旧 current、卡片或共享入口，不删除文件。
- content 仅维护本摘要及 `core-checks.py`；cleanup 的上述适配已停写。64路径逐字等于指定 donor，四个旧 Skills34 初始化/测试源逐字保留；cleanup 旧 Agent head/assignment/error 事实与四个拒绝子项完整保留，只新增普通 installation 查询无行分支。没有未解决的源码覆盖冲突。
- 最小入口复用 Tool Runtime `6eb3a62a` 的原 Wait/subreaper/group/runtime 方法，只替换测试集合、包范围及输出目录。一次 race 精确21个新 top（Agent capture3、Execution4、Registry current3、Authorization2、Runtime6、Skill Agent3），随后一次六个产品包及四个契约包 vet；不重跑旧 Agent18、Builtin6 或 Human10。输出固定为 `output/ai/agent-system-integration/combined-core-01/`，须等 root 分配热缓存和执行窗口。
- 已完成无 Go 的有限检查：Python AST、21个真实测试函数唯一存在、28个仓库导入目录存在、原监督方法除上述参数外逐字相同及 diff-check。入口保存后，root 又将 Human PG fixture 的正式 Audit action 常量修复导入，运行来源为 `219053f0`。
- 迁移顺序保持00032→00033→00034→00035→00036→00037→00038→00039。下方旧 schema02 PASS只覆盖32–35；36后继安装尚无成功验收，37–39真实SQL未跑。实际 Execution Tool authority、Snapshot/Model调用证明及标准 Schema validator仍未绑定，Builtin adapter不冒 Registry Source；缺 provider继续明确拒绝，不为组合添加成功替身。此候选不影响独立的 UI/Human HTTP 实际交付。

资源窗口获授后的唯一复现命令：

```sh
python3 -B .agent-state/agent-system-integration/core-checks.py \
  --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
```

首次 `combined-core-01` 原 wholeFAIL（2026-10-10 13:55:35 UTC，session5457→05b684）：outer811324/Go811327 实际Wait1，race14.258s，fresh6,252,564,480B；21top为19PASS/2FAIL，vet未启动。失败均在Skill39：`TestAgentInstallCurrentTransactionAndRecovery` 的publish、publish-revoked、published-recovery于第138/145行返回`DEPENDENCY_UNAVAILABLE`；`TestInstallationExecutionOriginKeepsHumanCompatibility`第210行来源记录roundtrip同码。原组双empty/runtime双empty、adopted空，热缓存已归还。原件在`output/ai/agent-system-integration/combined-core-01/{race.jsonl,result.json}`，不自动重试；cleanup原作者仅针对该来源记录路径定位，其他已通过18top及Skill拒绝top不扩大复验。本结果不是整体通过，也不改变上述真实SQL/未绑定范围。

## 已完成的32–35/schema有限组合

- 原阶段写域：coordination 维护 `tests/projectvariable/agent_configuration_schema_test.go` 与 schema 记录，content 写独立构造 helper，root 负责所有 Git。组合基线 main `7a693cb6` 加四组精确源，生产/迁移不由此测试修改。
- 新 `TestAgentConfigurationSchema` 已形成 1 top/3 sub 源码，覆盖真实连续迁移/重跑、00031 旧事实升级、Audit CHECK 回滚探针、refs/head 拒绝与固定真实构造中的 metadata 同 Tx 正向；Registry 配置及组合 NewService 缺实际 install Backend 均须明确 unbound，不冒完整 Create。
- 测试仍为 1 top/3直接sub；content 的四入口源已冻结，精确 family/实际 observer/逆投影有限独审接受；原预算/资源/Wait/TCP/input 门保留。首次真实结果及唯一夹具返修见下。
- 首次 `compile-01` 原预飞 FAIL：source `5ffab485`、outer740561 exit1，fresh 5,358,837,760 B 低于 5 GiB，commands=[]，零 Go/零 list/无候选。611 编译输入及固定旧 metadata 监督方法初末一致；原件留在 ignored `output/ai/agent-system-integration/compile-01/`，不自动重试。当时 content 在写的入口 Python 不属编译输入；无我方 Go/cache writer。
- root 另授的 `compile-02` 已 wholePASS：race-c 与唯一 top list 原 Wait0，611 编译输入/旧固定监督方法一致、group/desc/runtime 双空。固定候选 `schema-race.test` 46,644,478 B，SHA256 `0ee52a2faac7ef019fe138a9c43c830e99c123211d01990fd1ce182dd824adef`；原容量 FAIL 保留，热缓存已归还。
- `schema-01` 原 wholeFAIL 已全尾释放：升级子项第 71 行 replay 断言失败，另两子项通过；top39.03s，Go/driver/supervisor/outer 原 Wait1，7ID/private/runtime/desc/TCP 双尾及 1271 inputs unchanged 齐。原件位置见本目录 README，不改原失败结论。
- 唯一返修 `bde38042` 拆开 replay 服务错误分支，复用既有 `sameSecretReceipt` 完整值比较，替换深复制的 AuditID 指针地址比较。原 err 未记录，不能回填旧 replay 成功；产品/DDL/预算未改。
- 修后 `compile-03` wholePASS（77610）：race-c/list 原 Wait0、611 输入一致/双尾齐；新 `schema-race-02.test` 46,643,942 B，SHA256 `c229211e93851a9ba105ebe8ef71de31fc32e15a7c7d95b157be2c6c58f0d6b5`。原候选未覆盖。
- `schema-02` 原 wholePASS（39069）：1top3sub23.29s，outer759922/sup759988/Go761799 及 driver 原 Wait0，7ID14absent/private/runtime/desc/HOST_TCP 双尾，1271 输入 unchanged；supervisor119.036s。升级 receipt/后续旧写入/Audit CHECK 均实际经过。日志与完整边界见 README；窗口/cache已归还，无Go/资源在途。
- 下一步按已具备真实依赖继续组合，不重复旧纯矩阵。本次 00032–00035/schema/metadata 通过不外推 00036、完整 F1、生产 initializer、participant/Runtime 或 E01；原两种准备/实际 FAIL 保留。
