# Skills Cleanup 历史接缝方法窄审

结论：六路径冻结增量的静态方法有限接受，无剩余确认 must-fix。原 PUT 计数不能证明 payload 实存的缺口已修；本记录不表示新测试已编译、真实 PG/MinIO 已执行或历史缺口已取得动态结果。旧 Cleanup 两 top / 五 sub 的实际 PASS 保持原范围，不由本审重跑或扩大。

审查者 Model 未参与此增量实现；目标树 `/workspace/agenteam-skills-cleanup`，原基线 `a776829a5e57b170c41220e5d47f13314c41dbdd`，最终实际只读核到 clean `b5bb3ae8600a833d24947431803533318ff380f5`。范围仅：

- `tests/skills/lifecycle_cleanup_fixture_test.go`
- `tests/skills/lifecycle_cleanup_history_proxy_test.go`
- `tests/skills/lifecycle_cleanup_history_test.go`
- `docs/development/work-items/d10-skills-initialization.md`
- `docs/development/work-items/d10-skills-initialization-design.md`
- `.agent-state/current.md`

## 实际核对

- `1cd942` actual0：精确六路径、一 top / 两 sub；逆去 fixture 可选配置参数及调用循环后全文等于 a776829a，所有既有零参数调用保原行为。没有修改产品、权限、预算或已有两 top。
- `bae826`、`757c1c` actual0：实际读取返修后测试、代理与当前 HEAD/状态。没有运行 Go、PG、MinIO、HTTP/socket 或业务候选。作者 gofmt/diffcheck 的 `8558cb` 仅作为作者事实，不冒本人编译结果。
- 原审查 `.agent-state/skills-cleanup-review/review.md` 中现权、同 Tx、Unknown、调用返回及前缀/上游缺口结论复用；本次不重复已过受控或真实矩阵。

## 方法判断与已闭合缺口

1. 原代理 `puts` 在 upstream RoundTrip 前递增；产品仍会在 PUT 出错后执行验证，所以 PUT 失败与故障 GET 可以生成同形 unknown。首版仅计数和 native failed Audit 不足以证明预定的已存 payload 路径。这是测试证据缺口，不是产品失败。
2. 返修在首原调用返回、读取实际持久 old candidate key 后、同 key retry 前，以独立实际 MinIO client 读取该 key。真实 `ReadAll` 与 `Close` 均须无错，全部字节必须等于原 accepted bundle。它证明原 payload 实存且本次独立 reader 返回；不把此读取冒充原 SDK PUT 返回 nil、原私有 witness 或 canonical 事实。
3. 真实两 attempt 子项保原同 initialization command，仅重试一次。原失败后检查 native writer io_closed、Skill work joined、无活 lease、native failed Audit；成功后检查同 Object/upload、新 attempt、旧 cleanup ID/原 upload operation/AbandonedAttempt cause。实际 Cleanup 后旧项仍为原 cause/ID，新项为当前 ProjectDeleted，native Delete Audit 恰一次，再检查实际 marker/删除及两域最终清空、新 Service 重放。没有绑定 RecoverAttemptAccess 或扩大 per-command 上限。
4. 第二子项明确在真实物理 cleanup completed 后，仅插入 65 条本域历史 `object_attempts` 映射，复制已有真实父映射并实际 `SET CONSTRAINTS ALL IMMEDIATE`。不插 D05 状态、claim、witness 或伪 work join；不能称 65 个当前 API 生成的 attempt。此范围可独立证明历史 SQL/FK 兼容与本域压缩。
5. 历史压缩负例在同原 Tx 实际看到 66→34，执行 deferred FK 检查后才注精确错误，外部全部计数恢复；随后原调用须实际 32/32/1，保 current mapping/initialization anchors，最后两域清空及原 Audit 恰一次。未以 CHECK 拒绝、提前失败或单纯总量变化替代目标 rollback。
6. 代理仅改 transport；PUT 原签名 wire body 有界读取和 Close，转发实际 MinIO，拒绝限定 candidate GET。t.Cleanup 关闭 server/client connections、实际等待 handler WaitGroup、关闭 idle transport，并核 active=0。没有新增生产 stub、许可或清理白名单。当前还未执行，只有这些原操作实际返回才可认 join；外层超时/kill 不代原 handler 或 transport 退役。

## 后继最小验收边界

- 仍需新源码实际编译、唯一 exact selector/候选及完整输入闭包独审，再由 root 授 fresh 真实窗口。现有 c04 候选没有本次一 top / 两 sub，不能复用旧 PASS 宣称新历史分支通过。
- 实际运行须取得两子项及原全部资源/Wait/private/runtime/desc/TCP/input 尾；代理设置失败或原关闭不返回仍是 FAIL，不能用后置缺进程推断原 join。
- 若上述新组合通过，可关闭本域真实旧 cause/current cause 并存与 SQL 保留映射分批/FK 接缝；不因此证明 65 个当前 API 产生的 native 历史、D05 全部规模/PUT/扫描成本、foreign death、完整 BeginDelete/participant/root 或整个 D08。
- 正式交付仍受连续 26→27→28 及真实主线依赖装配约束。本方法调整明确区分当前 API 限制与历史兼容，没有为造测试扩大 Maintenance 或 Recover 权限。

本审只写本人 D10 树此记录，未修改作者源、候选或证据；结束无自有活命令或资源。
