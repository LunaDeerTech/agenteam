# Secret Owner 独立补验

只读生产基线为 `482c5ae5`；本实例未参与实现。复用该卡内 Migration 9、read 6、Atomic/Concurrency 10、Recovery 5 节点的作者证据及既有有限独审，保留 read01 原 FAIL。原 output/产物在当前环境不存在，因此本轮没有声称重查原始日志或重跑原矩阵。Git 对照证明迁移通过后生产、SQL、原迁移测试无变化；read02 后全部 Owner 测试与相关生产无变化。

静审核对了当前授权、原意图 Match、完整锁集合与一次 final Tx、私有事实及 AuditID 顺序、两域版本、completed-only 安全记录、Unknown 原因保留与 Stop/Drain、普通 type 隔离和 00030 约束。未发现生产 must-fix。库级候选的正式接受仍需下列定向独验，正式 main 仍受 00028→00029→00030 连续交付限制。

`owner_risk_test.go` 只补已有方法未实际命中的两项风险：

- 三条真实 Secret 的 keyset 续页、换 limit/同 User 新 Session、普通写入不使 Secret cursor 失效、跨 type cursor 拒绝、no-op/replay 不改 generation、跨边界改名后旧 cursor stale 且不返回半页。
- 在真实本域 `secret_references` 表插入明确披露的引用行，验证 Delete 拒 Busy、两域持久快照不变且无完成回执；撤销该行后相同命令恰一次完成。直接行 fixture 不代表 Agent F1 retain/release 已绑定。

沿原 `tests/projectvariable` 真实 Account/Project/D04/Audit/Outbox fixture；保留其 persistent Skills 初始化边界，不新增生产 fake。用 Go overlay 将本文件映射为 `tests/projectvariable/secret_independent_risk_test.go`，只增加独验源，不修改作者测试或产品。构建命令为固定 Go 1.27.1、`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`、root 指定只读 module cache 和本树独立 GOCACHE，执行 `go test -race -tags=integration -overlay <generated-overlay.json> -c -o <independent.test> ./tests/projectvariable`。

实际 PG 入口复用原 `.agent-state/task-planning-recovery/pg_only_driver.go` 与 `pg_only_supervisor.py` 的 generic exact-top 模式，selector 为 `^TestSecretOwnerIndependentRiskComplements$`。预期完整节点是父 top 与上列两个子项，共 3 RUN / 3 PASS；不修改既有 SecretOwner 四组白名单。原 Go 6m、driver 105+15s、supervisor 123+3s、TCP 尾与两个 owned resources 的 Wait/退役门保持。独验源码与方法先交 root 保存；未获 fresh 实际窗口不启动 Docker/socket。当前状态：源码待编译，真实补验未执行。

最小产品后继应由单一 HTTP/root 接入负责人承担同一交付：按 rev2 `/secret-variables` 的 GET/HEAD、POST、PATCH、DELETE、安全 commands/lookup 接入现服务；落实 strict bytes/material 生命周期、Cookie/Origin/CSRF、预算与安全输出；把同 Store 的 facts→Project→SecretWriteAuthority→D04 Secret→Owner 服务按序接入现根、事件目录及统一 Stop/Drain，不另建隔离运行服务。首条真实默认根链路为 Owner 登录→创建→安全详情/分页→覆盖/no-op→删除→新 Session 历史 Lookup，同时核普通 `/variables` 回归。该接入不等待 F1 材料消费，但须持续明确 F1/MCP/Runner 未绑定。
