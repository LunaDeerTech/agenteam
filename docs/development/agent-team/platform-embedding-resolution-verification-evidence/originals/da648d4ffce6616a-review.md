# Platform embedding 六 PG 源独立静审

STATIC PASS，未发现必须修正项；STOP。依据已接受 rev3 `f6535ef6`，完整读取原 `a442fcb8` 六候选，并核安装六源逐字相同，共 76,506 bytes。policy `c8ccd095`、pure `dd1e60b7` 复用此前接受，不重做纯阶段或产品审查。没有运行 Go、PG、网络或业务资源，没有修改六源及其它 Model 产品。

| 范围 | 静态核对结果 |
| --- | --- |
| fixture | 正向身份复用正式 Bootstrap/Invitation/Redeem/Login 和 Project.Create，同 Store 组合 Account/Project/Model/Secret/Audit/Outbox。只有未绑定的 Knowledge/Memory 事实使用私有关系表；分别保存 subject、Agent、operation、input 与接受事实。Discover 绑定完整 request/Session 与行版本，InTx 先验证同 Store、持锁、当前 Session/Project Mutate，再重读独立字段；Memory Agent/Project 与 Knowledge 闭集分开。 |
| Selection | 两 purpose、未初始化/未配置、普通 Owner 与 admin 无旁路、只读 Select 零接受、副作用计数、显式/四用途共同 version、独立 Summary version、合法但 unsupported 与 disabled、System/Project 边界均有对应断言。缺 input capability 和正 MaxOutput 的构造符合当前公共类型，能够到达目标 profile 拒绝。 |
| Atomicity | 有/无 ref 的外 Tx 接受事实、snapshot、binding、lease 提交/回滚一起核对；Secret 错误传播，无 ref 零 Acquire。真实选择更新及跨 Provider 删除替换使用 SH/EX holder、pg_locks/pg_blocking_pids 观察和实际 pending 结果，旧 plan 拒绝与新 plan 接受分开；同 Call canonical lease 与新 Call 独立验证。 |
| Authorization | 坏事实既针对旧 plan，又提交到私有事实表后重新 Discover，避免只证明 mapping 漂移。检查当前事实计数、零 Secret/最终副作用及恢复；包含正式 Logout/新 Session、另 Owner/admin/项目 gate、完整 opaque request、foreign issuer、同库异 Store、零/过期 Tx、缺锁/弱锁 poison、exact-Tx witness 与篡 LeaseID。 |
| Replay | prepared 保 SnapshotID；draft 改变递增 plan_version，只有 consumer mapping 改变不误承诺递增。committed 原 snapshot/lease 在选择切换、停用、endpoint 更新、合法删除/重建 authority/新 Session 后重验；接受后 input/phase 漂移不能借历史授权。两 purpose × 有/无 ref × prepared/committed 的改义矩阵与 rev3 的 canonical Forbidden 优先序相符，并实际断言目标授权已达；released 仅测试元数据反例，不宣称生产 release。 |
| Unknown 与退出 | stage 2/3 对真实 Store commit/rollback 后结果做装饰，保留返回的原 attempt/cause并核持久后态；原 Command holder/waiter、取消零结果、释放后同 key 确认分开。不是物理 COMMIT-ACK 丢失。新 holder/pending 的 cleanup 注册在等待前，取消后仍接收实际调用终局；正常路径也显式 wait/release，SQL 事务在结果发送前已返回。 |

复用助手的正式身份、当前权限、Secret witness、事务、结果装饰及 cleanup 调用关系已核；未以便利 SQL users/sessions 或 allow-all consumer 替代。私有事实坏行的 SQL 是受控反例，业务配置正向走正式服务。Secret 只 Initialize/StopMaintenance，本候选不启动 RunMaintenance；没有 Provider/Embed/材料调用、生产 consumer 或 serving 绑定。旧 direct/Memory/Summary 兼容结果仍依已接受纯阶段及后继规定的真实回归组合，不能仅凭新六源宣称通过。

作者 `pg-compile01` 的 15 个固定原件均指纹一致：Go1.27.1 的 race integration compile 实际 exit 0 / 7.512s、vet exit 0 / 8.975s；各内 40s、外 45s，direct wait 和两次 owned process-group 空记录齐。41 份 vet 配置原片段及六新源引用可对应；没有执行 binary、list 或测试体。486 输入 before/after 同一仅引用作者原记录；本次只核 22 个必要源/助手与其原指纹，pure 单独引用此前阶段，不把其测试文件列为 tests/model 编译输入。不重建图、不读当前 global dist/embed。后续 root 的 Audit private59 临时资产窗不回写此历史编译结果。

未验证：五新 top 的真实断言/耗时/失败 cleanup，六旧 Resolver 与三旧 S3 回归，独立实际 A/B，以及确切脚本/helper/list 和资源输入准备、actual PID/资源/TCP 双清。后续仍按卡使用单 offline 45s、每实际 top 120s 含 Cleanup、每包 6m、原内层 20s 或更短；真实窗口和驱动须 root 单独移交。静态意图及作者编译不证明这些动态门槛，也不完成 README #9、生产绑定或整卡。三停止与 Jina 边界保持。

证据见 `evidence.json`；保留本次证据整理的 KeyError（误把此前 pure test 当作 PG import）及只读定位失误，已按真实输入范围修正，没有据此改代码、重跑 Go 或扩闭包。另一个已接受的 Audit OpenAPI 文档安装记录单列，不混入 Model 结果。所有本任务命令已终局，无自有资源或后台工作；STOP。
