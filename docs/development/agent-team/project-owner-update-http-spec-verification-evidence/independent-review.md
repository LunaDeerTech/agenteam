# D08 Owner update HTTP rev1 独立 STATIC

结论：**限定完整 STATIC PASS，无 blocking / 无必须修卡项**。只接受规格可实施性与验收边界，不是产品或动态验收；业务、Go 和真实资源仍需 root 后续授权。

审查卡：`docs/development/work-items/d08-project-owner-update-http.md` rev1，SHA256 `f504baf6c91b291c38bdcc0b6590aaa68cc86554466c6ae15694b95de6b77669`；作者冻结 `frozen01.json` SHA256 `3151ebc3dfdf682437cea978741a1803fb08f7eb07dd151bd542881243884bf1`。产品依赖固定 `901eb54605d293d4308caadd278c2c3a7ae1b824`。使用 agenteam-design / agenteam-verification 技能，独立于规格作者。

| 检查 | 结论与实际依据 |
| --- | --- |
| §1–3 授权、历史与窄 lookup | PASS。固定 commands.go 在 User/Project/Command 锁及当前 Session/Owner/Read 后读 receipt；完成 replay 先于新 Mutate/version。HTTP固定 UpdateCommand，并再次收窄结果 union；同 key 异义冲突/跨有效Session同义重放/历史结果与当前GET分离、删除后不新增历史入口均与库一致。 |
| §2–3 严格输入与安全响应 | PASS。DecodeJSON 不能独自区分可选指针 null，卡已明确 presence-aware；原 key/UUIDv7/正int64/字段presence、不trim与64KiB请求/响应上限可实现。MaxInt64允许合法no-op，变化由库判溢出。十一字段DTO和lookup三态、额外active/target/Owner及expected或expected+1检查闭合。原Problem不暴露cause_id；公共错误分类保持。 |
| §3/7 事务、原子事实与验收 | PASS。真实变化最终事务闭合canonical/Audit/Event/receipt/Activity，no-op只有receipt+Activity，replay不重复Touch。两次名字检查、计划后竞争、真实Append后失败回滚、必须到达真实双validator、正式账户身份和实际HTTP原body schema有明确责任。 |
| §4 Unknown与I/O | PASS。planning Unknown确认后所有分支直接return；只有planning已Committed才可达final，所以一次Update最多一个WithoutCancel+3s观察。卡保留两阶段各自Unknown/NotCommitted分类与原attempt/cause，不作额外lookup或自动重试。30s PATCH/2s lookup只为发布/I/O期限，同步拥有服务与原确认尾，过期零迟到200/Problem，实际Close/Flush/callback join均明确；不宣称不合作端口会被context强杀。 |
| §5.1 真实根依赖 | PASS。前移createProjectUsage是纯构造，唯一ProjectAuthority可传给真实Audit.Projects、Outbox.Projects及Project producer，并共享给Reader/Usage/Service。RegisterProjectEvents在Seal前，三typed schema不开放三类命令/consumer。Account Authority本身实现TouchActivityInTx，可在core之前作Activity provider；这纠正准备阶段笔记中误把Activity构造等待到core之后的表述。真实process/guard、nil Initializer/Lifecycle/Model Projects/Resolution/Invocations与原初始化samectx均保持。 |
| §5.2 accountWork/关闭与精确白名单 | PASS。现accountWork四方法、accountAssembly.install/constructing/works/Joined足以纳入projects，无须改app/resources.go/app.go。works prepend Project后保留原Mail→core和独立partial sink条件；install先锁内登记再按stopped/forced实际Stop/原ctx Force。Drain首错返回保provider；Force原errors.Join循环保证首错仍遍历剩余工作。私有adapter Stop后真实Drain nil才能Joined，过期不能伪join，原resources联合门禁自动消费该结果并保DB最后ForceClose。 |
| §6–7 范围、执行与回归 | PASS。19唯一候选路径=18技术+单授README，11新路径不存在，8既有路径指纹匹配。9个旧PG精确top在固定Git实际存在，4新PG/3native及独立A/B责任清楚。45s离线、native测试/干预/退休区分、120s含Cleanup/包6m、完整7资源及daemon未wait边界均明确；禁止全app纯跑与schema模块+20数据遗漏亦已纳入。 |

机械静态检查：44来源全部匹配作者冻结；42与固定产品Git一致，另外两份是Owner-read产品提交后的已接受归档（原卡只加接受页首、新验收报告），没有产品依赖漂移。9个文档链接/fragment、UTF-8/LF/末换行/尾空白及新增文件diff whitespace检查通过。卡与冻结副本前后字节一致。精确只读命令和逐项结果在 `checks.json`，未执行 Go/Node/业务测试/schema动态/native/PG/浏览器。

保留自身检查首记录：新增文件 `git diff --no-index --check` 实际exit1且stdout/stderr空，首版私有脚本误要求exit0；已保留 `check-original.py` / `check-first-result.json`，仅修改scratch状态识别后整个文档/来源检查actual0。它不是卡缺陷或业务首红。既有Owner-read/Summary接受直接复用，没有重跑。

剩余门槛：本卡尚未实施，性能/实际最大表示、HTTP自然预算及尾部、真实Audit/Outbox、PG ACK丢失、根关停均只能由后续冻结实现和授权执行窗口证明。当前报告及检查原件已停写，等待root采纳。
