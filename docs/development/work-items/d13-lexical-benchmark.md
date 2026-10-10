# D13 中英混合文档 Lexical Benchmark

修订：SPEC rev1，数据lexical-v2。状态：本卡“固定语料＋可重复离线评分工具”已完成作者检查及独立语义、评分实现验收，无剩余must-fix。三真实后端、性能、许可证核验与最终选型仍未完成，不完成整个D13。下文技术边界保持已审rev1。

## 1. 独立结果与正式依据

本卡交付固定的内部原创评测数据、严格运行结果格式、可离线复跑的评分工具及其独立验证。它为后续 lexical backend 比较提供统一输入，不依赖尚未验收的 Knowledge 服务，也不实现生产检索系统。

正式依据是[开发计划 D13](../development-plan.md#d13-索引与混合检索)、[Retrieval §7、§25–29](../../architecture/knowledge-memory/retrieval-runtime.md)、[Indexing §3.6、§13、§17](../../architecture/knowledge-memory/knowledge-indexing.md)。第一阶段至少比较 `native_pg_fts`、`pgroonga`、`pg_search`。PostgreSQL FTS 的 `ts_rank` / `ts_rank_cd` 不称为 BM25；各 backend 的原始分数不直接相加或跨 backend 比大小。最终选型还需要固定 dense leg 的 hybrid RRF 对照，不能凭本卡 lexical 数据准备或单一指标完成选型。

本卡不新增正式依赖、lock 文件、SQL、数据库扩展、Knowledge/Model 绑定或生产索引；不执行 embedding、reranker、模型调用、网络下载和真实数据库。后续真实候选运行需单独固定版本、许可证、执行接缝及资源授权。D13 的 parser、chunker、generation、scope 过滤、索引生命周期和全模块验收仍由后续工作承担。

## 2. 现状与文件归属

基线为正式 main `3cea6076`，独占工作树 `/workspace/agenteam-search-benchmark`，分支 `ai/search-benchmark`。已核对本基线没有既有 corpus、query/qrels 或 lexical benchmark 实现；`go.mod` 没有上述搜索扩展依赖。已存在 Knowledge 纯契约不证明服务或检索已运行。本卡不消费其他活动分支的未验实现。

SPEC 固定并窄审后，实施范围限定为以下七个新文件，根目录和既有业务源不变：

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

另按必要交付范围保留本卡、分支检查点、台账中本结果的单行，以及未参与实现的Model所持有的[独立评分控制](../../../.agent-state/search-benchmark-review/evaluator-controls.py)；不复制其他活动分支状态。

## 3. 固定语料和 Query

当前数据修订为 `lexical-v2`，原 `lexical-v1` 保留Git历史。采用从零撰写的虚构项目文档，不采集用户项目、账户、日志、真实凭据、第三方文档或未授权数据。文本可使用普通架构概念与虚构标识符，但不得整段复制现有项目文档。来源声明记录创作方式和人工复核情况，不声称代表真实用户查询分布。

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

命令如下；实际验证范围见第9节：

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

## 9. 当前实施证据

七个新文件已落盘。四数据文件表达32文档/64section、64有答案+8无答案及全部4608等级；作者在无任何检索结果的情况下依据语义判断，补充相邻材料的背景/部分支持并将跨名称的相近意图置于同一split。结构计数不作为全量语义或隐藏同族的接受依据。

标准库工具在Python3.12.14实际运行：validate与export通过；17个测试方法包含手算graded指标、相关分母/截断、完整CLI宏平均与无答案隔离、严格JSON/ID/grade/family反例、新输出失败清理及同输入逐字评分重放，最终actual0。普通文件读取使用POSIX nonblocking打开并校验regular file，拒绝在输入FIFO上阻塞；没有启动检索器、数据库或额外网络。详见[运行说明](../../../tests/search-benchmark/README.md)及本分支检查点中的实际命令和日志。

测试构造的运行文件是评分控制，版本明确标记为非backend执行，不构成候选成绩。独立审查分别重新手算并执行CLI、核全部query/source判断及family边界；三真实候选/性能/许可证/hybrid对照仍未验证。

独立首轮语义审指出：通用query不能被作者intent暗限于Cedar/Flint，存在跨文档支持漏标；相邻Harbor/Moss/Page直接条件及少量背景分级不一致；启动时一次加载的同义问题跨split。lexical-v2保持query正文，修正19处grade/理由和q09 intent，q09的充分答案按字面升为3；合并相关同族并按整体意图调整q47/q55的split，各类4/4及所有数量不变。原v1及首次问题保留。Knowledge在原全部64×72语义扫描基础上逐项复核这些变化、背景/必要/充分等级及隐藏同族修正，977906 actual0，有限接受且无剩余must-fix；没有用检索结果或validator推导语义。

评分代码获Model限定独审接受：独立手算与真实CLI共128项检查74159b actual0，未使用作者函数计算期望，范围不含数据语义或backend执行。v2只使测试控制的dataset revision从固定数据读取，不修改评分器或黄金期望；最后版本窄复核563bcf actual0确认q01/q02、数量/分母及手算前提保持，复用原128控。独审首次误把q02当zh的夹具FAIL已按实际en纠正，不是evaluator缺陷。

作者修后17个测试方法、实际validate/export及逐项内容/数量比较通过；必要独验控制源码已保留，可用 `PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/search-benchmark-review/evaluator-controls.py` 在独占ignored输出目录复验。最终只归位来源判断状态与必要文档，语料正文、query、grade、family及评分器均保持已验输入。此完整独立结果可以供后续候选运行消费，不能据此填入任何backend实测分数或选型结论。

## 10. 独立后继：有界纯文本 Parser

本批在 `internal/central/retrieval/parser/` 实现可直接调用的纯算法，独立于前述 lexical evaluator；不消费或修改其 corpus、qrels 和成绩。`ParsePlainText(ctx, SourceIdentity, text)` 返回 `StructuredDocument`，`ParseBoundedContent(ctx, knowledge.ReadRequest, knowledge.DocumentContent)` 只适配已经完整返回的 D12 值。类型归 D13 包所有，不注册通用 Parser、不增加依赖、迁移、I/O、生产初始化或索引接线。

`SourceIdentity` 保留实际内容对应的 typed ProjectID、DocumentID、ContentVersion 和 ObjectID。四项采用现有领域校验，ObjectID 按 D12 `DocumentRef.Validate` 为必需；version 保留原整数，不经浮点或提前读取的元数据替换。来源标识不是授权凭据，未来发布/locator 消费仍需重新验证当前版本。输出包含 profile、原文总字节数、有序 paragraph 元素、逐元素原文、段落区间及句区间；Ordinal 从 1 开始，所有区间均为原始 UTF-8 字节坐标的零起始半开区间，句区间连续覆盖所属段落。空文档或全部 ASCII 空白行成功返回零元素；空白分隔行不构成元素。

### 10.1 冻结的 `plain_text:v1`

- 输入最多 1 MiB、最多 16384 段、全文合计最多 65536 句，等于上限允许，超过拒绝并返回零结果。保留 BOM、NUL、缩进、原换行及原 Unicode 字节，不裁剪、不归一化。
- CRLF 是一个换行，孤立 CR 或 LF 也结束一行。仅含 ASCII space/TAB 的行为空白行；连续非空白行合成一段，保留段内换行，排除末行终止符及段间空白行。
- 连续终止符集合固定为 `.?!。！？`。最大连续串包含中文 `。！？` 时总断句；纯英文终止符串仅在随后所有闭合符之后为 ASCII 空白或段尾时断句，不另推断缩写、小数或语义。
- 闭合符固定为 ASCII 双引号、单引号、右圆括号、右方括号、右花括号，以及 `” ’ 」 』 ） 】 》 〉`，紧随终止符的连续闭合符并入前句。ASCII 空白仅 space、TAB、CR、LF；句间空白归后句，段末仅剩 ASCII 空白时归最后一句，不产生空句。
- 改变上述语义须换 profile，不原地改变 `plain_text:v1`。

### 10.2 D12 值边界、失败和取消

适配器读取原 `DocumentContent.Document` 的实际来源，只接受 Active、Text、`text/plain` 且唯一 Text arm；完整校验 DocumentRef、ReadRequest、union 及页大小/offset/next。只接受 ByteOffset=0、Truncated=false、NextByteOffset=文本字节数、长度不超过该请求 MaxBytes 的完整值。不能把非空 Text 指针当成 union 合法，也不能拼接可能来自不同版本的分页。调用者仍负责原 D12 授权、事务、read/Close 结果；本适配不会重读、验证当前权限或承诺原 I/O 已成功。

非 plain media 返回 Foundation `UnsupportedMediaType`；非法 UTF-8、元数据、union、请求或 next 结构返回 `InvalidArgument`；合法非零 offset 页、truncated 页及超出本文限额或请求字节预算返回 `PayloadTooLarge`。领域错误均为 `NotStarted`、不带正文；任何错误均返回零 StructuredDocument，不暴露部分解析结果。每次扫描至多 4 KiB、元素处理及返回检查 context，原取消/截止错误保留 `errors.Is`，无内部 goroutine、timer 或流。D12 正文的 UTF-8 校验由分段核心完成，避免在完整字符串校验内跳过取消检查；标题先限字节，再复用完整 DocumentRef 校验。输出正文须由调用者显式取得，默认 fmt/slog 投影不含正文。

重叠错误固定按入口 context、请求、非 plain media 分类、plain 元数据/union、正文 1 MiB 硬限、D12 next 不变式/分段 UTF-8、合法 partial/请求预算、完整页 next 等值顺序判定。非 plain 分类不声称该 DTO 已合法；超过硬限不继续扫描正文。在硬限内，truncated 同时 next 为负或 UTF-8 非法仍为 `InvalidArgument`，不被 partial 分类掩盖。默认 JSONHandler 的 slice/map/struct 嵌套通过类型的安全 JSON 投影保护，显式 `Text` 访问保留；这些内存类型不是持久化/public DTO。

### 10.3 当前范围与验证

六个 Go 源/测试已实现，手工 byte oracle 覆盖中文/CRLF、BOM/NUL/混合换行、英文小数与缩写、固定闭合符、非 ASCII 空白、emoji/组合字符；另覆盖 UTF-8 错码、恰好/超限、全局句数、取消零结果、确定性、D12 非法 union/元数据/partial 页和真实版本复制。

首轮 unit01 真实 FAIL：最后一个不变性断言用 `reflect.DeepEqual` 比较含非 nil closure 的 CreatorRef，Go 函数值即使相同也不深相等。改用真实 DocumentRef JSON 编码前后字节比较，保来源/正文 oracle；此失败不认领产品元数据修改。原进程 actual Wait=1、原组退出且私有 runtime 空。非作者静审同时指出默认 JSONHandler 嵌套漏出 Text 和重叠错误分类未明确；本轮已定向修复投影、明确优先级并补有限反例。

2026-10-10 修后作者用固定 Go 1.27.1 实际完成 `go test -count=1 -p=1 -timeout=60s ./internal/central/retrieval/parser`（11 top、0.265s）、同命令加 `-race`（1.253s）及 `go vet -p=1 ./internal/central/retrieval/parser`，三项 actual Wait=0、各原进程组 absent、私有 runtime 空，监督工具均实际 terminal 0；gofmt 和 diff-check 通过。每个长命令在启动同 process fresh disk≥5 GiB，私有 telemetry mode off、移除三个旁路、共享只读 modcache/私有 build cache、GOPROXY/GOSUMDB off、readonly mod，未启动外部服务。可再生日志在 ignored `output/ai/d13-plain-text-parser/`，unit01 原失败保持。

未参与实现的 Secret 实例完成六源实际只读复核及返修窄审，确认来源/范围、UTF-8 byte split/全局限额、取消零结果、D12 完整 plain 分支和安全日志投影，有限接受且无剩余 must-fix；未运行 Go/资源，不冒作者测试或真实联调。纯函数及无 I/O 值适配已具备小范围联调输入，完整 D13 仍未完成。

真实 D12 ReadDocument→实际对象读/Close→Parser 小范围联调尚未运行，待单包基础检查后另行组织。Markdown/PDF Parser、chunker、ContextProvider、embedding、lexical backend、索引发布及生产 Project initializer 均不在本批，也不由本批证明 D13 整体完成或解除既有 STOP。

### 10.4 首条真实 D12 值联调候选

新增 `tests/knowledge/plain_text_parser_integration_test.go`，唯一入口 `^TestKnowledgePlainTextParserIntegration$`，固定三子：`full_current_bytes_after_actual_close`、`partial_and_nonplain_rejected`、`foreign_owner_produces_no_parser_input`。复用已有真实 Account Bootstrap/邀请/兑换/Login、同 Store Knowledge/Project/Object/Audit/Outbox；Project 初始化仍是既有明确的上游 SQL fixture，不冒生产 Create/Skills 初始化。正文全部通过正式 CreateDocument/UpdateDocument 和 D05 canonical 对象产生，没有 SQLRead、假 Domain 或内存成功 reader。

第一子正式更新至 version 2/新的实际 ObjectID，原文固定为 23 bytes 的 `甲。\r\n乙！\r\n\r\nA. B?`。委托原 ObjectReader，只有其真实 Close 返回后才 hold；期间 ReadDocument 未返回、原 caller 未退出，取消的 Drain 不能成功。释放 barrier、原 caller 实际 join、原 Close/Drain 齐后，才将同一个完整 D12 DTO 交给正式 ParseBoundedContent。核原对象 Meta/真实更新结果/返回 DTO 的一致来源、段落 `[0,14)`/`[18,23)`、句 `[0,6)`/`[6,14)`/`[18,20)`/`[20,23)`，读和解析不新增业务事实。短对象不要求持活 lease；最终无 reader lease 只补充原 Close/Drain 证据。

第二子实际读取 MaxBytes=6 的 partial 页并实际 Close，Parser 拒 PayloadTooLarge；实际发布/读取 Markdown 再 Close，Parser 拒 UnsupportedMediaType，均零结果。第三子用另一真实登录 Owner 请求原 Project，要求 NotFound/零 DTO、Object ReadObject 调用次数零（含拒绝尝试）、不调用 Parser且实际 Drain/业务事实无增量。失败路径也释放原 barrier、取消并等待原 caller，不把 timeout 当 join。

共享 root driver/supervisor 由 coordination 唯一接线，增加独立 exact selector、root-only 与四个 RUN/PASS 节点及唯一 actual Wait0 门；source inputs 在原闭包上只增加 `tests/knowledge/*.go`，尾部重新枚举集合并核字节，不借用正文 HTTP selector/Schema/native 模式。原 top 含尾 120s、调用 20s、Go 6m、root 540+60+3s、host TCP 75s、七资源及 private/runtime/desc 双尾不变。离线入口控制源为 `.agent-state/d13-plain-text-parser/entry-controls.py`，只检查真实函数及受控输入，不执行资源。

当前新 Go 源已落盘，尚未启动 PG/Object/socket。六个已验纯 Parser Go 源及 `plain_text:v1`、原 D12 服务/fixture 均不修改。真实运行须另获窗口，候选准备不计联调通过。

离线 race-c 首次实际完成：2026-10-10 07:36:12 UTC，同 process fresh 6135709696 bytes，通过 `go test -race -p=1 -tags=integration -c -o output/ai/d13-plain-text-parser/parser-integration-race-01.test ./tests/knowledge`；原 Go PID427925 actual Wait0、原进程组 absent、私有 runtime 空。候选 41743204 bytes，发布输入身份 SHA256=`e5e9baa664b349fe62030e209f3d4e7f5f6308d908e002ca202fbf98894b0129`。同监督随后准备 exact list 时 fresh=5293277184 bytes，低于 5368709120 门，因此原 outer58349 actual1；list 未启动、MinIO 复制未执行，不属于产品失败。容量恢复后只补同候选发现，不重编已成功输入。Go 的 `-test.list` 仅能发现 top；三子源码闭集可核，实际各 RUN/PASS 必须留到真实窗口。

共享入口已由 coordination 完成限定接线。入口控制固定 driver 两处、supervisor 七处增量；公开 `inverse(name, source)` 仅接受这些字节，剥离后以固定 SHA 和实际 Git 源全文核对 main `3a7a3fb5`，未知、缺失、重复增量及跨域门弱化均拒。真实 `main` 的非 root/alias 拒绝控制在 adapter、目录、进程和 TCP 哨兵之前退出；结果/Wait、精确输入重枚举和原 observer 的受控七资源双尾均保留。2026-10-10 本域离线 `python3 -B .agent-state/d13-plain-text-parser/entry-controls.py` 首次实际 exit0（session9853），没有启动 Go、Docker、socket 或真实资源观察；协调者的既有入口兼容控和非作者限定 review 仍独立记录，不把此方法通过计作联调通过。

正式 Knowledge/D18 主线 `04455194` 合入后的入口基线已更新；D13 原两处/七处增量保持逐字不变，剥离后完整还原这次已包含 D12 浏览器入口的主线。额外拒绝 D12 expected top、原 Wait0 和 input-tail 被弱化的 source，既有 D13/Secret 反例保持。本域同一离线命令及 diff-check 再次 actual0（dd93cd），未重编候选或运行业务；共享 union 的其它入口兼容控由 coordination 单独完成。

root 精确授权后，仅退役本树已无进程引用的可再生 `output/ai/d13-plain-text-parser/go-build`：无符号链接，原 468070400 allocated bytes；实际 free 5223272448→5691346944 bytes，cache absent，candidate/日志/私有配置/源码未动。随后同候选 `-test.list '^TestKnowledgePlainTextParserIntegration$'` 在新 list02/runtime、same-process fresh5691293696 下实际发现恰一 top；PID438212 Wait0、原组 absent/runtime 空、outer62370 actual0。原 list01 预飞失败保留，不重编。已将固定 shared MinIO 普通离线复制到本树 driver 既定 `output/ai/deps-minio/bin/minio`，109289632 bytes，SHA256=`dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`；未启动 MinIO、Docker 或任何产品用例。当前待共享兼容控、限定独审及明确真实窗口，联调仍未运行。
