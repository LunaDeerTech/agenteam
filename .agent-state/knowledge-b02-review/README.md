# Knowledge B02 独立产品风险审查

审查者 Skills，2026-10-09。没有参与 B02 产品实现；Knowledge 树全程只读。生产输入由作者声明为 `f1c94ee5`→`96049336`，后续保存仅 tests/harness/docs；本次直接消费实际工作区，20 个受审输入在控制前后逐字一致；不为已有 Git 输入另存冗余指纹清单。Git 由 root 负责，本实例未操作。

## 结论

**暂不接受完整 B02：P1 一项、P2 一项需收敛。** root 已接受两项实际源码复现为本域 mustfix 并授权作者定向修复。 授权／发布／树删除主链未发现其他 mustfix；不是完整测试矩阵或生产根接受。已真实通过的六组作者证据只按各自范围复用；Runtime、Unknown、Concurrency、CleanupUnknown、ProcessRecovery 的原准备输入仍须真实运行。Object Runtime 原停止项继续保留。

1. **P1：取消后的 canonical reader 会永久阻塞 Knowledge Drain。** `internal/central/knowledge/source.go:26` 的 `trackedRead.Close` 仅在底层 `Close()==nil` 时注销调用。真实 `internal/central/object/reader.go` 在取消后先执行关闭／release，再由 `Close` 等待 `monitorDone`，返回原 `context.Canceled`；重试仍返回该错误。故真正退出的 reader 永远留下 Knowledge call。离线控制调用真实 D05 私有 constructor（仅通过 overlay 增加导出桥）、真实 Knowledge `begin/Stop/trackedRead.Close/Drain`，内存 body 和受控 release：正常 Close 对照注销；取消回调实际返回、Close 实际 join 后仍保留 1 call，后续 Close 不改变。需要区分原读取错误与可靠资源退出事实，不能把任意非 nil Close 直接当作 join。原 `TestKnowledgeB02Runtime/stopped_canonical_reader_retires_after_actual_close` 应继续消费真实 PG/D05 输入确认，当前控制不是数据库 lease 证据。

2. **P2：首个 Pending 清理任务使其他项目长期饥饿。** `internal/central/knowledge/recovery.go:13` 每次 `RecoverCleanup` 从 `after=nil` 全局按 UUID 开始；`recoverCleanup` 对 live reader／Pending 返回 `ResourceBusy`，循环立即返回并丢弃本轮游标。因此最早一个持续 live reader 会使任意后续 Project 的独立 cleanup 永远得不到尝试，分页 `LIMIT 32` 没有提供公平推进。离线控制调用原公开循环及原 per-item 方法，受控两条不同 Project 的 object 阶段行和 Pending Cleaner；连续三轮只调用第一项，直接调用第二项的 per-item 对照能到达 Cleaner。应在保留每项精确 cause、Pending/Unknown 事实及原预算的同时，确保忙项不长期阻塞独立后续项。此处未执行 SQL、物理删除或真实 reader lease。

契约依据：D12 规格 §3 的 SourceInput/CanonicalRead 关闭责任要求保留原错误且不得把取消／wrapper 标志当 join；B02 §5 第 10 项要求真实登记／取消／等待，故必须让已确证真正退出的调用完成 Drain。B02 §5 第 7 项要求持久 exact cleanup 能持续收敛，单一 Project 的正常 Pending 不能使其他独立清理永远不可推进；分页本身不能消除该 liveness 缺口。此处没有推导整 Project participant 或 D05 全域 join 已通过。

## 静态审查范围与已核事实

- 本域 15 个生产文件：`service`、`repository`、`commands`、`planner`、`publication`、`recovery`、`runtime`、`object_authority`、`source`、`cleanup`、`read`、`query`、`tree`、`events`、`audit_authority`；迁移 `00025_knowledge.sql`；Project `audit_facts`、`events`、`knowledge_event_authority`、`object_audit_facts`。依据 B02 卡 §1–7、D12 规格及真实 B01/D05/D08 口。
- 同 Store／活 Tx／完整已有锁先于当前授权；completed receipt 仍先核当前可读门禁，再跳过旧输入、旧 token 和新增 Mutate 工作。全局 create target 串行、cross-Project source union、prospective 原 create cause 与 current object 指针均有实际重验。
- 原工作 claim 已知提交前不读源／spool；来源 resolve、leased open、final 原 revision 都受原 exact source 和当前门禁限制。外来进程证明在事务外获取后，第二次原 union 下重新核 attempt/fence/process。Unknown 保留原 cause/attempt，不升级完成。
- 内容 final 从当前行构造结果，UPDATE 不写 parent；Move 只写 parent/updated，不推进 content_version。Delete 完整 scope 与 mapping 重验后，同 Tx 写 tombstone、精确 cleanup、ReferenceCleanup、逐节点 event、receipt、单 root Audit、Activity；未发现删前缀成功分支。
- Audit private witness 绑定同 Store/Tx／当前 actor/session／完整 entry/key／原 scope，事件 private issuer 绑定完整 summary/current facts；Project 只提供当前 gate，继续消费各域真实 checker。00025 最小 tombstone、全局 ID、同项目 parent FK、原 Audit CHECK 增量与 Knowledge 闭集均核；没有把 00024 当 Knowledge 作者成果。
- 其余源码只读接受是上述风险范围结论，不能证明真实当前授权变更、真正 Object cleanup 或跨进程 join。共享 Project 合并由 root 保留 Variables 等其他域分支并按真实差异回归。

## 实际控制与重现

`61981` actual exit 0，race 1.042s：1 top／2 subcases（普通 Close 对照与取消缺陷）。追加清理调度控制后 `99769` actual exit 0，race 1.023s：2 tops／2 subcases。**测试故意断言已观察缺陷，所以复现 PASS 不是产品 PASS。** 没有 PG/Docker/socket/网络；受控源与 release 不证明真实 lease。三桥／probe 只经 overlay 加入编译，未改 Knowledge、Object 或 Postgres 产品文件。

在 `/workspace/agenteam-knowledge` 执行以下命令；复用 Skills 独占 cache，不复制／删除，不启动真实资源：

```sh
python3 - <<'PY'
import json,pathlib
probe=pathlib.Path('/workspace/agenteam-skills/.agent-state/knowledge-b02-review')
out=pathlib.Path('/workspace/agenteam-skills/output/ai/skills/knowledge-b02-review')
out.mkdir(parents=True,exist_ok=True)
root=pathlib.Path('/workspace/agenteam-knowledge')
mapping={
 str(root/'internal/central/knowledge/zz_independent_b02_review_test.go'):str(probe/'production_review_test.go'),
 str(root/'internal/central/object/zz_independent_b02_bridge.go'):str(probe/'object_reader_bridge.go'),
 str(root/'internal/central/postgres/zz_independent_b02_bridge.go'):str(probe/'postgres_rows_bridge.go'),
}
(out/'overlay.json').write_text(json.dumps({'Replace':mapping},indent=2)+'\n')
PY
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH \
 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 \
 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod \
 GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache \
 GOTMPDIR=/workspace/agenteam-skills/output/ai/skills/compile/tmp \
 /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race \
 -overlay=/workspace/agenteam-skills/output/ai/skills/knowledge-b02-review/overlay.json \
 ./internal/central/knowledge -run '^TestIndependentB02' -count=1 -timeout=30s -v
```

## 有限后续验证

先由本域作者独立修复确认风险，再由本审查者只复验受影响边界；保持原 Runtime binary 与失败历史。按 B02 §7 已约定有限真实补集继续：内容／权限／事实覆盖 DOCX、raw length/SHA 正式 D05 拒绝、final 当前授权变化、伪 Audit/Event；树／引用覆盖正文 Update 与 Move 交错、preview 后成员移入移出、旧 upload revoked 后重附着及错误 cleanup cause。既有 Activity 末端失败证据复用，不复制每条 SQL 的同类回滚矩阵。五个已准备异常／恢复组的原实测完整尾与关键判据由独立者消费，不默认新建大矩阵。
