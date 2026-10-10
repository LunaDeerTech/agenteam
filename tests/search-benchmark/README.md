# 中英混合文档离线评测

这是固定原创数据与评分工具，尚无候选后端实测。它不依赖 Knowledge 服务，不连接数据库、不建立索引、不调用 embedding 或安装依赖。正式范围见 [D13 benchmark 工作项](../../docs/development/work-items/d13-lexical-benchmark.md)。

数据为 `lexical-v2`：32 篇虚构文档、64 个固定 section、64 个有答案 query 和 8 个无答案 query，以及 4608 个显式相关性等级。作者依据文档语义先写判断，未运行检索器或从候选结果生成 qrels。独立首轮发现通用问题漏标和同族跨split，已按原query字面补全支持及调整family；Knowledge的全量语义审与返修复核、Model的评分实现独验均已有限接受，无剩余must-fix。校验命令自身仍只证明结构，不证明语义质量。原`lexical-v1`保留在Git历史，旧导出/评分不能冒充新修订。

## 运行

要求 POSIX 环境和 Python 3.11+，只用标准库。本次作者实际环境是 Python 3.12.14；没有验证其他 Python 版本的运行。本工具只接受显式文件路径和普通输入文件，不读取数据库/云环境变量。先在仓库根运行：

```sh
python3 scripts/search-benchmark.py validate --data tests/search-benchmark/data
mkdir -p output/ai/search-benchmark
python3 scripts/search-benchmark.py export --data tests/search-benchmark/data --output output/ai/search-benchmark/export-01
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests/search-benchmark -p 'test_*.py' -v
```

`export-01` 必须不存在；同名目录存在时拒绝并保留原内容。成功导出只有 `corpus.jsonl`、`queries.jsonl` 和最后写入的 `complete.json`。候选输入逐字使用 `title + "\n" + section + "\n\n" + raw_text`；不提供 grade、reason、answerable、family、category 或 split。`complete.json` 缺失的目录不能当作成功导出或评分。

日后取得实际候选的本地运行文件后，可执行以下命令。本卡当前没有可作为 backend 证据的 `run.json`，不要用测试控制文件充当它。

```sh
python3 scripts/search-benchmark.py score --data tests/search-benchmark/data --run output/ai/search-benchmark/candidate-01/run.json --output output/ai/search-benchmark/score-01
python3 scripts/search-benchmark.py score --data tests/search-benchmark/data --run output/ai/search-benchmark/candidate-01/run.json --output output/ai/search-benchmark/score-replay-01
```

相同输入的两次评分会产生逐字相同的 `report.json`、`measurements.json`、`complete.json`。重复评分不调用候选，不证明重复检索仍得到相同排名。原始运行文件、使用的 Git 修订、`python3 --version`、候选/adapter 版本和配置由评测负责人随实际执行记录保存；不把时间戳混入确定性分数文件。

无效输入退出1，成功退出0，CLI 用法错误退出2。失败没有 `status:ok`，不覆盖原输出，写入过程中失败会清理本次新建文件。工具不删除既有结果目录。单输入最多2 MiB，每行最多64 KiB；JSONL每行一个对象，普通运行JSON可以缩进多行，所有文件为UTF-8/LF并以换行结束。

## 数据与判断

`dataset.json` 保存固定数量、修订、来源与判断说明。`corpus.jsonl` 的稳定 document/source ID 和 section 是此评测的定位信息，不是生产 Knowledge ID，也不证明生产 parser/chunker 已通过。

`queries.jsonl` 中 `family_id` 表示同一检索意图，包括语言和格式变体。一个family整体属于dev或test，不能看test后再调候选参数而继续声称固定测试。上传验证、未知提交意图、原子替换、分页与历史回执的相近问题已作保守合组。v2另把q35/39/47的启动时一次加载归到同一dev族，q43/55/63的配置不承载实时业务事实归到同一test族；q47与q55交换split，path仍各4条。所有query正文保持v1字面，未收窄问题来排除正例。family标签并不能自动证明没有隐藏同族，独立审查仍要逐项找跨组语义重叠。

有答案的8个主类别各8条，每类dev/test各4条；无答案另4/4。qrels对每条query的全部64个source都显式给0、1、2或3，没有遗漏即视为0的约定。0是不相关，1是有用背景，2是直接必要支持，3是充分回答。非零项有理由，词相似但不相关的材料另留反例理由；相邻运行/配置/错误的支持等级及同族划分已按独立语义审的原问题返修并复核。

任何语义更改都需要新的数据修订，旧结果不能混用。公开的本地数据不是保密盲测；候选不接收答案文件的流程隔离也不等于秘密测试集。合成小集合不能代表真实用户分布或大规模检索性能。

## 运行文件

`run.json` 顶层只允许以下字段；完整合同见工作项第5节。

| 字段 | 要求 |
| --- | --- |
| `format_version` | 整数1，不接受布尔值 |
| `dataset_revision` | 与固定输入一致 |
| `backend` | `native_pg_fts`、`pgroonga`、`pg_search`之一 |
| `backend_version` | 明确非空版本文本，最多512 UTF-8 bytes |
| `config` | `analyzer`、`query_mode`、`ranking`、`candidate_k`、`tie_break`、`adapter_revision`，无额外字段 |
| `results` | 全部72个query各一次，空结果必须显式给出 |
| `measurements` | null或第6节规定的测量对象；未测不能填零 |

config的candidate_k固定20、tie_break固定`source_id_ascii`，其余字段为非空、至多512 UTF-8 bytes的说明文本；analyzer说明实际tokenizer及其配置。results的每项只有query_id和hits；hit只有source_id、从1连续递增的rank以及可空的有限native_score。每query最多20条且source不能重复；相邻同分以source ID的ASCII顺序打破平局。native_score只是原backend诊断，评分只消费排名，不跨后端比较原始分值。

工具拒绝重复JSON成员、额外字段、未知ID、缺query、重复source、非连续rank、布尔冒数值、非法UTF-8、非有限数、尾随第二个JSON值、错误数据修订、不完整qrels和声明family跨split。语义是否正确仍由独立阅读文档判断。

## 指标解释

Recall@1/5/10/20与MRR@20把grade≥2视为相关；nDCG@1/5/10/20的增益是`2^grade-1`、折扣是`log2(rank+1)`，理想排序使用全部64个source。grade1会贡献nDCG，但不会被算作找到直接答案。未返回的相关source仍在Recall分母，空结果计0；报告保留整体64条、split各32条、类别各8条和类别/split各4条的分母。

8条无答案单列返回量和有结果比例，不参与有答案指标，也不证明产品已具备拒答能力。JSON保留完整浮点计算结果；对外表格可最多显示6位小数，不能先截断逐query分数再平均。

报告标记`imported_run`：格式合法不能证明它来自真实后端。可选测量也标记`reported_measurements`，不能用评分进程耗时当查询延迟。无测量则明确`not_measured`及null；合法已知零保留零。latency只按同一cold/warm模式汇总实际样本，用nearest-rank计算p50/p95，同时保留逐query样本数；不把冷、热延迟混成一个百分位。缺失query的测量不补零。

测试用手写排名向量，只证明评分：例如grade为3、2、1、0而排名为1级、2级、3级、0级时，MRR是1/2，Recall@1是0，nDCG@1是1/7。完整CLI控制只给q01一个已明确手算的排名、其余有答案query显式空，核宏平均分母和无答案隔离；另测同分、K截断、缺少正例、错误输入、实际export无标签、输出失败与逐字重放。测试版本标记`evaluator-control-not-a-backend-run`，控制产物不登记为候选成绩。

未参与实现的Model另用独立手算期望执行128项公开CLI/严格输入/导出/确定性/百分位检查，并对v2版本绑定作限定复核；未用作者函数重算expected。[独立控制源码](../../.agent-state/search-benchmark-review/evaluator-controls.py)可用 `PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/search-benchmark-review/evaluator-controls.py` 复验，输出只进入ignored的 `output/ai/search-benchmark-review/evaluator`。作者17组方法与这些独立控制均不是候选检索运行。

## 后端与来源边界

| 候选 | 本卡真实运行 | 版本、许可和运维兼容性 |
| --- | --- | --- |
| native PostgreSQL FTS | not_run | not_verified |
| PGroonga | not_run | not_verified |
| pg_search | not_run | not_verified |

不把PostgreSQL的ts_rank/ts_rank_cd称为BM25。当前没有测得任何候选的索引体积、索引耗时或查询延迟。扩展、分词器、字典的准确许可证/NOTICE、PostgreSQL17兼容性、重建/更新/删除/升级成本都需后续真实材料与验证。

数据为内部原创测试材料，对外再分发许可未在本卡授予。没有抓取第三方文档、词典或用户数据，不猜测仓库未提供的公开许可。此工具没有新增正式dependency/lock/SQL，未运行任何正式backend。最终选型仍需三候选实际比较和固定dense leg的hybrid RRF对照；本卡不完成D13或生产索引验收。
