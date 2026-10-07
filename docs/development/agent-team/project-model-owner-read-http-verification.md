# D09 Project Model Owner 只读 HTTP 完整验证

2026-10-07：[工作卡](../work-items/d09-project-model-owner-read-http.md)完整14路径（13技术＋README末件）通过限定独立验收，root 已采纳；产品 `a0b012ce7acc6dd885407fe70bd7ee5b4448071b` 已推送且 root 核远端一致。[完整独审](project-model-owner-read-http-verification-evidence/originals/independent/final14/review.md)和[结果](project-model-owner-read-http-verification-evidence/originals/independent/final14/result.json)接续原13技术结论，历史 pending README 状态不改写。本归档不表示完整 D09/E01 完成。

## 1. 已接受行为与版本组合

五个 GET/HEAD 资源提供完整 Project Provider/Model 配置及七字段安全 chat 目录；当前 Session/Owner、逐页授权/状态过滤和既有 cursor 绑定保持，无管理员豁免或新增 TTL。完整表示先做8 MiB准入，再进入新增DTO校验/复制/编码；原 Read Unknown/Fault 优先，原2秒发布/I/O预算及实际 Write/Flush/Close/callback收尾保持。默认 app.Run 接入同一Authority，保留原Owner-read/Update/lookup/Usage/System/Summary初始化和关闭链，ready仍503。

[作者最终索引](project-model-owner-read-http-verification-evidence/originals/author/author-final06.json)绑定全部版本和原失败；[最终技术独审](project-model-owner-read-http-verification-evidence/originals/independent/final06/technical-review.md)核对有效13来源及独立证据。生产五源自production03不变，后继只是有界测试修订，以下不是最终二进制全量重跑。

| 验证 | 接受的实际组合 |
| --- | --- |
| 作者offline | 普通/race各Model 67top/270sub、App安全纯16top/139sub；schema6正33负；实际图、compile/vet/list、两cmd build与受影响增量。 |
| 独立controlled | 普通/race各1top/8sub；262144×32B合法efforts、HTTP前置拒绝增量TotalAlloc168B代表、原Unknown/HEAD零投影、Flush能力前置与escape边界；schema3正9负。 |
| 作者native | v02 keepalive、v02 slowbody、v03 writeclose三逻辑组3top/13sub PASS；加原v02 writeclose FAIL共4实际轮。 |
| 独立native | 1top/2sub PASS，自然2秒、受控Close尾真实join、HEAD及同连接跨deadline复用；2listeners，执行5.198s。作者＋独立共5实际轮，direct/adopted actual wait逐轮保留，TCP含TIME_WAIT与owned PID双空。 |
| 作者PG | projection/authority沿v03，bounded最终沿v05、root最终沿v06，旧10top/23sub沿v06；含bounded两次、root一次原FAIL共8实际轮。 |
| 独立PG | A 1top/3sub、B 1top PASS；实际事务权限屏障、新Session/分页重验、COMMIT ACKdrop原Unknown，以及默认root真实HTTP、旧路由、ready和实际drain。 |
| 原body/schema | 作者16份成功轮＋独立A/B5份原响应字节均保留实际status/type/length/X-Request-ID/target/run/input绑定并通过标准schema；失败轮partial body另保原失败标签。最后5份联验actual0/0.254s、actual wait/双空/输入同。 |

## 2. 原失败与修复边界

[原规格归档](project-model-owner-read-http-spec-verification.md)保留无TTL澄清。实现原问题由[作者索引](project-model-owner-read-http-verification-evidence/originals/author/author-final06.json)与[独立技术结果](project-model-owner-read-http-verification-evidence/originals/independent/final06/technical-result.json)逐项绑定：production01 R1/R2是setter panic收尾和writer状态链的STATIC缺口，cleanup03是失败路径释放/join的STATIC缺口；不冒称动态首红。typed-ID作者编译失败、独立私有IsZero编译失败及执行前URL/Reasoning修正均保原件。

native旧writeclose首红仅未观察到socket timeout，旧探针不能定位为编码阶段失败；v03增加真实Write/Flush进入/退出观测，仅调整该测试父期限，生产2秒不变。新GET真实partial Write及HEAD Flush均观察net timeout，不倒填旧失败缺失的阶段证据。

PG首次bounded在execute后ctx已到期，原日志不能分离库与HTTP准入成本；第二次三个超限GET/HEAD子例虽过，整轮仍失败，旧私有proxy的1MiB帧边界与合法大DataRow冲突，原日志没有具体超限帧观测。candidate05私有有界代理与同连接阶段断言后，实际D9437584B→send/tag COMMIT→Z(I)→唯一ACKdrop通过，later GET是另一连接。root首红是测试误用不存在的found字段，candidate06改为既定state/result并严格核原PATCH投影；后续断言只采纳成功复测。独立尚未执行的Root探针也有同一found错误，作为STATIC发现保留，修后重编/list并由B真实通过。

## 3. 实际清理与证据保存

[逐轮派生核对](project-model-owner-read-http-verification-evidence/derived/resource-rounds.json)引用全部原command/result/cleanup：作者8＋独立2共10轮PG，各7项资源记录、actual wait与双清，原exact_absent表另核70个ID互异。native5实际轮单列每轮direct/adopted等待，不把业务预算、清理观察时间和逻辑组数混算。40个不同PID/starttime的daemon/PID1 shim及同期2个git均非任务owned、未wait；作者shim当时208→240，独立再增8，使用原轮次live记录，不以环境重启后的基线替换，不宣称全机清零。

8 MiB仅限新增HTTP完整表示准入，不担保前序DB/库分配或RSS；168B仅为受控代表。受控writer/真实PG与native传输分别举证，私有Close屏障、proxy当前ForceClose先行调用链和原app/Object/forced-inner-join限制保留。无配置/凭据mutation、Resolver/production Resolution/Invocations/D24或原三停止任务的接受。

[来源映射](project-model-owner-read-http-verification-evidence/source-map.json)保存345份小原件（3,668,305 B），199个同字节来源别名；167份重复输入map元数据集中为5份明确标注的派生摘录，另1份资源核对，共6份派生文件。12项仅保完整指纹及复用依据；未复制产品源码树、初始整源diff、大型依赖图、cache、binary或私密runtime。原件内部引用保持原位置语义，不能据此声称所有被引用原件都已入仓；独立私有Go探针以`.go.txt`原字节保存。

归档核原件复制/来源SHA、JSON、Python语法、正文链接、新增格式、卡技术正文及四入口历史字节不变。原diff/TCP的尾空白、space-before-tab及空EOF逐行登记，原安全body缺末尾换行另登记，均不改原字节、不加忽略规则。本轮未运行Git diff检查、原脚本、Go、产品或资源。系统管理员统一会议initial/update含首轮标题、Project无override/复制默认、compaction/Execution Summary规则保持；ready503、D08–D28/E01未完、E01未开始及Object runtime join/OpenAI tools独立验证/SPA concurrent-publication三停止不变。
