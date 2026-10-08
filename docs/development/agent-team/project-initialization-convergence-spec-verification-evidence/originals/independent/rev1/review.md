# InitializationConvergenceAuthority scratch rev1 独立 STATIC

结论：**完整规格 STATIC PASS，无必修项**。仅覆盖冻结工程规格，不表示接口已经实现、编译、真实 PG 验收或生产绑定。作者与验收员独立；未读活动 UI 源，未写仓库、运行 Go/资源、执行 Git 或再委派。

审查输入是 `draft-rev1.md` SHA256 `34346798c4e526bf56c08cea24c4fe0ede8f59a73cb1fc8c9bbe222479db4f7b`，固定产品基线 `4089d13128da8680955005d9507c8a7da74af1f1`；`inputs01.json` 为 `057d9f57e0e7f2786444fcc685c11cda23557587c7bb9dab7bd45f478176367e`，`freeze-rev1.json` 为 `1edf2e50ddb0e90ceca74e9ff2e21767329d54cb1f8e1f37784ef23c7e288d48`。57 个来源快照 SHA256/按内容计算的 Git blob 与索引一致；与先前独立 prep 的接受技术指纹有 12 项交集，全部一致。此处没有重新查询 Git 对象。12 个正文相对链接、UTF-8/LF/末换行/尾空白、5 个新技术路径不存在、2 个旧 selector 的真实定义均静态核过。

## 实质结论

1. **必要前置及 active-only 可接受。** 固定 D10 设计第 23、57、128–134 行已经提出 failed/accepted 原工作收敛与独立可选接口；第 63 行将 archive/delete 停止交生命周期上下文。新卡只认 active，对未完成 Creation 沿 SQL 的未初始化/version1/无 Sprint/无 operation 事实；completed 的非 active 拒绝由现有 lifecycle cause/phase/participant 继续承担。Restore 后 active 可重新观察，不复活旧写。它没有改普通 Owner 的 archived Read，也没有给失败状态 Reserve/Send/Publish 权。未初始化 Project 本就没有新增取消/删除 API，此规格没有暗开一种。

2. **公开窄接口和同库事务链可实施。** 原 `InitializationRequest` 确为 CreationID/ProjectID/InitializationKey 三字段；`*project.Authority` 已持 Store 与可选依赖，新增文件即可定义方法和独立接口断言，不必扩旧 `ProjectAuthority`、constructor 或四方法 initializer。固定 `authority.go:224`、`repository.go:25`、`postgres/transaction.go:195,254` 支撑正式 service role/exact cause → RequireHeldLocks(Project EX) → InTx → 两行事实的顺序。Store 的 foreign-token/ended-token、missing-lock poison 与原内部 cause 必须原样保留；新 gate 自己不 Acquire、不持有 Commit/Unknown 的处置权。

3. **四状态与 NULL 对应完整且不误用历史。** 固定 migration 00013、`scanCreation`、`scanProject`、Creation writer 的 reservation/confirmation 与表中条件相容。新局部 helper 明确堵住旧 scanner 对非 completed 单边 protected 字段残值的布尔缺口；未完成请求两个字段非 NULL 且合法、与当前未初始化 Project 相等；accepted/initializing/failed 的 reason 各自闭集；completed 两 request NULL、reason 空、保护标识/revision 与 result 必有。writer 完成仍 Project version1，故历史 result 的初始形状可核；当前 Project 后续改名/描述/version 不与历史强等。当前 owner 必与原 Creation owner、历史 owner 同一；Skill 包可用性、event/work claim/其他域事实仍不由这个 gate 证明。

4. **拒绝分类和兼容范围明确。** 参数无效/未绑定、actor role/scope/cause、持锁/Store/scan、合法目标错配、内部 canonical 矛盾均有区分。合法另一 Creation/Project 配对是 Forbidden；先证明归属后出现反向指针或 owner 自相矛盾是 Unavailable，不以 WHERE 隐掉损坏。原成功 gate 的 accepted/failed 仍 InvalidState，initializing/completed 仍受旧 active 条件；新的只读 nil 不是 Human 当前 Session/Owner、可缓存 grant 或交易已提交证明。按当前授权设计无需给服务 gate 添加 SessionUser 锁或 Session 调用。

5. **五源内的纯/真实验收方案可执行。** `account.NewAuthority(Store, Keyring)` 可用真同 Store 构造 Project 必需 Sessions 依赖，不需要启动 Account Service，也不意味着此服务方法消费 Human Session。新私有 fixture 用正式 migrator/Store/registered Service，直接设置本域授权输入，并明确不调用旧 assemble/Skills stub，不声称真实创建端到端。SQL CHECK/FK 过不了的损坏只留纯扫描代表；真实跨行矛盾、外 Store token、SH/EX/poison、PG 实际锁等待和零写快照仍须实测。纯模拟/SQL seed 拒绝不替代这些真实断言。两旧 selector 在固定文件第 908/941 行实际存在，其旧 Account SQL 身份及 project_fixture.skills 持久 adapter 的限制已准确保留。

6. **预算、停止和范围没有绕过。** 5 新技术源 + 后授 README 可独立交付；无 migration、HTTP、root 或 Object/Skills 文件变更。离线 45s/纯 40s、真实 top 120s 含 Cleanup/包 6m、实际图与原 20 包 fixture/7 精确资源及每轮 fresh baseline/wait/双清均是未来执行门槛，未被当成已完成。方法不 Commit，因此无需为这个方法虚构 ACK-loss/Unknown 测试；caller 最终 CommitResult 仍由 caller 处理。原 D10 “没有 Object ProjectFactAuthority”的旧状态明确纠正为已接受能力，Object runtime join/OpenAI tools/SPA 三停止、Skills/root、D24/Resolution/Invocations 的未绑定未完成界线保持。

## 交付与限制

无需技术返修；正式归位或实施仍由 root 另行授权。后续产品独审应兑现规格已有的边界，尤其真 PG 约束可达负例、ignored-error 后真实 poison、锁直到 caller 终局才释放及所有辅助工作实际 join。当前没有新用户产品含义待决定。

`checks.json`/`check-command.json` 是本轮文档与输入检查原件；`check.py` 可复现该检查，不运行测试。一次定点只读查询误列未收录 `fixed/docs/development/backend/skills.md` 返回 exit2，随后改读 inputs01 明列的 D10 正式设计；它不是产品、编译或资源失败。此前 prep 原件保持。结论冻结后停止写入。
