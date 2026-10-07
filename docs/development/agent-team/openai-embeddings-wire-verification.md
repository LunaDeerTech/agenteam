# OpenAI Embeddings float wire 验证归档

状态：本卡十六路径已独立验收、主线程采纳并提交推送 `2debfdde347d8c9262ab83d3fe5e18e947a983e1`，远端已核一致。本报告归位固定版本的历史证据，不执行原脚本或产品，也不将此库结果扩为整个 D09。工作契约见[正式卡](../work-items/d09-openai-embeddings-wire.md)，原[规格档](openai-embeddings-wire-spec-verification.md)保留当时状态。

## 1. 接受输入与可复核入口

实现和六轮真实输入为 `c1427fa4fb7fa118b12b2fb1f5d5f8917113ce2c` 加冻结十五技术源；正式规格 `15d7e08fd72eb2c05f7ef64c14cc0de4f5a87597`，技术 §1–7 SHA `1320107dd2bebba97188c976780a111d0ec12e2fb11ae866c9ad3473b5e53133` 不变。末件 backend README 基于 `18b6fb5a18958a4146d3be5c064a1fba0fa959dd` 单独审查后成为第十六路径。[最终交付清单](openai-embeddings-wire-verification-evidence/objects/270cb2d85136f81552c679090a38d7fa4d17497b1aaa15affc2f49f71eb9690d)、[主线程整合记录](openai-embeddings-wire-verification-evidence/objects/3906137e6b95433754715b2c00e6b03b0bc65e95b13d57c67e6e497c10952183)逐项绑定 main16 与914固定依赖无漂移，三份既有源原态等于 c142；提交/推送实际0，远端一致，**没有主树动态重跑**。

[作者十五源报告](openai-embeddings-wire-verification-evidence/objects/01ab0db2acf93093827a5fc625e3f19b16cea6aa85bcce08520642fe8abcabe6)保留其当时“独立与README待完成”原句；后续[独立最终报告](openai-embeddings-wire-verification-evidence/objects/f4f967d0b2ecccce3c8b5c5ee6b082ef9c4de6d5c0b4cab9d6643dd7f285bb32)接受十五源，README及提交由较晚原件补足，不倒改历史。[十五源差量](openai-embeddings-wire-verification-evidence/objects/612d71f06f511e5d9e8555cd58032ad33a45b3d7134c2a2955dee899809879fb)和[README差量](openai-embeddings-wire-verification-evidence/objects/ddbd8fa59132a01b05fe9615dc5ff77d8d93dccf7cfa98c9e06a664b00c75435)保留原字节。精确路径、各版来源与运行记录在[索引](openai-embeddings-wire-verification-evidence/index.json)。

归档含604逻辑引用、427新对象/38,093,184字节、44固定Git复用路径及27保存产品源版本。源/原probe/runner、command/env/raw/result/input/cleanup按SHA去重；七份容量诊断所需标准库源码是有限原件。914依赖、工具图、生成testmain、可执行文件与cache只保留原指纹/固定Git引用，不复制整树。spec档的六份官方SDK原件及fetch/proxy403记录直接复用永久Git，无SDK执行或供应商conformance结论。

## 2. 分阶段结果与原失败

| 阶段 | 实际证据与版本边界 |
| --- | --- |
| A 输入、编码、严格解析 | pure01八top通过的是旧23MiB累计分配门槛；candidate02引入私有安全类型并收紧21MiB，pure02只两受影响top通过。原race01 actual1/6.178178s，七top通过、一top因TotalAlloc22,072,128超过22,020,096失败；无DATA RACE。Fatal后的坏值/取消尾部当轮未到，不能回填。 |
| A 容量诊断与测试修订 | [容量独立诊断](openai-embeddings-wire-verification-evidence/objects/986343d0dad915b62b570950a531fa04118f8edbc5a075f385a561086f9a634b)区分累计TotalAlloc与同时持有capacity；它不是完整A验收，也不是RSS上界。candidate02→03仅改embedding_json_test.go，硬验raw+向量原backing+clone+有限辅助小于21MiB，不以GC或放宽上限修复。pure03/race02只受影响容量及原尾部通过，组合为旧七PASS+新一PASS，非同版八组重跑；vet/format另有原命令。[完整A独立报告](openai-embeddings-wire-verification-evidence/objects/94a0c44adff8650ff71318de6cfe3537b71f8a497f69b1573e8dcbdfeedaf091)两top actual0/3.113693s，1823输入/5PID双清。 |
| B 共享Budget、transport与handle | 十源冻结；六作者轮中format1806（1805+gofmt），其余1805。独立[B报告](openai-embeddings-wire-verification-evidence/objects/8784c23b9f54d1862407716f4d7ed1128d5990cd14c2dee18b952bc521420ff6)两top/三sub actual0/3.112091s，1825输入/5PID双清；与作者差异包含旧生成testmain排除，不是固定产品源缺件。ioDone跨实际Do/read/parser、两个Close和Stop；Drain成功后才释放材料和槽，workerDone调度收尾另wait，不据此捏造产品缺陷。均是受控纯验证，无真实fixture。 |
| D 五源与最初driver | 五新增加B十源，373编译工作区依赖、2496 selected graph/388包分列；八轮actual0仅format/编译/vet/精确list（新3、旧6），无测试体执行。两条生成testmain记录共用一个路径，保留记录而不复制cache。初次weak-passwords embed门禁exit1是准备失败。 |
| startup01→02 | [原STATIC BLOCKED](openai-embeddings-wire-verification-evidence/objects/9e68c10064c47ed29e910de120650e93d92f7bcedbb541c5072ff3cca108c360)发现process TestMain无条件构建两cmd未入闭包；原907Git/3510runtime/455包与三次离线门禁通过均保留，不能冒运行就绪。[修后STATIC PASS](openai-embeddings-wire-verification-evidence/objects/f8dc85600feb792451e9e13974b6aecbcc8721ba43279dadbba424fa96095249)绑定914Git/3513runtime/459包，新增七Git/三runtime，明确CGO1、nonrace、无integration的两cmd加三hosthelper五roots，既有server CGO0保留。十五产品不变；四轮离线actual0之后才进入真实轮。 |

容量原race日志见[原RED raw](openai-embeddings-wire-verification-evidence/objects/6c0321e05070a96719cf6a27ce8fcaebb84cd3f98705666cc787a98bd7e080fd)，测试修订见[candidate02→03原diff](openai-embeddings-wire-verification-evidence/objects/455e247482cc7cc1d49223412ef4474278ab3890086b1b6971f545bea4962d5f)。A纯reader的取消尾部不冒D04真实handle join；B与后续真实结果分层。作者九个新/旧纯测试、受影响回归与原旧26纯等详细选择，以各command/raw为准，不把编译、list或其他包“no tests to run”计作业务PASS。

## 3. 六轮真实结果与资源终局

每轮调用原 `sh scripts/test-security.sh -run <精确选择器>`；command保存cwd/env、开始/结束/实际wait，原 `-race -count=1`、2m/top及6m/package预算未扩大。作者新三与旧六按五轮通过，独立另两代表同轮通过，共十一top；不称一轮全矩阵或独立重跑作者覆盖。

| 原轮 | actual exit / 秒 | 顶层PASS | 自有PID/starttime |
| --- | --- | ---: | ---: |
| author new-http01 | 0 / 101.179 | 1 | 108 |
| author new-terminal01 | 0 / 52.460 | 1 | 70 |
| author new-budget01 | 0 / 52.367 | 1 | 71 |
| author old-chat01 | 0 / 54.210 | 3 | 71 |
| author old-structured01 | 0 / 53.781 | 3 | 72 |
| independent01 | 0 / 50.779 | 2（另两个sub到达） | 73 |

作者原件入口为[五轮原索引](openai-embeddings-wire-verification-evidence/objects/1c16b4e783d27724d10288448f7990d9ed97cdcb02699baa39eae08e2c47cf58)；独立[raw](openai-embeddings-wire-verification-evidence/objects/b5be97e502ff69a1921cba29687397d9b270b43d4f2f6b5c94ecba36b916fe78)、[command](openai-embeddings-wire-verification-evidence/objects/e4f6912338bccd074e89b0479047e6ebbfe18cae5ed01e88249bbe24512b924f)、[result](openai-embeddings-wire-verification-evidence/objects/5c79759c407949a4f57a4e936aee4da2ed1b58c4e910ee4c7310dcd17f34d898)及[两次cleanup](openai-embeddings-wire-verification-evidence/objects/b9430d4e98e8c33af18fb76534f22b5bae85ab9e58e54b7a413427cf2e09ef06)全部保存。每轮两次fresh磁盘门禁至少5GiB，7exact IDs双absent、所属PID/starttime双空、runtime空、monitor0，actual direct wait及0 adopted；原baseline含完整Mount比较不变、十五源/914Git/3513runtime与私有输入前后一致。原cleanup保存比较布尔和baseline原快照，未另存每扫current全对象，不编造额外快照；历史PPID1 Z未被本任务回收。

HTTP证明精确批量编码、正式Policy、零发送预检、安全错误、TLS/POST重定向及闭合fixture；Terminal区别JSON前缀与实际EOF、截断/紧限额零候选、可靠usage后失败/取消和WroteRequest持有尾部；Budget验证混合text/structured/embedding的global64/Project8及先全取消再共同等待。独立以literal3×3 golden验证请求原文、索引重排、整数usage、完整EOF后维度错误仍保留可靠usage；第二top含withheld EOF与真实WroteRequest取消，实际callback退出/Close后才准入替代，Force join，无拒绝后发送。global64和structured宽度复用作者证据，非独立额外实跑。

## 4. 准备失败与可重建边界

- A最初SQL/embed准备、内联工具解析错误、容量独诊旧strconv路径查询错误按各原记录保存；无完整监督argv/raw的准备叙述不升级为可重放命令。没有把未运行的早期vet补成通过。
- startup02可选报告辅助脚本误断言“无embed”，原exit1/trace记录保留，stdin未存，不能完整重建该脚本。它不改产品，也不是资源轮失败。
- 独立source01 held-response缺model是执行前自查，原source与source02差量保留；四项offline（4452输入）通过只证明format/编译/vet/两个list及gate，真实体从independent01起计。
- 独立真实结束后辅助读取把cleanup数组当字典，工具chunk9acc49原exit1仅会话/最终报告叙述；stdin及独立command/raw未冻结。改只读读取方式未改原件/重清资源，不合成“原脚本”。
- 全部必要受测产品源与失败版本均可由对象或固定Git定位；这不意味着所有历史临时辅助脚本、工具二进制或运行环境都可完整重建。原external路径只作历史命令环境，不要求现在scratch仍存在。源码记录不等于未来任意机器重跑PASS。

## 5. 接受边界与后续状态

本卡提供空parameters `{}`、float文本批量wire；ExpectedDimensions由受信caller明确给定，不推断型号默认，不支持原生dimensions。可靠usage与失败向量候选分离，结果一次消费/深复制，材料与共享槽保持至实际I/O和取消尾部完成。真实为自有Provider fixture及正式D04/Account Authority/Audit/Policy接缝；任务账号种子不等于Nonchat，也没有通过Account/Provider管理HTTP建立完整业务consumer。

不接受官方SDK动态执行、真实供应商smoke、全进程RSS上界、Nonchat/InvocationID/Resolver/Facts/Usage消费者或生产root绑定。Jina/Anthropic后继阻塞报告不属于本产品proof。完整D08–D28/E01未完成、E01未开始，Summary待决、ready503保持；Object runtime join、OpenAI tools独立验收及SPA concurrent-publication三停止不重试、不改写、不转派。现有Runtime HTTP/UI等接受与旧规格档历史不改。

## 6. 只读离线复核

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/openai-embeddings-wire-verification-evidence/verify_archive.py --repo /workspace/agenteam
```

私有payload审查时追加 `--documents <payload>`。校验器只读本档对象和固定Git，核原字节、十六交付/914依赖、分阶段源版本、36离线轮与六真实轮的原退出/输入/双清、原RED、五行政差量及新增链接；不执行任何保存脚本、产品、网络或资源操作。外部toolchain只验证原指纹记录一致，不在本机重新哈希或复制工具。原日志与diff空白保留，格式检查仅本次八个非objects正式文件。
