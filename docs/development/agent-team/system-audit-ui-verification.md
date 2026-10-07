# System Audit 只读 UI：接受与验证记录

主线程已接受三十六路径产品并提交推送 `a0e73bd8fc7fa40e1f424f5817e7b1b3b281def1`，核远端一致。本报告归档已有验证结果，没有重跑产品。管理员可通过“审计 → 系统审计”显式应用结构化过滤、逐页读取并打开安全内联详情；页面没有写入、导出、重放或关联领域补读能力。九叶／四组和第十三个精确 return 目标已整合。当前状态取代[规格历史档](system-audit-ui-spec-verification.md)的实施待验描述，但不改写其当时记录。

[工作卡 rev1](../work-items/d27-system-audit-ui.md)技术§1–7 SHA256 `e7c96b588eed6218030ef6ce42154c7967667ff156bd8c343d0cdd14cf2bd8d5`保持原字节；[档案索引](system-audit-ui-verification-evidence/index.json)给出全部原始路径、SHA、字节数、内容寻址对象和固定 Git 引用。[只读校验器](system-audit-ui-verification-evidence/verify_archive.py)仅核原件、Git 和本次文档。

## 1. 接受输入与分版本边界

- 前端基线 `1ff0442`、[Audit HTTP](system-audit-management-http-verification.md) `b124650`，MIME schema 修复 `fa2d775`分别绑定。MIME完整原证据复用既有永久档，不重新复制或外推新运行。
- [作者最终35源清单](system-audit-ui-verification-evidence/objects/ccd05bdf009fcc6693299c5f78f689897fb86e0e00b6ef47f6c17917612d4ad8)与[独立35源清单](system-audit-ui-verification-evidence/objects/db0ee190a271bc3601fa47348eaa57930abcdc47499d8ee9d2c1dea3b6edc0a5)逐 SHA 相同；[README末件36清单](system-audit-ui-verification-evidence/objects/033402e2dbb747e121f05b7d858a81e58ed953c63749f5453742b7e5e636777a)精确对应接受提交的36个改变路径。README原基线为`86a882`，不是早期前端产品版本。
- 作者五组通过由 read03/input03、authority03/input05、navigation04及两组旧Navigation/input08组成。[作者最终索引](system-audit-ui-verification-evidence/objects/90b3cfc8849448a6215d54e7384b5554cfe70dd3ff8ddc6053f50037fe438d1e)保留全部12轮和未变语义复用依据；不是所有场景在input08重跑。
- 独立最终A/B在同一真实轮、input08/execution03通过。[准确最终报告 rev2](system-audit-ui-verification-evidence/objects/ac6e9c16a5495a1b05696e02d0e46a25018e7a0c1ffe56e5976f9efc10c821cb)、[原版报告](system-audit-ui-verification-evidence/objects/ab2d3ab5775f55f57b36675a673153e0bda160608efe1f52bbdeb550debe156a)和[原JSON](system-audit-ui-verification-evidence/objects/297c9473cda50ba87d71a8b77ece114337c232a1d9e0d833462af10d7e69a3aa)均保留；rev2仅澄清nav03列身份来自静态推导，没有改变测试结果或原raw。
- 所有40项dist只保留各次原清单/哈希和构建日志，不复制产物。最终真实输入绑定35源、943固定Git依赖、18工具、15独立私有输入及7604 runtime指纹；不把指纹库存误称已复制完整依赖树。

## 2. 离线阶段、原失败与后续覆盖

| 阶段 | 实际证据与修订 | 复用限制 |
| --- | --- | --- |
| API / typed metadata | 作者原178PASS+2测试前提FAIL，修正undefined被helper默认成null、提前abort改变超界分类后受影响5PASS；随后格式化五源，最终180/type/format通过。Go原oracle、MIME追加绑定及独立Go/Node原件保留。 | 三生产文件在前提修正中不变，格式化改变SHA；完整type闭包在冻结时核验，不回填为原每轮全闭包。独立Go actual0/2.544462s、Node两个组合actual0/0.217664s；受控JS流不是TCP或PG。 |
| Owner / controller | 原type exit2和43PASS+3fixtureFAIL保留；修公开观察类型/正式协议前提后14PASS/32skip，最终46/type/format通过。独立两组合actual0/0.293447s、248输入、2PID双清。 | 原Model调用未到native cancel，不能冒充已join；虚拟30000ms不是自然30秒。四旧域reverse native尾部独立证明、六域复用作者；late401是已解析API拒绝，不是abort后native解析。原runner无UTC的轮次不补时间。 |
| App / View / router | page-app01两个前提红、app03原总GET断言红和真实安全trace保留；app04新实例语义/native cancel尾部通过。早期C完整`npm run check`：46files/1763tests、type/build通过。独立App两组同轮actual0/2.649925825s。 | current403→checking→同身份恢复在旧route上先挂新实例的序列符合卡，不记为产品修复；app01没有后续trace，不倒填。jsdom焦点非native；旧8browser仅type/list没有业务运行。 |
| Harness / 运行准备 | D01 object资源种类、member首页系统入口为静态前提错误；改stored_object与bare入口0/exact users-return。后续只补safe诊断、正式Logout JSON、SQL casts、三rotation summary映射。 | 两次独立静审与勘误原件保留，不把静态发现称真实失败。原444是Account编译闭包，真实938缺5动态构建目标；补至943不冒称原read01完整。 |
| 最后CSS | nav02 caption1×8实际溢出，局部sr-only padding修正；nav03实际768px TD index38为scroll76/client68，固定DOM/tag静态推导其详情列身份；新增桌面末列表头120px后nav04完整通过。 | 两次都是一处View CSS窄修。最终源不是早期1763轮输入；各CSS受影响12PASS/8skip、type/build、native导航与最终独立两组提供后续覆盖，不写“最终全1763重跑”。 |

详见[API作者报告](system-audit-ui-verification-evidence/objects/df9a5385b6434ed5cc154bc025e6e70bffd1bec119ebb539585997bde0871c28)、[Owner作者报告](system-audit-ui-verification-evidence/objects/4735a5c0eaefa9421594bf3b1a595cf56739f055679495ee66cd00295171d380)、[页面作者报告](system-audit-ui-verification-evidence/objects/3cdd70929b92c10b3321d5185b80ba7e8af8769084a1680c1b130259691a6663)及索引中的三个独立阶段报告。字典切片辅助准备失败只有保留的叙述粒度；不补造独立原cmd/raw。完整1MiB响应是合法空白填充的边界样本，不声称真实producer必然生成该最大长度。

## 3. 十三轮实际退出、资源与原失败

下面的秒数均为原command的实际整轮时长；ID／PID／wait是原精确资源、所属PID/starttime与adopted实际wait计数。所有原输出、环境、driver、before/after与终局按原字节保存。

| 原轮 / 输入 | actual exit / 时长 | 顶层结果 | ID / PID / wait | 原件与清理 |
| --- | --- | --- | --- | --- |
| read01 / input01 | 1 / 92.919s | FAIL | 7 / 118 / 4 | [raw](system-audit-ui-verification-evidence/objects/8ccac9fb6176a1452c20b6d8a4eb3980ca21f5f2ff3defdc33688c52fd743303) · [command](system-audit-ui-verification-evidence/objects/7b4c528088fda97f5de0a0dfc75488fb8a9162d92e241a954d3596bbf645398f) · [result](system-audit-ui-verification-evidence/objects/ae0e482c50e0ab303897246f97a4e13ac95c596e816638d636f154d21ad1de3e) · [原双清](system-audit-ui-verification-evidence/objects/c963f1d0a612d0334dd35c20e955361f2e272108955aa195996b8708f6b88e8b) |
| read02 / input02 | 1 / 58.180s | FAIL | 7 / 86 / 4 | [raw](system-audit-ui-verification-evidence/objects/a824ce8d451f2218d2a7fa8d462e9bf6fc9665219c053141182813f77ec2a4e8) · [command](system-audit-ui-verification-evidence/objects/339c7b5ac209d437b901fee91183c7b993b5a3b62480ec48427783283ea95316) · [result](system-audit-ui-verification-evidence/objects/e194025fe1dfca0724cfd6166d2fa752747639c5a1c496a80f5c3acc773846f0) · [原双清](system-audit-ui-verification-evidence/objects/efe00e52ed9c31ac9a5cd81947b72b2c9e7485afb4cba93550363f35ffdef1ba) |
| read03 / input03 | 0 / 58.030s | PASS | 7 / 83 / 4 | [raw](system-audit-ui-verification-evidence/objects/708b7ed364562e26bb984e3029d92011768ffee9bc5f78b3a20f8a8645f6f6ca) · [command](system-audit-ui-verification-evidence/objects/d21c59c916e39663efd004d003cf023a6ee64c0bcd7da229b2216c82be7c7432) · [result](system-audit-ui-verification-evidence/objects/908fc77453561efc947f5e0a25ef4cab0e70ea1c3cdd352cc97c5940ebd5cb6f) · [原双清](system-audit-ui-verification-evidence/objects/92e499d0a2f08023336207898712ed591d40398365d0ffc78ede7adff16a00c4) |
| authority01 / input03 | 1 / 58.648s | FAIL | 7 / 84 / 6 | [raw](system-audit-ui-verification-evidence/objects/47cc9c65784e7a11adc5e77d67b20a0e903d74a0cfc680443d8dd3c28e450f83) · [command](system-audit-ui-verification-evidence/objects/83cee83abdadf7d3929e15fb3f89087d1fede1be20820da64b801a6b6c28c61d) · [result](system-audit-ui-verification-evidence/objects/48fd547e6a67e78c5d4d24cfd71f414c3974c74739027886ed76ce279ef20fd0) · [原 cleanup=false；后续恢复另列](system-audit-ui-verification-evidence/objects/99c31d799718200eb086b213fcbe1d082dfd771dadf6aaf476688ddc674df0ca) |
| authority02 / input04 | 1 / 55.741s | FAIL | 7 / 87 / 6 | [raw](system-audit-ui-verification-evidence/objects/2985dec1da65048066fd52072611b15fae13fa65baff9640c805c95ea409d3fd) · [command](system-audit-ui-verification-evidence/objects/d6d088330f1675acdab777ebefbb804459955958b7132dbbb09a1d9a991a7a49) · [result](system-audit-ui-verification-evidence/objects/5e35498a01f02c0afee9bb49f61d50b72a3c54c5cbc992165ffd068bc249fe71) · [原 cleanup=false；后续恢复另列](system-audit-ui-verification-evidence/objects/785525773d147be39d2121db326b9971c15af1a4878184b02a4d86adc29cdff9) |
| authority03 / input05 | 0 / 58.495s | PASS | 7 / 90 / 4 | [raw](system-audit-ui-verification-evidence/objects/552ff5e93f3a454ce097a63376d9f1db339742f8bf6f70985a1cd9963dc417c4) · [command](system-audit-ui-verification-evidence/objects/11761b4f9ef0882226de87b521e7ea8971b71287488a723404e9e6d31ca3d7f7) · [result](system-audit-ui-verification-evidence/objects/17b1ec5c54b8a028ff7010ab2305f4530a5663c4343731fd9f2180cbb9fa7f6f) · [原双清](system-audit-ui-verification-evidence/objects/29ab0eea649aea9513dcc2a54aea5d70ea6f76b6ca7236e334b9ca511372be12) |
| navigation01 / input05 | 1 / 56.441s | FAIL | 7 / 84 / 4 | [raw](system-audit-ui-verification-evidence/objects/152becb2b7eeecce9430f0b196fd5b23cd2a2af9fa7fe7583e2ba0f02c7830c5) · [command](system-audit-ui-verification-evidence/objects/6963d16cf455f3ba64943498a2cce9f5ae7b1e05669df16ba31f737737bf1351) · [result](system-audit-ui-verification-evidence/objects/4a691aa7139d72b994b52c1b4896ca8cd1b6c71f2828778a5aebbaefbc5eefbb) · [原双清](system-audit-ui-verification-evidence/objects/22e6fd3985fb160da854e254e3073e39a32eaa5fd232c6cc17ea2b2b19dc360d) |
| navigation02 / input06 | 1 / 52.054s | FAIL | 7 / 83 / 4 | [raw](system-audit-ui-verification-evidence/objects/0c711294e12596a4107b9bf568d051167ec405931e00fe4db364e466a25e9aa6) · [command](system-audit-ui-verification-evidence/objects/cedfcd62edc5a745f5a1f75938ca878c0b2acaac93c1b2b1e416070371fe5672) · [result](system-audit-ui-verification-evidence/objects/960e38979e5be27c5e8b08fa54498d7689757a5799a0b09e3ed479922f8d984d) · [原双清](system-audit-ui-verification-evidence/objects/953654f5b2a31b16a4ff795a5cf831beee9146530172bf53e669d914959facaf) |
| navigation03 / input07 | 1 / 55.748s | FAIL | 7 / 82 / 4 | [raw](system-audit-ui-verification-evidence/objects/2183c803ab843fbc10f7bbc3c9106e18684b6b0a217e0cf1499fb07be41c339f) · [command](system-audit-ui-verification-evidence/objects/de81918f71cfcef66d385612a7b76bd62b2b3929a0997284ab4f399e987b5267) · [result](system-audit-ui-verification-evidence/objects/4284142e0bd043bd2ac0cddc5f40342868578a35caf4e08942b792649bfecaa0) · [原双清](system-audit-ui-verification-evidence/objects/8a2b3cd601462fec477f8070bba0509211642ab7080c1b7ca9d2b6d6bbddd5eb) |
| navigation04 / input08 | 0 / 65.192s | PASS | 7 / 81 / 4 | [raw](system-audit-ui-verification-evidence/objects/9464f927518e960852bacd7d499de6f4705a998793b8a68264c2f88272a6da59) · [command](system-audit-ui-verification-evidence/objects/24d25b35af05850beec973bc21d4b4543e276cfffe3da43474fad4ff9937f778) · [result](system-audit-ui-verification-evidence/objects/bf1d03a57a468a334040261673c73e0843611cd383e423104f82ba5d17eb15b0) · [原双清](system-audit-ui-verification-evidence/objects/67e3e069c24acbaa854928e1ca981460513bc8751d7ff5f97d2187e711ab8b8b) |
| old-outbound-navigation01 / input08 | 0 / 70.225s | PASS | 7 / 84 / 4 | [raw](system-audit-ui-verification-evidence/objects/0626ee9e2a182b7fca97f50d0a96b25d4a79178516bfd4fb8c48f1531e9c43eb) · [command](system-audit-ui-verification-evidence/objects/6e067616facbbc5158baf35bcdb3410cae0b475f2103bc4fc04a7b4d5d098cc6) · [result](system-audit-ui-verification-evidence/objects/151a2ef943861ea104637c9963c44fa92f37cca4795bf9e8816657c36cfe67e3) · [原双清](system-audit-ui-verification-evidence/objects/3709e5ff2c0f23ac9a555780b1bd911898a50cf8753376598d15d61ee36615d6) |
| old-delivery-navigation01 / input08 | 0 / 70.845s | PASS | 9 / 89 / 4 | [raw](system-audit-ui-verification-evidence/objects/ad7453b32338d2c4f8e5c83a90c8ee95da1942000c9dd3895412e54e20f2bda1) · [command](system-audit-ui-verification-evidence/objects/9df678b7a0d99caa576efbe3deff2e4c0e451aa740d088c78359216e47b695bf) · [result](system-audit-ui-verification-evidence/objects/8492032e75c7959ed035153913713eb00b34a4472c9b3a53c37b35a26264e77a) · [原双清](system-audit-ui-verification-evidence/objects/fc9d9526f63eb85b4b45a4a9b6fbca60852d76f49f1da8d2b0d87d1c6bd18344) |
| independent01 / input08/execution03 | 0 / 76.226s | PASS、PASS | 7 / 98 / 8 | [raw](system-audit-ui-verification-evidence/objects/cca5ac7bd730b6dc9dc1152799a5b709619e5845e04c0f5a37fa31fb834adf39) · [command](system-audit-ui-verification-evidence/objects/26c6182dd38a5ddeeb94dfb99c17b32e7fdba2def8236dd628c8ebf55986c01f) · [result](system-audit-ui-verification-evidence/objects/4d69a931a923227eff8d681985980ef640fa223f78c870a1dff160c949ed9b06) · [原双清](system-audit-ui-verification-evidence/objects/06407698c317089c46359bd93fba3217b094683429fd1cd4c7c75216eafbc003) |

read01同时有动态构建缺5目标和DTO/DB comparison失败；read02安全raw只给两个summary差异，未给action/值，后续固定fixture分析才定位三rotation映射。不能倒填当轮没有采样的字段。authority01的请求体/Content-Type和authority02的text/uuid前提由静态分析与后续通过闭合，原raw未记录精确HTTP码/SQLSTATE，不写成已现场看到415/42883。navigation01只有aggregate false且零图；不能回填nav02/03的具体几何数据。

两Authority原轮资源已退出但browser目录残留，原cleanup=false永久保留。[recovery01结果](system-audit-ui-verification-evidence/objects/e6e0aa5ddb2092d5f8c95ca2c0b393b44572f1ef0a90d03b0003ad43fe87ca74)先因要求全机/proc可读而失败，未删除；[recovery02结果](system-audit-ui-verification-evidence/objects/cb1021cd0338c5fa8819996646353f8c7881facb0e97885deaf59a3767650880)与[recovery03结果](system-audit-ui-verification-evidence/objects/fd5e7aec058ece2480cf0eacb71dac68aa7a3b14f27ea9a1220ad42d62c7c21e)才分别清理精确所属残留并两次确认，未读业务内容。恢复成功不改原轮driver exit1，也不称原轮all-clean。资源恢复只保存安全名称/大小及原清理证明，不收浏览器profile或运行凭据。

独立A实际证明应用过滤与草稿分离、cursor翻页、正式DB完整安全投影和详情零额外读取；B证明响应取消的真实尾部、焦点Tab、空闲owner上的pageshow/Session503→同完整身份恢复、新实例默认一次读，以及正式Logout撤销后的门禁。A/B各Go 10.53s/9.17s、browser 5.5s/4.6s，同轮actual0/76.226s。取消hold在上游成功读取/Close之后，不代表DB事务被hold；pageshow显式触发真实listener，不声称物理BFCache往返。

当前双清只涵盖当轮自有精确ID/PID。原记录中的历史PPID1 Z及先前Outbound编译孤儿没有由这些轮次回收；不能以未来green补填旧actualwait。未选Go包的`no tests to run`不是业务PASS。

## 4. 截图与主树兼容

共保存26张900px局部图：nav03两张、nav04八张、两旧Navigation各八张。作者实际查看14张：前十张全部，加两旧组各dark1440/light390两张；旧组其余12张仅保存。主线程实际查看4张：nav03 light1024及nav04 light390/light768/dark768。原visual-review与PNG按SHA保存，本文作者未重复看图。light768布局密集，图像不覆盖整页下方表单/错误，也不证明原生zoom、BFCache或生产托管。

[主树兼容报告](system-audit-ui-verification-evidence/objects/764b8657ace604218f3881a2b5db23ef9a7f33a43db2028969abf9f3d26c28d6)绑定实际`86a882 + 35候选`，Runtime HTTP `9b074f8`的13技术源及149项前端固定依赖均匹配。仅运行graph、Account integration race compile、恰三个测试名list，actual0分别0.114s、6.462s、1.021s；805本地核验路径、3501外部/来源指纹、393packages/31modules，6所属PID双清、0adopt。48,393,108B编译二进制记录SHA后删除，不归档。未运行测试体、Central/Runner构建、监听器或fixture；不得称为`a0e73bd8`主树动态重验。

README首文档checker错误统计整份历史README中重复selector，actual1保留；仅将checker限到新增Audit段，README和35源/40dist未变，第二次actual0。[文档交付说明](system-audit-ui-verification-evidence/objects/daaf9edae37914d9f1319bb84e981f8fc35699261f951c0f8819b442eff94268)与两版checker/raw/实际退出记录均归档，这不是产品RED。

## 5. 原件缺口与离线复核

保留两个历史格式前态缺件：browser spec SHA `1a37607bc06e57148636e02a0556085e7af71678967049ab3475e448b15a8f1c`及`db1a27978b18792345b10adef7f753101097eb00a6b816db9a9fd389b1ed30c3`只在原format input-before记录；后续实际browser使用版本完整。另`prepare_runtime_closure02.py`恢复时未冻结旧版被覆盖，不能由后来同名脚本重建原字节。原input/cmd/raw/结果和resume-note保留；首次closure路径错误仅原tool/叙述，没有独立原raw/result。没有补造历史文件，也不宣称全部历史版本都可重建。

档案共1347个逻辑原件引用、815个新内容对象（52,232,034字节）及249项固定Git复用；不复制完整依赖、dist、缓存或二进制。37逻辑源码路径含36交付路径及Go oracle，66个版本按原输入关联。归档保留大指纹JSON原字节，Git自身压缩不改变原件SHA。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-audit-ui-verification-evidence/verify_archive.py --repo /workspace/agenteam
```

文档负责人实际只执行上述校验器的私有payload模式（`--documents`指向交付根），检查原对象/Git36、历史版本、13真实轮与恢复分界、主树3离线轮、卡技术和旧段原字节、限定新增链接/格式；原结果随私有交付清单提供。校验器不执行保存的probe、测试、driver或网络，离线完整性不产生新的业务通过。

本卡不改变[Runtime HTTP已接受结果](system-runtime-information-http-verification.md)；Runtime UI仅在私有规格整理。完整D08–D28/E01未完成、E01未开始、Summary初值待决、未绑定能力与ready503保持。Object原join、tools独立任务和SPA concurrent-publication三项安全停止继续，不重试、改写或改派；SPA受控/native已通过的有限结果不代表SPA产品接受。本次没有消费后继活动源。
