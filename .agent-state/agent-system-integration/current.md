# 当前有限组合：37–39真实事务通过

- 当前新候选：`TestAgentRuntimeSchema`，1 top / 4 直接 sub，唯一新增 `tests/projectvariable/agent_runtime_schema_test.go`。范围为36→39/fresh/repeat、Runtime attempt/terminal 与父身份约束、Execution active slot/immutable/cancel-wins及真实公共 Launch 缺 Trigger 拒绝、39后真实 Human 安装与 Agent/Human 来源互斥。SQL合成材料只在强制回滚事务内，不被业务服务消费作授权；不冒真实 Agent/Trigger/ToolCall/Snapshot 正向。
- 基线为 `2045c8bf` 的已验21top有效补集；00038 的text/uuid复合FK不兼容于源静读发现，原owner在 `87a92951` 改自有safe_id为严格UUIDv7 text domain，root只导入该迁移。原FK/regex/Go均保留；这是已修源码问题，不存在可回填的PG失败或通过。
- 新入口只增既有metadata/schema family的selector、必需测试源与exact expected set，原7resources/6m/540+60+3/Wait/TCP/input尾方法不变。受影响现control共7方法实际0，gofmt/Python AST/diff-check完成；source `db058002` 的首次新候选编译/精确列举及首次PG均已通过，未重旧pure或schema02。
- `runtime-schema-compile-01`：session70554→a08e7f，compile839889 Wait0/10.518s、唯一top list840103 Wait0/1.068s，outer839880 exit0；647编译输入初尾一致，原group/desc/runtime双空。新候选`runtime-schema-race-01.test`为49,539,466B，SHA256 `c63e4626f9cb04ca9d312956a07e549d7ba0671edb5a01335dcfce03097944dc`。
- `runtime-schema-01` 原wholePASS：session65794→885772，UTC14:24:38–14:26:49，1top4sub共34.15s（10.29/3.26/10.37/10.23s），Go842245、driver、sup840415及outer840369原Wait0。七ID14absent、private/runtime/desc及HOST_TCP双空、outer无adopted/survivor，1321 runtime输入初尾一致；sup128.903s。完整结果见README，窗口与热cache已归还。此轮接受真实37–39约束、公共缺依赖拒绝与39后Human兼容；rollback材料不证明真实Launch/Runtime writer/Agent安装授权，Snapshot和完整F1仍未绑定。

- root 已在原 `e028467b` 上按五组来源精确导入64个技术路径，加 cleanup 唯一合成的 `lifecycle_cleanup_test.go` 两行适配，保存为 `bc2bc5ea`。来源为 Human Install `7cf8cd15`、Registry/Builtin `19094bf5`、Tool Runtime `6eb3a62a`、Execution/Agent capture `e9b8fb14`、Skill Agent 安装 `b3e251ad`；不覆盖旧 current、卡片或共享入口，不删除文件。
- content 仅维护本摘要及 `core-checks.py`；cleanup 的上述适配已停写。64路径逐字等于指定 donor，四个旧 Skills34 初始化/测试源逐字保留；cleanup 旧 Agent head/assignment/error 事实与四个拒绝子项完整保留，只新增普通 installation 查询无行分支。没有未解决的源码覆盖冲突。
- 最小入口复用 Tool Runtime `6eb3a62a` 的原 Wait/subreaper/group/runtime 方法，只替换测试集合、包范围及输出目录。一次 race 精确21个新 top（Agent capture3、Execution4、Registry current3、Authorization2、Runtime6、Skill Agent3），随后一次六个产品包及四个契约包 vet；不重跑旧 Agent18、Builtin6 或 Human10。输出固定为 `output/ai/agent-system-integration/combined-core-01/`，须等 root 分配热缓存和执行窗口。
- 已完成无 Go 的有限检查：Python AST、21个真实测试函数唯一存在、28个仓库导入目录存在、原监督方法除上述参数外逐字相同及 diff-check。入口保存后，root 又将 Human PG fixture 的正式 Audit action 常量修复导入，运行来源为 `219053f0`。
- 迁移顺序保持00032→00033→00034→00035→00036→00037→00038→00039。下方旧 schema02 PASS只覆盖32–35；当时36后继安装与37–39真实SQL尚待。后继Human正式交付及本轮37–39结果分别记录，均不回填旧阶段。实际 Execution Tool authority、Snapshot/Model调用证明及标准 Schema validator仍未绑定，Builtin adapter不冒 Registry Source；缺 provider继续明确拒绝，不为组合添加成功替身。此候选不影响独立的 UI/Human HTTP 实际交付。

资源窗口获授后的唯一复现命令：

```sh
python3 -B .agent-state/agent-system-integration/core-checks.py \
  --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
```

首次 `combined-core-01` 原 wholeFAIL（2026-10-10 13:55:35 UTC，session5457→05b684）：outer811324/Go811327 实际Wait1，race14.258s，fresh6,252,564,480B；21top为19PASS/2FAIL，vet未启动。失败均在Skill39：`TestAgentInstallCurrentTransactionAndRecovery` 的publish、publish-revoked、published-recovery于第138/145行返回`DEPENDENCY_UNAVAILABLE`；`TestInstallationExecutionOriginKeepsHumanCompatibility`第210行来源记录roundtrip同码。原组双empty/runtime双empty、adopted空，热缓存已归还。原件在`output/ai/agent-system-integration/combined-core-01/{race.jsonl,result.json}`，不自动重试；cleanup原作者仅针对该来源记录路径定位，其他已通过18top及Skill拒绝top不扩大复验。本结果不是整体通过，也不改变上述真实SQL/未绑定范围。

cleanup 已确定并修正唯一测试 helper：`installationValues` 原把零 User 的全零 UUID 字符串模拟成数据库值，而正式 SQL 的 `COALESCE(actor_user_id::text,'')` 在 Agent 分支返回空串。00039要求该分支 User 为 NULL，产品 scanner 拒绝非空 User 正确。`install_read_test.go` 仅 +7/-1，使零 User 为空串、非零 Human 保原值；有限差额审接受，产品/DDL/断言不变，修后定向结果如下。

修后只在新授权窗口用上述命令追加 `--profile repair`：输出 `combined-core-02/`，只执行 `TestAgentInstallCurrentTransactionAndRecovery` 与 `TestInstallationExecutionOriginKeepsHumanCompatibility` 两top/3sub的 Skill race，随后执行原未运行的10pkg vet。原默认21top入口及原01 FAIL保留；19个通过top复用。profile 的 AST、旧结果语义、精确两top的正常/缺失/多项集合检查已通过，无 Go/资源；原预算、实际Wait和组/runtime退出尾未改变。

`combined-core-02` 修后原 wholePASS（source `4d387887`，2026-10-10 14:02:13 UTC起，session14891→e281c9）：Skill两top/3sub race818825 Wait0/8.065s，原10pkg vet819008 Wait0/10.295s，outer818820实际exit0。两阶段fresh分别6,142,386,176/6,129,848,320B，原组/runtime各双empty、adopted空，热缓存已归还。有效有限覆盖为原19top PASS加修后2top PASS及10pkg vet PASS，未重复整21项；原01 wholeFAIL保持。结果位于`output/ai/agent-system-integration/combined-core-02/{race.jsonl,vet.jsonl,result.json}`。仅受控领域接口与组合编译通过，不代表37–39真实SQL、真实Execution/Tool调用链、标准validator或完整F1已绑定。

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
