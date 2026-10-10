# Secret Owner 独立补验

只读生产基线为 `482c5ae5`；本实例未参与实现。复用该卡内 Migration 9、read 6、Atomic/Concurrency 10、Recovery 5 节点的作者证据及既有有限独审，保留 read01 原 FAIL。原 output/产物在当前环境不存在，因此本轮没有声称重查原始日志或重跑原矩阵。Git 对照证明迁移通过后生产、SQL、原迁移测试无变化；read02 后全部 Owner 测试与相关生产无变化。

静审核对了当前授权、原意图 Match、完整锁集合与一次 final Tx、私有事实及 AuditID 顺序、两域版本、completed-only 安全记录、Unknown 原因保留与 Stop/Drain、普通 type 隔离和 00030 约束。未发现生产 must-fix。库级候选的正式接受仍需下列定向独验，正式 main 仍受 00028→00029→00030 连续交付限制。

`owner_risk_test.go` 只补已有方法未实际命中的两项风险：

- 三条真实 Secret 的 keyset 续页、换 limit/同 User 新 Session、普通写入不使 Secret cursor 失效、跨 type cursor 拒绝、no-op/replay 不改 generation、跨边界改名后旧 cursor stale 且不返回半页。
- 在真实本域 `secret_references` 表插入明确披露的引用行，验证 Delete 拒 Busy、两域持久快照不变且无完成回执；撤销该行后相同命令恰一次完成。直接行 fixture 不代表 Agent F1 retain/release 已绑定。

沿原 `tests/projectvariable` 真实 Account/Project/D04/Audit/Outbox fixture；保留其 persistent Skills 初始化边界，不新增生产 fake。用 Go overlay 将本文件映射为 `tests/projectvariable/secret_independent_risk_test.go`，只增加独验源，不修改作者测试或产品。构建命令为固定 Go 1.27.1、`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`、root 指定只读 module cache 和本树独立 GOCACHE，执行 `go test -race -tags=integration -overlay <generated-overlay.json> -c -o <independent.test> ./tests/projectvariable`。

实际 PG 入口复用原 `.agent-state/task-planning-recovery/pg_only_driver.go` 与 `pg_only_supervisor.py` 的 generic exact-top 模式，selector 为 `^TestSecretOwnerIndependentRiskComplements$`。预期完整节点是父 top 与上列两个子项，共 3 RUN / 3 PASS；不修改既有 SecretOwner 四组白名单。原 Go 6m、driver 105+15s、supervisor 123+3s、TCP 尾与两个 owned resources 的 Wait/退役门保持。独验源码与方法先交 root 保存；未获 fresh 实际窗口不启动 Docker/socket。

初稿 `PAGE_E→PAGE_B` 没有跨过锚 `PAGE_A`，content 有限方法审指出这一刺激缺口；初稿仅编译/list 成功，没有 PG 结果。窄修为 `PAGE_E→PAGE_0` 与 fresh 顺序 `[E,A,C]`，content 对两行和既定方法复核接受，无剩余 must-fix；该审者未执行 Go/PG。修后固定 Go1.27.1、共享只读 `/workspace/shared/agenteam-deps/go-mod`、本树独立 cache、offline/race overlay 编译实际 Wait0，精确 list 仅一目标 top、exit0；原 driver 离线构建亦实际 Wait0。候选 `output/ai/secret-owner-independent/owner-risk-fixed.test`（36,772,239 B，SHA256 `025e5bf641dbdd1ce9775ac7400c21c74f8fe721add016af37a8900f71f61667`）与同目录 `pg-only-driver`（15,390,619 B，SHA256 `70c8541a33828be322bcf04830f4825714a94be6c6a9c4c3affde0b655d20713`）均 regular/nlink1；private tmp 已空。编译/list 和有限静审不计作业务通过。

`f5edcbf1` 保存并停写后，root 授予一次独占 PG/socket 窗口。UTC `2026-10-10T02:40:02.231385Z`、fresh 18,686,083,072 B、empty task Docker config（不读取外部凭据）、offline 环境下执行上述 exact selector；原 session69482 实际 outer exit0。唯一日志为 `output/ai/secret-owner-independent/sql-independent-01/pg-e50b62644b0f4117a9f4acf12e0ae4bd.log`。父与两子恰 3 RUN / 3 PASS，业务 29.94s（keyset 16.74s、reference 13.21s）；实际 Go54640 Wait0、driver54105 Wait0/46.137s、supervisor54079 terminal0/105.908s。两个原 owned ID 双 RETIRE clean、六私文件双 absent/run 仅 owned.json、runtime 无残余、原 descendants 双空、host TCP 双 empty、inputs unchanged 均齐；完整原尾后已向 root 释放窗口，无自动重试或后台命令。本实例保持未参与产品实现，独立补验范围接受；作者原 30 节点按版本复用，原 read01 FAIL 不回填。库可交付等待正式 00028→29→30，当前结果不替代 main 组合或 HTTP/root/F1。

最小产品后继应由单一 HTTP/root 接入负责人承担同一交付：按 rev2 `/secret-variables` 的 GET/HEAD、POST、PATCH、DELETE、安全 commands/lookup 接入现服务；落实 strict bytes/material 生命周期、Cookie/Origin/CSRF、预算与安全输出；把同 Store 的 facts→Project→SecretWriteAuthority→D04 Secret→Owner 服务按序接入现根、事件目录及统一 Stop/Drain，不另建隔离运行服务。首条真实默认根链路为 Owner 登录→创建→安全详情/分页→覆盖/no-op→删除→新 Session 历史 Lookup，同时核普通 `/variables` 回归。该接入不等待 F1 材料消费，但须持续明确 F1/MCP/Runner 未绑定。
