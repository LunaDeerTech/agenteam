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

确定的夹具缺陷是比较 `Fields().AuditID` 指针地址；该契约每次返回深复制，正确的非空 receipt 也会比较不等。现只将错误分支单独检查，并复用既有 `sameSecretReceipt` 比较完整安全 receipt 值；产品、DDL、原场景与预算未改。原复合断言未记录服务 `err`，不回填旧 replay 成功或断言这是唯一运行缺口。修后只完成格式/静态检查，尚未重编或真实复验；旧候选和原 FAIL 保留，后继需新候选执行受影响的 schema 入口。

停止条件：任一迁移、旧事实比较、实际指定 CHECK/FK、metadata 原 CommitResult 或零副作用断言失败，即保留该轮原 FAIL 和退出尾，向对应产品/测试 owner 报首个具体缺口；不自动重试、不扩大旧矩阵、不延长预算。
