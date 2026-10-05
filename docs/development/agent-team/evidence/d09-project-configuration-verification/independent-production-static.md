# D09 Project 配置 13 生产源独立静审

2026-10-05，recovery_verification。**发现一处需修正的 typed-nil 构造边界缺口；其余已核高风险路径未发现确定缺陷。** 主线程已采纳 F1 并限定作者修复，新差量须另行冻结复审。此结论仅针对以下固定 13 生产源；未参与实现，未读活动 4 个真实测试或其他候选。没有运行 Go、SQL、Docker 或网络 fixture，没有编译/行为通过声明，没有修改仓库/Git，未再委派。

固定 production-rev1/manifest.json SHA-256 `8ca0cd1a2f90a43a925ded7617fd740b6cbac4a29b7e06186e2d5dce0105e4ce`，13 项初检和末检逐字节匹配。基线 `81fe7427ceb4672247b3d30a51c10a2e2808ba04`；规格取 `6e0bda12835019ee8dcf20d77dd17b7a0e05bdcd` rev1，带采纳记录的卡 SHA 为 `478c8b5945be5add5a6e74c74fff510f792d35afc129f7532185919bbe0b3f55`。既有规格静审报告 SHA `3eba6ec43ba94693199c68a90223401af0af4b57aae3a1a787ee887c91ece8d2`，没有把它当实现证据。

`candidate/` 是仅此 13 源的精确副本，`base/` 为对应基线，`support/` 为只读相关上游；`production.diff` 保存实际差量，`inputs.json` 绑定所有本次固定输入。下面行号均指 candidate，明确标“基线”的除外。

## F1：typed nil channel 的 ProjectAuthority 被构造接受

`model/authority.go:33` 以 `d.Projects != nil && nilPort(d.Projects)` 拒绝显式 typed nil。基线 `model/service.go:47–56` 的 nilPort 仅检查 Pointer/Interface/Func/Map/Slice，没有 Chan。Go 允许具名 channel 类型定义方法并实现 ProjectAuthority；例如 `type projectPort chan struct{}` 配上正式 AuthorizeProject 签名，`var p projectPort` 的接口值非 nil，但动态值为 nil channel。其它原必需依赖合法时，该构造通过 line33 并到 line36 返回非 nil Authority、nil error，违反卡明确的 typed nil 拒绝要求。

这是固定源码分支可确定的构造规格问题，**未运行 Go 复现，不声称授权绕过或其它运行时安全影响**。nil pointer 等已覆盖；没有理由扩大到现有 System 依赖行为。最小修复限定新 `model/authority.go` 的 Project 检查补齐 nil channel，并在本卡新纯测试加入该正式方法集的反例及零调用断言；不修改只读 service.go。主线程已采纳此边界并交作者，待固定 delta 后仅复审受影响部分。

## 静态结论与证据

| 核对项 | 实际证据及判断 |
| --- | --- |
| 可选 Project 权限与零 I/O | `model/authority.go:29–89` 保留 Sessions/System 必需依赖，Projects=nil 可构造；typed nil 的 channel 缺口见 F1。普通未绑定 Project 路径先判 unbound。已有真实 Tx 时先同 Store InTx/RequireHeldLocks，再 Session 与 Project delegate，精确 grant.Matches(actor,scope,intent)，不补锁/重开 Tx。六 CRUD 的早期 unbound 分支在 `commands.go:443–500`，五查询与 Lookup 经 `query.go:42` 的 readScope 在 WithinTx 前判 unbound。零值 Service 沿 state()==nil 安全拒绝。基线 `model/service.go:31` 还保证 Authority 有效及同 Store。 |
| System 旧字节和锁序 | `commands.go:84–126` 的 System namespace/owner 数组、原 `model-command-v1` JSON 前段与基线源码完全相同，只有 Project 外层加独立 marker/Project/User；未将 Session 写入 semantic。新 record/plan 的 Project 字段均 omitempty。`131–173` 在真实 scope 过滤、原 identity/user/kind 匹配后才解码；缺 Project 字段只能对应 System 空 scope，Project 不会降级为 System。`configuration.go:29` 原命令/User/全局引用/Outbox/record 锁保留，新加 Project SH，Secret/Event 全计划加入后 `events.go:201` 统一 Normalize；基线 foundation 按 rank 排序，不按列表追加顺序取锁。未动态断言旧 JSON/receipt fixture 已兼容。 |
| Project Read→receipt→Mutate | `commands.go:186–213` 先当前 Read 再查原 receipt，只有确无原行才先 Mutate/prepare；原 Tx `224–247` 再当前 Read→原 semantic/receipt→首次 Mutate，覆盖归档历史重放。`recheckPreparationScope:304–349` 在原 Command EX+User/Project SH 下复查并发 receipt；`replayScope:358–403` 同样当前 Read、原 expected semantic，不把公共 Lookup 当私有 Unknown 确认；确认 absent/fail 不返 receipt，保留原 Unknown，异义 B 返回冲突。Lookup `query.go:170–201` 仅当前授权后读取 scope-filtered 原命令，Project selection 始终 unbound。 |
| 六 CRUD 与 scope/映射 | `configuration.go:23–145` 将 scope 写入 plan/record，按真实 Provider scope 加载 Model；Project Model 明确 chat，Provider/Model 不可变字段及原 policy 未放松，CredentialRef exact scope 在 `configuration_policy.go:28–55` 检查。`configuration.go:246–320` 锁后重读整个原 Provider/Model/reference 集合，变化整 Tx ResourceBusy。SQL 更新虽按已选唯一 ID 执行，前置是 scope-filtered load、对应 aggregate 锁与 exact record 重核；没有从裸 ID 先向调用者返回他域配置。 |
| 七字段目录与分页 | `project_query.go:23–34` DTO 恰七字段，无完整 Provider/Model 嵌入；`223` 的单 JOIN 同时取 scope、两层 enabled、chat 和安全字段，SQL 本身只允许 System/当前 Project。created_at 仅作内部 keyset；禁出的 endpoint/options/ref/provider_model_id/parameters/overwrite 没进入 projection。Capabilities 在 `269` 深克隆，基线 Clone 复制四 slice/两指针。`40–58` cursor 绑定 Project、稳定 user、query kind、固定筛选语义，不绑定 limit；`query.go:215` 执行原 1–100/水位校验。每页都经 readScope 的同 Tx Owner Read 与全局 references SH；不把 SH 当配置冻结。 |
| References 未绑定与删除 | `references.go:62–70` 对 Project 任意真实引用一律 DependencyUnbound，在准备阶段、配置/Audit/Event/receipt 写入之前发生；无 replacement 也不忽略。空集合后的 replacement 只查本 Project，未找到再查 System；类型/两层 enabled 校验，其他 Project 不可见。`configuration.go:293` 全局 EX 后重扫 exact refs/版本；Provider 非空也在锁下重查。System 只改原 platform_selector，agent/project_summary 仍无 canonical adapter，没有伪造改写或扩大交付。 |
| Secret / typed Audit / witness | `secret_authority.go:33,56–118` 将真实 CredentialRef/Purpose=model 与 private prepared plan 的 target scope/原 provider/retain-release 绑定；Usage issuer+request binding+plan digest、完整 held locks、prepared canonical fact 均保留。基线 Secret Usage 只改引用，不删材料/租约，并重读同 scope metadata/purpose；原 SecretUsageRouter 仍仅分派这两个 Model action。`audit_authority.go:98–117` 用完整 Actor.Details 的 contextPlan、原 key/cause/ordinal0/metadata/scope/resource，以及同 Tx prepared command/实际后态核对。Audit 基线先 ModelAction 路由 Models checker，不走 Project 普通 Owner allow。Secret 自身 mutation/resolve 的同 Store/Tx 私有 witness、完整 Actor/Entry/Key 比较保持未改；Model reference 不伪造 Secret mutation witness。 |
| Outbox 两阶段、full summary 与隔离 | 新 `project/external_event_authority.go:18–77` 闭合 Human+AppendProject+model/configuration_changed/v1/configuration/version/no sequence/project；purpose+完整 Actor.Details+完整 Summary（含 digest）+Project+固定两阶段集合哈希，private issuer 与 opaque 再域分离，deps 锁不可变。Discover 仅 CurrentAccess，Validate 同 Store/Tx/精确锁后分别 RequireOwner(Read/Mutate)，没有补 Acquire。基线 ProjectRequest 构造仅允许 CurrentAccess/NewFact，外 stage 不能构造合法请求。User→Project 顺序与 Foundation rank 一致，和 Normalize 后 deps 比较不会误拒正常请求。`project/events.go` 只增加两处分派，不动原 Project/lifecycle/Delivery 分支。Model Producer `model/events.go:144–182` 仍独立匹配原 typed event/header/payload digest 与 private prepared 后态，Project helper 不替代 producer facts。 |

要区分两条归档语义：Project gate 的 CurrentAccess 可以 Read，但 Model Producer 仍只接受当前 prepared Mutate 事实；卡未承诺历史 Event 任意 reappend。公开历史配置重放在 receipt 分支返回，不再次经过 Audit/Event。不能把此差异误报成 Project NewFact 全拒绝，也不能用归档 receipt 成功代替 gate 两阶段实测。

## 仍须动态过的门槛

F1 修复之外，以下四类不能从源码检查推导通过，最终 21 源冻结后必须绑定实际输入/命令/原日志：

1. 命中配置、Secret 引用、Model Audit、Outbox 与 receipt 的**真实最后 COMMIT**，不是外层 Project 授权只读 Tx；丢 ACK/回滚/原 writer 未终局、A/B 异义竞态、失权不泄露和原 Unknown 都需实际原 writer 锁证据。
2. archived/archiving 的六命令原 receipt 重放、prepare 失败后并发 receipt 复查为零新 Audit/Event；deleting/未初始化/换 Owner/撤 Session 在各精确入口拒绝。System legacy 原字节 fixture 与旧完整行为必须实跑。
3. 合法 typed 输入下修改 Actor.Session/scope/key/ordinal/summary/metadata，错 Store/Tx/issuer/缺锁及 ignored-error poison，观察整 Tx 零配置/引用/receipt/Audit/Event；Secret witness 的已有真实拒绝证据仅在未变接缝可复用，不用 fake allow。
4. 普通 Owner 目录 canary/跨 Project/禁用/nonchat、分页每页撤权与 cursor 跨 user/project/query；真实 references 行缺 adapter 的有无 replacement 两支、EX 后集合变化、Secret 删除与 release 竞争，不能只看错误码。

既有 ProjectSecret 报告中的 Outbox 原红仍按原证据保留，本轮没有运行/归因，也没有声称新 Model 分派修复了它。Summary settings/初值、D10 canonical reference adapters、Resolver/Usage/Provider runtime、HTTP/root/app 和完整 D09/Project lifecycle 均不在此静审结论内。

## 实际操作与停止状态

实际只执行固定 `git show` / `git grep` / `git ls-tree`，自有副本上的 `rg`/`sed`/`nl`，Python SHA/文本结构/差量生成；`static-proof.json` 记录 System 原 JSON 前段/namespace 源码相同及目录七字段/安全 SQL。没有执行这些 Go 代码，不称纯测试通过。

两次只读工具定位失误也保留于 checks.json：在自有 tmp 末尾调用 git grep，因无 Git 目录失败，随后在仓库用指定 commit 重查；猜测的 contract/requests.go 不在基线，之后用 git grep 定位到 configuration.go。均未产生代码/行为检查结果，不是产品失败。

自有固定输入末检保持 13 个 SHA 相同，报告格式/引用自查后冻结，停止写入与命令；无需资源清理，因为本轮没有启动服务/fixture。后续生产 delta 必须重新固定受影响输入，不能把本次静审套到活动源码。
