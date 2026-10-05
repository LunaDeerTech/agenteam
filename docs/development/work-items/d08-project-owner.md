# D08 Project 与 Owner 工作项

修订：rev2，已获独立静态审查通过并由 root 采纳，现正式归位。当前阶段：S01 规格完成；B01 正式契约/纯规则库已独立验收并提交推送 `199554b`；B02 存储与 Owner/创建/元数据服务、00013 和 fixture 接缝已独立验收并提交推送 `6319d03`。D08 模块仍未完成，B03 完整生命周期、B04 HTTP/app 与 D10 真实 Skill 初始化尚待实施和集成。

共享规格：[D08 实施规格](d08-project-owner-design.md)。业务基线固定 `062ae2c050e2f6fa1549d2e67f924b4e5160d754`，49项输入指纹及冻结稿位于 `/tmp/agenteam-d08-design-rev2-nm2kwP/`；后续卡下发时须补足新采纳依赖的准确提交/manifest，不继承活动工作区为已验事实。独立报告 `/tmp/agenteam-d08-independent-spec-review-cofn9wvo/review-report.md` SHA-256为 `287c4c85491f7574af0e8358efae7d77dbb5214d8b0d9f767ceb5118491808a7`，121项证据索引为 `20efa17c2ae2c866ef55ba7cafa108f56cd1322a6f34e23825bf7359b51fdfa4`；仅证明定向静态规格通过，不代表业务已经运行。

rev2已按root工程裁决固定Create请求的稳定target ProjectID。所有重试保留target+key，命令scope为project/[targetID]；不同target是新意图，不能借换ID当Unknown重试。永久删除仍仅保原最小delete receipt，服务端据此永久拒绝旧target，不新增Create key墓碑或已删内容。

## 开工状态

- B01 已完成：12 个新 Project contract 文件独立验收并提交推送 `199554b`，只消费固定已验 foundation/identity/event；没有提前绑定生产服务。
- B02 已完成：7 个纯契约/角色/Audit 闭集结果已提交 `769ec8c`；其余 21 源与 00013/fixture 接缝在固定 `da5caab`（含已验 A2/00012）上完成真实验收，提交推送 `6319d03`。稳定 ID 服务与已验 A1 CurrentUserRoute 的路径组合不依赖完整账号 HTTP。
- B03 的完整删除确认仍须接入已验 A1 CurrentUserRouteInTx；B02 路径已真实接入。root 已采纳主体与后段分阶段推进；§9.1/9.2 的 C0 精确 Go 口已独立定向审查并由 root 采纳，四个新契约/测试文件作者实现及 unit/race/vet 已完成，待独立验收；Project 主体按下面 P 卡独立推进。对象范围停止口和 typed Audit cause 验证须由明确上游作者完成，不能以空实现代替。
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

正式端口窄澄清：`RetryLifecycle` 返回已有 `LifecycleResult`（`Operation|Receipt`）；正常/归档路径返回 Operation，completed delete 在当前 Session/原 Owner/精确 Project与Operation 下仅只读确认最小 Receipt，不声称已证明 retry-key 同义，不恢复旧 operation 或已删内容；原 Delete 重放仍严格匹配原 key/digest。

升级条件：需要改已验公共契约、新增规范外状态、扩大名称规则、引入依赖或发现规格内在矛盾，停止受影响部分报 root/设计负责人。

## B01 独立验收与提交

root 已采纳最终 rev3 实现，12 新源与 Retry 返回值对应的两文档澄清精确提交并推送 `199554b`。独立报告 `/tmp/agenteam-d08-b01-independent-70h8rbu6/final-report.md` SHA `b25aa8c3daf6ce259badf9accc557ea6549da41032ba181f9ec2bdb01db92e2d`，110 项证据索引 SHA `7c759b5b39525ab7af1c9f47a510d6449baaf9543b313dcf80e6b33871e5e4ec`；最终作者 manifest `/tmp/agenteam-d08-b01-rev3-bk7wqi/manifest.json` SHA `474bb5899f48d1edf58ae19c2d63c91bf89ecb594d9a10df46b92d1c8e5d172f`。

R01 已由 root 裁决并闭环：RetryLifecycle 返回既有 Operation|Receipt 联合型，§5/§11 同步，不新增隐式转查协议或保留字段。R02 独立发现 completed-delete 完整 Operation 可逃出结果联合型；三断言原红 `/tmp/agenteam-d08-b01-independent-70h8rbu6/completed-delete-red.log` SHA `97643063b55218a61aa49fb7931c048b7e5054fe5edc7265bb22043db235fb68` 保留，原探针字节不变，修后同探针及 archive/未完成 delete/最小 receipt 正例 race 通过，日志 SHA `32ba2fcb6a61955aba39c526251266d1323c662bc03b0b666f96f976153242e8`。

复用指纹匹配的作者 Go1.27.1 unit/race 各29主+15子、vet/build；独立4主+22 DTO子及576状态组合通过，最终定向2主复验通过；12源/15固定依赖/2文档末次匹配，9链接/9表与格式通过。作者与验收者 all-stop，无 Docker/数据库/端口占用。本结论只覆盖纯类型/接口/规则与安全编码，不证明真实 Session/Owner、PG、COMMIT Unknown、join/death、物理删除或 D10 初始化已经运行；D08 整体仍未完成。

## B02 Project 存储与 Owner 服务

角色：backend_worker（`d08_design`）；独立验收：`parallel_plan`；状态：已验收，21 文件提交并推送 `6319d03`，此前 7 个纯结果已提交 `769ec8c`。

目标：按稳定 ID 的 Owner读写、创建持久编排、名称唯一、版本/幂等/Unknown、typed Audit/Event 原子组合和恢复库。缺 D10 的生产 Create 按规格明确拒绝。

授权建议：共享规格 §13 B02 行，迁移编号由 root 单独分配；精确旧文件窄增量仅在 D07 B03 冻结验收解除且单作者交接后实施。不得读取 account表、活动 B03/B04源码、复制CSRF、实现未来Skill服务或改旧SQL。

依赖：已验 B01 账号身份库、A1 CurrentUserRoute、D03/D04/D06、A2/00012 与 Project B01。实际组合绑定固定 `da5caab`，00013 已通过真实 fresh/populated 升级和失败回滚；本卡不以此声称全部 D08 完成。

验收：T02–T05/T07/T11相关真实 PG/Audit/Outbox。真实初始化端口用隔离fixture验证成功、失败、竞争、Unknown和错误计划，生产unbound分支独立验证；已完成当前授权receipt不因后来初始化缺口/容量而失效。Audit failure原子回滚、同scope同key异义、同target异key/跨Owner冲突、当前撤权先于receipt、原writer未终局保持Unknown必须独立审查。

### B02 独立验收与提交

root 已采纳并提交推送 `6319d03`。独立报告 `/tmp/agenteam-d08-b02-pg-review-ty9oi72j/final-report.md` SHA `9ab034850a899ec58a9099e09f5b98f8d1c412bfc9172ed736f32e81c4e4f228`，1123 项证据索引 SHA `4caa4df9fcb054f5cf6c6a9b363d51b6b1cad6efa50406242212818f8da69e74`；精确 21 源指纹清单 SHA `fa88b1d73dc1ecf6c435b4748bd6c31ab91f243d16c8be058e78136996cfae23`，不含临时独立探针。

首轮 Project `67.631s`、exit1 原日志 SHA `f9dea4ddc10c774cd04754eb1456508abbbd4f0c39672853449e315ab748f827` 保留：两处同名 23505 被 Store poison 投影为 INTERNAL_ERROR；另一处新 Session 撤销 fixture 缺 reason。原范围四文件窄修保留 D03、DB unique 与授权→receipt→版本/资格→名称顺序，并补两个已落库计划争新名的真实 barrier 场景。唯一修后原 fixture 组合在 Go1.27.1、race/count1/每包6m 下 exit0，Project `95.075s`；实际库存为 20 个业务主例、独立 1 主 6 子及普通入口跳过的 owned child helper，其他包均 no-tests，不计兼容通过。执行由作者完成，独立探针由验收者编写、冻结并在运行后核证，没有声称两次独立执行。

真实运行 manifest `/tmp/agenteam-d08-b02-pg-r2-v05xxgbs/execution-input-manifest.json` SHA `85792fe1e261dd304f430de338eaaf34140418ace4fd2875ca3445062df520d2`，最终日志 SHA `0df455b76d59ee58697b281132fbbc4d058ddd3bfa77dd6776047236a599dd6f`；21 源、516 固定输入、644 外部源和原字节 probe 执行后均匹配。三 nonce 与全部 owned 资源/18 个捕获 PID 清零，Docker 已交回 B。B02 的初始化正向使用隔离 provider，生产新 Create 缺 D10 时明确拒绝；tombstone 查询不代表 B03 物理删除已经实现。

## B03 生命周期与现有领域集成

角色：backend_worker；状态：B02 已验并提交 `6319d03`；C0 规格已独立定向审查并由 root 采纳，四源作者纯检查通过、待独立验收；P 主体已派 `d08_design`，D05/Secret 文件与资源由 root 独占交接。契约或阶段准备不作 B03 业务完成证据。

目标：真实归档/恢复/删除进度、对象项目停止、现有Secret/Object/Artifact/Outbox/Audit权限与清理完整组合。依赖未来领域的注册保持真实记录。

授权建议：共享规格 §9/13；范围外必要旧入口改动先列精确文件/方法/理由，主线程在冻结输入中统一授权。Object与D07 B04头像补口可能共文件，必须串行交接或同作者整合，不能并写。

验收：T06/T08–T14，含实际在途 Object I/O与Outbox callback、cancel与join区分、旧cause竞态、COMMIT unknown、死亡证明、MinIO清理和最小receipt。所有 required stop确认前不得进入archived/cleaning；Outbox清理位于生产者收束之后，Audit清理随后，最终不能制造Project残留事件/Audit。迁移/安全/协议风险由独立实例验收。

C0 候选修订与独立意见闭环：固定 `/tmp/agenteam-d08-b03-c0-r2-qu3_uo_7/`，正文 SHA `b07d902a1ff8995db4c7bf95a13fa64cc8fcaae820cdeeb370108930ed5baa9c`；两处精确 delta SHA `cf61173116fa138679c135026649e0b8c6af67ae71d0662acc85b45b8b5af852`。R01 补 P 的 commands.go/tests 生命周期 Lookup/Unknown 窄接缝，R02 明确 AccessDependencies 不是私有 issuer plan；没有新增公共类型或产品语义。完整精确口径现以共享规格 §9.1/9.2 为准。

completed-delete Retry 窄澄清已由 root 采纳：当前授权与输入语法仍校验，删除后仅以精确 Project/Operation 只读确认最小 Receipt；不同合法 retry key 不产生写入/Touch/Event/Audit，不保留旧 retry 历史。无法辨识的 retry Lookup 返回 RESOURCE_DELETED，原 Delete key/digest 重放规则保持。已核 B01 结果联合型与 D01/§8 最小保留无明文冲突；真实验收须覆盖 foreign/撤销或到期 Session/错误 Operation/不同合法 key/原 Delete 异义，并核零复活、零新增保留。

### B03-C0：可独立完成的 typed 新口

唯一作者 `parallel_plan`；独立审查 `d08_design`。新文件白名单：

1. `internal/central/object/contract/project_lifecycle.go`
2. `internal/central/object/contract/project_lifecycle_test.go`
3. `internal/central/audit/contract/project_fact_authority.go`
4. `internal/central/audit/contract/project_fact_authority_test.go`

第 4 个文件仅作真实外部消费接口的编译闭包检查，不写镜像运行测试。不得改旧 contract、identity、D03、迁移、go.mod/sum。以固定真实依赖编译，unit/race/vet，给精确 manifest；独立审核后可以单独提交这个完整契约结果，不能称停止能力已经可用。

### B03-P：Project 真实生命周期主体与适配

接收冻结 C0 后独立开工。唯一作者预授范围为正式卡已定的五个新生产文件及同名 `_test.go`：

- `internal/central/project/lifecycle.go`
- `internal/central/project/participants.go`
- `internal/central/project/object_authority.go`
- `internal/central/project/outbox_authority.go`
- `internal/central/project/secret_authority.go`

必须触及的既有 Project 接缝，仅同一作者：`service.go`（deps/生命周期服务接入及真实 Stop/Drain）、`authority.go`（当前 cause/phase 与新 stop authority/producer 注册）、`events.go`（现有 lifecycle event）、`audit_authority.go`（既定生命周期动作与 provider 分派）、`commands.go`（仅生命周期 Lookup 分派与 Unknown 串行确认；Create/Update 路径保持已验行为）。上述同名旧测试（含 `commands_test.go`）允许增加本次回归；B02 行为和生产 contract 保持已验。若新 helper 需要拆文件，作者先列精确新文件再由 root 授权，不自行覆盖旧文件。

当前可完整实现/审查的结果：Begin/Retry/Get/Lookup/Restore 与真实持久 claim/fence/checkpoint/recovery，冻结 manifest、停止/清理顺序和 Project 最终 receipt 原子退休；正式 Outbox/Secret/Audit/Object/Artifact authority 适配。使用已提交 00013 Project schema。不得实现目录之外的上游能力，也不能在生产注册空 participant。P 可以先给编译与纯规则/故障注入准备结果，真实生命周期成功必须等待 D 的实际 stop/inspect 和 PG/MinIO 组合；test provider 只作局部验证，不当生产绑定。

Project 稳定 ID/当前 Session/Owner/确认路径来自已验正式口；不碰 D09 创建模型、Project create 初始字段或 D10 AddSkills 产品决定。若 00013 缺支持既定 lifecycle 的必要字段，先交事实/最小迁移候选，不改旧 SQL、不私占下一编号。

### B03-D：D05 停止第一段与后段 Audit provider

root 已采纳独立定向审查结论并授权 `d02_backend` 实施第一段：精确范围见固定 `/tmp/agenteam-d08-b03-d05-scope-pvns68a1/proposal.md`，SHA `39a505d98307bedf832b3dea8e14b88f18e03742342a4847a01648824c0ebc00`。第一段拥有 Object/Artifact 已列旧接缝、新 project_lifecycle/project_work 实现与专项测试，以及唯一新迁移 `00014_object_artifact_project_stop.sql`；旧迁移 00001–13、旧 D03、Secret/Audit runtime、app/fixture driver 均不在本段写权。方案是授权依据，不是实现或验收通过证据。

00014 只增加两个域各自的 `project_stops` 与 `project_work` 四张技术表，用于前置 I/O 登记、不可倒退 stop epoch、实际 join/原 writer 终局与最小 stopped receipt。永久删除必须清掉所有旧 archive receipts 和扫描游标/临时 work，仅留 current delete 的最小 stopped receipt；不能留已删内容或借 receipt 新建 phase。Artifact `source_project_id` 必须进入源 Project 的 delete 扫描与真实 join 判定，不能只扫描 target project 或只凭本机 map/TTL/取消响应成功。

第一段与 P 并行：A 唯一写 Object/Artifact 旧接缝，P 只写 Project 自有范围；C0 四新文件与本两份文档本轮只由 `parallel_plan` 写。具体构造绑定不得默默 allow nil port。D1 的真实 PG/MinIO/ProcessGuard、13→14/失败回滚、跨 Project source/旧 plan/真实 stop 验收尚未完成，Docker 仍等 root/acceptance 明确交接。

§9.2 的 Object/Secret 同 Tx Audit checker 与私有见证属于后段，另列精确旧文件权限后实施，不阻塞当前停止 C0/P/D1。不能借第一段授权修改 Secret 或提前宣称 B03 全部 provider 已绑定。

## B04 正式 HTTP 与 Central

角色：backend_worker；状态：等待 B03与已验 D07 B04装配。

目标：D08 OpenAPI/HTTP、真实Cookie/Origin/CSRF身份复用、已有消费口装配、共享运行时恢复/健康/关闭；不提前实现正式UI。

授权建议：共享规格 §11/13；app、resources、对象authority分派及fixture列表同一整合作者。所有格式与Schema变化同提交同步，未知能力不注册成功占位。

验收：T15及与真实账户/下载/Outbox权限集成，真实进程共享启动/健康/关闭预算、未join工作不提前释放ProcessGuard。ready显示实际未绑定能力。

## 未绑定与后续责任

| 能力 | 责任工作项 | 完成前行为 |
| --- | --- | --- |
| 本人当前username原子投影 | D07 B04-A1 已验 `59b38c8`；B02 路径已接入，B03 删除确认待集成 | 未绑定的删除确认拒绝，不读account表替代 |
| 受保护Add Skills真实初始化/确认 | D10 | Create503；隔离fixture成功不能作生产完成证据 |
| Work/Agent/Memory/Execution/Scheduler/Meeting/Governance/MCP/Runner/Usage/View清理 | D10–D25对应事实owner | 启用模块前注册正式participant；缺必要adapter保留pending/failed |
| D25真实订阅/断开与迟到事件不复活 | D25 | 不注册通用allow订阅；D08提供已验Owner/gate端口 |
| 全 Project永久删除与生命周期跨模块完整性 | D28 | 逐个真实adapter+完整manifest集成通过前不冒称完整平台清理 |

交付时同步实际命令/版本/输入hash、全部检查与未执行项、真实绑定、未提交文件和下一步。设计/独立静态审查只证明规格可实施，不证明业务运行。
