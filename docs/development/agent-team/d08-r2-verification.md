# D08 B03-R2 独立验收

2026-10-05，独立 `verification_worker`：**通过，建议采纳 R2 接受、查询与重放范围**。没有发现产品阻断。未参与实现，未改生产或作者测试，未执行 Git 写操作。主线程已将恰好 15 个源文件提交并推送为 `73db0d45d673f804a37a7232a79a611a1ef447aa`；提交内文件已独立逐项匹配冻结输入。

## 固定输入与可复跑证据

依据 [R2 卡](../work-items/recovery-d08-b03-acceptance.md) rev1（SHA-256 `45079bd7e433234733422640af9f4fe95af712f59748059230c862438e08bedc`）和 [验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。执行输入为 `git archive 39ae1f3415b8095096b0027679cf1f35650f20c4` 加作者停写的 15 源；没有运行活动主树。并行 S1 的 `00016` 与其他后续输入不在此结论内。

[源清单](evidence/d08-r2-verification/source.sha256) SHA-256 为 `d2133560cd18b1560da9b703f7c2057f31c512d8beaeb8dca6b33cb7e4a81d8a`，验收前后及提交内均匹配。[inputs.json](evidence/d08-r2-verification/inputs.json) 保存 archive、原卡、依赖锁、00013、R1 registry、旧 fixture、工具链和源指纹。使用 Go **1.27.1**、锁定模块缓存、`GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off`；MinIO 二进制、PG 17.8/16.12 与 vector 0.8.1 同恢复基线。

[独立探针完整源码](evidence/d08-r2-verification/independent-probe.go.txt) SHA-256 为 `7582788dd1516876d52efb2e9cd3e1d48fc67c5b3b6dba83738e61a5a1550e97`。它通过 Go overlay 添加测试，复用固定的真实 fixture/COMMIT 代理和帮助函数，独立编写断言及同 Tx 观察器；没有替换 Store 的 CommitResult。[复跑脚本](evidence/d08-r2-verification/reproduce.py) 从持久 Git 输入重建同一快照，不依赖旧 `/tmp`。在已获独占 Docker 窗口时运行：

```sh
python3 docs/development/agent-team/evidence/d08-r2-verification/reproduce.py /workspace/agenteam /tmp/d08-r2-recheck --fixture
```

不加 `--fixture` 只重建输入。复跑者仍须记录自有 exact ID、进程和清零；此脚本本次仅作语法检查，实际运行的完整 cwd/env/argv/退出值在 [独立命令](evidence/d08-r2-verification/independent-commands.json)。

## 实际结果与证据复用

| 检查 | 结果与范围 |
| --- | --- |
| 独立静审 | 4 个旧生产接缝与全部新生命周期生产实现、00013/契约、作者测试及原始证据相符。当前 Session/Owner 先于合法请求的回放语义冲突；新删除的路径在 User EX + Project EX 内经正式 route 端口复核；计划与最终接受分离；最终 gate/operation/participants/Audit/Event/命令 receipt/Touch 同 Tx；无 stop/inspect/cleanup 调用、无迁移。 |
| 作者 project/... unit/race/vet | 复用冻结生产输入上的成功结果；原始分段输出见 [author-checks.log](evidence/d08-r2-verification/author-checks.log)，准确命令见 [author-commands.txt](evidence/d08-r2-verification/author-commands.txt)。未机械重复全树 check-go。 |
| 作者真实 R2 组 | [首轮](evidence/d08-r2-verification/author-initial-fixture.log)执行 13 顶层、Project 44.033s，唯一失败是 failed-init 测试前置错误要求 nil error。其他 12 顶层含两命令 × planned/final × late commit/rollback 两套 Unknown 矩阵，源未变；成功覆盖 SH 阻塞、Typed 无 canonical 拒绝、原 manifest 恢复、并发、晚期回滚和查询投影。 |
| 修后 R2 与 B02 | [后轮](evidence/d08-r2-verification/author-corrected-fixture-and-b02.log)执行修正的 1 个 R2 顶层 + 15 个受影响 B02 顶层，exit 0，Project 49.754s。其余旧组复用 [98262b4 完整 B02 基线](d08-b02-recovery-baseline.md)；无关包 `no tests to run` 不计覆盖。 |
| 独立真实探针 | `sh scripts/test-objects.sh -run '^TestProjectR2Independent'`，保留原 driver 的 integration/race/count=1/timeout=6m，**4 顶层、10 子例全部通过**，Project **16.972s**。完整 [原始日志](evidence/d08-r2-verification/independent-fixture.log)。 |
| 独立工程检查 | 固定快照两 cmd 构建与 overlay integration 编译通过；构建输出为空且 exit 0，见独立命令记录。 |

作者修复仅为 [前置差异](evidence/d08-r2-verification/author-precondition-fix.patch)：现在明确检查 `DependencyUnavailable + Committed`、`LookupCommitted`、`CreationFailed/ReasonOperationFailed`、原 `CauseID/creation_id` 和 `initialized_at IS NULL`，随后保持原生命周期拒绝断言。不是吞 error，也没有修改生产或其余 12 个 R2 测试。旧输入可由最终文件反向应用此 patch 恢复，旧文件 SHA 保存在 inputs.json。

独立探针进一步证明：

- 真实 COMMIT 代理仅在原事务内核全量计划/最终 canonical 事实后武装；实际命令 EX waiter 和 `55P03` 被观察到。外部看不到未提交接受事实；planned 回滚/提交分别恢复 `not_observed/in_progress`，final 回滚/提交恢复 `in_progress/committed`，同 key 最终仅一个接受。final late commit 时调用取消仍不删除事实；保持原 operation/event/manifest 身份和 Unknown AttemptID/cause。
- 在真实 gate/operation 已写入后移除一个必需 participant，再调用合法 typed Audit/Event：两命令均拒绝，已写事实和 Touch 全回滚，保留原计划后可成功恢复。作者无 gate/operation 的 typed 反例与此互补。
- 当前撤权或外部 admin 在原 key 和变更 digest 上均先被拒绝；同 User 新 Session、用户名已变且 routes/registry 未绑定时仍可回放原操作，查询/回放无 Touch。
- 仅有最小删后 receipt 时，错 key 不可回放；撤权先于 key/digest，更新 Session 的原 Owner 可读原 receipt，不恢复 body、operation、command、Audit/Event，不 Touch。

## 失败归因、资源与边界

没有未修复产品失败。作者早期编译失败（测试常量与未使用 import）及真实首轮失败均保留原日志；不计为通过。独立第一次启动向仅接受 `-run` 的脚本传入 `-v/-failfast`，在创建资源前被拒绝，见 [原日志](evidence/d08-r2-verification/independent-launch-argument-failure.json)。仅修调用方式，使用 Go 的 `GOFLAGS=-v` 后运行原 driver；未改断言、超时或输入。

独立运行结束后核实 **4 容器 + 3 网络 exact ID 全部不存在，runtime 空，所属进程 0，既有资源不变**，见 [清零证据](evidence/d08-r2-verification/resources-final.json)；作者两轮 8 容器 + 6 网络记录见 [作者清零证据](evidence/d08-r2-verification/author-resources-final.json)。窗口已交回主线程，后续没有再次启动 fixture。

通过仅涵盖 R2。测试中的 Skill initializer、归档前置、进度变更和最终删除 receipt 是明确的自有 fixture，不证明 D10 绑定或真实 stop、drain、cleanup、archive/restore runtime、物理删除/finalizer、D05/Outbox adapter、Project HTTP/生产 root、整个 B03 或 D08 完成。报告及此证据目录为本验收唯一仓库写入范围；交付时停止写入与命令，all-stop。
