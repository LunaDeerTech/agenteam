# D08 Project Owner 只读 HTTP 验证

2026-10-07：完整17路径（16技术路径及后端README末件）已独立有界PASS并由root采纳，产品 `901eb54605d293d4308caadd278c2c3a7ae1b824` 已提交推送、root已核远端一致。依据[已接受规格](project-owner-read-http-spec-verification.md) `ac33ea15` /归档 `d5a21f63`，沿已接受S2产品 `4e615c7d`。这完成本只读切片，不代表完整D08/D09/E01。

## 1. 固定交付与边界

[技术完整独审](project-owner-read-http-verification-evidence/objects/e10e890b2d91698c7b290b2f31014d4d0ec00ac9ce5148ab9183b0f58fa58d9c)（SHA `e10e890b2d91698c7b290b2f31014d4d0ec00ac9ce5148ab9183b0f58fa58d9c`）、[技术结果](project-owner-read-http-verification-evidence/objects/b2490b7e19d371c82266a35c80c457706002392d30777dc239b938af3a8e8cf1)及[17路径最终结果](project-owner-read-http-verification-evidence/objects/380f0b0cc68e0ee59e88450d00e0795e213aa319355f033d6a20fae77a128b7a)绑定candidate04 `097d47b4f4cdb5c92a102f62f6398c0d691bc918111c76b95a7972cd54004b38`。[精确17路径](project-owner-read-http-verification-evidence/objects/22f909c6f802f9b4d09207ed03b53bb9a10a59aa6df377aa34c9b1c7fe423459)复用最终产品提交，不复制源码树。[README末件独审](project-owner-read-http-verification-evidence/objects/3ada0f39723d83e6dfe25b75936fdec13c104c504a0b2a1ff909d4f135b72edb)确认只新增四段，其余完整旧文字（含Summary/Usage）原字节保持；不为文档重跑业务。原technical16报告的“README待审”保留其历史时点。

交付窄Reader与默认root的Project Owner列表/详情GET/HEAD，沿相同Store/Project Authority和当前Session/Owner事务。完整行及隐藏哨兵先验证再裁剪；owner/filter/order/cursor绑定、Close后Err、Unknown/取消抑制候选、严格query/wire/schema、完整最大页编码、两秒实际I/O完成和原resolve/Usage精确路由兼容均在限定验证内。没有新增Project创建/写HTTP、UI、迁移或生产Skills/生命周期。正式身份来自Bootstrap/Invitation/Redeem/Login；Project.Create、持久Skill和特殊canonical lifecycle状态属于明确的测试事实，不冒生产初始化或生命周期runtime。

生产Resolution/Invocations与D24仍未绑定，系统统一会议initial/update含首轮标题、Project不override/复制初值及compaction/Execution Summary规则保持。ready503、D08–D28/E01未完、E01未开始及Object runtime join、OpenAI tools独立验证、SPA concurrent-publication三停止不变。

## 2. 实际验证与版本组合

| 范围 | 实际证据与限制 |
| --- | --- |
| [作者最终交付](project-owner-read-http-verification-evidence/objects/9e5ff03a0a662f2b62f94b677518ce58f2b6d0aef568767b528b1ab2d3b4d113) | Project普通采用原未改通过项与candidate03受影响5top重跑组合，完整race52top/111nested；HTTP普通/race各12top/91nested，native3在pure中明确跳过；app最终精确无资源7top/94nested普通/race。相关race vet、两cmd build、integration compile/精确发现和固定依赖图闭合；不称最终ordinary全套重跑。 |
| 容量/schema | 作者真正编码/完整反解4,948,120B（上限5,242,880B），100×8192B描述、819200次ampersand转义，标准schema27代表通过；独立controlled HTTP race1top/2sub采用另一组`<`数据得到4,939,930B GET/HEAD，并验证同步Flush与取消callback并发阻塞后的实际join。受控buffer不是真实PG/root响应。 |
| [正式native三组](project-owner-read-http-verification-evidence/objects/cca6fbf5a61533777f8f1070c4f64bf579e9366b0e8c136b762b64f7f9f943f0) | keepalive/slowbody/writeclose共3top/9sub、10实际listener；direct3/adopted3均实际wait、status0。strace事前绑定端口/连接，关联TCP所有状态（含TIME_WAIT）及owned进程两次空；TIME_WAIT约一分钟后的清理不能写成测试完成时立即零。 |
| 作者真实PG | [六轮原结果及组合](project-owner-read-http-verification-evidence/objects/cf3f0809b46989ae552acbd1137568440200a1c57f35ecd00df2e348cc9cf62c)：candidate03 new1 Projection、new2 Terminal通过；candidate04 new3-rerun Root通过；old1五个旧top通过与driver-v02单Usage恢复通过合成旧6。实际六轮含new3和old1两次首红，未统一重跑新3旧6。 |
| 独立真实A/B | A01通过1top/7sub：当前Session/页大小/筛选顺序下cursor绑定、隐藏哨兵坏事实/恢复、正式nonowner/admin与撤权、真实User/Project锁含缺失Project、committed后取消抑制、同Reader事务物理COMMIT+idle ACK丢失保原UnknownAttempt及零候选。B02通过实际app.Run列表/详情GET/HEAD、旧resolve与真实Usage账本/聚合、路径status/code/method、单连接和实际shutdown。A原结果复用；只重跑修正私有断言后的B。 |
| [真实B02 body/schema](project-owner-read-http-verification-evidence/objects/5efe613b4beea7713fba83685a15c8904867d5605a1b1d9e75227176061a5d0d) | 原socket-read列表185B、详情380B，经固定Draft202012+FormatChecker、闭合本地refs同字节通过2例；[来源manifest](project-owner-read-http-verification-evidence/objects/698e84163888e386424c3bbe79f3a6c6a40b9988757ef243aa88c47d4d3d5bfa)绑定实际B02、path/id、producer、frozen输入及安全metadata。无parse/stringify重编码、无网络取schema，不用合成JSON冒真响应。 |

## 3. 原首红与限定修正

保留typed-ID编译失败及用reflect.DeepEqual比较含函数字段的opaque LockKey假失败；只改测试typed zero与正式CompareLockKeys/Mode，生产自candidate01起未返修。candidate03 new3 root测试错误查内部列名 `model_id:null`；candidate04只改#16为严格len3/canonical id/version1/显式`model:null`。首红后的POST405/foreign404/ready503/同连接/shutdown断言当时未到达，在new3-rerun才完整到达。

old1原六旧中五PASS、Usage因缺`AGENTEAM_USAGE_SCHEMA_PYTHON`失败。driver-v02只补原解释器环境和失败单topgroup，产品/旧测试未改；usage-rerun1恢复PASS，五个旧PASS不重复。独立B01把trailing slash与canonical unknown都误断言404；原Account path.Clean实际给前者400/InvalidArgument、后者404/NotFound。[B01原归因与修订](project-owner-read-http-verification-evidence/objects/d1b0ee77810bff2db5783abdce21efa0144b06ec2314bfe2a1119ebfc6782fd2)只修私有B断言，A与产品不改，B02完成实际重跑。原失败command/raw/result与各轮清理均保留。

[app误宽原记录](project-owner-read-http-verification-evidence/objects/dc9f7e3cee11658b4b3553d3e607c8bf7898cf5072d7def6be588adf70583f4d)保留全ordinary包误执行11个旧网络/真实进程top的授权偏差。该轮虽actual0/directwait/两次owned空且事后所记PID/starttime消失，缺事前native trace，确切端口/归属TIME_WAIT无法追补；排除在授权native/root验收之外，不借后续正式三组倒填证明。

初始16个旧Project测试输入缺口、过宽图/提取器问题与独立图解析器误计未选dependency TestGoFiles的extra1461保留。Python模块闭包最初漏jsonschema_specifications的20份官方数据，后续仅补既装数据指纹并实际重跑schema，不安装依赖、不改旧冻结。[独立schema导入首红](project-owner-read-http-verification-evidence/objects/7d041b9a26d862ae0cc4ded40bb1ed7542367ecaf4234e31175bf2ef28979953)记录audit hook向被遍历列表追加路径导致循环超时，actual exit−15/wait/两次owned空；私有修复改遍历tuple快照后闭包通过，未执行业务main或改body。这些准备失败不当作生产缺陷，也不从历史中删除。

## 4. 资源及保存范围

[九轮完整资源集合](project-owner-read-http-verification-evidence/objects/6d468211a97b0cf7a439897e91a6ce2e51d38190c494d6714527ae62fc86664b)逐轮绑定作者6与独立A01/B01FAIL/B02PASS的command/raw/result/cleanup。全部9轮各7个精确resource、输入不变、actualwait及两次owned进程/资源清理，adopted0、forced0；原top120秒含Cleanup、package6m和fresh≥5GiB预算保持。native另有direct3/adopted3。实际daemon/PID1 shim的PID/starttime集合由140→176，新增36（作者24、独立12），非task-owned且未由任务wait，不与全PID1-Z口径混算或声称全机清零。外层owned进程结束也不把既有内部组件有界清理扩大成每个goroutine实际join证明。

[source-map.json](project-owner-read-http-verification-evidence/source-map.json) 将474个原来源映射到291个新去重对象（3,033,584B），另117个来源复用已提交S1/S2/S3相同字节对象。保存必要command/raw、probe/driver、原红/修订、真实safe body/来源、actualwait/资源双清；原manifest中引用的每项不都复制。116项大型图/重复输入/解析展开/产品schema及官方数据仅指纹；中间TCP raw poll明示省略，保完整cleanup-observations与首/末两次原raw。[命令摘录](project-owner-read-http-verification-evidence/command-excerpts.json)共72份精确字段投影，逐项绑定完整源SHA/bytes和省略字段，是派生数据而非原件。完整环境/树不能只从此小档重建；不复制源码、cache、binary、module或私密runtime。

归档只读取固定证据与当前文件指纹，没有运行产品、原driver/资源命令或Git。产品commit/推送/远端事实来自root确认；17当前路径与最终冻结指纹逐项核对。原件逐字节、JSON、派生字段、链接、Markdown/UTF-8/LF/末尾换行与生成内容空白检查通过；原patch/raw尾空白为保留原件例外，不规范化或增加忽略规则。卡片只新增接受页首，§1至末及全部旧字节保持；四当前入口另行接续。
