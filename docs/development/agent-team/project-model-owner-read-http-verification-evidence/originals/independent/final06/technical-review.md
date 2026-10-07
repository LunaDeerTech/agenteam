# D09 Project Model Owner read HTTP — 独立技术最终结论

**13 项技术范围完整限定 PASS；README #14 待另行冻结静审。** candidate06、effective-candidate06 的13份工作区源码与各固定 snapshot 全部一致，生产五源自 production03 未再修改；无剩余技术阻断。本人未参与产品实现，只写独立 scratch 探针及证据。

五个精确 GET/HEAD 资源覆盖完整 Project Provider/Model 配置和七字段安全 chat 目录。当前 Session/Owner、逐页权限与状态过滤、原 cursor 绑定、8 MiB 前置计数/JSON escape 保守上界、Read Unknown 优先及零成功投影、原2秒预算与实际 I/O 收尾均已按冻结契约验收。默认 app.Run 保留原 Update/lookup/Owner-read/Usage/System/Summary 初始化；ready仍503。未引入 cursor TTL 或业务 efforts 个数上限。

| 证据 | 最终采纳范围 |
|---|---|
| 独立 STATIC | 完整13范围与生产R1/R2、测试cleanup、各候选差量；当前最终源码逐份匹配 |
| 作者离线版本组合 | Model每种普通/race67 top/270 sub，App安全纯16 top/139 sub，schema6正33负，actual图/compile/vet/list及必要增量 |
| 独立 controlled | 单top/8sub普通与race，合法262144×32B efforts前置拒绝；该代表HTTP增量TotalAlloc168B；Unknown原cause/attempt、GET/HEAD零投影、缺Flush能力前置失败、escape/raw边界；schema3正9负 |
| 作者 native | 三逻辑组版本组合PASS；原writeclose首红保留，真实Write/Flush timeout、actualwait及含TIME_WAIT全TCP双空 |
| 独立 native |1top/2sub PASS：自然2秒+受控Close尾实际join、HEAD/同连接跨deadline复用；执行5.198s，direct/adopted均actual0，2listeners全TCP/PID双空 |
| 作者 PG | 四新top与旧10top/23sub最终组合PASS；含三原FAIL共8轮，每轮actualwait/七资源双清/输入同 |
| 独立 PG A |1top/3sub PASS，5.31s top/54.540s全链：formal Logout在已过外边界、实际Tx第一权限语句前的屏障；新Session；目录 continuation重验enabled与watermark/crossbinding；真实COMMIT ACKdrop原ReadUnknown、不自动第二读 |
| 独立 PG B |1top PASS，4.94s top/53.996s全链：默认app.Run真实HTTP配置/目录/HEAD、当前Owner、原Update完整历史、旧路由、ready及实际drain；80owned全部wait/清理 |
| 原始响应联验 | 作者16份PASS轮原字节+独立A/B5份；实际status/type/length/X-Request-ID/target/run/input绑定，标准本地refs schema均PASS。最后5份Python联验actual0/.254s、actualwait/双空/输入同 |

原始失败与修订全部保留：无TTL规格修正，生产deadline setter panic/业务writer状态两STATIC缺口，typed-ID编译错误，测试失败路径清理STATIC缺口；native旧120ms观察不足首红；bounded首轮execute后ctx过期、次轮旧代理1MiB拦截大DataRow；root错误found断言首红。修复后同连接实际D9437584B→send/tag COMMIT→Z(I)→唯一ACKdrop得到新证据，后续读在另一连接，不倒填原失败缺失的阶段观测。

独立未执行Root探针也含过时found断言，已如实记录为STATIC发现，不称动态失败。保留原探针/冻结件后，只修Root的受保护map/state/result.command/原Project与PATCH完整投影断言，受影响PG重编/list后B真实通过；A/native不变。原私有IsZero编译失败及执行前夹具修正同样保留。

8 MiB仅约束新HTTP完整表示的前置准入，不担保前序DB/库分配或RSS；168B仅为受控代表。作者受控writer/真实PG与native transport分别举证；私有Close屏障、proxy当前ForceClose先行调用链及原app/Object/forced-inner-join限制保留。无新mutation、credentials、Resolver/生产Resolution/Invocations/D24、完整D09/E01或三项停止任务的接受。

作者8轮与独立2轮共10个PG实际轮全部自有wait/七资源双清；新增40个daemon/PID1 shim Z及同期2个非owned git Z不属于任务owned等待链、未wait，不声称全机零。使用各轮当时live基线，不以重启后的总数替代。重启恢复确认native/A/B及最后schema均已完整终局，仅复核持久原件与当前13hash，没有重新运行资源或复用旧基线。

全部精确命令、raw、SHA、候选及失败链入口见同目录technical-result.json；作者总索引、独立controlled/native/A/B和schema原件按引用保留，没有复制全树。当前无活动命令/自有资源，无产品或Git写入。剩余仅README14窄STATIC与root最终整合。
