# D12 Knowledge 文档与文档树

状态：B01 纯契约、[B02 Human canonical/树服务](d12-b02-knowledge-service.md)、[Owner metadata/树 HTTP](d12-knowledge-owner-read-http.md)、[树命令 HTTP](d12-knowledge-owner-tree-http.md)与[有界正文 HTTP](d12-knowledge-owner-content-http.md)已分别完成有限交付；正文卡 §6 记录默认 root 的有限真实组合通过。下一片段为下述 Owner 只读 UI，当前仅规划，尚未实现或验收；D12 整体与既有 STOP 未关闭。下列 B01/C1 段落保留当时记录，后续实际状态以相应子卡为准。

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

## 下一独立交付：Owner 文档树与正文读取 UI（规划，未实现）

本段只落实[知识库布局](../../frontend-design/layouts/knowledge-base.md)和[项目工作台](../../frontend-design/layouts/project-workspace.md)中已有文档的读取部分：当前 Owner 进入项目知识库，分页展开文档树，选择父文档或子文档，查看元数据、祖先路径和当前有界正文。默认不选中文档；空库显示“暂无文档”。普通入口不恢复旧选择，指定文档链接按当前权限重新定位。首片段可独立交付，不等待完整知识库编辑器。

实施使用 root 已创建的独立 `ai/knowledge-owner-ui` 树；前端执行者唯一写本段列出的客户端、会话接缝、组件与页面，配套真实 fixture 由指定后端/测试写者负责，共享 harness 仍由原 owner 集成。本轮仅在新树更新本卡，代码尚未开工。无迁移、新后端契约或生产 root 改动；保留 Object Runtime join、来源获取及其他既有 STOP。

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

实施先交四方法客户端、Session 接缝和一个可运行页面，完成定向基础检查后尽早联调，不等检索、编辑或全布局完成。建议新增 `internal/central/app/knowledge_owner_web_test.go` 的 integration top `TestKnowledgeOwnerReadWeb`，配 `tests/account-captcha-web/e2e/knowledge-owner-read.spec.ts`，消费实际默认 `bindAccounts` 实例和锁定 Playwright。fixture 沿已验 root 组合方法：正式 Account bootstrap/Login、原 Project.Service.CreateProject→同 Skill 初始化确认，再由同 Knowledge.Service 创建含多层树的真实文本。仅测试准备使用这些正式服务，不插业务 SQL，也不宣称已有 Project/Knowledge Create HTTP。

首个正常 case 从真实登录后的项目入口进入知识库，确认默认未选中，展开父文档、选择子文档、核真实 metadata/祖先及首段 UTF-8 文本，再取下一段核实际 byte offset；同时验证父文档也可选读。先以小 fixture 打通这一次原客户端→Session→HTTP→B02/D05→原响应完整消费→页面发布。沿既有 task-owned 静态 dist/同源反代测试入口与七资源 fixture；它们不是 SPA 生产发布。浏览器、Go/driver/outer、原请求消费和取消尾、领域/root 退役与自有资源/TCP尾全部结束才算该次完成，HTTP 200、native EOF 或截图单独不算 UI 发布。

与本片段风险对应的增量检查限定为：四端点完整/坏尾表示与偏移解析；根/子级 cursor、空树/空正文/tombstone/unavailable；当前 Owner 拒绝与 archived 读取；切换文档/项目或身份、held reader/cancel/finally 时禁止迟到发布及提前放行下一 Cookie 请求。复用未变后端权限/native/root 证据，不重跑其全矩阵。真实浏览器补窄屏 Drawer、键盘选择/展开、焦点恢复、长文本滚动和现有明暗/reduced-motion样式；独立验证者只核新权限、消费/发布和页面接缝。具体 selector、预算、输入和资源窗口由实施者冻结后交 root 调度，当前未运行任何新增 UI 检查。
