# Secret Variable Owner 库：独立静审

范围为 `/workspace/agenteam-secret-variable-owner-service` 保存的 `eaf209f5`：`internal/central/projectvariable/secret_{service,plan,commands,mutation,facts,repository,reader,write_authority,records}.go` 与 `.agent-state/secret-variable-owner-service/owner-storage.draft.sql`。审查时树为 `5dc89d9d`，上述十路径相对 eaf209f5 无差异；作者新增 PG tests 未纳入结论。依据本库工作项及已接受 Secret rev2/870b 顺序合同；此前路由独验不外推到这些写路径。

本轮源码审查未发现确认的 must-fix，仅接受为继续真实组合验证的输入，不是完整库或 SQL 验收。未编译、运行作者十六子控制、启动 PG/socket，未修改作者源码。

- 构造绑定同 Store 的事实/写 authority 和同一 Project port；调用受本 Service admission 跟踪。当前 Human/Session/Owner Read 在原 command/User/Project 锁内先于历史；新写另过 Mutate/type/前像/expected。历史按稳定 User 重建、每次仍使用当前完整 Actor/Session；不会因删除/归档要求旧 live target。已知异 Store 的 Tx 由各实际 adapter 拒绝，接口注入本身不证明生产已装配。
- 准备不写 D10 planned。D04 私有 prepared 先在原锁内实际 Match，再生成私有 discovery 与事件计划；最终锁集合在事务前完整归一化。final 再 Match、重验 current/preimage，随后依次 D04 Apply、D10 后像/generation/history、同 Tx 私有 mutation witness、真实 Audit 返回、含真实 AuditID 的 completed、Outbox/Activity。D10 Audit checker 重新读取本 Tx 后像/history；Outbox NewFact 再核 completed、原 D04 observation 与实际 AuditID。观察结构的公开构造器不能生成包私有 witness。
- 显式 value 始终覆盖；metadata-only 保 Credential version，普通 Variable version 独立递增。no-op 仍保存 D04 原意图 proof 与安全 completed，但没有新 D10 history/Audit/Event/Activity；已完成重放在新写分支前返回原 receipt。全部跨域变更处于同一个调用者 Tx，nonce 预留沿既定例外。
- Unknown 保原 CommitResult/Attempt/Cause；最多一次独立 3s，持原 prepared 做 Match，不能用 identity-only Lookup 认领异语义结果。失败保 Unknown。只有明确 NotCommitted 的私有 preparation-change 标记可退出原 Tx 后再准备一次；Stop 会取消原调用与确认，Drain 观察实际调用结束，不凭取消回收所有权。
- 目录读不访问明文；安全记录不存材料或裸 semantic digest。活 Secret 与普通变量共享 ID/name，查询及容量各自 type 过滤；删除核本域实际引用和 D04 reference/lease。分页绑定 User/Project/type/独立 generation，Rows 关闭后才结束 Tx。Secret tombstone 不保 live credential 映射；历史恢复核 D04 原 receipt，不能从当前 GET 推原命令结果。
- SQL 草案扩互斥 payload、保原不可重用 ID/type 触发器及跨 type 活 name unique；Secret 独立 generation/history/completed/reference。history→completed FK 为同 Tx 延迟检查，避免为 AuditID 提前造 incomplete receipt。Audit guards 保原表达式并只增加精确 Secret tuple，额外 metadata/actor/scope/producer 约束独立收紧。静读不证明 PostgreSQL 实际约束、迁移升级或父 FK/索引行为。

后继仍须真实当前 Account/Project+D04/Audit/Outbox 组合，实到故障阶段的整事务回滚，同 key 同/异义、ordinary/Secret 名称及 expected 锁竞争，物理 COMMIT before/after/pending 与 Stop/Drain，00030 空库/存量升级/重复启动/CHECK/FK。未绑定 F1、HTTP/defaultroot/UI、外部材料消费不在本库结论内；不得将作者 controlled Store/Rows 或本静审称为这些结果。
