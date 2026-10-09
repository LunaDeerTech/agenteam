# Task Timeline Reader 检查点

- 正式基线main `3cea6076`；树 `/workspace/agenteam-task-timeline`、分支`ai/task-timeline-reader`。root管理所有Git；`/root/work_ui`唯一作者。
- 仅SPEC卡`docs/development/work-items/d11-task-timeline-reader.md`与本文件有写权，rev1完整草案已冻结待独立SPEC审查。未写产品/迁移，未编译或真实运行。
- 目标：四类已真实存在的Human Task历史、当前Owner授权、typed双向稳定分页；复用既有TaskReader/同表索引，零迁移优先，不接HTTP/Tool/UI/Context/comment/未来流转。
- 已读正式架构、planning/B0-P卡、实际表/索引/Reader/旧codec。已补齐精确错误/锁/Rows与Tx实际尾、六新文件闭集、三作者PG top及同时间真实writer刺激；待root安排独立SPEC审查后授产品写权。
- 与Work UI冻结输入完全分离；Work recovery06已全尾释放且wholeFAIL保持，Timeline当前仅SPEC。全局current/tasks保持冻结，不复制其它线历史；Object join等停项不变。
