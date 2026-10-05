# Object Project Audit 独立验收结论

建议采纳规格 fcae355 rev2 的窄 Object Project Audit 能力。固定生产基线 6658a6c + 作者 final-source 13 源；manifest SHA256 061a4c19791e812e68a0755059a171984bc3bf8070197ae6f034f7943f119558。末检全部匹配。无新增产品阻断。

已独立静审 8 生产源、GET 历史完成修复 delta 和作者真实首红/修正证据；复用作者最终 Object/Audit unit/race 43 顶层/231 子例、vet、integration compile、两 cmd build，以及明确分段的新真实 10 顶层/19 子例和受影响旧 17 顶层/17 子例。integration-new-2 整组仍为 FAIL，仅其未变通过项复用；PUT payload 分类与最终双 Audit Unknown 在 final-real-1 重新通过。

独立新增 2 顶层/6 子例，最终探针 SHA256 6fa231f1e18b2d29388cd2583dcf6693e761b226a412e2c148d482e5954444c6。原 scripts/test-objects.sh -run '^TestObjectAuditIndependent'，Go 1.27.1，GOFLAGS=-v，原 -race -count=1 -timeout=6m 保持。tests/objects 9.816s；driver exit0 42.933s（连同外部清零检查 43.260s）。

- stop 普通 Audit 拒绝以及实际 committed / rollback / pending COMMIT Unknown：真实 preparation context.Err=nil、输入 Close=0、Read 仍阻塞。COMMIT 选择器命中真实 revoke Audit 与同 Tx stop/revoke，PID/xid 为 140/896、148/981、156/1067；pending 原 writer 真正终结后仍无取消。当前 stop manifest 撤销时仍不取消；恢复并确认 stop gate 后实际 context canceled、Close=2、PreparePayload 返回，再正式 retirement 与原预算的最终 join。
- 已真实释放的 GET lease 在 Complete callback 内改坏本域 retirement 或 lease owner：真实 checker 拒绝，Audit/phase/改坏的 facts 全部回滚。撤销当前 Session 拒绝；恢复后合法历史 GET 一次完成且 Audit 恰一次。

首个 startup 多传 -v 仅参数拒绝，零资源、未跑测试；改为 GOFLAGS=-v。下一轮在 Go 编译阶段因 /tmp 容量耗尽退出，未进入 probe；实际 4 容器/3 网络已两次清零。仅迁移本人可重建 cache/TMPDIR/输出到 /workspace 自有临时目录后，以上最终原测试成功。原失败命令、日志、两次清零均保留，不计产品失败或通过，未放宽断言/预算。

最终实际 4 容器/3 网络 exact ID 两次 absent，初始 2 容器/4 网络 ID/name/labels 精确不变，owned process=0，runtime entries=0。fixture 窗口已交回 root；不再 Go/Docker/源码操作。

边界：未绑定生产 Project ObjectProducer 分派、权限 gate/root/HTTP；严格 ports 的隔离组合成功不代表 D05/D08 完整模块通过。作者 joined=0 不作零 cancel 证据，由本次直接观察补足；作者初次 ResourceBusy 返回点仍非唯一定位。作者第三短轮历史外部 ID 采集不完整，只复用其窗口末全量 baseline 恢复与已捕获 ID absent 证据，本次独立轮的全部 4/3 IDs 已完整采集。
