# 当前执行检查点

- 目标：交付 D13 中英混合文档 lexical benchmark 的固定内部原创数据与离线 evaluator，作为后续三候选真实比较的独立前置结果。
- 状态：进行中，SPEC rev1 候选已写，尚未窄审；未写 corpus/query/qrels/工具，未运行候选 backend。正式来源：[工作项](../docs/development/work-items/d13-lexical-benchmark.md)。
- 工作树 `/workspace/agenteam-search-benchmark`，分支 `ai/search-benchmark`，正式基线 main `3cea6076`；root 新建本树，仅历史证据目录稀疏，source 未改。当前作者唯一写域为本检查点和新卡，Git 写操作归 root。
- 已读 AGENTS、团队流程、设计/文档技能、正式计划 D13、Knowledge Indexing 与 Retrieval 相关段落；基线没有既有 benchmark 数据/工具。正式候选 native PostgreSQL FTS、PGroonga、pg_search；最终选型还需固定 dense leg 的 hybrid RRF 对照，FTS 不冒 BM25。正式 PG 约束17 patch≥8/vector0.8.1。
- rev1 固定32文档/64section，64有答案query（8类各8）+8无答案为上限，family整体dev/test隔离；qrels逐query穷举全部64source，独立人工语义判断，不由候选命中生成。Python3.11+仅stdlib，严格输入与可重跑指标，三候选真实结果/许可/版本保持未核，不擅自引依赖或公开再授权。
- 当前无数据/产品/SQL/lock变更，无PG/browser/socket/网络或活命令。Variables UI原scope独立冻结，CRUD03仍归其fresh资源窗口，不与本树混用source/cache/输出。
- 作者文档检查：UTF-8/LF/末尾换行、尾空格、代码围栏与5个本地链接及fragment实际a7a660通过，git diff --check 089e61通过；没有业务测试或实际性能实验。两文档已冻结待root保存和SPEC窄审。
- 下一步：SPEC固定后才按卡中七个新数据/工具/测试/README路径实施。所有结果落独占 `output/ai/search-benchmark/`；未完成源码/数据须随检查点保存，不仅留临时文件。
