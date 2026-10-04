# D08 Project 与 Owner 工作项

修订：rev2，已获独立静态审查通过并由 root 采纳，现正式归位。当前阶段：S01 规格完成；B01 正式契约/纯规则库已独立验收，提交并推送 `199554b`；B02–B04 保持各自真实依赖与所有权门槛。D08 模块仍未完成，Project Service、schema、HTTP 和真实 Skill 初始化尚未实施或验收。

共享规格：[D08 实施规格](d08-project-owner-design.md)。业务基线固定 `062ae2c050e2f6fa1549d2e67f924b4e5160d754`，49项输入指纹及冻结稿位于 `/tmp/agenteam-d08-design-rev2-nm2kwP/`；后续卡下发时须补足新采纳依赖的准确提交/manifest，不继承活动工作区为已验事实。独立报告 `/tmp/agenteam-d08-independent-spec-review-cofn9wvo/review-report.md` SHA-256为 `287c4c85491f7574af0e8358efae7d77dbb5214d8b0d9f767ceb5118491808a7`，121项证据索引为 `20efa17c2ae2c866ef55ba7cafa108f56cd1322a6f34e23825bf7359b51fdfa4`；仅证明定向静态规格通过，不代表业务已经运行。

rev2已按root工程裁决固定Create请求的稳定target ProjectID。所有重试保留target+key，命令scope为project/[targetID]；不同target是新意图，不能借换ID当Unknown重试。永久删除仍仅保原最小delete receipt，服务端据此永久拒绝旧target，不新增Create key墓碑或已删内容。

## 开工状态

- B01 已完成：12 个新 Project contract 文件独立验收并提交推送 `199554b`，只消费固定已验 foundation/identity/event；没有提前绑定生产服务。
- B02 稳定 ID 服务不需要 SMTP、头像或完整账号 HTTP；D07 B03/00011 已验并提交 `ffa65f0`，A1 CurrentUserRoute 已验并提交 `59b38c8`。独立新 Project 服务/repository 代码和测试准备可由 root 分阶段派发；A2 仍独占未验 00012、旧 D05 窄文件及 Docker，00013 正式 schema 与真实 PG 运行须等 00012 冻结验收、root 编号确认和资源交接，不缺号、占位或抢占。
- B03 的完整删除确认需要接入已验 A1 CurrentUserRouteInTx；D08 当前尚未绑定。对象范围停止口和 typed Audit cause验证仍由明确上游作者完成，不能以空实现代替。
- B04 HTTP/app 等 D07 B04 稳定输入和共享文件交接。
- D10 初始化真实绑定仍缺失；D08 正向 fixture 不意味着生产项目创建成功，D10/I28 继续承担真实集成。

## B01 正式契约与规则库

角色：backend_worker（`d08_design`）；独立验收：`parallel_plan`；状态：纯契约/规则库已完成，提交并推送 `199554b`。

目标：独立可构建的 Project canonical 类型、请求/结果、Owner gate 闭集、名称/路径规则、typed Event、生命周期和 Skill 初始化正式消费端口。服务/数据库/HTTP 不在本卡完成声明内。

已验收的精确交付白名单如下，全部相对 `internal/central/project/contract/`：

| 新源文件 | 新同包测试 |
| --- | --- |
| `types.go` | `types_test.go` |
| `commands.go` | `commands_test.go` |
| `lifecycle.go` | `lifecycle_test.go` |
| `initialization.go` | `initialization_test.go` |
| `events.go` | `events_test.go` |
| `validation.go` | `validation_test.go` |

初次归位前核对（历史记录，12 文件现已提交）：Git HEAD `d6d18e45920396896801a3864601e40dfc69211b` 及工作区均没有 `internal/central/project/`，上述12路径均不存在，无现有接口或`.gitkeep`获覆盖权。实际下发/落笔前再次核对；若路径已被其他任务创建，先报root确认所有权，不覆盖、不自行扩大文件范围。私有helper放在白名单文件内；新增其他文件须root补充明确授权。

现有只读依赖限 `internal/central/foundation/`、`internal/central/identity/contract/`、`internal/central/event/contract/` 以及 `go.mod/go.sum`；不得改 identity、Audit、account、Object、app、迁移、Go依赖或fixture列表，不提前创建schema/Service/HTTP或未来模块实现。未来注册的 ProjectInitialization 服务角色不在本卡修改现有 identity 闭集；相关运行授权正向等 B02 注册后验证。

编译基线：Go必须为 `1.27.1`；本卡只消费 `062ae2c050e2f6fa1549d2e67f924b4e5160d754` 的上述三个已验契约包与module文件。归位前核15个非测试编译输入，当前HEAD和工作区均与该固定基线逐字一致；manifest位于 `/tmp/agenteam-d08-placement-se1meE/b01-compile-inputs.json`，SHA-256 `b1b09e4dbbace98085b60787243948e9e1f26a658489e66a15f32716c379eaaa`。此为静态依赖核对，未运行Go编译或产品测试。B01可和D07 B03验证、B04准备并行，无Docker/迁移资源；不得把活动Account/SMTP或全库测试当本卡编译输入。

必读：[Go开发技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、共享规格 §2/4–8/10/12/13。

验收：T01及全部 enum/presence/错误variant/安全编码边界，包括Create必填合法target UUIDv7与按Project区分的command identity；固定 Go 单元、race、vet。Plan/Report不得成为可由任意JSON反序列化的权限令牌。冻结源码/依赖指纹交独立验证；没有对应实现的adapter保持 unbound。

正式端口窄澄清：`RetryLifecycle` 返回已有 `LifecycleResult`（`Operation|Receipt`）；正常/归档路径返回 Operation，completed delete 的原重放仅返回最小 Receipt，不恢复旧 operation 或已删内容。

升级条件：需要改已验公共契约、新增规范外状态、扩大名称规则、引入依赖或发现规格内在矛盾，停止受影响部分报 root/设计负责人。

## B01 独立验收与提交

root 已采纳最终 rev3 实现，12 新源与 Retry 返回值对应的两文档澄清精确提交并推送 `199554b`。独立报告 `/tmp/agenteam-d08-b01-independent-70h8rbu6/final-report.md` SHA `b25aa8c3daf6ce259badf9accc557ea6549da41032ba181f9ec2bdb01db92e2d`，110 项证据索引 SHA `7c759b5b39525ab7af1c9f47a510d6449baaf9543b313dcf80e6b33871e5e4ec`；最终作者 manifest `/tmp/agenteam-d08-b01-rev3-bk7wqi/manifest.json` SHA `474bb5899f48d1edf58ae19c2d63c91bf89ecb594d9a10df46b92d1c8e5d172f`。

R01 已由 root 裁决并闭环：RetryLifecycle 返回既有 Operation|Receipt 联合型，§5/§11 同步，不新增隐式转查协议或保留字段。R02 独立发现 completed-delete 完整 Operation 可逃出结果联合型；三断言原红 `/tmp/agenteam-d08-b01-independent-70h8rbu6/completed-delete-red.log` SHA `97643063b55218a61aa49fb7931c048b7e5054fe5edc7265bb22043db235fb68` 保留，原探针字节不变，修后同探针及 archive/未完成 delete/最小 receipt 正例 race 通过，日志 SHA `32ba2fcb6a61955aba39c526251266d1323c662bc03b0b666f96f976153242e8`。

复用指纹匹配的作者 Go1.27.1 unit/race 各29主+15子、vet/build；独立4主+22 DTO子及576状态组合通过，最终定向2主复验通过；12源/15固定依赖/2文档末次匹配，9链接/9表与格式通过。作者与验收者 all-stop，无 Docker/数据库/端口占用。本结论只覆盖纯类型/接口/规则与安全编码，不证明真实 Session/Owner、PG、COMMIT Unknown、join/death、物理删除或 D10 初始化已经运行；D08 整体仍未完成。

## B02 Project 存储与 Owner 服务

角色：backend_worker；状态：B01 与上述账号窄口/00011 已验；独立新文件准备待 root 派发，正式 schema/真实 PG 等 00012 冻结验收和共享资源交接。

目标：按稳定 ID 的 Owner读写、创建持久编排、名称唯一、版本/幂等/Unknown、typed Audit/Event 原子组合和恢复库。缺 D10 的生产 Create 按规格明确拒绝。

授权建议：共享规格 §13 B02 行，迁移编号由 root 单独分配；精确旧文件窄增量仅在 D07 B03 冻结验收解除且单作者交接后实施。不得读取 account表、活动 B03/B04源码、复制CSRF、实现未来Skill服务或改旧SQL。

依赖：已验 B01账号身份库、D03/D04/D06；已验连续迁移前缀；Project B01。Route窄口未绑定时路径/删除相关能力拒绝，本卡不以此声称全部D08完成。

验收：T02–T05/T07/T11相关真实 PG/Audit/Outbox。真实初始化端口用隔离fixture验证成功、失败、竞争、Unknown和错误计划，生产unbound分支独立验证；已完成当前授权receipt不因后来初始化缺口/容量而失效。Audit failure原子回滚、同scope同key异义、同target异key/跨Owner冲突、当前撤权先于receipt、原writer未终局保持Unknown必须独立审查。

## B03 生命周期与现有领域集成

角色：backend_worker；状态：等待 B02 及已验 CurrentUserRoutes 的 D08 接入，待root分配D05/Secret共享文件。

目标：真实归档/恢复/删除进度、对象项目停止、现有Secret/Object/Artifact/Outbox/Audit权限与清理完整组合。依赖未来领域的注册保持真实记录。

授权建议：共享规格 §9/13；范围外必要旧入口改动先列精确文件/方法/理由，主线程在冻结输入中统一授权。Object与D07 B04头像补口可能共文件，必须串行交接或同作者整合，不能并写。

验收：T06/T08–T14，含实际在途 Object I/O与Outbox callback、cancel与join区分、旧cause竞态、COMMIT unknown、死亡证明、MinIO清理和最小receipt。所有 required stop确认前不得进入archived/cleaning；Outbox清理位于生产者收束之后，Audit清理随后，最终不能制造Project残留事件/Audit。迁移/安全/协议风险由独立实例验收。

## B04 正式 HTTP 与 Central

角色：backend_worker；状态：等待 B03与已验 D07 B04装配。

目标：D08 OpenAPI/HTTP、真实Cookie/Origin/CSRF身份复用、已有消费口装配、共享运行时恢复/健康/关闭；不提前实现正式UI。

授权建议：共享规格 §11/13；app、resources、对象authority分派及fixture列表同一整合作者。所有格式与Schema变化同提交同步，未知能力不注册成功占位。

验收：T15及与真实账户/下载/Outbox权限集成，真实进程共享启动/健康/关闭预算、未join工作不提前释放ProcessGuard。ready显示实际未绑定能力。

## 未绑定与后续责任

| 能力 | 责任工作项 | 完成前行为 |
| --- | --- | --- |
| 本人当前username原子投影 | D07 B04-A1 已验 `59b38c8`；D08 待接入 | 未绑定时 Resolver/删除确认503，不读account表替代 |
| 受保护Add Skills真实初始化/确认 | D10 | Create503；隔离fixture成功不能作生产完成证据 |
| Work/Agent/Memory/Execution/Scheduler/Meeting/Governance/MCP/Runner/Usage/View清理 | D10–D25对应事实owner | 启用模块前注册正式participant；缺必要adapter保留pending/failed |
| D25真实订阅/断开与迟到事件不复活 | D25 | 不注册通用allow订阅；D08提供已验Owner/gate端口 |
| 全 Project永久删除与生命周期跨模块完整性 | D28 | 逐个真实adapter+完整manifest集成通过前不冒称完整平台清理 |

交付时同步实际命令/版本/输入hash、全部检查与未执行项、真实绑定、未提交文件和下一步。设计/独立静态审查只证明规格可实施，不证明业务运行。
