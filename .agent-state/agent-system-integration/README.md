# Agent 连续 schema 首次组合

基线为正式 main `7a693cb6`，root 精确导入 Agent/00032、Registry/00033、Skills 初始化/00034、Mount/00035 与已通过纯检查的 References；content 唯一维护固定构造 helper，coordination 唯一维护本测试及本目录记录。产品源、公共 fixture 和迁移不由本测试修改。

首问题：真实 PostgreSQL 能否按正式 Migrator 应用连续 00001–00035、从带真实业务数据的 00031 升级，并保持旧事实/Audit 契约，同时执行已具备的 metadata 提供方。精确选择器为 `^TestAgentConfigurationSchema$`，一个 top、三个直接 sub：

- `fresh-prefix-and-repeat`：空库完整前缀、journal/Goose 及约束落地、重复运行、15 张新配置表零事实。
- `upgrade-preserves-facts-and-audit-checks`：真实 Account/Project/ordinary/Secret 服务生成 00031 数据，升级与重跑后原事实、读取、命令 receipt 保持；升级后旧 ordinary/Secret 仍能正式追加。Agent create/update 的 Audit SQL tuple 只作原事务内显式回滚的 CHECK 探针，错误版本/空字段/多余材料字段/未知动作须命中指定 CHECK。
- `schema-invariants-and-unbound-dependencies`：通过 `tests/testsupport/agentconfiguration/assembly.go` 固定真实构造，在同 Store 原 Tx 调 Model Selection/Secret Directory，读后持久计数不变；Registry 配置口及组合 `NewService` 缺安装 source 时均 `DependencyUnbound`，新配置表为空。非法 canonical 名称、孤儿 refs、非法 head version/sequence 与真实 deferred assignment FK 均拒绝；异常接受也由原事务回滚，不能作为后继正向数据。

禁止 SQL 种一个有效 Agent、伪造 owner witness、用 fake install Backend/空 catalog 通过 Create。沿用 Project fixture 的持久测试 Skills 初始化已明确披露；它不证明生产 initializer 或 F1。首次创建/版本更新/各域 refs 与 assignment 同事务提交、真实安装发布和完整生命周期仍未验。00036 不在本次范围；意外混入的后续迁移使精确前缀检查失败。

只复用现有真实 PG fixture、root-chain 的原阶段/预算和资源退出尾；尚未新增 selector 接线或运行命令，不创建新 wrapper。入口接线、候选 race 编译/精确 list、非作者方法审和 root 的唯一实际窗口须分别就绪。当前仅 gofmt/静态检查，不将其称为编译或 SQL 通过。

停止条件：任一迁移、旧事实比较、实际指定 CHECK/FK、metadata 原 CommitResult 或零副作用断言失败，即保留该轮原 FAIL 和退出尾，向对应产品/测试 owner 报首个具体缺口；不自动重试、不扩大旧矩阵、不延长预算。
