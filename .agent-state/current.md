# Task Timeline Reader 检查点

- 正式基线main `3cea6076`；树 `/workspace/agenteam-task-timeline`、分支`ai/task-timeline-reader`。root管理所有Git；`/root/work_ui`唯一作者。
- rev1完整SPEC已获Skills未参与者有限接受，root已授权卡中六新技术路径，作者实施其中五项，第六独立测试保留给未参与者。零迁移/不改HTTP/root。
- 目标：四类已真实存在的Human Task历史、当前Owner授权、typed双向稳定分页；复用既有TaskReader/同表索引，零迁移优先，不接HTTP/Tool/UI/Context/comment/未来流转。
- 已读正式架构、planning/B0-P卡、实际表/索引/Reader/旧codec。已补齐精确错误/锁/Rows与Tx实际尾、六新文件闭集、三作者PG top及同时间真实writer刺激；SPEC可实现性获有限接受，不是PG/Clockwrapper已验。
- 与Work UI冻结输入完全分离；Work recovery06已全尾释放且wholeFAIL保持，Timeline已开始限定实施。全局current/tasks保持冻结，不复制其它线历史；Object join等停项不变。

- 首可恢复技术片段：contract/task_timeline.go、其task_timeline_test.go、work/task_timeline_reader.go三新源已实现，严格四类union/筛选、原wire、Owner每页同Tx锁、时间ID seek与Task.Version水位，未改旧源。172fc4/38147最终actual0：定向`^TestTaskTimeline`下contract两top通过；work包仅编译成功并明确no tests to run，不能冒Reader行为已验。gofmt/diffcheck通过。没有Timeline真实运行。
- 新增Reader三项纯控已实测：输入拒绝不碰Store、签名cursor绑定与固定水位、两族原wire及损坏/未知行拒绝。首次72f654为测试源编译错误并保留；修复后71793/56146e actual0，`^TestTaskTimeline`下contract两top与Reader三top通过。测试未替代真实Owner/锁/Rows/事务/PG分页证明。
- 作者PG矩阵首可恢复片段已新增`tests/work/task_timeline_test.go`：正式Create+200次Update+Add/Resolve共203条四族历史、sameStore实际clock Scan限定覆盖、receipt持久关联、Reader重建、默认50/改7/200分页、反序types/同User新Session、双向W排除后写与原重放无第二事实。仅gofmt/差异检查通过，尚未集成编译或PG执行；AuthorityAndCancellation/IntegrityAndCompatibility两top仍待写，不能当整矩阵ready。
- 当前该新集成测试与card/current三路径冻结供root checkpoint；离线只读modcache为原model-ui-recovery/go-mod，复用本人无竞争的Work implementation/gocache，Go1.27.1/local/off/-p1/GOMAXPROCS2；未新建GB缓存。无在途Go/测试进程。下一补正式writer PG矩阵，再交独审；真实窗口另fresh grant。
