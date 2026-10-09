# Skills Cleanup 消费层独立有限审查

结论：首完整消费层实现有限接受，无本轮已确认 must-fix。审查者未参与 Skills 消费层实现；曾实现 D05 上游，因此本审查**不对 D05 自有实现作独立接受**，只核 Skills 如何消费其正式端口。真实 PG/D05/两域原子提交仍待验。

输入为 `/workspace/agenteam-skills-cleanup` 的 `cc68e6ab` 加作者停止写入的11路径：

- `internal/central/skill/{cleanup_audit,cleanup_authority,cleanup_maintenance,cleanup_authority_test,lifecycle_cleanup,lifecycle_cleanup_checkpoint,lifecycle_cleanup_test,lifecycle_cleanup_transaction}.go`。
- 两张 `d10-skills-initialization{,-design}.md` 与 current。

同时只读首片段 `cleanup_repository.go`、原 Object authority/maintenance dispatch、Service call/Stop账本、初始化映射与正式设计§16，以理解完整消费调用链；Object/Project/SQL产品没有写入。作者后续 `tests/skills` 新文件不作为本轮输入或结论。

静态主链核对：

- 每个事务重验同Store活Tx、原Project/command/Skill/Object完整锁和当前CleanupPhase；Release discovery只发现拟建身份，真实Validate核同Tx的closed serving/gated事实。Release与本域gate在同事务，已提交后才physical；Unknown原cause/attempt保留，后继真实gated不重复Release。
- 原2s及更早parent deadline贯穿调用；物理同步实际返回后才另Tx记录completed。Cleanup纳入实际call账本；取消/空map不等于原work终局。
- local completed后按剩余集合每次一批至多32，保当前attempt及五核心；没有游标跳过低ID。D05 Pending只提交其历史进度；正确Completed后同原Tx删除Skills五核心，任何错误都退出原事务。最终Unknown重放仍取当前gate并核全部六表及本地work，不再调用已无父映射的Object。
- CleanupRelease/ObjectCleanup/Purge和三维护操作闭集、原candidate与当前worker分列；metadata仅completed、physical/Claim/Checkpoint/Finalize仅gated/pending。私有Audit外层当前gate/安全metadata后转发原ctx/Tx给真实D05 checker；普通三方法仍委托，不造native witness。

## 独立实际控制

资产 `independent_test.go` 通过 overlay 加入真实 Skills 包；使用作者明确的事务Store夹具作为测试设置，并自行提供异常Object结果。没有复制产品实现或执行D05产品。运行：

```sh
python3 -B .agent-state/skills-cleanup-review/run.py --repo /workspace/agenteam-skills-cleanup
```

`11239/3973b8 → 194d92` actual exit0，race 1.127s，4top17sub：

- 物理Completed/Pending正控，以及错operation、Failed、非空reference、原Unknown拒绝；更早parent deadline没有续期，错误不推进phase/删历史/调用Purge。
- metadata Pending保anchor；错Object/operation/State和原Unknown不发布完成、不删除本域核心，受控事务恢复先前provider修改。
- 全空正控；当前gate撤销、held reader、已cancel但未返回reader、实际returned但registration Unknown的原work、部分核心缺失均不得冒Completed，且不调用缺父Object。
- 一条joined work＋65旧attempt按各调用真实剩余集合处理，单批≤32；受控未提交Unknown不推进确认进度，下一轮33/1等边界继续，当前attempt与两域core保留。

固定Go1.27.1、offline/local/readonly/-p1/原Knowledge独占cache；首sameprocess available5,646,389,248B，末5,600,071,680B，overlay tmp已实际删除，无命令/资源在途。没有PG/socket/native。作者7top21sub、全skill race/vet在未变输入上复用，不重复执行。

## 结论限制

上述事务回滚与Unknown为显式double，不是PostgreSQL/FK/驱动COMMIT证据。真实Project→Skills→D05的opaque issuer、Release/native Audit、历史32推进、原两类COMMIT Unknown、最后D05四anchor＋Skills五核心及后序Object扫描还须独立真实窗口验证。D05规模/全部lane/00028升级与EXPLAIN、完整participant/root、Object Runtime join停止项不被本审查补足。现设计仍有历史“CleanupPhase unbound／metadata口当前不存在”措辞，应在下一必要状态归位时区分当时设计与已装配但尚未组合实证的当前能力；不因此更改正式授权契约。
