# 系统会议 Summary 模型选择 S1 验收

2026-10-07，S1 完整限定 PASS，主线程已采纳并提交推送 `c210d249600d98871513c56fb9a8fff7c50a4c34`，远端一致由主线程核实。作者新6/旧12真实测试、独立 A/B 与两份真实 body/schema/公开 client 联验均已通过，见[独审最终报告](system-meeting-summary-selection-verification-evidence/objects/9c6790dfbedf8f6ad1d0b1061cbd49d079a64e3f117a644cf88d7de6b044acea)和[最终输入冻结](system-meeting-summary-selection-verification-evidence/objects/d808b8e2c01603824275fc47f7d2cb6c80c80823e05c5da11b9ff4f57199d245)。本次提交29个改动路径，含最终 backend README；35技术路径范围包含先前已接受4契约及原字节复用项，不能把路径范围计成35个新增改动。[README独审末件](system-meeting-summary-selection-verification-evidence/objects/30d964bd6ee839cfc3f9dfb5bd80feac6d9a350c9a4a20cb8682e59ae91584bb)已通过，README SHA `b011e9f9ca395d2325a4bc6badb26652874052536f2a6c1a87e8854fa0bf9696`，文档收尾没有产品重跑。归档仅整理固定原件，没有运行产品、旧脚本、Git 或资源。

## 固定输入与范围

产品基线 `03a4a0b87b21c9d3583c01dc3d543bb4ee31ea05`；旧升级生产输入 `6fa2ee721a75ea34a1ccd6523b8b25d68328c5b9`。[candidate04](system-meeting-summary-selection-verification-evidence/objects/da01aba5921573ff03acaf9e4d24df047bd2306f82c24936a90f5343a2311fe2) 固定后端27路径，SHA `da01aba5921573ff03acaf9e4d24df047bd2306f82c24936a90f5343a2311fe2`；[frontend frozen04](system-meeting-summary-selection-verification-evidence/objects/051e5f3c5cac38e142cb62dd1109fa0411da38cc7754cd82c53443ffdab6d9d0) 固定前端4路径，SHA `051e5f3c5cac38e142cb62dd1109fa0411da38cc7754cd82c53443ffdab6d9d0`。另4契约复用已接受提交 `e6cb70bdfc6ef7569d740f767f7dca3211a2ef7a`，构成[工作卡](../work-items/d09-system-meeting-summary-selection.md)的35技术路径范围。

用户原话“系统管理员统一配置用于 summary 的模型”限定为 Meeting Rolling Summary initial/update，含首轮标题。S1 交付独立 singleton/version、当前管理员 get/update/原命令查证、未配置状态、引用及原子删除替换、旧四用途与历史 receipt 兼容，并扩既有 deletion-impact HTTP/schema/client。它不新增 Summary 配置 HTTP GET/PUT 或设置 UI，不绑定默认 root 的 Summary 初始化、resolver 或 Meeting consumer；S2/S3 仍是后续工作。

技术初始化显式执行，依赖旧 singleton，创建不同稳定 ID 的 version=1/model=null；迁移20本身不造默认行或 Model。初始化前合法空态与缺表/坏行/孤立引用区分，旧 Initialize 不隐式调用新方法。选择更新必须在原事务内重验当前 Session/admin、Model/Provider 与精确 reference，禁用后保存的 ID 继续存在。旧四用途请求与独立 Summary 请求保持各自语义，同 action 的原 generic lookup 可返回安全历史 receipt，不能据此改变请求同义性或权限。

## 静审首红与产品修复

[原四阻断 FAIL](system-meeting-summary-selection-verification-evidence/objects/c65b3ede3460c717a541b0e2329f79bf1de2238af924b2fc0b7a5d1febd05d96) 保留 candidate01/02 的四项确定阻断；[candidate03/04 STATIC差量](system-meeting-summary-selection-verification-evidence/objects/cfbb8da7632b79759b8aecf9051cce768668531ebef66fa067cfd9efd86c320e) 仅表示 candidate03/04 的差量静审闭合，不能代替动态结果。

| 原阻断 | 已冻结修订 |
| --- | --- |
| 旧 binary 私有 handoff 直接 JSON 编码含 Scope 的请求，既有 authority 禁止反解 | 使用只含数据的私有 DTO，严格解析后经正式 Human/SystemScope 构造器重建；没有开放 authority 的 JSON 反解 |
| History 使用 `json.RawMessage` 导致 marshal 压缩/转义 SQL 原字节 | 使用 `map[string][]byte` 经 base64 无损封装，限定原三张表、非空 ID 集合，按原 SQL bytes/hash 比较 |
| 新 Summary model.delete 持久 plan 未完整核旧/新双 owner | 生产分支补齐 before/after 成对、同 ID、版本递增、目标角色替换和其他角色不变；精确重建全部引用，拒绝漏边、多边、第三 owner，并核 receipt/metadata/affected count |
| 新分支独立 safe_receipt 只做通用 Validate，可与 plan 不符 | 生产新 Summary 分支精确绑定 safe_receipt、plan.Receipt 与行身份；无新字段的旧历史路径保持 |

后两项是实际生产校验缺陷修复，不能统称为 harness 修补。管理读另外补齐合法双 owner 五边的两个排序方向及第六边哨兵，异常必须零 DTO；HTTP 原件补取真实 Content-Type。candidate04 只修 candidate03 的合法旧 plan 序列化夹具，未放宽生产验证。旧失败源码、修订源码及差量都保留。

作者离线原失败包括错误 package cwd/SQL matcher panic、零 Digest/Version 的 JSON Marshal 夹具错误、错误 `cmd/runner` 参数；独立 compile01 另有私有 probe 的类型/错误码命名问题。[作者offline04](system-meeting-summary-selection-verification-evidence/objects/82bdb3845da7266a19cb77c5dfc7b241d7dd3e30d92fb7fb8886c7ce223d7df2) 保存原始退出及对应输入。最终 Model race 48 top、contract pure/race 各31 top、race vet、两 cmd build、integration race compile 和精确新6/旧12 list 通过。编译和发现不代表测试 body 执行。

## 作者真实结果

[author-final04](system-meeting-summary-selection-verification-evidence/objects/fae66a8f504b7e7c03f4af35c91cc18361c761100984e669dd7fc6bce60f7d2b) 固定作者四轮，均保持原 fixture 的 integration/race、count=1、包6分钟预算；新top的120秒 RUN→最终结果 watchdog 包含 Cleanup。

| 轮次 | 实际退出 / 秒 | 顶层结果 | owned PID/starttime |
| --- | --- | --- | --- |
| new1 | 0 / 100.839 | Schema、Selection 两项通过 | 139 |
| new2 | 0 / 66.065 | Replacement、Unknown 两项通过 | 88 |
| new3 | 0 / 55.981 | ExistingHTTP、UpgradeReceipts 两项通过 | 90 |
| old1 | 0 / 87.618 | 原 Resolution、Project、System selection/引用/权限/HTTP/管理读共12项通过 | 94 |

新6均在完整120秒top预算内。Unknown 覆盖四种真实 PostgreSQL COMMIT 终局组合，包含 ACK 丢失、持锁及回滚/异义确认；不把 HTTP CommitResult decorator 当真实 Unknown。删除替换验证双 owner、引用、Audit/Event 和原子回滚。主管理员来自正式 Bootstrap/login；辅助 `addBrowser` 由 SQL seed User 后正式 Login，schema `base.human` 也是既有 SQL fixture。本结果不把辅助身份创建写成 invitation/Account 创建 E2E。

## 真正跨旧 binary 升级与历史原字节

旧 producer 输入固定305个 Git原源加1个私有helper，共306输入。[old-input02](system-meeting-summary-selection-verification-evidence/objects/976c5fffe4ca2c9812bf95bfd25a405c73da8edb720fb8d58941071efa3b8a93) 和 [旧Git提取provenance](system-meeting-summary-selection-verification-evidence/objects/da270317643d53c4d78a7dd346d28cba6acf32a643d424f50d0f58e05b8fdcf0) 保存路径/hash/blob provenance，[旧producer私有helper](system-meeting-summary-selection-verification-evidence/objects/438ca815ca2c79621e13cb7662b7a609f1e739f2387bd791eaa9080c1de96a65) 保存修订后的私有 helper；305源树、完整依赖图及 binary 不复制。旧 helper只用旧正式 Bootstrap/login 主管理员执行三个 accepted 命令，未注入候选生产逻辑。

[new3 raw](system-meeting-summary-selection-verification-evidence/objects/cacb4695b847033d7fa8dc8a126eff52ad1f8d161e75d8b1766cc2f51cd96a52) 记录旧 producer PID189948 actual exit0，stdout SHA `fd04aec252617cc580b2bd594399f12ba154f93c5172373378277cdcda874753`。[升级固定源码](system-meeting-summary-selection-verification-evidence/objects/1bcc1bf9189313e5ee4cebc0b2a1db8ca4c8b2fee7c54c61b22789098f8a666d) 固定执行顺序为旧进程实际 Wait、确认 schema1..19且无Summary表、再应用20。0600私有handoff解码后删除，测试临时目录清理；身份/Session/请求材料不进入本归档。

迁移前后及新双owner替换后，原9条 commands、15条 Audit、10条 Events 按原 IDs 比较 SQL bytes，其 new3 原件 hash 如下；不是将 JSON 两端重编码后再比较：

| 原历史表 | 行数 | 原 SQL bytes SHA256 |
| --- | --- | --- |
| `agenteam_model.commands` | 9 | `9dd9a10e90b43ff308399ef476b3e7436b6b1893aeb3c970b641bc16909d00c9` |
| `agenteam_audit.audit_records` | 15 | `37111ca95f143d61bf43feef33f6a1405eb6ad8caa349e422a80a0bcbfb4db23` |
| `agenteam_outbox.events` | 10 | `73b44bdab64b0c162d5eb4d12266689cece69e1413dd31ea19d7f2bcceb6c712` |

原三请求历史 receipt 可重放。公开证据保存安全计数/hash和测试断言源码，私有历史内容没有为归档另作泄露副本。

## 独立 A/B 与前端边界

[独立preparation](system-meeting-summary-selection-verification-evidence/objects/c48a9ad52bc4025324ce53cdfca158e4b39e967ff65f03df375dbc74cc6292d0) 保留其“仅离线准备”的原时点，最终真实结果另看 A/B 原 command/raw/result。独立 A actual0/53.995秒、87 owned，用真实双owner删除后四种持久故障注入，公开 lookup/replay 均拒绝坏 plan/receipt，恢复原行且无额外副作用；旧无新字段 plan 保持原加载行为。另丢失真实 COMMIT ACK，在私有确认前正式 Logout，保留 Unknown且零receipt；同用户正式新Session重放无额外效果。原 SQL-role 未执行草稿保留；最终证明 Session 撤销，不声称管理员角色变更。

[独立B raw](system-meeting-summary-selection-verification-evidence/objects/b7e43fe4a00bd54baa24106b33ebc4117dd97947314cf0e1f371a86ba4fbc50a) 的独立 B actual0/57.270秒，top5.92秒，两子例为旧binary/原历史3.84秒及真实Summary+memory HTTP body2.08秒。独立启动同一固定旧producer、实际Wait后验证1..19→20、原历史/重放、无默认Summary、双向shared-key冲突与旧generic lookup的新安全receipt。主管理员均走正式 Bootstrap/login；SQL corruption只作为故障注入，不充当授权证据。

前端产品仅将 `platform_selector/meeting_summary(required)` 加入严格闭集、最大组数7→8，并增加“系统会议 Summary 模型”删除标签；composable与旧固定输入相同，只有精确 `platform_selector/memory` 要求 json_schema，旧 project_summary 仍独立且unbound。[前端作者原件](system-meeting-summary-selection-verification-evidence/objects/c845e137fe8f34c9f68c53c0194c6fb291e5ceeabfe22999e7e82d278d9e3b8d) 记录作者239项pure、type/build通过。[独立前端offline](system-meeting-summary-selection-verification-evidence/objects/87fd0f65d9d01582f24b711ba8e564b9c24047343cf12c2abfb2a8c3a4378cd5) 记录独立最终5项全通过：256个合法组合、八组/10000边界、32种负例及公开Session/controller/Dialog行为。Summary-only可plain替代、禁止clear；重新读取加入memory后撤销原plain选择并要求json_schema。均为合成Fetch/离线受控组件，不是浏览器或真实后端响应。

独立p03原4过1失败来自按钮helper把aria-hidden文本算入选择器；只修私有helper后p05实际全5通过，未伪写成仅重跑1例。作者两次格式首红原样保留。固定锁包 `go-captcha-vue@2.0.7` 的依赖恢复见 [固定依赖恢复](system-meeting-summary-selection-verification-evidence/objects/b2cc8fd9af3feadb3c7a84c0142392303e3a51c335000ec65f68c8ac9c221674)：原curl exit6/DNS失败，随后同官方URL、同锁SHA512成功，仅新增缺失48文件，既有依赖及三份lock/package文件字节不变；未运行npm install/ci或包脚本。该恢复本身不是产品验收。

两份真实 body 联验均通过，见[前端最终报告](system-meeting-summary-selection-verification-evidence/objects/80ea8528118691bff16b12fc47311f8087ec22c7440aec5837c6aa7da18f6a40)与[原始结果](system-meeting-summary-selection-verification-evidence/objects/16f4bd78d8fadbe47d5d9b47408c2d4102efbc233df6f997924ad6318ba7faa6)：作者 new3 的 Summary-only 原 body SHA `2ccad1b1d55edc0c496d3de87767bfe30db4376b1813b9f959b1453a76066242`，独立 B 的 Summary+memory 原 body SHA `39b25a8b83cbce1128723132d168d4e0dce9d00bc79188e1fdaf86d0c68bcf6e`。两份真实 GET200/application/json 的 status、Content-Type、target/path/run 与原 producer 结果分别绑定；HEAD空体由 producer 的实际断言证明。客户端直接把原 Buffer 交公开 `createSystemModelAPI(fetcher).getDeletionImpact`，未 parse/stringify 重编码、未补造 RequestID。标准 Draft202012Validator 校验同原 bytes，冻结 model-system.json 的 GET200 schema 与唯一 common.json localref，关闭外部 retrieval。Summary-only client/schema 实际0、1.023/0.118秒；Summary+memory client实际0、1.122秒，schema实际0。两次是对已产生真实 body 的离线重放，未重启后端或再发HTTP，也未声称native网络/browser验收。每条45秒预算、workers1/retries0，Node禁listen/connect，context.close与direct wait实际完成、adopted0及双次owned清零，输入前后相同。

## 资源、等待与未验范围

作者4轮与独立A/B均为原七精确资源拓扑，实际source/driver/input前后匹配，完整labels/canonical Mount baseline不变，main/watchdog实际等待、adopted0、forced0和双清；每轮有精确资源ID及owned PID/starttime原件。B为87 owned，双清后无自有资源/进程/runtime文件。只读归档不执行任何资源操作。

PID1 daemon shim zombie另列：初始基线24；四作者窗口各+4，累计40；独立A40→44，B44→48。新增项均非task后代，未wait/未signal；owned链双清不等于全机零残留。既有历史zombie和停止任务没有被倒填清理。

后续S2新设置HTTP/UI与默认root Summary初始化、S3 resolver、D24真实Meeting消费、生产Runtime/Invocation Facts、完整D09/E01均未接受。ready503与Object join/OpenAI tools/SPA publication停止状态保持；首轮标题的用户决定不表示已绑定真实生成。

## 原件与归档检查

[source-map.json](system-meeting-summary-selection-verification-evidence/source-map.json) 按来源、SHA和对象位置去重保存原件。大依赖manifest/Go list、binary/cache与旧305源树仅保留固定来源/指纹；私有handoff、runtime目录及凭据不复制。原报告中的历史pending状态保持，由本档最终结论另行界定。归档最终[自查结果](system-meeting-summary-selection-verification-evidence/archive-checks.json)见原件映射；只检查原件/hash、JSON、链接和文本格式，不运行产品、证据脚本、Git或资源。工作卡仅更新页首，技术正文从§1起 SHA `977c6124cd0b54a9776a3a135a0ab47dc3a85ddaf0637d6aa582db2a9f018a04` 保持不变。原证据中的空白和历史pending状态不改写。
