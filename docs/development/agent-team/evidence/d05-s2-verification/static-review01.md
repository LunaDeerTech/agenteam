# D05 S2 固定生产副本提前静审

2026-10-05，独立 verification_worker。结论：原副本有两项须修复的生命周期问题，不可据本轮静审采纳 S2。没有运行 Go、Docker 或真实 fixture；没有参与实现或读取活动修复。第一项为确定静态调用链，动态反例仍待作者；第二项已有作者旧回归失败日志，静态链解释了其失败，尚未独立复跑。

## 固定输入及已做检查

- 生产输入：作者只读 `production-review-01/input.json`，SHA-256 `b81c98af9084172e952a4af5b02680738efc26c07e53d17d7e8761bab218aa45`；18 个文件复制到本目录 `fixed/`，逐项 SHA 与 manifest 相同，末检仍相同。
- 基线：`49c6589c3919cad62a4bae2c993b5fd953d36b1e`。15 个旧接缝逐项对比该 commit；三个新增文件全文静读。基线 `spool.go`、`cleanup.go`、`download_attempt.go`、旧 source cleanup 测试及 rev2 卡另以 `git show` 与先前只读固定副本比对，五项字节相同。
- 作者旧红日志：`author-compat-core-01.log`，原始 `/tmp/agenteam-d05-s2-author-ufzmxn_g/logs/compat-core-01.log`，SHA-256 `1f9171ebe4d01a9cdda5a247ec306426e631acf8c3e28f155789384a0ab35f8d`。这是作者证据，不是独立运行。
- `checks.json` 保存本轮哈希和五项基线核对；`diffs/` 保存基线到固定生产的差异。未索引或据主树的活动候选作结论。

实际命令类别：`git show 49c6589...:<path>`，`nl -ba <固定副本> | sed -n '<限定行>'`，`rg -n '<函数/断言>' <固定副本>`，`sha256sum <manifest/log>`；自有 tmp 中以 Python `hashlib.sha256` 核 18 源并以 `subprocess.check_output(['git','show',...])` 核五项基线。所有这些命令 exit 0。没有 Git 写操作或仓库写入。

## S2-SR-1：独立 prepared operation 可在真实 writer 结束前 join

严重程度：高；状态：确定静态缺陷，尚未动态复现。以下行号均指本轮原固定候选，`spool.go` 行号指固定基线。

触发路径是公开的独立 `PreparePayload` → 外部原 Tx `ReserveUploadInTx` → `UploadPrepared`，不经 `PutObject` 的外层 operation。准备工作由 `PreparePayload` 自己拥有 operation/release；attempt work 在 `newPrivateCandidate` 随 prepared.operation 登记。

1. `upload.go:546–550` 在 PUT 返回后先 `body.Close()`，随后调用 `backend.verify`；`spool.go:551–575` 表明这个 Close 已令 spool.busy=false，但 verify 与后续 `finishWriter` 尚未结束。
2. `service.go:413` 的 context.AfterFunc 在 prepared.operation 被 stop 取消时调用 `DiscardPrepared`。`service.go:429–438` 只凭 spool.Discard 成功即调用 `entry.release()`，没有检查 `r.writers` 或完整 UploadPrepared 是否实际返回。
3. `service.go:159–168` 的 finish 调 `finishOperationWork`；`project_work.go:355–375` 对同 operation 的全部关系直接标 ended 并尝试写 joined。它没有观察真实 writer 的 `done` 或 verify/finishWriter 尾部。
4. `project_lifecycle.go:223–237` 消费已 joined/ended work；`checkpointWorkNative` 在 `project_lifecycle.go:364–375` 据 preparation work 把尚有真实 writer 的 attempt.io_closed/lease 标终局。因此 stop 可先于真实 verify/回写返回 Stopped；operation 计数也可能过早删除。

最小修复边界：保持已有 API 与授权，显式持有 UploadPrepared 从启动到 PUT、verify、finishWriter 及尾部全部返回的生命周期所有权。DiscardPrepared 只能完成准备资源的部分，不得替代已启动 writer 的实际 join。不要只依靠 PUT body 的 spool.busy。

必要反例：独立 Prepare/Reserve/UploadPrepared，大 body；明确 PUT 完成且 spool body 已 Close；在真实 verify/body/Close 或 FinishWriterAccess 的完整锁 AcquireAll 之前保留不合作阻塞，再 Archive/Delete 或显式 DiscardPrepared。必须在真实 writer 仍活时观察 Pending、work 未 joined、native writer 未被伪造关闭、共享生命周期仍持有。阻塞若只放在已经取得 Project/Object 锁后的 Tx 内，会由锁暂时遮住本缺陷，不能单凭那一种场景宣称修复。随后释放阻塞并证明真实收尾后可收敛。

## S2-SR-2：未 open SourceLease 的 Force 后 join 重新启用取消预算

严重程度：中；状态：作者实际旧回归红 + 固定输入静态定因；尚未独立复跑。作者日志明确：`TestObjectSourceLeaseUnopenedCancellationSharesShutdown/force`，`source_cleanup_test.go:150`：`Force did not join cancelled source release DEPENDENCY_UNAVAILABLE`；objects 包 28.289s，失败日志保留。

1. `source_reads.go:313` 为 CancelSourceLease 的异步 release 登记 cleanupContext；release 争用真实 Object 锁。
2. `service.go:218–225` 的 Force 取消所有已有 cleanupRequest；原 release 因其已取消 context 返回，且 durable lease 应保留 active。
3. 新增 `source_reads.go:326` 随即调用 `finishProjectWork`；`project_work.go:350` 又申请 fresh cleanupContext。`service.go:244–250` 以尚未到期的 forceContext 为 parent，故这是一个仍活的 DB 重试预算，而非被取消的原 release 预算。
4. joinProjectWork 再争同一 Object EX 锁，直到 Force deadline。旧测试 `source_cleanup_test.go:96–159` 故意保持该锁直到 Force 返回；因此 Drain 的 500ms 预算同步到期，产生上述失败。

最小修复边界：source release 后的 join 尝试沿用或取交集于原 cleanup 取消/剩余预算；已取消时保留 ended 与可恢复 checkpoint，不以新的活 context 立即重试。Force 应能等待实际已取消 release 返回，同时保留未成功持久释放的 native lease；不能修改旧测试断言或延长 Force 预算。

## 其它已核路径与剩余门槛

静读已追踪：源/目标各关系集合、Prepare/IssueDownload 在 body/resolver 前的 confirmed 登记、AcquireSource 原 Tx/未 open handle、普通 reader/download 尾部、独立 verifier、cleanup 实际 worker/fence 与旧 claim、跨项目 recovery child operation、preflight/gate/checkpoint 三段提交、精确 process 原锁屏障、5 lane keyset、100 主项及 1000 关联预算、独立最终全量 EXISTS、Archive 普通读区分，以及本地 transfer join 与真实 remote retirement 分离。目前没有形成额外确定硬阻断；这不是上述行为测试通过的结论。

两项修复后仍须：冻结完整 28 源与实际差异；读修复 delta；保留失败/原红证据；按既定 21 新顶层及冻结差异确定的旧 objects/download/transfer/runtime 集合做真实独立验收。Unknown 原 writer、cleanup 原/新 claim、multiple-project source 与 Archive 合法读都仍属真实验收范围。作者小 body 自动 EOF 前置失败、后来修正 fixture 以及已清理原 DB 无法补取 SQL 的限制，不由本轮静审覆盖或消除。

本轮到此 all-stop：未启动资源，未拥有 fixture 窗口；自有 tmp 证据之外没有写入。最终 S2 验收尚未进行。
