# D05 rev1 独立设计审查

结论：**有限接受，无设计 must-fix**。这只接受 rev1 SPEC＋独立可选纯合同作为后续实现输入；不接受尚不存在的 Service/SQL provider，不代表性能、实际退休、完整 D05 或 Skills Cleanup 已通过。审查者未参与方案或纯类型实现，作者树只读。

输入为 `/workspace/agenteam-object-metadata-cleanup` 的 `e32b62c672490bd19ce7e835ecf16f0affe0fa56`，实际 `git status --short` 空。范围是工作项 `docs/development/work-items/d05-bounded-metadata-cleanup.md`、current 和 `internal/central/object/contract/metadata_cleanup{,_test}.go`。对照原 `cleanup.go`、`reference_cleanup.go`、`access.go`、`project_work.go`、`project_lifecycle.go`、`project_stop_store.go`、`project_audit.go`、download/transfer 原入口及 migrations 00005/00006/00007/00014/00016。消费约束只读 Skills 权威树 `d10-skills-initialization-design.md` §16.3–16.7；没有复制或修改消费方方案。

## 已核边界

- 32 的纯常量约束跨表实际 DELETE，含最后 Object anchors。SPEC 要求同 live Tx 只消费一次真实实例的 metadata provider、exact ID 和实际受影响行计数；不按表各 32，不藏 cascade。物理候选与 Stop 主 ID 各自至多 32，属于不同算法预算，不能由它们反推整个 SQL 成本已有常数界。最终 D05 四行＋Skills 固定核心同 Tx 的数量不形成超额历史扇出。
- 新 operation 目前只声明，原 AccessRequest 不接受它；可选接口无默认成功实现。Validate/MatchesOperation 是可重建结果形状，不能充当授权、actual join 或 commit proof。方案绑定原 cause/Object/Upload/command/批依赖、private issuer、同 Store/live Tx、完整 Acquire 并集以及当前 Skills CleanupAuthority。
- `project_work.go:468` 原 maintenanceAdmission 在 stop 后拒绝新 maintenance。新许可限当前 Cleaning、ProjectDeleted canonical cause、Skills gated/pending 和原同 Tx；保留普通 deny。completed 后仅 metadata，不再新发物理 claim。原 work 技术 join 仍需 original handle/Tx/锁，不因业务许可关闭而伪造退休。
- `reference_cleanup.go:51` 原 revoked 分支要求全部历史 cleanup 同 cause，确实会与合法旧 AbandonedAttempt 冲突。新分支以 current published attempt 的 ProjectDeleted canonical anchor 重放，旧原因不改写；不以任意历史 cleanup 存在授予权限。Unknown 原子 gate 必须重取全锁确认，不把缺行解释成成功或回滚。
- `cleanup.go:171/194/404` 原历史扫描、`project_stop_store.go:100` 原 LIMIT1001 全历史 fanout 确实存在。新 Stop 五 lane 用当前相关主行和固定原关联，按主 ID 推持久 cursor，独立完整 `projectStopPending` 决定终态。预准入无 native 的 Prepare/IssueDownload 与有 Object 的 candidate work 分开；原 writer、cleanup worker/fence、download stream、transfer retirement 缺证据仍阻断。
- 原 stopWriters 的 `writers[id] == nil` 捷径和旧 cleanup claim 对 applying worker 换 fence，均不能原样继承到本有界路径。rev1 明确禁止以取消、map 缺失、marker 或 phase=cleaned 代 actual join；同一组候选贯穿 cancel/wait/claim/I/O/checkpoint，余项由完整 EXISTS 阻断。finalize 对本次已实际 I/O 返回且 checkpoint 已知提交的 worker 例外，不流入 metadata 的全尾前提。
- 00007 中 staging→transfer 和 transfer→staging 都是 INITIALLY DEFERRED；candidate/source lease 入边是立即约束。cleanup→staging→transfer 整包删除与先保留 candidate/external/source leases 的顺序一致。额外 staging 入边、外 Object 引用仍阻断，不空置 transfer_id 伪造 kind。00006 download grants.object_id 没有 Object FK；不能虚构它为本次对象删除 FK 包，但 download 实际工作仍受 Stop 最终谓词约束。
- 00005 uploads.current_attempt_id 是立即 FK，最终四行必须先置 NULL，再清 current cleanup/attempt/Upload/Object。Skills 最后核心紧接同 Tx；Known Commit 前不能宣布完成。最终 Unknown 仅由 Skills 原 Project EX/current gate 下的共同六表终态恢复，不再调用已失父映射的 Object 维护。
- zero_marker 首版永久保留；物理完成是 payload 消除、空 marker 核实和独立 writer/lease/work 退休。没有给本任务授权修复已停止的 Object Runtime join，也没有为其未知关系制造成功。

## 后续实现必须保留的具体检查

1. §7 的 FK 入边清单是开放清单。另核到 `uploads.current_attempt_id → upload_attempts`（00005:110）和 `upload_attempts.transfer_id → object_transfers`（00007:77）当前无对应首列索引；删除本 Object 的一个父行也可能触发跨库扫描。实现必须连同卡中已有入边逐项确认索引与真实 FK trigger/EXPLAIN/BUFFERS，不能只测 SELECT、LIMIT 有返回或小库。此项已告作者，作者确认纳入实现；当前 SPEC 已要求所有入边，因此不是 rev1 漏掉的阻塞要求。迁移号仍由 root 新分配，00028 仅 Skills reserved。
2. 原 native ObjectDelete Audit 与 Deleted 在同 Tx，digest 使用当时最早 cleanup cause（cleanup.go:476、project_audit.go:160）。metadata 删除历史后应依赖该持久单调不变式；不能重新取剩余历史的新“最早 cause”来验证或追加 Audit。作者已确认此解释与 §4/6 一致。
3. 删 joined work/旧 cleanup 后的恢复必须沿“只删已证退休历史、当前 Deleted/revoked 禁止新工作”的单调不变式，不能把从未证明的缺 work/native 记录作首次退休凭据。当前 current/Upload/Object 与原 authority 必须持续保留到最后共同事务。
4. 新算法不能偷偷调用旧全量 inspect/attemptIDs/projectStopFacts 或 bulk native join。65/1000 以上历史、最后低 ID 活关系、Unknown 两种结果、原 2s 剩余预算、调用实际尾与 foreign ProcessGuard、原 cause/Audit 恰一次、同 Tx 32 反例及跨域最后原子性均须以后实际验证。本次没有证明这些运行时性质。

## 本轮独立执行

`fk_contract_controls.py` 从实际 00005/00007 读取 15 条 FK 边，以独立、内存中的最小关系图检查上述删除顺序；没有导入或复制作者清理实现。正控是完整 PUT 包、保留 candidate/lease 的中间事务和最终四 anchors；七个反控为单独半包、单 transfer、过早 candidate/source/external lease、额外 staging 入边、未解除 current FK 的最终事务。

```sh
python3 .agent-state/object-metadata-cleanup-review/fk_contract_controls.py /workspace/agenteam-object-metadata-cleanup
```

Variables 树执行 `f4c64f` actual exit 0；同工具调用的 `git diff --check` exit 0。这是基于实际 DDL 的静态 FK 模型和索引来源检查，**不是 PostgreSQL 约束执行或查询计划实测**。作者纯合同 race/vet 未独立重跑，没有用其输出代替本次源审。没有启动 PG、browser、socket、网络或 Go 大编译，没有修改作者树，当前无在途命令。

审查资产仅本目录两文件；Variables Authority02 的 binary07/dist03 和原测试输入未变，仍等 root 单独 fresh grant。
