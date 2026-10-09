# 当前执行检查点

- 目标与状态：本卡“固定语料＋可重复离线评分工具”开发及独立验收完成，现准备主线程正式整合。只完成这个独立结果，不完成D13，也没有三后端、性能、许可或最终选型结论。正式来源：[工作项](../docs/development/work-items/d13-lexical-benchmark.md)。
- 工作树 `/workspace/agenteam-search-benchmark`，分支 `ai/search-benchmark`，正式基线main `3cea6076`；SPEC已保存5cf539dd，首数据0b6a1404、完整工具5dc38928、lexical-v2及独验控制00ab72ba已保存。Git写操作和正式main整合归root，不回填本次保存编号。
- 最终范围：卡/current、台账仅本结果一行、`scripts/search-benchmark.py`、`tests/search-benchmark/{README.md,test_evaluator.py}`与data下corpus/queries/qrels/dataset四文件，另Model独占的 `.agent-state/search-benchmark-review/evaluator-controls.py`；共11条最终路径。未改既有产品、锁文件、SQL或共享测试工具。
- 数据lexical-v2：内部原创32文档/64section，64有答案query（8类各8、dev/test各4）+8无答案（4/4），共4608显式grade。query/source正文和locator未因返修变化，family按意图整体分组；candidate导出不含答案/grade/family/category/split。语义判断在无检索结果时完成，未从候选命中生成正例。
- SPEC由Skills限定独审接受。Knowledge对原全部64section×72query/grade作独立语义扫描，指出通用query被作者intent暗限scope的漏标、q09充分答案压低、相邻条件/背景漏标和startup-once同族跨split。lexical-v2修正19处grade/理由及q09 intent，q35/39/47统一dev、q43/55/63统一test，q47/q55交换split，f15并f05、f16并f06，保留原v1和原问题。
- Knowledge最终977906 actual0逐项复核返修及原语义不变部分，有限接受无剩余must-fix；不是validator或检索结果推导语义。dataset.json最后仅将judgment_method的来源状态归位为已获该复核，不改变revision/计数/正文/grade/family。
- 实现仅POSIX Python标准库。实际环境Python3.12.14，Python3.11是最低要求但本次未实跑；无安装、模型调用、PG/browser/socket/网络或后台命令。严格有界JSON/JSONL、全ID/grade/family校验、显式空结果、Recall/MRR/graded nDCG及reported测量、新输出不覆盖和确定性评分已实现。
- 作者实际检查：最终原版17方法b40e5e actual0，v2的17方法8e3dd1 actual0；命令 `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests/search-benchmark -p 'test_*.py' -v`，日志 `output/ai/search-benchmark/evaluator-controls-{01,02,03,04}.log`。validate/export92a841 actual0，修后产物export-author-02；29dd40 actual0逐项证实全部query/source内容保持及恰19grade、两split变化。测试控制明确不是backend执行。
- Model评分器独验74159b actual0：128个独立手算/公开CLI/strict输入/标签隔离/确定性/冷热百分位检查，不用作者函数算expected；初轮误把q02当zh的独审夹具FAIL已按实际en纠正，不归因评分器。563bcf actual0最终窄核：scorer相对5dc全文不变，test只有control_run从metadata读取revision一行，q01 zh/dev与q02 en/dev及golden grade/分母不变，原128控复用。
- 必要独验源由Model写入上述单路径，本作者未修改；可用 `PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/search-benchmark-review/evaluator-controls.py` 复验，输出只到ignored `output/ai/search-benchmark-review/evaluator`。评分控制报告始终为imported_run，不能冒真实候选成绩。
- 正式候选仍为native PostgreSQL FTS、PGroonga、pg_search；全部真实run、index/query性能、具体扩展/字典版本与许可、运维兼容性、固定dense leg的hybrid RRF及最终选型尚未验证。FTS不称BM25；无生产Knowledge/index/embedding依赖，不解除既有停止项。原创材料仅内部使用，不擅自授予公开再分发许可。
- 最终收口仅更新必要接受文档、dataset判断来源状态与台账本结果单行；台账删除该新行后逐字等于正式main基线，其它活动树状态未复制。最终validate 8c9624 actual0，9本地链接/fragment/格式及台账单行逆投影24c6a2 actual0，diffcheck通过。已有不变作者/独验证据复用，全部最终路径冻结交root保存并正式整合；无自有活命令或真实资源。
