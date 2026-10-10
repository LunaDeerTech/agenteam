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

## 真实组合测试源码续审

固定输入为 Skills 树 `58073b6768f6192aa2506323b46b4a9a1d687739` 的三个新增文件 `tests/skills/lifecycle_cleanup{_fixture,,_unknown}_test.go`；结论为测试设计及源码有限接受，无本轮已确认 must-fix。这里只审未参与实现的 Skills 消费层及测试有效性，不对本人实现的 D05 上游作独立接受。作者另行修改中的两个 root 工具不在本次范围。

- fixture 绑定同一真实 Store 上的 Project LifecycleAuthority、Skills Authority/Service、Object Service/backend 与真实 native Audit checker；观察器只转发原 ctx/Tx。Project 创建、ready、accepted Delete、其他 participant 的停止状态和 scheduler 进入 Cleaning 是明确的上游 SQL 前提；Skills 与 Object 的本地 Stop 必须实际返回 Stopped 后才推进此前提。没有把未绑定 ProcessGuard.Close 当作死亡证明。
- 65 条 reader 历史由实际 OpenPackage、完整 EOF、Close 与 Joined 产生。调用前核 66 条本域 work、原 canonical reference 与零 active lease；物理完成后查实际 Deleted/cleanup completed/native Audit，并核永久零 marker 或对象实际删除。本域历史逐调用要求 66→34→2→0，保持当前 attempt 和五核心；后续 D05 元数据逐批减少 1..32 且保最后四 anchor。
- gate 注入在真实 callback 成功后的原事务内观察 serving 关闭、Release/revocation 与清理事实，再返回 sentinel；独立事务核双方事实回滚。最终注入在原 Tx 中观察 D05 四 anchor 与 Skills 五核心均已删除，并核原 Project/Object EX 锁；回滚须恢复两域，随后真实提交和新 Service 全空重放不得再调用缺父映射的 Object。
- 两个 Unknown 场景只覆盖 gate 与最终两域 anchor 原 COMMIT 被截留后晚转发。hook 读取实际 backend PID 并 Arm 原代理；代理截取完整原 COMMIT，关闭调用方、保留服务端连接，不伪造 CommitResult。另一连接核旧持久事实；实际 waiter 和 pg_locks 验证精确 advisory key、原 writer PID 及阻塞关系。释放后等待服务端 COMMIT 确认及 HeldJoined，才以 fresh Service 恢复；原 Unknown attempt 不被事后证据改写。
- 失败路径的优先 cleanup 注册在 fixture 之后，先 Release，再在已 Reached 时等待 HeldJoined，之后才进入原 Skill/Object/Store drain。后创建的锁 waiter 有自己的 cancel 与实际返回等待；后创建的 fresh Service 仅发生在原代理已经实际 join 之后，因此其后进先出 drain 不会挡住 held COMMIT 的释放。

本次仅静态逐源核查及只读 Git 状态，未启动 Go、PG、MinIO 或任何 socket。源检查 `40ef1e` 确认固定提交，三目标文件无浮动改动；两 top 五子仍待编译及真实完整窗口。上述源码接受不证明实际 2s 预算、SQL/FK、真实组合通过、全部历史规模、全 participant/root 或 Object Runtime join。
