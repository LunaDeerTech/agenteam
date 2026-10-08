独立 Model A＋B 实际 PASS，完整退役后 STOP。

在两份独立单轮 grant 下，本人亲跑已独审 overlay；实际 driver每轮先通过999输入门禁，四份前后记录一致。A为两purpose的真实归属／当前输入撤权与selector写入锁竞争，9 RUN/PASS（6叶）；B为提交后Unknown结果修饰＋交付取消后的历史确认，以及provisional acceptance后取消并实际回滚，7 RUN/PASS（4叶）。这两top与作者测试不同，未另跑编译/list或重试。

A top6.43s／fixture63.889s／outer118.580069s；B top6.28s／fixture57.358s／outer112.947926s，实际工具均exit0。每轮fixture和driver各自actual wait，0 adopted、2 watchdog joins；88／84是进程观察数。各7个精确ID两扫absent，共14互不重用；owned/runtime双空。TCP尾部分别51.422573／52.453116s，75s预算内双清，仅补充host轮询；8个新增非owned PID1 shim未wait或处置。所有owned命令／资源已退役，窗口释放。

B中的COMMIT／ROLLBACK确已在数据库发生，Unknown是返回结果的受控修饰；不称物理ACK丢失。新Authority＋正式新Session确认同一snapshot/lease，不等于进程重启。私有Knowledge/Memory严格事实未绑定生产consumer，亦无Provider外部调用、serving／维度／Invocation验收。原作者首FAIL及cleanup=false等历史原件保持；作者五新／两旧证据独立复用，本报告不替代整卡收口。

A短交接4c0644f9保持；本evidence仅引用必要A原交接与B原件，不复制source树或大闭包。无后继执行。
