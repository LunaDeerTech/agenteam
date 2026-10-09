# Skills P2 有限独立补集

固定输入 eaad50fd；作者生产保持冻结。独立性针对 Skill 实现与业务判据，不把本测试作者的 harness 自查称未参与者验收。root 已批准以下范围；编译、精确发现、harness 接缝审查与真实授权分别完成。

| 场景 | 前置与刺激 | 可区分的断言 |
| --- | --- | --- |
| 纯确认 | 实际 Service 四口和正式 opaque plan，受控 Store/Project 返回当前权限变化及原 Unknown | 已有 plan 不缓存当前 grant；foreign issuer/原 request/弱锁不成功；不补锁、不开嵌套事务、不做 Object I/O；原 Fault/Unknown 不被改义 |
| 纯 Package | 实际 Service/P1 typed PackageReader 与显式 Object reader，分别 hold 原 Read 返回、Close 返回和取消回调 | 原调用实际返回前本地 Joined/工作不能提前完成；原 Close 错误保留；两个返回边界分列，不以 EOF 或 cancelled 代 join |
| 真实确认正负 | 一次真实 D05 发布；精确原 plan、同 Store/Tx/全锁确认，对照弱锁、重建 issuer | 原 Skill/revision/Object/canonical/Audit 恰一次；拒例无新增物理/持久发布 |
| 真实当前重验 | discovery 后用正式合法 fixture 状态使当前 gate 不再允许；不变原 plan/actor/request | 原计划仍拒，精确原物理身份不替换；规范 seed 恢复单列，不称生命周期命令 |
| 真实包当前权限 | ready seed 后当前 Owner OpenPackage 正向；错 Session/非 Owner 负向 | 拒绝不交付包、不新增 Object reader lease 或 Skill work；不复用初始化 Service 权限 |
| 真实包取消尾 | 同一个真实 D05 reader外层原 Read/Close 返回用 channel hold，原 context 取消 | held期间实际 Skill/P1 状态与SQL work、D05 lease分别观测；放尾后保原返回和错误、查实际记账，全部 goroutine actual join |

最后四行为一个新 exact top `TestSkillIndependentP2ConfirmationAndPackage`，复用既有七资源 fixture。held adapter 只延迟正式 reader 的原调用返回，仍转发原参数/字节/error，不制造 Object 成功或额外读；这种刺激不证明 MinIO 网络阻塞，也不把本地 Joined 当 D05 lease 持久释放。

不重复已通过作者全矩阵。真实 PG COMMIT Unknown、迁移和 Stop 的已有原范围证据按卡复用；本补集不会扩大它们。未实现的 Cleanup/root、D05后段清理/00028、完整participant、Runtime join停项和foreign进程退休保持未验证。

实际已验：`python3 .agent-state/skills-p2-independent/pure.py`（21592→238565 actual0，race 1.068s）执行前两行 2top/7sub；通过只说明真实 Skill/P1 代码在明确受控 Store/Project/Object 输入下的判据成立。后四行尚未编写完成、编译或实际运行；无真实 SQL、D05、harness 或全 P2 完成结论。
