# Lookup01 四源限定 STATIC PASS

只适用于卡 #1–4、作者 lookup01 manifest `0a217020e6a7050774bff10d1938bee6538cf60f5c44df037ac8222e1a5fca56`。四快照实际 hash 匹配，base01 四旧源与已接受规格来源相同，独立重算 diff 与作者 delta 完全一致。必要固定依赖及来源见 `checks.json`。未发现这四源的生产阻断；没有 Go 编译/测试/PG 结论，不是整卡产品接受。

契约 `WriteCommandLookupRequest.Validate` 保留 Human、secret namespace、kind、Model purpose、版本上界及 ref 绑定。System owners 仍精确单 User；新 Project owners 精确 `[ProjectID, UserID]`，顺序、数量和同 scope 均受约束；Create 无 ref/expected，Update/Delete 同 Project ref 且可形成合法 successor。没有新增 Service actor、其它 purpose 或材料字段。

实现对合法 Project 不要求 System 端口，先固定 Command/User/Project 三 SH，再同一次 WithinTx 中 AcquireAll、取得同 Tx executor、RequireHeldLocks、当前 Session/Owner Read，最后查询 receipt。Project nil/typed-nil 时仍先当前 Session 拒绝，再报告未绑定；正常分支调用原 authorize，校验 grant 的 actor/scope/intent。System 仍原两 SH/原 System 依赖与原授权链。原写入和 Mutate-before-receipt 没有修改。

查询仍只读安全五列，按 scope/scope_key/command digest 定位；不读取 payload、分配 nonce 或调用 Metadata/Execute/Resolve/Audit。kind/purpose/ref/version/deleted 的既有完整组合校验不变；合法不同绑定返回 KeyReused，损坏事实返回安全不可用，均不返回候选。只有 CommitResult Committed 后再核 ctx 才发布 present/absent。

终局精度：原 `commitError` 对 NotCommitted 返回原 Fault（包括私有 cause）；Unknown 保留既有 Secret CommitUnknown/公共 unknown 分类，零 observation。该旧 helper 本来不把 CommitResult 的 AttemptID/Cause 附入返回 error；本审不声称新增了此能力，也不把测试中的装饰 UnknownResult 当物理故障。提交后 cancel 的公共 Fault 明确 Committed、cause 为 ctx.Err，零 observation。真实 PG 原 writer/查证的终局与当前权限仍需整卡后续验收。

测试源码新增 Project 三 kind identity/ref/version 边界、三终态×present/absent、无 System 端口、三锁、同 Tx、Session/Project/坏 grant/缺 held、nil/typed-nil 与提交后取消。旧 System 场景保留，仅共用 store 的 scope/key 预期扩展。它们使用受控 Store/authority，未实际执行，也不证明真实 PG lock 或正式 Owner grant；独立 A 和作者集成必须补真实组合。Project cancel-after-commit 当前只设 absent，present 由共用发布逻辑静态及旧 System present 例覆盖，后续必要 probe 可按风险补。无必要为此阶段重复整套旧证据或阻塞后继作者实现。

所有只读检查已结束；仅写本独立 scratch。HTTP/root/集成仍活动，未读取作候选结论；README #23 未授权。下一步等待作者阶段离线原件或完整停止写入候选，按 root 的后续具体权限继续。
