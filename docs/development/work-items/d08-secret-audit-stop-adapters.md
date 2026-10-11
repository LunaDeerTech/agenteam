# D08：Secret/Audit 的 Project 停止屏障

本片从[领域绑定卡的四个 stop adapter](recovery-project-domain-bindings.md#四个-stop-adapter)中只取 Secret/Audit 两项，依据 [D08 §8](d08-project-owner-design.md#8-生命周期编排)。两库没有自主 Project 业务 worker；已确认的生命周期接受 Project EX 和当前 gate 可以屏蔽普通写。实际 lease 使用者、Object/Artifact/Outbox 技术工作仍由原拥有者停止。本片不恢复原 22 路径整卡或 Object Runtime join STOP。

## 行为与范围

新增 `NewSecretStopParticipant(*LifecycleAuthority)` 和 `NewAuditStopParticipant(*LifecycleAuthority)`，返回固定名称的 `ProjectLifecycleParticipant`。构造只捕获已构造的具体 Authority；没有 SQL、goroutine 或生产注册。

Request/Inspect 只接 exact ProjectLifecycle service actor、cause 和 Project scope。以原 Authority 的同 Store 短事务取得 Project SH、检查实际 held locks、完整 frozen manifest/声明/当前 Project、operation 和所有 participant 的严格记录。Request 仅在当前 stopping 且本项未 failed 时可返回 stopped。Inspect 沿原 R3 允许态；非 stopping 只消费该项已持久的 stop 状态，required/pending/failed 不改判 stopped。持久 refs 不区分 active/unknown，原值保守投影为 UnknownRefs。最小 deletion receipt 无原版本和 participant 证明，返回 unbound。

一次调用至多 2s 或更早 parent，始终同步等待原 WithinTx 实际返回；只在 Committed 且调用仍有效后公开报告。NotCommitted/Unknown 返回原安全错误和零报告，Unknown 保留原 attempt/cause。读取不 Touch、不更新 participant/checkpoint/operation，不执行 Retry 或 claim。Cleanup 恒 unbound。该报告不证明 Secret lease 使用者已退出，也不表示合法 read/technical Audit 从此为零。

写域仅新增 `internal/central/project/domain_barrier_participants.go`、同名纯测试、`tests/project/domain_barrier_participants_test.go`、本卡与本树 current，以及原 `root_chain_driver.py` / `pg_only_supervisor.py` 的本组精确字典项。新 selector 的有效目录显式为 `tests/project`；原 selector、函数、预算和尾门不变。无迁移、新依赖、Object/Artifact/app/web 或生产 registry 变更。

## 验证与当前状态

技术冻结为 `be0dc8ec30d786982ce7c587459468f91d173305`。作者在离线 Go 1.27.1 / max1 下执行定向纯控：4 top / 9 sub race PASS，含身份/Scope、同 Store/实际 held lock、Unknown 身份和原调用返回屏障。首轮 `unit01` 因测试比较不可比较的 CauseDetails 编译失败；修正测试后 `unit02` actual0。原失败保留，产品未因该失败修改。`vet01` actual0；最终 PG `compile02` race-c / `list02` 均 actual0，精确列举仅命中本组。

真实 `native01` 复用已有自有资源原链，唯一 `TestProjectDomainBarrierParticipants` 和两个子场景 PASS（8.83s）。业务范围：真实 Secret 写在真实 Audit 调用处持 User SH + Project SH，真实 BeginArchive 依全局锁序先实等 User EX；释放原写后接受成功。Archive/Delete 接受及 accepted→stopping 使用真实 Project 服务和原 LifecycleStopDriver。两个 adapter 返回 exact Name/cause/scope 的 stopped，未写 Project/operation/participant/command/Audit/Event/Secret 或 Session activity；当前 Owner 的既定 Read 仍允许，普通 Secret Mutate 被 gate 拒绝。

failed/cleaning/completed 使用明确的严格持久记录观察 fixture；实际 required/pending/failed 不伪装 stopped，其他 participant 坏记录、跨项目/错误 cause、最小 deletion receipt 均拒绝。Unknown 控制是原真实读事务 Committed 后装饰其公开返回结果，并非网络 COMMIT 故障；取消控制持原 WithinTx 返回屏障至真正释放。Account User/Session 为 canonical 测试种子，使用真实 Authority；Skills 初始化为已有持久 SQL 测试 receipt，其他 registry 项为不得调用的声明占位。这不证明真实登录、生产初始化、完整 registry 或 cleanup worker 成功。

原 Go PID 215504 与 driver PID 213703 均 Wait0；七资源两次 absence、desc/private/runtime 双尾、HOST_TCP 双空、inputs_unchanged 全齐，supervisor PID 213702 actual0。作者亲见外层原工具 58404 exit0（105.702s），计本组限定 wholePASS。编译前后 1,649 个输入逐项相同；运行冻结加入最终 binary 共 1,650 项，binary SHA256 `a370f68e2bb892b6b578c38ffd63d7899c191e11ca9e982720c6da1d97d689b7`。

原件保存在本树 `output/ai/secret-audit-stop-adapters/checks/`（unit01/02、vet01、compile01/02、list02、profile01），`compile02/inputs-{before,after}.json`，`native01/{invocation.json,plan.json,input-hashes.json,process.json,outer.log,terminal.json}`；原监督日志 `/tmp/sab01/pg-644a24c5dc884ac3bd1510f717bb9a4c.log`。variables_review 独立接受冻结源码、方法、输入准入、业务及完整退出原件，无 must-fix；外层工具 58404 的实际退出由作者亲见，审查者核其落盘终态，计本组限定 wholePASS。

生产 initializer、完整清理/foreign join、F1、ready=false 和全部原 STOP 保持。两个报告不补齐 Object Runtime join，也不赋予生产启用权限。
