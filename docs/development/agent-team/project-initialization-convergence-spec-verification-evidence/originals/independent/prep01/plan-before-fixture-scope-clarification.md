# InitializationConvergenceAuthority 独立准备

状态：PREPARED，仅固定接受 `4089d131` 的只读检查；未读取作者活动草稿，未审新规格、未执行 Go/数据库/网络/停止项探针，未改仓库或 Git。来源指纹见 sources.json；13项技术源逐字节匹配已接受 Audit 的 full-input02，四份 D08/D10 正式文档另冻当前指纹。此次是 D10 §10 已明确可拆前置的验收准备，不是 Skills/创建 HTTP 的实施或接受。

已核实的基础：D10 §10 给出独立可选 `InitializationConvergenceAuthority.ValidateInitializationConvergenceInTx(ctx, tx, actor, InitializationRequest) error` 形状。现 `project.Authority.ValidateInitializationInTx` 只接受 active Project、initializing/completed creation 与一致 initialized 位；它要求原 registered project-initialization Service、同 Project/Creation cause、原 initialization_key、creation↔project↔owner映射，以及同 Store 活 Tx 内的 Project EX。新口不能通过改旧写口成功条件实现。

冻结新卡后按以下顺序独审，不预设未冻结的错误分类或 lifecycle 扩展：

1. **公共口和能力范围。** 保持原 ProjectAuthority 和 initializer 四方法、opaque issuer/plan 不变；新可选接口只确认本次原工作收敛资格，不返回 Owner grant，不发起 Tx、补锁、网络、Audit写入、Reserve/Send/Publish 或 initialized 更新。后继失败 Audit/对象清理仍需各自正式 authority；本口 nil error 本身不是外域写权限。D05 facts/初始化 Audit wrapper、完整 Skill服务和生产根绑定仍是不同前置。
2. **真实 Tx 和锁。** nil/取消 ctx、零/伪造/结束/异 Store Tx、缺锁/Project SH/错 Project EX；在查询前 RequireHeldLocks，底层错误即使调用方忽略也须真实 poison/回滚。SQL 只从 caller InTx 取得；不 Tx 外先查后复用，不新增无界重试或 WithoutCancel。测试须证明真实 foreign Store/token 和两连接 EX 竞争，受控调用顺序不能冒真实锁。
3. **精确身份。** 只收 registered project-initialization Service；Human Owner/admin、Agent、其它 Service、System scope、错 Project/cause、形状合法但另一 Creation/key 全拒。InitializationKey 与 creations.command_key 分开；请求只有原三字段，不接受 caller owner/state/completed 标记。两个持久 owner 必须相同，Project.creation_id 和 Creation.project_id 双向匹配。
4. **状态一致性。** 明确 accepted/initializing/failed/completed 对 initialized、Project lifecycle、版本/creation state、request保留/清除、safe_reason、protected Skill/revision、safe_result 的合法与损坏组合。已有 scanner 会验证基本 typed字段，但仍需跨行关系。completed 的历史安全结果只能核原 ID/Owner 等身份，不盲要求等于后来合法 rename/version 的当前 ProjectRef。新卡应明列 archiving/archived/deleting 的行为；删除使用既有 lifecycle cause，不借初始化口逃逸。新写重试必须先由原 D08 creation重新进入initializing，失败收敛不能直接授新发布。
5. **Human 与服务边界。** 既有普通Owner/creation-status入口仍先当前Session再Owner，管理员无旁路，未初始化项目不成为正常ProjectRef。服务持久恢复不冒充原Human，也不把原Session仍在线作为恢复原工作的替代权限；只按当前持久的原creation/project/owner事实收敛。保持原写Authority的accepted/failed拒绝与initializing/completed正例，证明新口未改变它。
6. **验收最小组合。** 纯契约/受控状态矩阵先核接口与无副作用；真实PG用正式Account身份及原Create流形成创建事实，受控initializer只隔离状态，明确不能证明D10/对象发布。辅以合法SQL可存而新跨行关系拒绝的损坏代表；SQL CHECK先拒的种子失败不得冒待测Authority拒绝，原错误与静态归因分开。状态只读前后比较必要canonical字段/行数，不拿跨Login新增System Secret refs的全表快照误判。真实缺锁/foreign Store/poison/竞争保留 actualwait；outer CommitUnknown不能因一次成功校验就变Committed或外部可用。

可复用旧 `tests/project/b02_fixture_test.go` 的真实Store/Project/initializer接缝，但它的用户/Session采用SQL种子，不能称新增主链正式Login身份。后继卡应给精确来源和主链责任。原fixture/actualwait工具可按共同冻结输入做最小差量，不复制全树；所有 Go/资源命令等待正式授权和实际闭包，真实资源须新取live PID/starttime、空间与精确资源baseline。

当前无产品决定需要用户补充；待作者冻结卡后形成正式 STATIC 结论。Object runtime join/OpenAI tools独审/SPA并发发布三停止不动；完整Skills、创建HTTP与Object发布仍被真实依赖阻挡，当前准备不触及其探针。
