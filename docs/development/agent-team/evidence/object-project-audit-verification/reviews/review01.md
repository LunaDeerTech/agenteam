# Object Project Audit production-review-01 独立静审

结论：发现 1 项高优先级兼容阻断，当前 8 源候选不建议采纳。仅静态审查，未执行 Go、Docker、数据库或动态复现；作者测试不在本轮输入内。

## 固定输入

- 基线：`6658a6cb1f29299521773bc8dc86b2f607b8c809`（已验 S2）。
- 规格：`fcae3558f00131a0bcfbd40723b36525030bee63` 的 rev2，SHA256 `f63ed15f709af4d577830886cc7b7b5e18810ce598b4a0e5d7cec0e08a4bb575`。
- 作者冻结清单：`/tmp/agenteam-object-audit-author-t97qa_1s/production-review-01/input.json`，SHA256 `18721eecef8df72a2cb99df8ea657f82a8bc0784db0ee8abaa10fb54d740f3e5`，8 源逐一核对通过；自有副本在本目录 `candidate/`，六旧源对固定基线的 diff 在 `diffs/`。
- 相关固定依赖按需读取：本目录 `dependencies/` 与 `fixed-reads.json`；另复用 `/tmp/agenteam-object-audit-prep-nhs0a134/fixed/` 的 6658a6c 文件及其 `inputs.json`。未读作者活动生产或活动测试。

## 阻断 R1：GET complete 将历史完成事实误当成新读取准入

位置：候选 `internal/central/object/project_audit.go:250` 对 complete 调用 `checkTransferAuditLease(..., true)`；同文件 `:315` 强制 lease active；`:326` 拒绝 object cleaning/Deleted，`:330` 对 GET 强制 Available。四处共同使合法的迟到 GET 完成无法通过新 Audit，整个 complete 事务回滚。

固定基线可达路径如下（条件是当前真实 Runner/Operation、Owner、Project Converge 仍授权，不用历史事实绕过当前权限）：

1. 真实 GET grant 已签发且收到可信 Completed evidence，但业务 `CompleteTransfer` 尚未执行。独立 retirement 证据先到：`ConfirmStopped`（`transfer_recovery.go:139`）在当前全授权下 checkpoint 真实 retirement，再走 `retireInTx`（`:228`）。GET 不进入 PUT staging 分支，最终调用 `ReleaseLeaseInTx`（`:275`）。后者经真实 lease authorization，把 lease 置 released（基线 `read.go:443`）。这一流程不改 GET transfer 的 phase/revoked，也不虚构 complete。因此可留下 `phase=issued, revoked=false, lease=released` 的合法状态。
2. 此时调用 `CompleteTransfer`，旧代码在当前 Runner/Operation、owner/gate `Converge` 后，核确切 Completed evidence/digest、拒绝 revoked/failed，再 UPDATE complete 与 Append。它没有“lease 必须仍 active”的条件。新 checker 却在 `project_audit.go:250/315` 拒绝，即使对象仍 Available。
3. 还必须处理对象清理状态：`DeleteUnreferenced`（基线 `cleanup.go:235`）在正式 cleanup 授权且 refs=0 后可 `gateObject`（`:149`）设 cleaning；active transfer lease 只阻止后续物理删除和 finalization，不阻止 gate。若 retirement 已完成、全部引用和 attempts 已合法清理，`finalizeCleanup`（`:476`）可 UPDATE object state=deleted，但保留对象 metadata、transfer 与 released lease 行。单对象清理不等于 Project 的最终全事实 purge。
4. 上述 cleaning／逻辑 deleted 状态仍不被旧 GET Capture 路径排除：`access.go:148` 对 TransferAccess 直接进入 `transferAccessFacts`，其绑定的是 transfer/upload IDs/版本，不含对象可读状态；`transfer_access.go:198` 对 GET Capture 使用 Converge。`t.within`（`:222`）没有调用通用 `accessWorkBefore`，实际顺序为 Acquire、authorize、extras Validate、observe/register native work；`project_work.go:161` 的注册核 epoch/identity/revoked/joined，不读 payload 可用性。`transfer_store.go:47` 直接加载 transfer，其存在不以对象 Available 为条件。因此新 `project_audit.go:326/330` 也会误拒这些完成回报。

这不是要求在撤权、transfer revoked/failed、错 evidence，或 Project 最终 purge 已移除 transfer 后接受回报。它只要求新 checker 保留既有、仍获当前正式端口授权的历史 GET 完成记账；完成记账不需要新 backend GET/PUT，也不恢复对象字节或重新签发 material。

最小修复建议：在 checker 中区分 issue 与 GET complete。issue 继续要求真实 active lease、Available 且未 cleaning；GET complete 核当前授权阶段 proof、精确 transfer/object/owner/manifest、真 Completed evidence/digest、原 lease 绑定，以及 released 分支所对应的真实持久 retirement/lease 事实，不使用新的可读 payload 门槛。保留 PUT publication 的实际 candidate/committed/Available 条件、所有 current-auth 与 revoked/failed 拒绝。不改旧业务 API、contract 或 retire 流程。

作者最小真实回归可用三个状态分支：retirement→Complete（对象仍 available）、refs 已合法释放→cleanup pending→Complete、retirement→单对象完成清理→Complete。都通过正式端口形成事实；配一项当前撤权或错误 evidence 否定，确保补丁不把历史 witness 变成授权。先保留原候选红例，再复验修复，独立最终验收按受影响输入补测。此处仅提出可复现路径，尚未运行这些用例。

## 其余静态核查

- constructor 无 SQL/Service/backend 依赖；nil/typed-nil/zero 拒绝，使用 `reflect.Value.Comparable` 避免动态不可比较值 panic。exact Store/Tx、全 Entry/Key 再读当前 InTx；不是 SemanticDigest 授权。Audit Entry 的 associations 为字符串值字段，Metadata.JSON 返回字符串生成的新字节片；本次未见 caller 共享 mutable slice 可同时改 witness/entry 的通路。
- 私有 stage 只在既有实际 Acquire/Validate、native work 或已核 stop callback 后 mint。外部 `PublishVerifiedInTx` 在它自己的 Validate/owner/gate/verified 检查后补 stage；checker 自身不开 Tx、不再次 Acquire、不调用 backend。Transfer primary/extras 原 union 仍先全部验证。
- Publication/Delete 保留原 Audit 在终态 UPDATE 前的顺序。Writer failure 允许 upload committed，核原 attempt/process/已释放 writer lease；recovery ord1 核 abandoned/cleaning 与实际 cleanup operation。Delete cause 仍取最早 `created_at,id` operation。未见把 private candidate/staging 混为同一阶段的代码路径。
- Transfer 完成重读 canonical row，保留实际 Completed proof；PUT 内层 Publish 只在其函数局部派生 ctx，外层 callback 原 transfer ctx 未被修改，后续 transfer Audit 仍可取原 stage。此项仍需最终双 Audit 原子回滚实测。
- 唯一重排在 `gateProjectTransfers`：先固定原 ids/rows，在原 Tx 执行原谓词 UPDATE，再逐项 Append；错误沿原 callback 回滚，未新增提交/IO。普通 Cancel 与 S2 stop 都在原 revoked UPDATE 后 Append；未强加 failed/cleanup_gate。
- S2 stop 仍在 gated `commitError` 成功后才能进入 cancel，新增 proof/Audit 均位于该原事务内；read-only/terminal 预检没有新 stage。六旧源 diff 未改变 UploadPrepared writer-reference 注册/Discard 排除/return join 修复。以上为控制流核对，不能替代真实 Unknown、原 writer/locks、callback actual join 与资源清零证明。

## 交付边界

本轮仅写本自有 tmp；所有命令为固定 `git show/ls-tree/grep`、副本 `rg/sed/nl`、Python hash/复制/diff。仓库与作者冻结副本未改，无 Git 写操作，没有启动 Go/Docker/数据库进程，没有占用 fixture 窗口。8 源和 input.json 在交付末再次核对。报告冻结后 all-stop；等待作者修复冻结 delta，再按主线程安排做最终独立动态验收。不宣称 Object checker、生产 Project 映射/root/HTTP 或整个 Object 模块通过。
