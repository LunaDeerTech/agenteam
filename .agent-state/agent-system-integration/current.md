# 当前有限组合

- 写域：coordination 仅 `tests/projectvariable/agent_configuration_schema_test.go` 和本目录两份记录；content 写独立构造 helper，root 负责所有 Git。组合基线 main `7a693cb6` 加四组精确源，生产/迁移不由此测试修改。
- 新 `TestAgentConfigurationSchema` 已形成 1 top/3 sub 源码，覆盖真实连续迁移/重跑、00031 旧事实升级、Audit CHECK 回滚探针、refs/head 拒绝与固定真实构造中的 metadata 同 Tx 正向；Registry 配置及组合 NewService 缺实际 install Backend 均须明确 unbound，不冒完整 Create。
- 测试仍为 1 top/3直接sub；content 的四入口源已冻结，精确 family/实际 observer/逆投影有限独审接受；原预算/资源/Wait/TCP/input 门保留。首次真实结果及唯一夹具返修见下。
- 首次 `compile-01` 原预飞 FAIL：source `5ffab485`、outer740561 exit1，fresh 5,358,837,760 B 低于 5 GiB，commands=[]，零 Go/零 list/无候选。611 编译输入及固定旧 metadata 监督方法初末一致；原件留在 ignored `output/ai/agent-system-integration/compile-01/`，不自动重试。当时 content 在写的入口 Python 不属编译输入；无我方 Go/cache writer。
- root 另授的 `compile-02` 已 wholePASS：race-c 与唯一 top list 原 Wait0，611 编译输入/旧固定监督方法一致、group/desc/runtime 双空。固定候选 `schema-race.test` 46,644,478 B，SHA256 `0ee52a2faac7ef019fe138a9c43c830e99c123211d01990fd1ce182dd824adef`；原容量 FAIL 保留，热缓存已归还。
- `schema-01` 原 wholeFAIL 已全尾释放：升级子项第 71 行 replay 断言失败，另两子项通过；top39.03s，Go/driver/supervisor/outer 原 Wait1，7ID/private/runtime/desc/TCP 双尾及 1271 inputs unchanged 齐。原件位置见本目录 README，不改原失败结论。
- 唯一返修 `bde38042` 拆开 replay 服务错误分支，复用既有 `sameSecretReceipt` 完整值比较，替换深复制的 AuditID 指针地址比较。原 err 未记录，不能回填旧 replay 成功；产品/DDL/预算未改。
- 修后 `compile-03` wholePASS（77610）：race-c/list 原 Wait0、611 输入一致/双尾齐；新 `schema-race-02.test` 46,643,942 B，SHA256 `c229211e93851a9ba105ebe8ef71de31fc32e15a7c7d95b157be2c6c58f0d6b5`。原候选未覆盖。
- `schema-02` 原 wholePASS（39069）：1top3sub23.29s，outer759922/sup759988/Go761799 及 driver 原 Wait0，7ID14absent/private/runtime/desc/HOST_TCP 双尾，1271 输入 unchanged；supervisor119.036s。升级 receipt/后续旧写入/Audit CHECK 均实际经过。日志与完整边界见 README；窗口/cache已归还，无Go/资源在途。
- 下一步按已具备真实依赖继续组合，不重复旧纯矩阵。本次 00032–00035/schema/metadata 通过不外推 00036、完整 F1、生产 initializer、participant/Runtime 或 E01；原两种准备/实际 FAIL 保留。
