# D10 Skills 当前 Human Owner 目录与详情 HTTP

状态：独立 HTTP adapter 已实现并获未参与者正式有限接受，正在由root限定集成。作者 pure/race 11top/47sub、初Schema21/HEAD18、vet 及后补正式 Account Problem 标准Schema单top32控/HEAD18通过；native3top/6sub、修后PG恢复02的4top/12sub及独立Session撤销诊断02的1top/2sub均取得原完整PASS。PG恢复01因HTTP fixture漏掉正式Account.Initialize而FAIL，独验恢复01因原TCP尾残1row而整体FAIL，两者原件与终态均保留；修后不改产品或放宽断言，新诊断也不反推旧残留归因。基线为正式 main `3b7ed9da`，消费已接受的 [P2 初始化与当前 Owner 读取](d10-skills-initialization.md)及[正式设计](d10-skills-initialization-design.md)。本卡接受只读 HTTP adapter；后续默认根的既有数据读取组合已按末节有限接受，不开放生产 Project.Create、完整生命周期或 UI。

## 范围与真实依赖

公开构造为 `NewHTTPHandler(service *skill.Service, boundary *account.HTTPBoundary) (http.Handler, error)`，两者必须非 nil；构造不启动 I/O 或恢复，生命周期归调用者。包内私有接口仅供测试。实际调用 P2 `ListSkills`、`GetSkill`，不调用 `OpenPackage`，不直接查询表或生成初始化事实。Account 负责浏览器来源与当前 Human Session；P2 在同 Store 活 Tx 的完整 User/Project/Skill 锁下重验当前 Session、Owner 与 Project Read gate，管理员没有跨 Owner 豁免。

当前 P2 目录只有已发布的 builtin Add Skills 一项，不开放外部安装分页、搜索或排序参数。未初始化 Project 沿 Project gate；Project 已初始化但本域没有 published 初始化时沿 P2 `INVALID_STATE`，不得转换为空目录成功。合法归档保持 Read，Deleting 或其它不准读取阶段沿现有 Project 错误。一次成功读取不缓存后续授权。

adapter 本身不改变 P2、Cleanup、Project 或 Account 实现；无迁移。默认 Central 的后续接入边界见末节。不含包/正文流、安装、分配、Agent、UI 或新 Project 创建。Object Runtime join、OpenAI tools 独立动态、SPA concurrent-publication、Jina/Image 四停项保持。

## 路由、输入与响应

| 路径 | 方法 | 真实服务 | 成功表示 |
| --- | --- | --- | --- |
| `/api/v1/projects/{project_id}/skills` | GET、HEAD | `ListSkills` | `{"items":[SkillMetadata]}`，恰一项 |
| `/api/v1/projects/{project_id}/skills/{skill_id}` | GET、HEAD | `GetSkill` | `SkillMetadata` |

两 ID 必须是 foundation 可解析且与规范字符串逐字相同的 UUIDv7；不接受尾斜线、多余段或路径别名。两路由均不接受任何 query，包括空 `?`、重复/未知 key、cursor/limit、分号和非法转义。请求体必须为空：拒绝非零 Content-Length 或 Transfer-Encoding；未知实现的零长度 body 至多读取 1B 并要求实际 EOF，所有路径实际 Close。

先执行 Account 浏览器检查和 RequireHuman，再分派路由、方法及参数。Account 原前置先拒绝非规范路径（包括尾斜线、转义路径别名），返回 400；通过该前置后，非法已匹配路由参数为 400，规范形状的未匹配路由为 404。已通过身份前置的其它方法返回 405 和 `Allow: GET, HEAD`；未认证或非法 Origin 仍沿原 401/403，unsafe 方法保留 Account 原 CSRF 检查。缺失/非法 request ID 不进入领域读取。

`SkillMetadata` 显式投影八字段：`id`、`project_id`、`name`、`normalized_name`、`description`、`protected`、`current_revision`、`version`。ID/版本沿正式标量；不输出 RevisionMetadata、ObjectID、对象 key、manifest、包摘要、正文、租约或创建私有事实。先完整 `Metadata.Validate`，核目标 Project、详情 SkillID 和目录恰一项，再完整编码；坏结果统一安全 `DEPENDENCY_UNAVAILABLE`，不发部分成功。最多 64KiB 表示，足以容纳当前正式字段的 JSON 最坏转义大小；未来扩目录须另改正式契约，不截断或假分页。

成功为 200、`Content-Type: application/json`、`Cache-Control: no-store` 和准确 Content-Length。HEAD 执行同授权、查询、完整校验与编码，成功长度与对应 GET 表示一致，但不写 body。错误原样交 Account HTTPBoundary：保 Fault、commit_state 和内部 cause；公开 Problem 不含原错误/SQL/原请求路径或 query，instance 沿正式 `/api/v1`。HEAD 所有错误同样无 body，但保留 Problem 表示类型/长度等 header。跨 Owner、未知 Skill 沿原 404；失效 Session 沿原身份错误；合法形状的未初始化/非法阶段沿原 409；未绑定/不可用及读事务 Unknown 沿原 503，不发布候选。不把后续 GET 当原 Unknown 提交确认。

## 总预算与资源责任

从浏览器检查前开始一次 2s 总预算，继承更早 parent deadline；包括身份、body 探测、真实 SQL/Rows/Tx 尾、完整编码、写入/Flush、body Close 与取消 callback 实际 join。复用已验 [Knowledge Owner HTTP](d12-knowledge-owner-read-http.md)的原 I/O 方法，在本包保留窄实现，不改共享库。原生 read/write deadline 必须可用，unsupported writer 安全 abort；取消/期限/短写/Flush/Close 错误不得改发成功或重复 Problem。原 callback 实际退出后才能清 deadline、复用连接。构造和请求均不增加后台恢复/worker。

GET/HEAD 不产生 Skill、Object、Audit、Event、receipt 或业务 Activity 新事实。只读事务的实际终态仍必须确认，读 COMMIT Unknown 不能发布候选。HTTP 只承诺原领域读取的事实与本请求 I/O 退役，不宣称 Project 全域停止或 Object lease 已清。

## 必要验收与写域

纯控制核严格路由/query/body、两 GET/HEAD 同体长度、安全八字段/坏结果/跨 Project 或目标、身份先行、原 Fault/Unknown 保留，以及 deadline/短写/Flush/body Close/原取消 callback join。Schema 检查覆盖成功和 Problem，HEAD 所有状态不声明 body。

真实 PG 组合消费正式 Account 登录/注销、P2 初始化与真实 Store/Project gate：目录/详情、未初始化/未知/跨 Owner/admin、归档可读/Deleting拒绝、每请求撤权及 Owner 变化、原 SQL 取消与 Tx 尾、读 COMMIT Unknown 零候选；独立连接核无新增业务事实。初始化/生命周期上游 fixture 必须说明是否真实命令或合法 seed，不冒 Project.Create/BeginDelete。native 组合覆盖原 2s/更早 parent、同连接 deadline 清除、真实写背压/断连及 callback/handler 退出。真实窗口另由 root 调度，纯控不代 PG/native。

唯一作者写域：新 `internal/central/skill/http/**`、`api/openapi/skill-owner.json`、新 `tests/skills/owner_http*`、本卡和本树 `.agent-state/current.md`。不改依赖锁文件、迁移或默认路由；必要运行入口按真实验证准备另交接。短规格由未参与实现者静审，产品与高风险验证另按稳定输入独审。

离线准备终态：源码98bb09f0未变，native候选86279→bf099f、PG候选81480→b49722实际race-c0；后续PG driver首磁盘门16cc49 exit78且未启动Go，保留此记录。空间协调后仅补两driver与两list，34104→6839c1、ba9efe、63489→422f18、30701→bcd478均实际0，逐条独立fresh≥5GiB及outer Wait，未重编候选。两包没有TestMain，list不执行测试体。候选/driver完整SHA、各次可用量、日志及未使用真实输出目录见本树current；冻结输入预飞为native15/PG34项。后继PG4top/12sub及native3top/6sub必须各自原完整资源与TCP尾后才判业务结果，不能用已通过pure或发现代替。

Native首轮24202→c0f5e4已取得原outer actual0：3top/6sub全部PASS，Go/driver实际Wait0，原runtime/private退役、两次desc空、TCP两次delta空及inputsame完整；supervisor终态0/68.173s，未改原预算或方法。固定e60候选+c11 driver，首同进程2026-10-10T02:02:54.242191Z/5,411,155,968B；原日志路径与资源身份见current。结论覆盖真实TCP read/更早parent期限、同连接清deadline、Close错误、真实输出Timeout与断连/原领域尾；native的领域/身份控制是明确替身，不能代替待运行的真实Account/P2权限和PG事务。窗口已完整释放，PG尚未执行。

## PG 恢复与实际结果

新环境 PG 恢复01的确定夹具缺项：P2 原 fixture 只构造 Account Authority 以消费 seed 身份，新 HTTP fixture 引入真实 Login 却未调用正式 `Account.Authority.Initialize`。`NewAnonymousContext` 更新 `account_key_registry` 要求恰一行，未注册 key 时返回安全 DependencyUnavailable；与四 top 构造阶段的实际错误一致。只在 HTTP 自有 fixture 中补正式 Initialize 并给启动/anonymous 错误加安全阶段标签，不修改 P2 fixture、产品、权限或用 SQL 伪造凭据。原日志保留于 [topic checkpoint 3b97fe69](https://github.com/LunaDeerTech/agenteam/blob/3b97fe69/.agent-state/skills-owner-http/pg-recovery01-failure.log)；原 child/driver/outer 均 actual exit1，两个自有 PG 资源双退役、私目录/desc/TCP 双尾齐，输入未变。修后另用新候选和新窗口，不回填该 FAIL。

修后 fixture `3b97fe69`、86fd 候选的恢复02原 outer `30222→fc38c6` actual0：4top/12sub全 PASS、0 skip，实际 Account Bootstrap/Invitation/Redeem/Login/Logout、P2 目录/详情/Schema/HEAD、当前 Session/Owner/Project gate、User SH/EX两序、真实SELECT取消与原Tx退役、读 COMMIT 两种 Unknown 均闭合。原读取之外的 Store 查询核九项事实与原 Object 调用数不增。原 child67853/driver67244实际 Wait0，driver55.293s；两个精确自有 PG 资源双退役、private/desc/TCP/input 尾齐，supervisor0/114.362s。完整路径与资源身份见[固定 HTTP 恢复记录](https://github.com/LunaDeerTech/agenteam/blob/1e260d55316314923e50e36ecf73ac5ea3a9019e/.agent-state/current.md)。P2 Object 是声明的受控端口，Project/Creation/ready 是合法 seed，不能外推默认 root、Project.Create 或新 D05 物理发布。

未参与者 coordination 独立恢复01沿同一86fd候选/0e6 driver、独立单top入口验证 HTTP 认证后真实 Logout，再由原 P2 读取重验 Session，GET/HEAD 两 sub 的原业务断言PASS（1top/2sub，23.52s）。原 child71642/driver70983实际Wait0，两个精确PG资源双退役clean、private/exactCases正确、desc双空、inputs不变；原hostTCP75s尾却残1row，没有双empty，supervisor1/113.891s，outer `50063→96a6c8` actual1，故整体FAIL。本轮不能正式接受；作者已闭合的4top/12sub与native结果保持。原记录只保存TCP残留数量，不能判定其身份或归因；不自动重试或后采样补原终态。原1988B日志留在 topic 分支 `.agent-state/skills-owner-http/independent-recovery01-failure.log`，由root保存，不随独立adapter交付重复可重建日志。

独立诊断02由 coordination 本人使用同86fd候选/0e6 driver与已保存d4cdbbc0同步诊断入口执行，原 outer `68413→205cd5` actual0：1top/2sub全PASS/11.45s（GET5.36s、HEAD6.09s），child103042/driver102489 actualWait0；两个精确PG资源双退役clean、private/exactCases正确、desc双空、HOST_TCP双empty、inputs不变，supervisor0/75.271s。原日志与身份见[固定 HTTP 恢复记录](https://github.com/LunaDeerTech/agenteam/blob/1e260d55316314923e50e36ecf73ac5ea3a9019e/.agent-state/current.md)；同步诊断沿原534次TCP采样观察TIME_WAIT差值两个→一个→零，inode0、无fixture端口匹配，未改变75s/双empty拒绝门，也未增加后台或后验采样。该新轮原全尾支持未参与者正式有限接受；不能更改独验01整体FAIL或补其缺失的归属证据。默认根的后续限定读取接缝见末节；生产 Project 创建和 UI 仍未交付。

## 默认根的限定接入与未交付边界

默认 Central 在同一 Store、Project Authority、Account Service 与实际 Object/ProcessAuthority 上构造真实 Skills Service，以同一实例和 AccountBoundary 处理本卡两条路由。Object 按 Avatar、Knowledge、SkillRevision 固定 owner 分派，保留当前权限和原维护事务；初始化 Audit wrapper 组合原 Object 私有 witness 与 Skills facts。实际进程持有 Skills 的 Stop/Drain/Joined，不以 HTTP 返回代替领域退役。

生产 Project initializer 保持 unbound。D08 §7 与 D10 设计 §§1、14 要求完整 participant manifest 和共享 guard 后才启用；公开 Create HTTP 尚未开放不豁免内部默认创建。原 `649e6ad3` 根测试的业务与退出尾 PASS 保留，但其默认 initializer 接缝被后续规范独审拒绝，已在 `0747b02c` 撤回。

整改后的 `TestKnowledgeSkillsDefaultRootComposition` 原单 top/零 sub 完整 PASS：先证默认 Create 返回 `DEPENDENCY_UNBOUND`/`not_committed`，13 项 Project/Skill/Object/Audit/Event 事实为零；再由显式 test-only Project 服务使用同 Store、原 Audit、真实 Skill/Object 端口建立读取数据，并实际 Stop/Drain/Joined。它不替换默认服务、不以 SQL 写 ready。随后才验证默认 Skills 目录、Knowledge 正文 GET/HEAD、Avatar 维护与原进程退出。用例 7.39s，Go243371/driver241594/outer241447 均实际 Wait0，七资源/private/runtime/desc/TCP 双尾和 inputs 一致齐，总111.577s。该结果接受既有数据的默认读取组合，不接受生产初始化或完整 participant。

后继 Skills UI 应消费本卡八字段和安全 Problem，覆盖当前 Session/Owner、未初始化及归档/Deleting 状态；不附带包正文、安装、分配或 Agent 能力。本批没有 Skills UI，Object Runtime join 等原停止项保持。

## Human Owner 普通安装与分页发现：有限交付

既有普通安装 Service、Human HTTP 适配与默认 root 的同实例分派已交付。新增 `POST /api/v1/projects/{project_id}/skills`、`POST .../skills/commands/lookup` 和 `GET/HEAD .../skills/catalog`；安装请求仅接收正式 `text_files` 包，使用原 Account Session/Origin/CSRF、当前 Project Owner 和原幂等命令。receipt 只公开 Skill ID、revision、version。Lookup 保留原 key 和完整原 intent；Unknown 不自动重发，NotFound 不证明先前已回滚。catalog 是最多 100 项的当前授权分页，游标绑定 Project/Owner；原 builtin-only collection 与 Skill 详情读合同保持。公开字段见 [OpenAPI](../../../api/openapi/skill-owner.json)，复现入口见 [Human Skill recipe](../../../.agent-state/skill-install-owner-http/README.md)。

原 root 复用同一 Skill Service、Store、Object 服务和 Account boundary，不新增服务或停止 owner。旧只读请求仍为 2s，新命令为 30s，包含 body/build、事务、Object 及原消费/取消尾；真实调用退役之前不释放服务工作计数。普通安装的原命令、attempt、revision、Object reference、审计和清理锚点均持久化；已发布包的读取、同 key 重放及普通清理沿实际 Object 生命周期完成。

验收按实际版本组合：安装库 pure-03 的 10 top / 27 sub race 与本包 vet 通过；HTTP 10 top / 59 sub race 通过，Schema 45 向量、30 个 HEAD 状态及三包 vet 的必要修后补集通过；combined02 的 domain 安装/读取重放、普通清理两 sub 通过；HTTP03 的真实安装/Lookup/分页/旧读兼容及当前 Owner/CSRF 两 sub 全部通过。后者包含真实 Logout 后原请求返回 401、安全 Problem、清 Cookie 与零新增事实。两次真实运行的原 Wait、七资源与 private/runtime/desc/HOST_TCP 双尾均闭合；combined01/02 原 wholeFAIL 保留，不能将分项通过改写为旧轮整体通过。HTTP03 使用真实 Account 和持久服务配合 ResponseRecorder；app 分派由定向纯控证明，未宣称新增完整 app 进程或浏览器验收。

本次连续迁移为 00032–00036。32–35 与 schema02 已验来源逐字一致，其 fresh/repeat、旧 31→35 升级及约束验证可复用；36 已由本次真实安装与 HTTP 链实际执行。引入这些表不等于启用 Agent 配置、Registry、Mount 或 F1 服务；本片不导入其未交付实现。Skill 清理对 00034 的 assignment heads/rows 实际查表，任何残留（含 disabled 初始化事实）均阻止完成，不跨域删除 Agent 数据。

生产 Project initializer 继续 unbound，AgentRun 安装仍明确 DependencyUnbound；完整 F1、Agent assignment、可执行 Install Backend、Skill 更新/删除 API、Runner 安装、physical ensure 与旧 Object runtime STOP 均未由本片放开。后续模块必须消费真实绑定，不以空 provider 或本次 Human receipt 替代运行授权。
