# D11 rev1 后端作者规格消费

**rev1已消费；未发现需要扩大17技术写域或改变公共接口的新增阻塞。STOP，待修正文案后的完整SPEC组合接受和root实施移交。** 这不是独立SPEC审查或技术实现通过。固定6d4f3335/59,476B及freeze bc88d06b；读取正式卡时为停止rev1，root通知rev2正在修后不再读活跃卡，本记录绑定不变的card.rev1.md副本。复用[先前只读准备](../handover.md)和[其20源指纹](../source-refs.json)，本轮仅7个必要签名增量，不重扫38输入/全Go图。

- 新公共边界可消费：六个Create/Update/Reorder＋Lookup归Commands；四读和ReadPlacementInTx归Reader。现pc.ProjectAuthority.RequireOwnerInTx包含实际caller Tx与Read/Mutate，Store五口可承载；NewAuthority/NewReader/New依赖分离使只读不要求Outbox/Activity。已规定typed nil、同Store/liveTx与未绑定错误，无需偷看pc.ProjectAuthority内部或新增跨域SQL口。17源内可放私有helper；不新开执行器、HTTP、root或第19产品路径。
- rank/持久约束已从前沿建议定型，可直接按正式规格实施：32位lower-hex开区间整数，组4096上限、固定before_id、确定midpoint/必要同序rebalance、no-op不维护、单组generation恰一次。五表与同Project父子/可延期unique都属新00021。一次target至多一个event，兄弟重编号不增锁/业务version/time；先前NormalizeLocks原始512限制已由常数锁union闭合。无MilestoneAggregate/公共锁扩口，现migrations/embed.go的*.sql会自然包含00021，不需改embed源。
- 六命令的完整语义、当前Read→identity/receipt→Mutate/expected/completed/anchor顺序明确；稳定Human user进入摘要，RequestID/Session不进入业务identity，fresh Session仍须当前授权。准备持久planned但不变canonical/Event/Activity；外部PrepareAppend后最终union锁事务重验plan_revision和真实facts，再将canonical/generation/Outbox/receipt/Activity原子提交。至多3轮重规划不改expected/target/before；旧计划不能复用。此两阶段可用现Appender/producer接口完成，不把planned当成功。
- producer与Project gate两层责任已闭合：Work Discover绑定持久命令与exact事件摘要；CurrentAccess允许严格验证completed历史，不要求当前目标仍是旧快照；NewFact仅exact planned＋active Mutate＋caller Tx中的真实postimage/generation/邻居。Project精确两Work schema分派只做自身gate，其他producer/action继续拒绝。Work/Project typed definitions须在Outbox.New Seal前注册；completed旧event若消失，NewFact拒绝补造。没有把Model始终Mutate的私有validator策略直接移植为Work重放规则。
- Unknown与生命周期可消费：原CommandIdentity串行确认、至多一次独立3秒只读观察，planned/失败/授权变化不推成功或回滚；明确Execute才继续准备。Stop取消/拒绝admission，Drain实际等待本实例calls、confirmation、Rows/Tx，不关共享Store或冒Reader/其它实例退出。无后台worker、死亡抢占或生产Object依赖；真实PG COMMIT代理后续只在获授tests/work fixture域实现并实测。
- 新读接口核合现签名：f.PageRequest默认50、范围1–200与显式0/null拒绝已有；cursor.Binding/Position支持Project scope/digest/order、Text(rank)+UUID、非空generation，Work Reader仍须检查精确两scalar/type、generation正值与stale规则。f.CommandMeta已提供RequestID/key/ExpectedVersion，create vs其余presence由Work自身加严。Activity现要求User EX与当前Session且沿caller Tx，首次no-op成功同样Touch、回放/Lookup不Touch可落地。cursor.CanonicalJSON只负责typed projection canonical-v1，不承诺原始Work JSON孤立surrogate/大小写/闭集；必须在Work授权codec中先严格拒绝，不能因此改共享foundation/cursor。

已知D1单独保留：rev1 §8.4原写“owned TCP暴露总窗≤75s；fixture Memory上限≤5GiB”与继承基准不符。root已确认应为**fresh磁盘≥5GiB；cleanup后TCP尾观察≤75s**，architecture仅修这一行政预算措辞。这里不回写旧rev1、不把预期修正当已接受rev2，不在业务实现中采用原错误预算。完整独审/root接受的正式修订和实际窗口仍待交接。

六新PG top、既有Project7 pure/Project3＋Outbox2旧PG以及两个不同构造独验均仍待真实准备/运行；没有编译、发现或testbody已过的声明。B02正常Human持久输入＋真实Project Create/隔离initializer边界沿用先前准备；不接Object stopped、不冒D10/Login/Task/删除/lifecycle/Work cleanup/生产绑定。#12–14安装、迁移/Go输入与Model UI窗口必须由root错峰；当前无资源owner也不等于安装许可。README仍第18末件另授。

来源与实际阅读差量见[refs.json](refs.json)。一次只读rg误猜migrations.go的exit2保留，改读规格已经明确的embed.go；未伪造旧成功。只写本两小件，原8dd318/ea50不变；无产品/正式doc/Go/Node/Git/网络/资源操作。STOP。
