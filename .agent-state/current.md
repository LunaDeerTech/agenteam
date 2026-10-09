# Task Timeline Reader 检查点

- 正式基线main `3cea6076`；树 `/workspace/agenteam-task-timeline`、分支`ai/task-timeline-reader`。root管理所有Git；`/root/work_ui`唯一作者。
- rev1完整SPEC已获Skills未参与者有限接受，root已授权卡中六新技术路径，作者实施其中五项，第六独立测试保留给未参与者。零迁移/不改HTTP/root。
- 目标：四类已真实存在的Human Task历史、当前Owner授权、typed双向稳定分页；复用既有TaskReader/同表索引，零迁移优先，不接HTTP/Tool/UI/Context/comment/未来流转。
- 已读正式架构、planning/B0-P卡、实际表/索引/Reader/旧codec。已补齐精确错误/锁/Rows与Tx实际尾、六新文件闭集、三作者PG top及同时间真实writer刺激；SPEC可实现性获有限接受，不是PG/Clockwrapper已验。
- 与Work UI冻结输入完全分离；Work recovery06已全尾释放且wholeFAIL保持，Timeline已开始限定实施。全局current/tasks保持冻结，不复制其它线历史；Object join等停项不变。

- 首可恢复技术片段：contract/task_timeline.go、其task_timeline_test.go、work/task_timeline_reader.go三新源已实现，严格四类union/筛选、原wire、Owner每页同Tx锁、时间ID seek与Task.Version水位，未改旧源。172fc4/38147最终actual0：定向`^TestTaskTimeline`下contract两top通过；work包仅编译成功并明确no tests to run，不能冒Reader行为已验。gofmt/diffcheck通过。没有Timeline真实运行。
- 新增Reader三项纯控已实测：输入拒绝不碰Store、签名cursor绑定与固定水位、两族原wire及损坏/未知行拒绝。首次72f654为测试源编译错误并保留；修复后71793/56146e actual0，`^TestTaskTimeline`下contract两top与Reader三top通过。测试未替代真实Owner/锁/Rows/事务/PG分页证明。
- 作者PG矩阵首可恢复片段已新增`tests/work/task_timeline_test.go`：正式Create+200次Update+Add/Resolve共203条四族历史、sameStore实际clock Scan限定覆盖、receipt持久关联、Reader重建、默认50/改7/200分页、反序types/同User新Session、双向W排除后写与原重放无第二事实。仅gofmt/差异检查通过，尚无PG执行。AuthorityAndCancellation/IntegrityAndCompatibility两个作者top随后已补当前Session/Owner/lifecycle、原锁与SQL/Rows取消、坏行/lookahead及旧codec/Reader兼容，尚待动态证明。
- 最新首次集成编译65213 actual1因作者测试类型名ErrorCode/TaskEventMarker误用；按正式Code/TaskEvent修正后99817 actual0，普通integration可构建。三个作者top源码已齐，真实PG与作者矩阵独立验收未完成。
- 当前该集成测试与card/current三路径冻结供root checkpoint；离线只读modcache为原model-ui-recovery/go-mod，复用本人无竞争的Work implementation/gocache，Go1.27.1/local/off/-p1/GOMAXPROCS2；未新建GB缓存。无在途Go/测试进程。下一按三个精确作者top分别申请真实PG窗口；无整组联合selector，原预算不变。

- Reader四技术源获Knowledge未参与者有限独验接受：独立overlay23681/acd731 race0（2top5sub，受控Store/Authority＋实际Rows方法），作者strictcodec52491/24163d race0；不冒真实PG/Owner/物理锁。本人当前三作者top race候选41409实际编译0，后继-list恰PersistenceAndPaging/AuthorityAndCancellation/IntegrityAndCompatibility三项；未执行任何top。复用原Work独占gocache和只读modcache，无新GB缓存。
- 实际候选：`output/ai/task-timeline/task-timeline-race.test`。原PG-only监督工具源未改，另仅构建原driver到本树输出。计划每个top独占fresh窗口，Go6m/driver105＋15/outer123＋3/TCP75保持；首top坚持正式203历史＋2后写与两重放，第二top含当前权限/生命周期及3实际取消，第三top含6隔离损坏行与旧接口兼容，每top一个owned测试DB。无实测耗时，超过105必须原FAIL，不减事件量或扩预算。本卡只读库不增加HTTP/defaultroot产品接线；PG实际Wait、两ID双退役、runtime/desc/TCP/input尾仍需完整。
