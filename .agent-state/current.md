# 当前执行检查点

- 目标：交付 D13 中英混合文档 lexical benchmark 的固定内部原创数据与离线 evaluator，作为后续三候选真实比较的独立前置结果。
- 状态：进行中，已保存5cf539dd的SPEC rev1获Skills独立有限接受；首数据0b6a1404、完整工具5dc38928已保存。Model已有限接受评分器独验；Knowledge首轮全量语义发现must-fix，现以lexical-v2返修并冻结待复核，未运行候选 backend。正式来源：[工作项](../docs/development/work-items/d13-lexical-benchmark.md)。
- 工作树 `/workspace/agenteam-search-benchmark`，分支 `ai/search-benchmark`，正式基线 main `3cea6076`；root 新建本树，仅历史证据目录稀疏，旧source未改。当前作者写域为本检查点、新卡及卡中七个技术文件，Git 写操作归 root。
- 已读 AGENTS、团队流程、设计/文档技能、正式计划 D13、Knowledge Indexing 与 Retrieval 相关段落；基线没有既有 benchmark 数据/工具。正式候选 native PostgreSQL FTS、PGroonga、pg_search；最终选型还需固定 dense leg 的 hybrid RRF 对照，FTS 不冒 BM25。正式 PG 约束17 patch≥8/vector0.8.1。
- rev1 固定32文档/64section，64有答案query（8类各8）+8无答案为上限，family整体dev/test隔离；qrels逐query穷举全部64source，独立人工语义判断，不由候选命中生成。Python3.11+仅stdlib，严格输入与可重跑指标，三候选真实结果/许可/版本保持未核，不擅自引依赖或公开再授权。
- 已获root七路径实施授权；corpus/queries/qrels/dataset四数据文件已写，32篇64section/72query/4608显式grade。未运行任何检索器；作者按语义补相关背景并合并上传验证、原意图确认、原子替换的同族split，避免只换名称穿越dev/test。独立全量语义审查尚待进行，不把结构计数当相关性接受。
- 无产品/SQL/lock变更，无PG/browser/socket/网络或活命令。Variables UI原scope独立冻结，CRUD03仍归其fresh资源窗口，不与本树混用source/cache/输出。
- 作者实际检查：Python3.12.14；validate 883de6 actual0，初轮17方法e4bb53 actual0；普通文件打开增加nonblocking后定向f98f6f actual0，最终17方法b40e5e actual0。命令 `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests/search-benchmark -p 'test_*.py' -v`，日志 `output/ai/search-benchmark/evaluator-controls-{01,02,03}.log`；测试含手算graded/Recall/MRR、K截断、宏平均分母、空结果/无答案、实际CLI导出/评分/重复评分、strict坏输入与失败输出，控制版本明确非backend。actual export f05be7=0，产物 `output/ai/search-benchmark/export-author-01`；没有候选真实score、性能或许可结论。
- 作者文档与源检查：原SPEC的5链接/fragment/UTF8/LF/结构a7a660通过；新增三路径AST/格式/README本地链接de403c通过，限定diffcheck通过。当前Python3.11只属最低版本要求，未实际运行；独立语义判断与实现/CLI接受均待完成。
- 独立评分验证：Model对5dc38928实际执行128个独立手算/公开CLI/严格输入/导出/确定性/percentile检查，74159b actual0，限定代码无must-fix；不含数据语义或真实backend。其必要独验源由Model独占写入 `.agent-state/search-benchmark-review/evaluator-controls.py`，我未修改，随本轮统一保存。原独审夹具误把q02当zh的FAIL已按实际en修正，不归因为evaluator缺陷。
- Knowledge对0b6a1404全部64section/72query/4608grade的独立语义扫描发现：通用问题被作者intent暗限scope而漏跨文档支持、q09充分答案被压低、少量同域直接条件/背景漏标、startup-once同义路径跨split。现lexical-v2保留全部query正文和source正文/locator，19处grade/理由和q09 intent修正；q35/39/47同族dev、q43/55/63同族test，q47/q55交换split，f15并f05、f16并f06。仍64+8/64/4608及每类4/4，原v1保留Git与既有export-author-01，不覆盖旧结果。
- 修后作者29dd40 actual0确认全部72query和64source内容未改、恰19grade及两split变动；validate/export92a841 actual0产物 `output/ai/search-benchmark/export-author-02`；17方法8e3dd1 actual0，日志evaluator-controls-04.log。评分器未动，测试只有control_run的revision从固定dataset读取一行；q01 zh/dev与q02 en/dev及全部golden grade/分母未变。Knowledge和Model分别复核语义变化与该一行绑定，当前尚待结论。
- 下一步：保存本轮四data、测试绑定、README与卡/current八路径，连同Model独占的必要控制源码；独立复核通过后再归位本卡接受范围。所有结果落独占 `output/ai/search-benchmark/`；未完成源码/数据已在跟踪路径，原始日志可重建，无活命令或真实资源。
