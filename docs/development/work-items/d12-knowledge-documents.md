# D12 Knowledge 文档与文档树

状态：B01 纯契约、[B02 Human canonical/树服务](d12-b02-knowledge-service.md)、[Owner metadata/树 HTTP](d12-knowledge-owner-read-http.md)、[树命令 HTTP](d12-knowledge-owner-tree-http.md)与[有界正文 HTTP](d12-knowledge-owner-content-http.md)已分别完成有限交付；正文卡 §6 记录默认 root 的有限真实组合通过。下述 Owner 只读 UI 已完成实现、作者正常链及新主线组合 `read04` 整体检查，新增三项独立组件风险验证通过；D12 整体与既有 STOP 未关闭。下列 B01/C1 段落保留当时记录，后续实际状态以相应子卡为准。

正式依据：[实施规格](d12-knowledge-documents-design.md)、[D01 资源契约](d01-contracts/resources-skills.md)、[文档领域](../../architecture/knowledge-memory/knowledge-document-domain.md)。规格输入固定 `16595ad1e78e5283dfe85fb812095acde382edd1`。原候选 `/tmp/agenteam-d12-s01-rev2-21if98iv/d12-knowledge-s01-candidate.md` SHA `c1ae54e6a6d4ea111d4e662ba3c75cbd4c7a42d8f7ab2768b3bbbf45309adf23`；独立复核 `/tmp/agenteam-d12-rev2-review-0nxov8_a/report.md` SHA `35c733111d44052d124e7cc731433905794840b4b82b2777da102806ca78acf9`。R01 namespace/OwnerIDs 与 R02 跨包 typed 载体已闭环；静态采纳不代表生产能力通过。

## B01 纯契约与规则

作者：`parallel_plan`；独立验收：`d01_verify`。交付六个新生产源与各自相邻测试，共12文件，全部相对 `internal/central/knowledge/contract/`：

| 新源 | 相邻测试 |
| --- | --- |
| `types.go` | `types_test.go` |
| `source.go` | `source_test.go` |
| `commands.go` | `commands_test.go` |
| `tree.go` | `tree_test.go` |
| `confirmation.go` | `confirmation_test.go` |
| `events.go` | `events_test.go` |

落笔前已核两文档和 knowledge 目录均不存在；不覆盖旧接口。只读固定165的 foundation、identity/contract、object/contract、project/contract、event/contract、outbox/contract、cursor 及 module 文件；不改旧口、依赖、SQL、cursor.Text、服务/provider、app、全局计划台账或其他工作项。缺真实端口保持明确未绑定，不生产 stub。

实现：Human 当前文档类型/严格DTO、业务主体摘要、父关系与子树规则、独立签名确认、单次源流/受控读取载体、typed Event、完整计划/issuer/Tx绑定的纯载体。计划构造不能证明 held/auth，Go 载体不证明实际 join；这些仍由后续服务验证。

必读：[Go 开发技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[文档技能](../../../.agents/skills/agenteam-documentation/SKILL.md)、实施规格 §2–7。使用固定 Go1.27.1，隔离必要依赖副本对新包运行 unit/race/vet；不跑全库/Go集成/Docker。验证 §7 B01 边界，原失败与修复证据保留，停止写入后交独立验收。源码或公共语义超出上述12文件必须先报主线程。

## B01 作者冻结记录

固定隔离副本 `/tmp/agenteam-d12-b01-vt2dgnub/repo` 仅含165基线必要契约/基础依赖与本次12新源，不消费活动稿；未采入后续 PrepareReadAccess 变体。Go1.27.1 首轮29主测试通过，补充预览一致性后受影响2主测试通过，最终全包 race 30主/18子通过（1.127s），vet通过。实际命令和原日志见同目录上级的 `author-report.md` / `validation.json`。本记录仅为作者自测；独立验收、真实Session/Owner、活Tx/持锁、D05 I/O/join、Outbox持久发布、PG/MinIO/服务/provider、D08生命周期与D13/Agent adapter 均未因此通过。

## B01 独立验收与 B02 接续

B01 独立报告 `/tmp/agenteam-d12-b01-verify-49kvfiqa/report.md` SHA `b95a4e96a583b3e72097d027f9e372d3f7dc5b58d1a031ed5634d08fc5b6523e`：复用匹配的作者 pure/race/vet，并独立验证 Source 真实一次 Close/并发责任、严格DTO及精确数值、签名域隔离，三项风险测试 race 3.169s 通过。12文件及两文档已随 `914fd84` 提交推送；只认定纯契约，不证明真实权限、活Tx、对象流或数据库。

[B02 正式卡](d12-b02-knowledge-service.md) 保留完整 Human CRUD，明确29新领域与共享6新/9旧范围。当前只授权 C1 的 `cursor.go`/`text_test.go`；其余按固定输入和文件所有权另行接续。A 的真实 D05 Object Audit/stop、P 的通用 Human Outbox gate及Knowledge Audit路由尚需独立验收，B持有的Audit闭集需先冻结交接；无编号DDL仍在卡中指向的原 `/tmp`，没有占用迁移号。B02当前静态采纳不等业务通过，也不等待D13作为canonical前置。 C1 作者以固定 Go1.27.1、local/readonly、GOMAXPROCS=2/-p=1 对 cursor 全包10主测试执行 unit（0.008s）、race（1.186s）及 vet，全部通过；原日志与命令在 `/tmp/agenteam-d12-c1-yq_7vblk/`，不作独立验收或真实分页查询通过。

## 后续门槛

B02 新 canonical/树服务库可依真实稳定端口继续准备；真实 PG/MinIO、唯一迁移号、cursor.Text、Knowledge Object/source/download/Audit 与清理能力分别交接验收。B03 participant 必须等待 D08 lifecycle/D05 stop 的真实稳定绑定，不以 C0 或 pure 通过替代。D13 parser/index/retrieval、Agent destructive 的 D18/D19/D21/D22 适配与正式 HTTP/UI 后续单独接入；不阻断当前 Human 契约，也不宣称已有这些生产能力。无 D09 全局等待门槛。

## Owner 文档树与正文读取 UI（限定读取范围）

本段只落实[知识库布局](../../frontend-design/layouts/knowledge-base.md)和[项目工作台](../../frontend-design/layouts/project-workspace.md)中已有文档的读取部分：当前 Owner 进入项目知识库，分页展开文档树，选择父文档或子文档，查看元数据、祖先路径和当前有界正文。默认不选中文档；空库显示“暂无文档”。普通入口不恢复旧选择，指定文档链接按当前权限重新定位。首片段可独立交付，不等待完整知识库编辑器。

实施使用 root 已创建的独立 `ai/knowledge-owner-ui` 树；前端执行者唯一写本段列出的客户端、会话接缝、组件与页面，配套真实 fixture 由指定后端/测试写者负责，共享 harness 仍由原 owner 集成。客户端、controller、树和页面已实现，沿现有 Session/Project 工作区接入；作者四 GET 正常链及新主线 Account/Secret 根装配合入后的组合均已真实通过。无迁移、新后端契约或生产 root 改动；保留 Object Runtime join、来源获取及其他既有 STOP。

### 四个正式读取接口

`P = /api/v1/projects/{project_id}/knowledge/documents`。首版 `KnowledgeOwnerAPI` 只提供下表四个方法，均传入原 `AbortSignal`，使用当前 Cookie Session；输入和完整返回按[metadata Schema](../../../api/openapi/knowledge-owner.json)及[正文 Schema](../../../api/openapi/knowledge-content.json)严格解析，不能将 `unknown` 直接断言为领域对象。

| 客户端方法 / 页面动作 | 固定 GET 与输入 | 完整返回与发布约束 |
| --- | --- | --- |
| `children(projectID, parentID, query, signal)` / 根层、展开、继续本层 | `P/children`；`parent_document_id=null` 或 UUIDv7 必须显式给出；首版每页 `limit=50`，后页带原 cursor | `{items: Document[], next_cursor?: string}`；仅合并同项目、同父级、同读取代次的完整页，按 ID 去重，沿服务端 title/ID 顺序；没有目录快照保证 |
| `get(projectID, documentID, signal)` / 选择、显式重读 | `P/{document_id}`，无 query | 严格二选一 `{active: Document}` / `{deleted: Tombstone}`；核对目标 ID/Project，tombstone 不触发正文读取 |
| `ancestors(projectID, documentID, signal)` / 路径与深链定位 | `P/{document_id}/ancestors`，无 query | `{items: Document[]}` 是 root→parent 完整路径，不含自身；按真实父关系定位，不以标题猜节点 |
| `readContent(projectID, documentID, request, signal)` / 当前正文分段 | `P/{document_id}/content`；`byte_offset` 默认 `0`，`max_bytes` 默认 `65536` | `{document, text:{text,next_byte_offset,truncated}}` 或 `{document,unavailable:"dependency_unbound"}`；完整核对目标、版本、union、UTF-8 字节数和偏移关系后才发布 |

客户端 `parentID: string | null`，`query: Readonly<{limit: number; cursor?: string}>`，`request: Readonly<{byte_offset?: string; max_bytes?: number}>`；捕获时校验正式范围，生成规范 query。`Document` 复用正式 12 字段表示；UUIDv7、版本与偏移等精确数值不经 JavaScript `Number` 舍入，游标只作不透明内存值。每个请求重新授权，已成功的项目、树页或 metadata 不是后续请求的权限凭据。已有 `ListDocuments`、`SearchTitles` 及五个 POST 命令保持原后端能力；全项目标题查找、编辑/重命名/移动/删除 UI 后续独立接入，不加入这次首链。首次页面只发 GET，不先发 HEAD 冒充正文读取成功。

正文的 `65536` 来自正式 `DefaultReadRequest` / HTTP query 默认值，与 Object 预读缓冲无关；服务接受 `max_bytes=1..1048576`。偏移按 UTF-8 字节，不是字符数；只沿返回的 `next_byte_offset` 前进，EOF 的 `truncated=false` 关闭下一段。小预算可能合法返回空文本、原偏移和 `truncated=true`，客户端不得循环重发同偏移；首版固定使用默认预算。每次请求重新打开当前 canonical，并在原 2s 预算内跳过 offset、读取和实际 Close；大 offset 没有随机访问或恒定耗时保证，超时就地失败，由用户显式重读，不自动重试或增长预算。

首版展示单段正文与上一段/下一段，只保留必要偏移导航，不无限拼接全文。返回版本变化时清空旧偏移/正文并提示从开头重读；metadata、祖先与正文为分别授权的当前观察，不伪装原子快照。text/plain 和 Markdown 均安全显示源文本，不使用 `v-html`；PDF/DOCX 的 unavailable 显示“此文档暂不支持正文读取”，不触发解析、预览、下载或来源获取。索引 pending/processing 显示“索引中”，failed 显示“索引失败”，均不屏蔽可读 canonical 正文。

### 最小前端模块与复用接缝

| 文件 / 模块 | 本片段职责 |
| --- | --- |
| 新 `web/src/api/knowledge-owner.ts` | 四个可复用 typed 读取方法、输入捕获及严格 DTO/关系解析；只依赖正式 HTTP 表示，不持有页面状态 |
| 既有 `web/src/api/client.ts`、`web/src/composables/useSession.ts` | 固定四端点接入现有 `accountTransport` 和唯一 Cookie 请求 owner，沿 `runAuthorized` 的身份、取消、原 Promise 与实际 `finally` 退休；不建立第二套 fetch/Session。成功 JSON 上限直接对齐现有 metadata `5 << 20`、content `7 << 20` 的完整表示上限，Problem 继续既有边界，不放宽其他端点 |
| 新 `web/src/composables/useKnowledgeOwner.ts` | 消费 `useProjectWorkspace().currentReadContext`，管理项目/选择/读取代次、每层 cursor、祖先及当前正文段；四请求按同一个 owner 依次执行，页面切换/取消退休旧观察 |
| 新 `web/src/views/projects/ProjectKnowledgeView.vue`、`web/src/components/knowledge/KnowledgeDocumentTree.vue` | 页面只负责呈现和交互，树封装负责文档到通用节点的映射；复用 `UiTree`、`UiDrawer`、`UiBreadcrumb`、`UiBadge`、`UiState`、`UiButton` 及既有样式 token |
| 既有 `web/src/components/ui/types.ts`、`UiTree.vue` | `TreeNode` 仅增可选 `expandable`，缺席时仍按原 `children?.length` 行为；支持尚未查询子级的展开，成功空页后撤去展开标记。展开与选择分离，保键盘和焦点，不用假文档/假文件夹充当占位节点 |
| 既有 `web/src/router/index.ts`、`router/auth.ts`、`components/layout/ProjectNav.vue` | 增 `/:username/:project_name/knowledge` 与 `/knowledge/:document_id` 两个严格叶子和“知识库”入口；后者仅接规范 UUIDv7，安全返回仍拒 query/hash/编码绕过/未知后缀。项目地址由现有 Owner Resolve→Get 确认，不重建工作区 |

窄屏使用现有 Drawer，选中文档后收起，关闭恢复入口焦点；宽屏左树右正文。每层 cursor 用“继续加载”而非虚构总页数；树加载不预取正文。祖先路径可先展示已授权节点，展开对应层仍取得真实 children 页，不用祖先数组伪造完整目录。页面无编辑草稿，不增加丢写确认，也不清除既有 Project/System 的草稿。

### 状态、权限和完成边界

页面须等待 `currentReadContext` 的当前 identity、Project ID 和读取代次后才发子请求；仅 Resolve、旧 Get、checking、未初始化或 deleting 不启动读取。archiving/archived 沿当前服务 Read gate 读取，本片段始终只读。项目、身份或 Session 改变时清空树、选择、cursor 和正文；取消/离页/超时先退休发布资格，原 fetch/body reader/cancel/finally 未完成前仍占原 Cookie owner。迟到成功或错误不能更新新选择/身份，自身占用 owner 也不能被误判为当前响应未发布。

树、metadata/祖先和正文各有 loading/empty/current/error 与显式重试。有效当前 `401` 走既有 Session unavailable→检查会话/登录；局部 `403/404` 显示文档不可用且不冒管理员权限；metadata tombstone 或正文 `410` 清除该正文并显示已删除；`409`/跨段版本变化要求重读；`500/503`、transport、非法表示或取消不发布半页/半段，不声称空内容。读取 `COMMIT_UNKNOWN` 只表示本次失败，不产生命令确认或重放入口。身份不再可信时隐藏受保护内容；同一上下文保留的旧完整观察必须明确标旧，不能当当前授权成功。

### 首条真实链与有界验收

实施先交四方法客户端、Session 接缝和一个可运行页面，完成定向基础检查后尽早联调，不等检索、编辑或全布局完成。已实现 `internal/central/app/knowledge_owner_web_test.go` 的 integration top `TestKnowledgeOwnerReadWeb`，配 `tests/account-captcha-web/e2e/knowledge-owner-read.spec.ts`，消费实际默认 `bindAccounts` 实例和锁定 Playwright。fixture 沿修后 root 有限组合方法：默认 Project 创建仍返回 `DEPENDENCY_UNBOUND` 且零事实；只在 `_test.go` 显式构造真实 ports 的隔离 Project 服务，复用原 Store/Auditor/Skill 完成准备并实际 Stop/Drain，再由默认 Knowledge.Service 创建含多层树的真实文本。可复用 `rootCompositionProjectFixture` 的测试边界，不把 initializer 重新注入默认根，不插业务 SQL 或造 ready 事实；完整 lifecycle participant、生产 initializer 与 Project Create HTTP 继续未绑定。

首个正常 case 从真实登录后的项目入口进入知识库，确认默认未选中，展开父文档、选择子文档、核真实 metadata/祖先及首段 UTF-8 文本，再取下一段核实际 byte offset；同时验证父文档也可选读。先以小 fixture 打通这一次原客户端→Session→HTTP→B02/D05→原响应完整消费→页面发布。沿既有 task-owned 静态 dist/同源反代测试入口与七资源 fixture；它们不是 SPA 生产发布。浏览器、Go/driver/outer、原请求消费和取消尾、领域/root 退役与自有资源/TCP尾全部结束才算该次完成，HTTP 200、native EOF 或截图单独不算 UI 发布。

与本片段风险对应的增量检查限定为：四端点完整/坏尾表示与偏移解析；根/子级 cursor、空树/空正文/tombstone/unavailable；当前 Owner 拒绝与 archived 读取；切换文档/项目或身份、held reader/cancel/finally 时禁止迟到发布及提前放行下一 Cookie 请求。复用未变后端权限/native/root 证据，不重跑其全矩阵。真实浏览器补窄屏 Drawer、键盘选择/展开、焦点恢复、长文本滚动和现有明暗/reduced-motion样式；独立验证者只核新权限、消费/发布和页面接缝。精确选择器为 `^TestKnowledgeOwnerReadWeb$`（1 top / 0 sub），锁定 Playwright 1 case / 45s / retry0；Go 120s 含 cleanup，包6m、root540s及原退出/资源/TCP尾沿已有共享入口。Go/browser fixture 已实现；旧输入作者 `read03` 和合入新主线后使用新编 Go 候选的 `read04` 分别实际通过，结果各自绑定原输入。

### 当前证据与完成边界

四个前端测试文件的有效最终证据为：客户端35项、UiTree/公共组件14项、目录失败清层修后state/页面29项，共78项；相关输入未变时复用。既有Session/认证/Project工作区126项兼容检查保持原范围。最初166项组合早于目录返修，不写成修后重新全跑；失败保留在[固定topic恢复记录](https://github.com/LunaDeerTech/agenteam/blob/23f448d7c423dc9b5e872b4fb4ebf16e8b86a784/.agent-state/current.md)。完整类型检查、返修后的私有正式dist构建与有限静审已完成；受控Fetch/jsdom证据不冒真实权限或浏览器。

目录继续页失败或取消会清该层行和cursor，保留其他ready层；新增根403、子503及持住原尾的Stop控制。当前67个dist资产来自返修后冻结源，并已由作者 `read03` 真实消费；生产11源、四个前端测试及锁未变，本轮不重复构建或重跑未变矩阵。

作者正常链 `read03` 在方法源 `0f2194c8` 与旧Go候选下整体PASS：12个四GET原请求完成原reader/取消/Close与Session同Promise typed发布、DOM及Schema核对；两observer首次explicit/pending0与全部尾join通过。原Node、Go、driver、outer及七资源/private/runtime/desc/TCP双尾完整退出。首两轮wholeFAIL及其UNKNOWN保持原样，该轮ERR_ABORTED只由当轮现场观测支持；[旧输入安全结果](https://github.com/LunaDeerTech/agenteam/blob/23f448d7c423dc9b5e872b4fb4ebf16e8b86a784/.agent-state/knowledge-owner-ui/read-third-pass.json)与[恢复说明](https://github.com/LunaDeerTech/agenteam/blob/23f448d7c423dc9b5e872b4fb4ebf16e8b86a784/.agent-state/knowledge-owner-ui/README.md)固定到已保存topic，不要求把历史JSON/current导入正式范围。

Skills/coordination已对产品、fixture和消费方法实际差异做有限独立静审。coordination另独立执行 `knowledge-owner-risk.spec.ts` 三项实际App/Session/Workspace、受控Fetch/Stream验证：当前401隐藏原内容、reader未退役时跨Project不提前请求、outer cancel未退役时跨Session不提前restore，释放后各有正常完成证明；unit/类型/格式通过。其最初按钮精确文本及Problem URN刺激错误已窄修，原方法FAIL保留；这不是新增真实Owner转让或Logout PG验收。

合入主线Account/Secret根与共享并集后，`26e3928d` 新race候选经精确1 top/list准备；`3c7958ac` 输入的作者 `read04` 使用该新候选和未变67个dist资产，1 Go top/1 PW case整体PASS（Go27.31s）。12原四GET与Schema12、树键盘/UTF8下一段/Drawer、原typed发布和两个observer首次explicit/pending0/实际join全部通过；4个原请求现场ERR_ABORTED且finished调用0，另8个正常finished调用1/null。原Node/Go/driver/outer及4 adopted Wait0，七资源14次absent/private/runtime/desc/TCP双尾齐，1455运行时输入初末一致，wholePASS130.643s。新增纯测试由既有collector排除，不人为扩成1456输入。

本次有限完成范围仅Owner已有文档的四GET与只读页面，复用既有后端权限证据；旧01/02FAIL与read03旧输入PASS不升级。默认initializer、完整participant、Object Runtime join与来源STOP保持，不扩编辑、parser、Project Create HTTP、整D12或生产SPA。

## Owner 已有文档改名 UI（实施规格，尚未验收）

本片段提供“改名→查询原命令结果→重新读取当前文档与目录”，只消费[树命令 HTTP](d12-knowledge-owner-tree-http.md)的 rename/lookup，普通当前 Human Owner 可用。创建、正文替换、移动、删除不在本片段；前两者虽已有 Service，尚无正式写 HTTP。D13 只消费已完整返回的版本绑定正文，改名不等待 Parser、不发布索引成功；生产 initializer、participant、Object Runtime join 和来源 STOP 保持。

### 固定接口与原意图

沿前段 `P`，仅新增两 POST，无 query，原 Cookie Session、单个 Idempotency-Key、原 CSRF 和 JSON；正式 16 KiB 请求、5 MiB 成功完整表示与服务 2s 预算保持，Problem/四 GET 上限不变。title 保原 UTF-8、1–512 Unicode scalar、无控制字符、不 trim；版本为正数规范 int64 字符串，不经 Number。

| 调用 | 精确输入 | 完整结果与关系 |
| --- | --- | --- |
| `rename(projectID, documentID, input, write)` / `P/{document_id}/rename` | `{expected_version,title}` | `{document}`；复用安全 12 字段解析，核同 Project/Document、原 title，version 为 expected 或安全 expected+1；不补造 changed |
| `lookup(projectID, documentID, originalInput, write)` / `P/commands/lookup` | `{command:"update",document_id,request:{expected_version,title}}`，原 key | `{state,receipt}`；in_progress/not_observed 必须 receipt:null；committed 为 `{command:"update",document,changed}`，changed 与版本及原 title/目标一致 |

真正改名增加 content_version、置索引 pending、产生 content_changed，ObjectID/正文不变；同名 no-op 不涨版本。Move 不增加内容版本，rename 用事务内实际 parent，客户端不得要求 receipt.parent 等于提交前 parent。文件也可改名，正文 unavailable 不阻止合法 metadata 写入。

Session 唯一持有 identity、Project/Document、捕获输入、原 body、CSRF、key 和命令代次；新增 rename/lookup actions 显式接入 Human current/revision/failure/取消/实际 finally，不落 System/admin，不另造通用命令引擎。controller 仅持草稿、baseline 和安全 progress。构造保持现有位置参数，新增尾 `capabilities: {knowledgeCommands?: KnowledgeCommandsAPI}` 使用有名能力，后继独立 Skills 读口只扩同一对象，不继续增加位置参数。新写须同身份当前 Owner、当前 active Project、当前 active 文档和空闲 Cookie lane；checking、不可用、未定命令或冲突禁止新写。

### 状态、恢复与有界重读

- 完整 typed 响应或合法 committed Lookup 才确认原命令。confirmed 是历史事实，任何后续读取失败不得倒退 rejected 或重发 rename；历史 receipt 不直接成为下一写 baseline。
- 原 Problem 明确 not_started/not_committed 的版本冲突保草稿，重读后由用户显式采用新版本，再新意图/新 key。transport、Unknown、解析或消费失败保原未定意图；用户 Lookup，in_progress 等待，not_observed 可人工原 key/body 重放，committed 后重读。Lookup 失败不改原写结论；IDEMPOTENCY_KEY_REUSED 禁重放/自动换 key。
- same-session checking 暂停写、查证、发布而保未定原材料；同 User/Session/CSRF 恢复后可人工查证。真实 identity/CSRF 变化才清私有材料与发布资格；当前 401/CSRF 失效沿 Session，局部 403/404 不污染 System gate。仍属当前 identity/Project/doc/编辑代次的拒绝使本域观察不可用并清受保护展示，保已确认历史命令事实；旧晚尾不得清新上下文。
- 发布资格同时绑定工作区、所选文档与编辑代次。同项目选别文档、同组件路由参数变化、离页都处理草稿/未定意图的显式放弃确认。放弃退休发布资格并取消原调用，实际 reader/outer/finally 未回仍占 Cookie lane；迟到不改新页面，取消导航不复活已退休请求。

确认后沿原单队列重读所选 metadata→祖先→offset0 正文。同 doc 的 current GET 版本不得低于确认 receipt；更高版本/不同 parent 是合法当前观察，相等也用当前 DTO。tombstone/403/404 如实不可用且保原确认事实；重读失败不采用历史 receipt 充当前数据。

目录刷新固定 root 加提交前 parent、receipt.parent、成功 current GET.parent，去重、null 只一次，非根仅已加载，最多四层首屏。各 parent 只是其时刻观察；获知即清相应 items/cursor，不把 receipt 插旧分页，不无限刷子树或翻页找新位置。current GET 失败只处理已知集合，不猜新 parent；后续明确重读再纳新观察。自动刷新不能重新选回已离开的文档。

### 文件域与首条真实链

新生产源：`web/src/api/knowledge-commands.ts`、`web/src/composables/useKnowledgeRename.ts`、`web/src/components/knowledge/KnowledgeRenameDialog.vue`。旧窄接缝：`web/src/api/client.ts`、`web/src/composables/useSession.ts`、`web/src/composables/useKnowledgeOwner.ts`、`web/src/views/projects/ProjectKnowledgeView.vue`。复用现 `parseKnowledgeDocument`、UiDialog/UiField/UiInput/UiButton/UiState；不改 Workspace/router index/nav/UiTree、Go/backend、锁、D13。首基础测试确认同组件 route update 晚于全局认证恢复，故另授权 `web/src/router/auth.ts` 一个 Knowledge 导航注册点，在原 restore 前询问页面确认并按 owner 注销；保 Project 导航顺序与 Skills canonical 路由闭集，不改其他路由门。必要测试为 command-client、command-state、rename 三文件与受影响读取/会话兼容检查。

基础控制聚焦正常改名、明确冲突、Unknown→原 Lookup/重放及身份/选择变化原尾；完成类型/格式后尽早联调。真实 fixture/共享入口另由 root 分配：default Project initializer 仍 unbound，test-only 真实 ports 造已有文档并 Stop/Drain，普通 Owner 登录→UI 改名→原完整 typed 发布→当前 GET/有界目录刷新，核 title/version、正文原 hash、一次真实 command/event，再闭合全部原浏览器/服务/资源尾。复用未变 read04/backend 证据，不扩阅读方法矩阵。当前仅规格获独立有限接受，代码、纯检查与真实链均待完成。
