# D05 Service/SQL source review

有限接受固定 `26df655a` 的生产阶段及 `eda849dc` 的三生产增量，未发现本轮剩余 must-fix。这是独立源码与受控 Store 控制结论，不是 PostgreSQL、MinIO、ProcessGuard、D08/D10 组合或整卡接受；作者活动中的 integration tests 未纳入。

本轮读 `internal/central/object/{access,bounded_cleanup,cleanup,metadata_cleanup,project_audit,project_audit_witness,project_lifecycle,project_stop_batch,project_stop_store,project_work,reference_cleanup,references,service}.go` 及 `contract/{access,metadata_cleanup}.go`，对照已经接受的本卡 SPEC 和旧 Service/SQL。`eda849dc` 仅补物理候选 `NOT cleanup_gate`、四历史分页按原 UUID 列排序、active lease 闭集谓词和 reference_cleanup 合同注释。原 SPEC 审见同目录 `review.md`，不重复扩写。

已核的关键链路：

- Purge 通过真实 issuer/live Tx/实际 Acquire/重新计算 binding/当前 CleanupAuthority 后，先消费本实例同一 live Tx 一次预算；错误或 Pending 不重置。选择跨表至多32个 DELETE，exact ID 每次 RowsAffected=1，最后四 anchors 计入额度；先清 current_attempt_id，再清 current cleanup/attempt/Upload/Object。它不替外层提交事务，也不在错误时证明回滚。
- current published attempt 的 ProjectDeleted cause 是 Skills 重放/Recovery 的唯一原锚点。旧 AbandonedAttempt 保持原原因；恢复缺或坏 current cause 不回退无界历史算法。stop 后只有精确 Cleanup claim 在原 Tx 经当前 provider 许可，普通 maintenance 无新豁免。
- 物理 current 占1，旧 pending 从两个各31集合 UNION 再取31；同一候选集合用于后续 writer/claim/I/O。原实际 I/O 返回且 checkpoint 已知提交后才进入私有 completed workers。finalize/Object Audit 只可排除该同调用 exact operation/worker/fence 的末尾自等待；Purge 完整 work/native 退休检查没有这个例外。永久 zero_marker 的原物理语义未改，metadata 不删除后端 marker。
- Stop 五 lane 只选本轮当前主ID，固定指针展开；Project/Object/command/work/transfer 真实锁并入一次重建验证。原主ID推进游标；取消在已知 gate 提交后，Unknown 保持错误并由下轮读持久 cursor。完整 pending 检查独立于32项或空诊断，实际本地 ended 仍需 origin Tx 结束，foreign ProcessGuard 仍核 exact process。
- PUT cleanup/staging/transfer 按同一个三行包选择、固定依赖锁和原 DEFERRABLE FK 顺序删除；candidate 与 leases 等入边解除后才后续删。此静核不证明真实 FK 提交或触发成本。跨 Object source/candidate 关系没有越权删除路径，真实阻断/回滚仍需后续场景证明。

持久独立控制为 `implementation_test.go`、`run.py`。命令：

```sh
python3 .agent-state/object-metadata-cleanup-review/run.py
```

运行器拒绝 Object 包偏离固定 revision 或出现额外未跟踪 Go 输入；仅用 Go overlay 加入独立测试和一个受控 Rows 构造器，不替换任何生产函数。测试实际调用 Service 的 access facts/Acquire/Purge、metadata physical gate、completed workers、五 lane discovery 和完整 pending；Store/Planner/Authority 返回显式刺激，SQL 结果不作为真实数据库 oracle。

最终 `46327 → e70e7a` actual exit0，5个 top、24个 subcase（另1个无 subcase的狭义例外控制）：32 leases、32 work、16个cleanup/attempt包、最后四anchors；同 live Tx 重入、当前许可拒绝、planner拒绝、漂移、ended Tx、原本地工作/原Tx未完、持久 pending、影响行数0、写入错误/foreign plan；最后五写位置失败保持 Pending；五空 lane 仍受完整 pending 阻断。前一版3个top `58153 → a68d6d` actual0，最终覆盖其范围，不冒并行 SQL 或 rollback 证明。首 `32355 → 4c301a` 为独验夹具编译错误（Query 返回类型及不存在的 cause.Equal），已修正；不是产品红。所有命令终态，没有 PG/browser/socket/网络资源。

后续真实门仍保留：65+及1001+历史末端活 writer/cleanup、原2s与 Unknown 提交双态、真正本地 reader/callback及 foreign guard、PUT互引提交与跨 source/candidate保护、每轮实际32总额及最后 D05四行与 Skills五核心同 Tx 一起保留或消失、服务重建与原 Audit恰一次。`00028` 新索引需真实 EXPLAIN (ANALYZE, BUFFERS)，包括空集/其它Project大量历史/本Project大量终局Object、五 lane/31+union/metadata分页/完整 pending 和每条父 DELETE 外键 trigger 成本；SELECT LIMIT、受控 Store 或候选索引文本都不能代替这些门。原 Object Runtime join 停止项不因本审解除。
