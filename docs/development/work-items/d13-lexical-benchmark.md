# D13 中英混合文档 Lexical Benchmark

修订：rev1。状态：SPEC 候选，尚未窄审；本次只写规格与检查点，未创建语料、评分工具或候选后端结果。

## 1. 独立结果与正式依据

本卡交付固定的内部原创评测数据、严格运行结果格式、可离线复跑的评分工具及其独立验证。它为后续 lexical backend 比较提供统一输入，不依赖尚未验收的 Knowledge 服务，也不实现生产检索系统。

正式依据是[开发计划 D13](../development-plan.md#d13-索引与混合检索)、[Retrieval §7、§25–29](../../architecture/knowledge-memory/retrieval-runtime.md)、[Indexing §3.6、§13、§17](../../architecture/knowledge-memory/knowledge-indexing.md)。第一阶段至少比较 `native_pg_fts`、`pgroonga`、`pg_search`。PostgreSQL FTS 的 `ts_rank` / `ts_rank_cd` 不称为 BM25；各 backend 的原始分数不直接相加或跨 backend 比大小。最终选型还需要固定 dense leg 的 hybrid RRF 对照，不能凭本卡 lexical 数据准备或单一指标完成选型。

本卡不新增正式依赖、lock 文件、SQL、数据库扩展、Knowledge/Model 绑定或生产索引；不执行 embedding、reranker、模型调用、网络下载和真实数据库。后续真实候选运行需单独固定版本、许可证、执行接缝及资源授权。D13 的 parser、chunker、generation、scope 过滤、索引生命周期和全模块验收仍由后续工作承担。

## 2. 现状与文件归属

基线为正式 main `3cea6076`，独占工作树 `/workspace/agenteam-search-benchmark`，分支 `ai/search-benchmark`。已核对本基线没有既有 corpus、query/qrels 或 lexical benchmark 实现；`go.mod` 没有上述搜索扩展依赖。已存在 Knowledge 纯契约不证明服务或检索已运行。本卡不消费其他活动分支的未验实现。

当前仅授权本卡及 `.agent-state/current.md`。SPEC 固定并窄审后，实施范围限定为以下新文件，根目录和既有业务源不变：

| 文件 | 作用 |
| --- | --- |
| `tests/search-benchmark/data/corpus.jsonl` | 32 篇原创文档的 64 个固定 section |
| `tests/search-benchmark/data/queries.jsonl` | 64 个有答案 query 与 8 个无答案 query |
| `tests/search-benchmark/data/qrels.jsonl` | 每个 query 对全部 section 的穷举相关性与判断依据 |
| `tests/search-benchmark/data/dataset.json` | 数据修订、固定数量、类别、family/split 规则与来源声明 |
| `scripts/search-benchmark.py` | 仅 Python 标准库的 validate、export、score 命令 |
| `tests/search-benchmark/test_evaluator.py` | 手算评分向量、输入负控、实际 CLI 与确定性检查 |
| `tests/search-benchmark/README.md` | 命令、运行格式、解释范围、候选与许可状态 |

可重建导出、测试输出和运行结果放 `output/ai/search-benchmark/`。必要语料、qrels、工具与反例必须在上述跟踪路径，不能只有临时文件或文字摘要。Git 写操作仍由 root 完成。

## 3. 固定语料和 Query

数据修订为 `lexical-v1`。采用从零撰写的虚构项目文档，不采集用户项目、账户、日志、真实凭据、第三方文档或未授权数据。文本可使用普通架构概念与虚构标识符，但不得整段复制现有项目文档。来源声明记录创作方式和人工复核情况，不声称代表真实用户查询分布。

固定 32 篇文档，每篇恰好 2 个 section，共 64 个 source。每个 corpus 行包含 `dataset_revision`、`document_id`、`source_id`、`title`、`section`、`raw_text`；ID 使用本数据集稳定 ASCII 标识，不冒充生产 UUID。`source_id` 唯一，section 是定位信息，文档内两个 section 不重名。标题和 section 各最多 160 UTF-8 bytes，raw_text 为 128–4096 UTF-8 bytes。文本严格 UTF-8、LF、无 NUL；冻结后不自动 Unicode 归一化、翻译或改写。

所有候选收到逐字相同的 `index_text = title + "\n" + section + "\n\n" + raw_text`。raw_text 与前缀保持分离。section 已预先固定，不在本卡实现或声称验收生产 parser/chunker；不通过调节 chunk 边界迎合候选命中。

64 个有答案 query 按以下主类别各 8 个，另设 8 个无答案 query；72 是本卡明确上限，不在实现中扩大：

| 主类别 | 必须覆盖的内容 |
| --- | --- |
| `zh` | 中文自然语言与同词不同含义 |
| `en` | 英文自然语言与常见词形差异 |
| `mixed` | 中英文同时出现的真实问题句式 |
| `identifier` | 精确名称及仅少数字符不同的干扰名称 |
| `case_style` | snake_case、camelCase 及相似拆词 |
| `path` | 文件路径、共同路径段、不同末段与标点 |
| `error_code` | 错误码和只差一个片段的干扰码 |
| `architecture` | 架构术语、职责和相近但不同的边界 |

每个 query 行包含 `dataset_revision`、`query_id`、`family_id`、`split`、`category`、`answerable`、`text`。query 为非空、非全空白 UTF-8 文本，最多 256 bytes；不带 backend DSL。类别是固定主类别，无答案 query 的 category 固定为 `no_answer`，不能运行后重新划组提高成绩。至少 16 个有答案 query 存在两个或更多相关 section；至少 8 个有答案 query 同时具有共享词语但语义不相关的干扰 section。

family 以同一事实/检索意图为边界，改写、语言变体、格式变体和只换关键词的同义 query 属于同一 family；不得仅换 ID 后分到另一 split。整个 family 只能归 `dev` 或 `test`。有答案 query 在每主类别按 dev/test 各 4 个分配，无答案按 4/4 分配。独立语义复核需主动找跨 split 的隐藏同族；发现后先调整分配并固定数据，不以随机逐 query 切分代替。

dev 可以用于候选配置探索；最终 test 配置必须先冻结。看过 test 后再调参需要新实验修订并明确探索性质，不能继续称原固定 test 结论。此小型合成集不承诺对大规模或真实文档的外推性能。

## 4. 穷举 Qrels 与独立语义判断

每个 qrels 行包含 `dataset_revision`、`query_id`、`intent`、`grades`、`reasons`。`grades` 必须为全部 64 个 source_id 提供整数等级，共 72×64=4608 个判断；缺项不默认为 0。`reasons` 对全部非零 source 提供简短理由，对同词干扰项记录明确反例理由；intent 说明问题所求事实及相似材料为什么不满足。

| Grade | 语义 |
| --- | --- |
| 0 | 不相关，或只有字符串相似而不能帮助回答 |
| 1 | 有用背景，但不能直接回答该 query |
| 2 | 直接支持答案的必要部分 |
| 3 | 直接、充分回答该 query |

有答案 query 至少存在一个 grade≥2；无答案 query 必须全 0。Recall/MRR 的二值相关阈值固定为 grade≥2；nDCG 使用所有 grade 的 `2^grade - 1` 增益。不能为了使某次结果好看而临时改变阈值。

作者先依据语义逐 query 检查全部 source。随后由未参与候选调参的独立审查者进行人工语义判断，核对 query 意图、全部 grade、正例理由、hard negative、同族隔离及零答案；不是用字符串匹配、候选输出、模型评分或程序自动把命中当正例。程序只能验证结构与数量。审查者如有争议，先记录具体 query/source 和理由，修订数据并复核，再冻结运行；未解决项不能静默忽略。

候选 runner 只取得 corpus/query 导出，不能取得 qrels、理由或 test 答案。qrels 从不由候选命中生成；任何语义修订产生新 dataset revision，旧结果保留且不混合比较。公开的本地测试集只能实行流程隔离，不宣称具有保密盲测能力。

## 5. 离线工具与运行结果合同

工具要求 Python 3.11+，仅标准库；不 import 业务包、不安装模块、不启动子进程或监听 socket，不读取隐式数据库/云凭据。所有输入和输出路径由 CLI 显式提供。export/score 的输出目录必须新建成功且原先不存在；失败不覆盖既有结果。

JSON/JSONL 使用闭合字段，拒绝重复 JSON member、未知字段、非法 UTF-8、非有限数值、布尔冒整数、尾随第二个 JSON 值、重复 ID、缺 query、未知 source、错误 dataset revision、无效 split/family 与不完整 qrels。单输入文件最多 2 MiB，单行最多 64 KiB；先有界读取再解码。纯评分失败退出非零，不写貌似成功的部分报告。

命令合同如下；本修订只规定命令，尚未实现或运行：

```sh
python3 scripts/search-benchmark.py validate --data tests/search-benchmark/data
python3 scripts/search-benchmark.py export --data tests/search-benchmark/data --output output/ai/search-benchmark/export-01
python3 scripts/search-benchmark.py score --data tests/search-benchmark/data --run output/ai/search-benchmark/candidate-01/run.json --output output/ai/search-benchmark/score-01
python3 -m unittest discover -s tests/search-benchmark -p 'test_*.py'
```

export 的 corpus 行仅含 `dataset_revision`、`source_id`、`index_text`，query 行仅含 `dataset_revision`、`query_id`、`text`；不导出 qrels/reasons、answerable、family、category 或 split。dev/test query ID 清单由固定数据提供给评测负责人，不能改变候选接收的文本或向其暴露答案。score 只消费已经存在的本地运行文件，不运行 backend。实现不提供一个自写“等同 PostgreSQL/PGroonga/pg_search”的替代搜索引擎。

运行 JSON 顶层固定为 `format_version`、`dataset_revision`、`backend`、`backend_version`、`config`、`results`、`measurements`，format_version 固定整数 1。backend 只允许三正式候选名；backend_version 是明确的非空版本文本。config 的闭合字段为 `analyzer`、`query_mode`、`ranking`、`candidate_k`、`tie_break`、`adapter_revision`；candidate_k 固定整数20，tie_break 固定 `source_id_ascii`，其余为非空、最多512 UTF-8 bytes 的固定说明文本，analyzer 必须说明 tokenizer/config。数据和运行元信息不从环境推断。测试黄金向量只用于 evaluator 控制，不形成 backend 成绩报告。

results 恰覆盖全部 72 个 query，每项含 `query_id`、`hits`。每个 hit 包含 `source_id`、连续正整数 `rank`、可空的有限 `native_score`，每 query 最多 20 个且 source 不重复。空命中必须显式给空数组。rank 是评分顺序，native_score 仅供该 backend 诊断；同分时 adapter 以 source_id ASCII 顺序稳定打破平局。输入顺序、评分顺序和 qrels 无隐式对齐关系，必须按 ID 关联。

导入工具能验证运行文件的结构和内部一致性，不能证明该文件来自真实 backend。报告始终标注 `imported_run`；真正执行、进程终态、资源退出与配置证据由后续候选 runner/独立验证承担。本卡的手算向量不允许填充候选运行事实。

## 6. 指标、报告与可复现性

有答案的 64 个 query 全部参与宏平均，按整体、dev/test、8 主类别分别给分母和结果；缺结果是输入错误，显式空结果为 0，不从平均值中删除。K 固定为 1、5、10、20，仅是评测设置，不是产品默认值。

- Recall@K：前 K 中 grade≥2 的唯一 source 数 / 该 query 全部 grade≥2 source 数。
- MRR@20：前 20 中第一个 grade≥2 source 的倒数名次，没有则 0；报告保留截断标记。
- nDCG@K：`DCG = sum((2^grade - 1) / log2(rank + 1))`，除以全 corpus 理想排序的 IDCG@K。IDCG 对有答案 query 必须大于 0。
- 8 个无答案 query 单独报告每 query 返回量和有返回 query 比例，不混入 Recall/MRR/nDCG，也不将空返回当成生产 abstention 能力已通过。

报告包含逐 query 指标，便于比较候选差异；计算使用完整精度，展示最多 6 位小数。结果排序固定，重复评分应逐字一致。时间戳、机器环境和真实性能记录分开，不进入确定性评分正文。按同一 dataset revision、split、K 和候选配置比较，不能合并不同修订结果。

`measurements` 可为 null，或为闭合对象 `index_bytes`、`indexing_ms`、`environment`、`latencies`。前两项可为 null，否则分别为非负整数和非负有限数；environment 是非空、最多1024 UTF-8 bytes 的硬件/条件说明。latencies 为至多144项的数组，每项恰含 `query_id`、`mode`、`samples_ms`；query 必须存在，mode 为 `cold` 或 `warm`，query/mode 组合唯一，samples_ms 为1–100个非负有限数。只按相同 mode 聚合样本，重复次数取实际长度，p50/p95 使用有序样本的 nearest-rank。报告另给每 query 的实际返回候选数。

缺失测量保持 null / `not_measured`，不得补零、用 scorer 耗时或推测值冒 backend 性能；导入样本明确为 reported measurements。真实性能采集协议在后续候选 runner 卡固定；本卡只验证导入类型和报告，不声称获得生产吞吐/规模结论。

运维比较至少记录 PostgreSQL 17 兼容性、扩展与分词器安装、字典依赖、更新/删除/重建方式、备份恢复与升级成本。未核对的项目是 `not_verified`，不是通过。正式数据库约束见[数据库基础库](../backend/database.md#版本与配置)：PostgreSQL 17 patch≥8、vector 0.8.1；候选扩展不能静默改动这些前提。

可重现输入使用本卡/数据/工具的 Git 修订、准确 Python 版本、候选版本和配置、固定 query 顺序。没有候选随机性时不编造 seed；有随机性时后续 adapter 必须记录 seed。每次导出/评分使用新目录，原失败与结果保留。无必要不另造全树 manifest 或文件镜像。

## 7. 来源、许可证与依赖边界

当前只使用内部原创合成 fixture。基线根目录未见项目级 LICENSE，不能因此假定公开许可，也不擅自为仓库材料授予新许可证。数据声明明确“内部原创测试材料；对外再分发许可未在本卡授予”，不复制第三方数据或词典。此边界不妨碍本仓库内部实施与验证。

三候选的具体版本、扩展包、中文 tokenizer/词典、许可证及 PostgreSQL 17 安装兼容性目前没有经本任务核验的材料，全部待定；候选名称不证明已安装、可再分发或可商业使用。后续依赖研究必须核准确版本的一手许可、NOTICE、传递依赖和再分发要求，保存必要来源定位，不凭常识填写许可证结论。任何正式依赖变更仍在选型之后进行。

本卡不修改 go.mod/go.sum、npm lock、正式迁移、镜像、现有共享测试工具或生产日志策略。未来数据中如需真实或第三方材料，必须另立具体来源与授权范围，不能通过扩大本卡 dataset 偷渡。

## 8. 接受条件与未完成范围

SPEC 窄审固定边界后才能写数据与工具。完成本卡要求：固定全部 corpus/query/qrels；作者语义检查和独立人工语义复核完成；实际 validate/export/score 的正常与异常控制通过；评分黄金向量由审查者独立手算并执行公开 CLI；空结果、零答案、多个正例、grade 阈值、截断、同分顺序、重复/缺失/坏数/错误修订/不完整 qrels 等负控有效；同输入重复评分逐字相同，候选导出不含 qrels 或答案标签。

验证者须未参与所验实现，不用作者自己的函数重算期望值。保留真实命令、结果、失败与修复范围。独立审查通过只接受本卡的离线数据与 evaluator，不接受任一 backend 的质量、延迟、运维或许可。

三候选真实运行、真实 PostgreSQL 兼容性、固定 dense leg 的 equal-weight hybrid RRF、reranker 对照、选型 ADR、正式依赖及生产索引均仍未验证。本卡不完成 D13、不解除任何既有停止项；后继必须消费同版 corpus/qrels 或明确的新修订，不用准备阶段替代真实比较。
