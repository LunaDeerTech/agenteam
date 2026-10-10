# Agent 连续 schema 首次组合

基线为正式 main `7a693cb6`，root 精确导入 Agent/00032、Registry/00033、Skills 初始化/00034、Mount/00035 与已通过纯检查的 References；content 唯一维护固定构造 helper，coordination 唯一维护本测试及本目录记录。产品源、公共 fixture 和迁移不由本测试修改。

首问题：真实 PostgreSQL 能否按正式 Migrator 应用连续 00001–00035、从带真实业务数据的 00031 升级，并保持旧事实/Audit 契约，同时执行已具备的 metadata 提供方。精确选择器为 `^TestAgentConfigurationSchema$`，一个 top、三个直接 sub：

- `fresh-prefix-and-repeat`：空库完整前缀、journal/Goose 及约束落地、重复运行、15 张新配置表零事实。
- `upgrade-preserves-facts-and-audit-checks`：真实 Account/Project/ordinary/Secret 服务生成 00031 数据，升级与重跑后原事实、读取、命令 receipt 保持；升级后旧 ordinary/Secret 仍能正式追加。Agent create/update 的 Audit SQL tuple 只作原事务内显式回滚的 CHECK 探针，错误版本/空字段/多余材料字段/未知动作须命中指定 CHECK。
- `schema-invariants-and-unbound-dependencies`：通过 `tests/testsupport/agentconfiguration/assembly.go` 固定真实构造，在同 Store 原 Tx 调 Model Selection/Secret Directory，读后持久计数不变；Registry 配置口及组合 `NewService` 缺安装 source 时均 `DependencyUnbound`，新配置表为空。非法 canonical 名称、孤儿 refs、非法 head version/sequence 与真实 deferred assignment FK 均拒绝；异常接受也由原事务回滚，不能作为后继正向数据。

禁止 SQL 种一个有效 Agent、伪造 owner witness、用 fake install Backend/空 catalog 通过 Create。沿用 Project fixture 的持久测试 Skills 初始化已明确披露；它不证明生产 initializer 或 F1。首次创建/版本更新/各域 refs 与 assignment 同事务提交、真实安装发布和完整生命周期仍未验。00036 不在本次范围；意外混入的后续迁移使精确前缀检查失败。

只复用现有真实 PG fixture、root-chain 的原阶段/预算和资源退出尾，不创建新 wrapper。content 已冻结 Schema family 的两共享入口与两纯控制源；coordination 只读逆投影 driver 4/supervisor 9 个精确增量后整字节回 main `7a693cb6`，实际 observer/初末 input/原资源门有限审接受。新6与旧metadata6离线控制是作者结果，不冒独立动态或 SQL 验证；运行闭包、固定依赖预飞及 root 唯一实际窗口仍须另行就绪。

首次候选准备 `compile-01` 在原容量门前停止：source `5ffab485`，2026-10-10 12:55:15 UTC、outer740561 实际 exit1，fresh 5,358,837,760 B 低于 5 GiB；零 Go/零 list、未生成候选，保留 FAIL，不自动重试。611 项实际 Go/import/embed/mod/tool 输入及方法初末一致；编译不包含可并行修改的入口 Python、README/current。ignored `output/ai/agent-system-integration/compile-01-launcher.py` 复用已验 metadata compile03 的原 Wait/descendant/group/runtime 方法，固定旧 `01157924` supervisor；原结果与输入在同级 `compile-01/`。原资源方法未因本次缺容量放宽；当前没有本轮 Go/cache writer。

root 定向释放容量后另授一次 `compile-02`，同 Go 源/611 编译输入实际 PASS：session83580、outer742193/compile742197/list743141 原 Wait0，race-c 40.661s、exact list 1.069s，仅 `TestAgentConfigurationSchema`；两阶段 group/desc/runtime 双空，输入与固定旧监督方法未变。候选 `output/ai/agent-system-integration/schema-race.test` 为 46,644,478 B，SHA256 `0ee52a2faac7ef019fe138a9c43c830e99c123211d01990fd1ce182dd824adef`。原件在 `compile-02/`；没有执行正文/PG，不回填原容量 FAIL，热缓存已归还。

首次真实 `schema-01` 为 wholeFAIL：fresh/repeat 与 schema/unbound 两子项通过，升级子项在原第 71 行的 Secret replay 断言失败；top 39.03s。session10916、outer748638/supervisor748725/driver748748/Go750984 原 Wait1，7 个资源的双退役、private/runtime/desc/TCP 双空及 1271 输入不变全部闭合，窗口已释放。原日志 `/tmp/acs01/pg-ff1a7622a24e4aa298da0364423d6066.log`，安全外层结果为 `output/ai/agent-system-integration/schema-01-control/result.json`。两个子项通过不替代整轮通过。

确定的夹具缺陷是比较 `Fields().AuditID` 指针地址；该契约每次返回深复制，正确的非空 receipt 也会比较不等。返修只将错误分支单独检查，并复用既有 `sameSecretReceipt` 比较完整安全 receipt 值；产品、DDL、原场景与预算未改。原复合断言未记录服务 `err`，不回填旧 replay 成功或断言这是旧轮唯一运行缺口。该返修先保存为 `bde38042`，再另授以下新候选与实际轮次；旧候选和原 FAIL 保留。

修后 `compile-03` 原 wholePASS：session77610、outer759460/compile759464/list759618 原 Wait0，611 编译输入/方法初末一致及 group/desc/runtime 双空。新候选 `output/ai/agent-system-integration/schema-race-02.test` 为 46,643,942 B、SHA256 `c229211e93851a9ba105ebe8ef71de31fc32e15a7c7d95b157be2c6c58f0d6b5`，不覆盖原失败输入。紧接的 `schema-02` 原 wholePASS：session39069、outer759922/supervisor759988/Go761799 原 Wait0，driver 原 actual wait 为 true/code0；1 top/3 sub 共 23.29s（3.31/11.15/8.83s）。7ID 的 14 次 absent、private/runtime/desc 双空、HOST_TCP 双空与 1271 输入不变全部闭合，supervisor119.036s，外层 UTC13:13:37–13:15:38。原件为 `/tmp/acs02/pg-6ee5d8c51c984a56814549cca1134d31.log` 和 `output/ai/agent-system-integration/schema-02-control/result.json`。升级子项实际完成原 receipt 值重放、旧 producer 追加及 Audit CHECK；仅本有限 schema/metadata 组合通过。窗口和热缓存已释放，无后继自动重跑。

停止条件：任一迁移、旧事实比较、实际指定 CHECK/FK、metadata 原 CommitResult 或零副作用断言失败，即保留该轮原 FAIL 和退出尾，向对应产品/测试 owner 报首个具体缺口；不自动重试、不扩大旧矩阵、不延长预算。


## 37–39真实事务有限通过

新 `tests/projectvariable/agent_runtime_schema_test.go` 的 `TestAgentRuntimeSchema` 固定四个直接sub：`prefix36-upgrade-and-repeat`、`runtime-attempt-and-terminal`、`execution-slot-and-unbound-launch`、`human-compatibility-and-agent-origin`。第一项沿正式Migrator只核36→39及fresh/repeat的新表/约束；第二、三项以真实同一Tx的强制rollback探针验证operation/attempt/terminal bytes、Execution父FK、唯一active slot与不可变启动/取消规则。探针的Tool definition、Execution、Agent与terminal字节只是数据库约束材料，不能当作实际注册Backend、Launch、Runtime receipt或执行授权。每个可接受探针在明确rollback前执行`SET CONSTRAINTS ALL IMMEDIATE`；拒绝项核实际PG code/table或明确constraint，外层要求真实NotCommitted，不用最终rollback掩盖延迟FK。

公共拒绝链使用既有真实Account/当前Project Owner、同Store ExecutionAuthority及AgentExecutionConfiguration构造；Task/Meeting未绑定时Launch明确DependencyUnbound、Lookup无结果、外人NotFound，无created/slot事实。最后一项复用已验Human安装fixture完成一次39后的真实Human安装和Lookup，只新增来源互斥/immutable/attempt同Actor父键的rollback检查。无SQL伪造已发布Skill、Agent canonical或成功ToolCall；不重跑旧domain清理矩阵。

源码静查发现37的Tool P/A/E为text domain，38原父列为uuid domain，直接复合FK不兼容。原Execution作者在`87a92951`只把38自有domain改为严格UUIDv7 text，保留完整regex/所有FK/索引/trigger；root导入该单源。修前没有实际PG失败，修后首次真实迁移结果如下，不为已知源错误刻意先跑一次失败。

沿原成功schema入口准备一次新的`tests/projectvariable` integration/race `-c`候选，精确列举`^TestAgentRuntimeSchema$`（1 top），后续由root分配唯一真实窗执行：

```sh
python3 -B .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --driver .agent-state/work-owner-http/root_chain_driver.py \
  --binary "$CANDIDATE" --run '^TestAgentRuntimeSchema$' \
  --output "$FRESH_OUTPUT" --root-chain
```

使用已有schema成功launcher的完整私有环境、固定MinIO/解释器及原fresh/实际Wait/7resource/private/runtime/desc/HOST_TCP双尾；只替新candidate、selector和全新output。共享入口本轮仅4个driver/3个supervisor数据hunk；既有schema control以同一parser/collector/observer复用新四sub，7方法离线通过。没有新监督层、manifest规则或业务成功替身。

source `db058002` 首次 `runtime-schema-compile-01` 实际PASS：session70554→a08e7f，outer839880/compile839889/list840103原Wait0，race-c10.518s、精确唯一top列举1.068s；fresh5,994,541,056B，647编译输入及方法初尾一致、group/desc/runtime双空。候选`output/ai/agent-system-integration/runtime-schema-race-01.test`为49,539,466B，SHA256 `c63e4626f9cb04ca9d312956a07e549d7ba0671edb5a01335dcfce03097944dc`，普通nlink1/mode0700。编译结果在`output/ai/agent-system-integration/runtime-schema-compile-01/result.json`。

随后同源 `runtime-schema-01` 原wholePASS：session65794→885772，UTC2026-10-10 14:24:38–14:26:49，同进程fresh5,917,450,240B。1top4sub共34.15s（prefix10.29/runtime3.26/execution10.37/human10.23），Go842245、原driver、supervisor840415、outer840369均实际Wait0，supervisor128.903s。七个资源14次absent、private/runtime/desc/HOST_TCP双空、outer两次desc/TCP空且无adopted/survivor，1321运行输入初尾一致，SHA256 `9c68152687b9dc3c74486c288cf2d32529d47c7fb4cab314d81382fa7a80ddde`。原日志`/tmp/ars01/pg-bbb361b9db8a44b1a52a74fa97708a2e.log`，安全结果`output/ai/agent-system-integration/runtime-schema-01-control/result.json`，实际环境/命令与闭包在同级`runtime-schema-01-inputs.json`；原成功schema02 launcher仅换本轮字面参数，热cache及资源窗口已释放。

本次真实接受连续迁移与37–39事务约束、公共Launch缺真实Trigger的拒绝/零事实、39后真实Human安装兼容；SQL回滚材料仅证明约束，不能冒真实Runtime attempt/receipt writer、Execution Launch成功、Agent安装授权、Snapshot或完整F1。已有纯检查、旧schema02及本轮结果按各自输入复用，原历史FAIL保留，无后继自动重跑。


## 后继 preparation / Snapshot 接缝（接口准备，未实施）

下一切片先由Execution持久化preparation input及原attempt/fence，沿`DiscoverPreparation → CapturePreparationInTx → PreparationRef`在完整锁并集、preparing/无取消及来源重验后调用真实Trigger CaptureInput，再供应现Agent capture口的sameStore/Tx/PAE私有witness，与受保护引用原子保存。恢复只用原ref/digest，不能重新读取另一版正文。事务外Build完成真实环境准备后，Seal再核原attempt/ref、取消与gate，同Tx插不可变Snapshot、更新running/started_at和Started Outbox；提交确认前不发布，Unknown保留原身份查证。当前不新增迁移，编号仍由root分配。

D01完整Snapshot包括schema版本/Execution/捕获时刻、Project context及固定AGENTS.md内容或保护引用、完整AgentConfig/version、平台prompt revision、TriggerContext/input_ref、ResolvedModel、ExecutionTool/NameTable、原policy、initial Skills/assignment sequence、Mount metadata、普通变量值、Secret metadata/lease refs。不能先发布缺字段Snapshot。现Agent消费口、Model正式Resolve口与Registry definition/currentBuiltin/NameTable可复用，但Execution capture/current witness、Task/Meeting CaptureInput/Build、各环境owner的捕获保护与Execution Tool选择仍未绑定；Model Discover会落prepared事实且现profile拒tools/reasoning，Human配置Directory也不能充当Execution环境口。缺依赖继续unbound，不用空集合、伪Human或SQL运行行填齐。
