# Object Audit new1 失败与 new2 测试修正独立核查

结论：配置与停止/退休前置修正可接受，未见以此削弱原业务条件或超时预算；PUT I/O 计数红尚未闭合，new2 整组仍失败，不能宣布通过。仅固定证据静审，未运行 Go/Docker/PG，未写仓库或 Git。

输入已独立核验：new1 的 13 源逐字节 hash 全部等于原 `evidence/integration1-input.json`（与恢复副本 input.json 相同，SHA256 `5a1e2059c3f0b9c163d76a5d8ceff97871b87d7f2fc5d7535a240ccd63d87d98`），因此两测试的反向还原与原运行清单一致。new2 的 13 源全匹配自身及原 `integration2-input.json`（SHA256 `d396e96877f5ba8601d30b6c8a9c7aafb368ae6ef9a043fc1dddbd9c092739cf`）。两轮仅 `tests/objects/project_audit_transfer_test.go`、`project_audit_fixture_test.go` 改变；提供的 delta.patch 可由两份冻结源逐字重生（SHA256 `2b7525f9525a569bd0dfc7ddd4b3ebf4e73f443284e96b9a7545d945c05379d6`）。8 生产源均保持已审 review02，没有用测试改动掩盖生产 delta。

| 首红 | 证据与判断 | 修正边界 |
| --- | --- | --- |
| ExternalPublication、WriterRecovery/writer、PublicationUnknown 三个 INVALID_ARGUMENT | 都停在 `project_audit_test.go:67/184/301` 的 auditStorageConfig。旧 helper 把 httptest 的 http URL 配为 verify-full，固定 config.go:72 明确拒绝，尚未进入目标产品操作。 | 按 scheme 选择 TLS；https 仍 verify-full+原 CA，http 本地代理用 disable 并由既有代理继续访问真实 TLS fixture。修正未改生产 TLS classifier/配置约束。new2 对应三案通过。 |
| PUT nested rollback 的“不重复实际 bytes” | 首轮同时比较全部 stageGET/candidatePUT；修后仅 candidatePUT 排除 decoded length=0 marker，仍比较全部 stageGET。固定 transfer_network_test.go:70 统计所有 staging GET，旧 transfer_protocol_test.go:225 已明确一次正常完成有一次源 GET 加一次 marker verification GET。成功重试会在最终 publication 后执行 cleanup，故原计数口径确实包含不应禁止的清理 I/O。 | new2 仍在同一断言失败；日志没有前后分类数字，不能唯一断言当前红只来自 marker，也不能排除真实 payload 重读。必须保留红，定向观察真实 payload GET/PUT 与零 marker 验证，不删除“不重复 payload”断言；当前整体不通过。双 Audit 同 Tx、回滚、独立 verified candidate、实际 retry Complete 断言在失败点前仍执行。 |
| StopRevoke/archive、delete 的 RESOURCE_BUSY | 旧测试要求 RequestProjectStop 必须 nil+Pending，未容许已验 stop API 的 ResourceBusy+Pending。取消已准入 preparation 后，异步 Discard/join 与新 facts 可能竞争；旧 stopUntilSettled 本就允许 ResourceBusy。原日志不足以把具体错误唯一定位为“sealed preparation Cancel”。 | new2 仅接受 nil 或 ResourceBusy，且 report 必须 Pending，其他错误仍失败；随后真 Discard、真 retirement、最终 Stopped 与单条 revoke Audit/terminal Inspect 不变。new2 两案通过。不能将它写成已证某个内部 Cancel 返回点。 |
| StopUnknown committed/rollback/pending 最终 work_pending | 原 fixture 只 ConfirmStopped 一次且忽略 LeaseActive。固定 retireInTx 的 PUT 分支在 staging 未 cleaned 时只 gate+cleanup_gate 后返回；ConfirmStopped 在原 Tx 后 cleanObject，未再次进入 ReleaseLease。因此收到真 terminal proof不等于该次调用已退役 lease。 | 新 helper 只生成一次同一真实 Stopped evidence，最多三次正式 ConfirmStopped，每次错误立即失败，最终必须 !LeaseActive；不是换证据、直接改表或假完成。之后原 stopUntilSettled 仍必须最终 Stopped。new2 三案通过。 |

Unknown 证据未被修正触碰：objectAuditCommitStore 按同 Tx Audit action/resource/xmin 选择，并要求 transfer revoked 与 project_stops 相同 xmin，再记录原 PG pid/xid。committed proxy 实际收到 CommandComplete(COMMIT)+ReadyForQuery(I) 后丢 ACK；rollback 在发送 COMMIT 前关闭原连接；pending 保持实际 backend_xid/idle-in-transaction，再放行原 COMMIT，最后 waitWriter 证明原 writer 结束。三案均要求 CommitUnknown、分支对应的实际可见 revoked 数、当前 manifest 撤权拒绝及恢复后单条 revoke Audit。零提前 cancel 目前只以 preparation joined=0 观察，不能当作直接 callback 调用次数证明；最终独立动态 probe 仍需补直接取消观察。

预算核对：Unknown 请求 800ms、命中/返回/放行等 3s 等待均未改；原 stopUntilSettled 仍为总 3s、最多 40 步、每步 5ms，不增加循环或延时。新增最多三次 ConfirmStopped 是补齐真实多阶段退休前置，并显式要求 LeaseActive=false，不替换 stop 最终条件。两轮均沿原 scripts/test-objects.sh driver；本次未重跑。

冻结日志：new1 SHA256 `0edf30ff644f370847c87cb00f65f2cf07d37a47d96bb56452952226eb528a2b`（objects FAIL，32.786s）；已结束 new2 SHA256 `320ec203a67c83d359c8785c40c68a201f4216a71ea60f0d71a67975ec551f4b`（objects FAIL，24.109s，唯一剩余顶层为 PUT/GET 组的 PUT 计数子例）。副本、清单和精确 delta 保留在本目录。仅静审这些证据，未审活动日志/源码，不重新评估已审 8 生产源，也未独立核本次作者资源清零。所有自有命令结束、无 fixture/窗口，报告冻结后 all-stop。
