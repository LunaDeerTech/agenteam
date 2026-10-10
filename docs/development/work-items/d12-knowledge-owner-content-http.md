# D12 Human Owner 有界正文 HTTP

状态：有限正文后端结果已通过。HTTP9top纯race（17个实际响应Schema向量）、领域3top15sub race与两pkg vet通过；native3top6sub真实race与完整尾通过。PG01原整组FAIL保留：两GET小正文可在ReadObject返回前同步预读EOF并释放lease，原活lease前置未成立，原计数未打印不推测为0。只修两GET为131073B真实正文并核Meta ID/size/digest，保active==1及后继全部断言；Skills窄独审接受，新候选编/list及Object既有holdback方法控通过。PG02在冻结 `1e5833bc` 使用原03候选，同4top14sub全部真实RUN/PASS，Go/driver/outer实际Wait0，七资源、private/runtime/desc/TCP双尾及input初尾全部齐全（212.663s/0）。产品与方法独立有限审查均已接受，早期构建FAIL/PG01FAIL材料保留，命令及边界见 [固定本域恢复入口](https://github.com/LunaDeerTech/agenteam/blob/9b9d1e7cc367a1adfef357af5fbd6e56b4620ada/.agent-state/knowledge-content-http/README.md)。后续默认根既有数据读取与默认创建unbound的有限组合通过，原initializer接入的规范拒绝另记于 §6；UI与Object Runtime全局join停止项不变。基线 main `3b7ed9da35844e3a367cc5e9da0cf424ab36499a`。原作者 Variables UI；依据 [D12 合同](d12-knowledge-documents-design.md) §3/5/6、[B02 Service](d12-b02-knowledge-service.md)与已交付 [metadata HTTP](d12-knowledge-owner-read-http.md)。正文包只提供当前正文的有界结构化读取，不包含写入、下载 URL、原文件流、D13 parser 或 UI。

## 1. 唯一入口与依赖

新增独立 `internal/central/knowledge/contenthttp`；公开 `NewHTTPHandler(*knowledge.Service, *account.HTTPBoundary) (http.Handler, error)` 与 `HandlesPath(string) bool`。构造拒 nil，不启动 I/O 或后台任务；实际服务初始化、Stop/Drain 与关闭仍由装配者负责。包内窄 `ReadDocument` 端口仅用于明确的局部控制，公开构造只接真实服务。

唯一 `/api/v1/projects/{project_id}/knowledge/documents/{document_id}/content`，GET/HEAD。Project/Document 必须规范 UUIDv7；不接尾斜线、额外 path 段。与既有 metadata/五 POST HandlesPath 不交叠。先原 Account `CheckRequest`/`RequireHuman`，后 method/path/query；错误 method 405/`Allow: GET, HEAD`，未认证或来源非法仍按原前置拒绝。管理员不替代当前 Human Owner。

HEAD 执行同 GET 的当前授权、实际正文读取、Close 和完整安全编码，成功及 Problem 都不写 body，Content-Length 对应同一表示；不能只 Stat 或用旧 metadata 冒正文成功。GET/HEAD 不接受请求 body，按既有读 adapter 最多探测 1B 并实际 Close；拒请求 Content-Encoding、Range/If-Range，不实现 HTTP byte-range/206。总 RawQuery 上限 256B；空裸 `?`、未知/重复 decoded key、坏转义、空值、非规范十进制均 400，不接受业务 key/版本/locator。

| 可选 query | 默认与范围 |
| --- | --- |
| `byte_offset` | `0`；规范非负十进制，沿 `foundation.Progress` 的 int64 范围 |
| `max_bytes` | `65536`（64 KiB）；规范正十进制 `1..1048576`（1 MiB） |

调用一次正式 `ReadDocument(ctx, actor, project, document, request)`；它拥有 Get/OpenCanonical 当前权限、对象读与实际 Close。HTTP 不直接调用 raw Object API、不查表、不实现第二套 UTF-8 切片或把 reader 暴露给浏览器。

## 2. 响应、安全投影与状态

成功 200，`application/json`、`Cache-Control: no-store`、精确 Content-Length；不签 token、不输出 ETag/历史版本能力。先验证完整 `DocumentContent`，再校请求 Project/Document、active 与当前返回版本，最后显式投影，不能直接 Marshal 带 ObjectID 的领域 DTO。

`document` 与已交付 metadata DTO 同形：id/project_id/parent_document_id/title/content_version/source_kind/media_type/status/indexing_status/created_by/created_at/updated_at，共 12 个公开字段。creator 沿 Human/AgentRun 闭集，版本与 byte offset 为十进制字符串。新包自有安全投影，不改既有 adapter。

正文严格二选一：

- text/Markdown 或 plain：`{"document":安全元数据,"text":{"text":"…","next_byte_offset":"…","truncated":false}}`。按返回 UTF-8 实际字节检查 `len(text)≤max_bytes` 与 `next_byte_offset=原offset+len(text)`，防溢出；不根据 title 或 indexing 状态伪造内容。有效多字节字符不可截断；offset 在字符中间或超过 EOF 沿 Service 的 InvalidArgument。EOF 上 offset 返回空文本/原 offset/false；max_bytes 小于首字符时可返回空文本/原 offset/true，客户端需增加预算，HTTP 不擅自推进 offset。
- PDF/DOCX 文件：`{"document":安全元数据,"unavailable":"dependency_unbound"}`。当前没有 parser/readable provider，不能返回假文本、旧解析版本、ObjectID 或原二进制。只接受这次实际已绑定行为；未实现的 processing/failed/file-ref 分支不得因 DTO 合法就冒可用。

完整 JSON 编码上限 7 MiB（覆盖 1 MiB 文本的 JSON 最坏转义与有限 metadata），超限/跨 Project/错误 target/坏 union/不一致 offset 为安全 DependencyUnavailable，零候选发布。只输出请求授权的文本及明确 metadata；无 object/upload/lease/内部来源/Secret/token/SQL/错误链。原服务 Fault/CommitState/Cause 保持，Unknown 不发布读取候选或被下一 GET 冒确证；读操作没有 command/receipt/Audit/Event/Activity 新事实。

## 3. 当前权限与最小领域修正

每次请求由真实 B02/Project/Account 在活 Store Tx 中重验当前 Session、当前 Owner、initialized 与 Read gate；archived 可读，Deleting/未初始化拒绝，foreign Project/非 Owner/缺行保原 NotFound。已删正文按 D12 §6 返回 `ResourceDeleted`（统一 Problem 410），metadata tombstone 查询仍是原 200。

原 `source.go` 的 `ReadDocument` 与其调用的 `OpenCanonical`，各在成功 `GetDocument` 后 `head.Active==nil` 返回 NotFound。这两处已经由领域完成当前权限与同 Project 查询，结果只会是合法 tombstone；二次 OpenCanonical 还可能在两次查询间首次观察到删除。现已仅将**同一文件这两个精确出口**改成 `ResourceDeleted`，让明确已删除正文不因竞态退回 404。HTTP 不将任意 404 改为 410；数据库缺行、foreign、权限、Object 端口故障继续原错误。

打开 reader 后的 current version/ObjectID/active 复查仍是原 VersionConflict，并先实际 Close；不改 SourceResolver、新 source、preview 或其他 B02 分支。本次没有权限扩大或公开合同变更。领域两出口方案经 Work 独立审后实施；定向控制已实证 authorized tombstone 410、missing/foreign/拒权保持原错误且 Objects 未被调用、两次读取间删除正确分类及 typed reader Read/Close 的原错误、阻塞和实际退役。这些控制使用真实 Service 与明确 Store/authority/body 替身，不是实际 PG 或 D05 lease 证明。

## 4. 原总预算与资源完成

从浏览器检查前建立同一 2s 总预算，继承更早 parent deadline，包含请求体、认证/当前权限、SQL Rows/Tx、跳过 offset、实际对象 Read、reader.Close、完整编码、原生写出/Flush、请求 Body.Close 与取消 callback 的实际 join。不得重开计时或在超时后追加重试；大 offset 的处理同样受这次原 context 约束。

复用既有有界 HTTP I/O 方法做包内适配；不支持原生 read/write deadline 时 abort，不开无界 fallback。必须等原 Service 调用及底层同步 Close 返回，不能因取消信号、EOF、handler 数为零或另开的 goroutine 超时而宣称完成。Read 与 Close 的原错误均保留；未实际 join 的调用仍未结束，不把它作为成功或可复用连接。失败/超时/短写/Flush/Close 错误 abort，无备用成功；正常完成原尾后才清 deadline 允许 keepalive。

不改变 D05/B02 的实际 reader lease/Stop/Drain，不以 HTTP 测试恢复全局 Object Runtime join 停止项，也不承诺已发出的字节可召回。

## 5. 有限验收与写域

验收按风险分层，不以编译或替身代替真实资源：

1. 实际领域定向纯控：上述两处 tombstone/缺行/权限与竞态；原 ReadText 的中英文、UTF-8 边界/EOF/小预算规则复用未变控制，新增 HTTP 参数和实际返回字节的交叉检查。
2. 本包 pure/标准 Schema：精确路由/decoded 重复 query/整数边界/当前 actor、HEAD 成功与 Problem 无 body、完整安全 DTO/隐藏 canary/坏末字段、PDF/DOCX unavailable、原服务错误与 Unknown 零候选。
3. 原 I/O 与 native：held Read/Close/取消 callback 先证明 pending、原实际返回后才结束；parent/自然 2s、慢 body/写背压、断连、keepalive 正常清 deadline 与失败不复用。native 控制的领域替身只证明传输，不替代 SQL 权限。
4. 真实 PG+Account+B02+D05：规范 Login/当前Owner、UTF-8 真实 canonical 文本与分页、PDF/DOCX不可读、授权 tombstone410/foreign404/归档读取、Session/Owner撤权及原锁序、真实 reader/Tx 退出和零业务写事实；原七资源及 test/driver/outer Wait、private/runtime/desc/TCP/input 完整尾。Project 初始化/Owner变更如用上游 SQL fixture须明确，不冒生产创建/转移API。

新增写域：`internal/central/knowledge/contenthttp/**`、`api/openapi/knowledge-content.json`、`tests/knowledge/owner_content_http*`、本卡/current 与必要本域可恢复测试工具。领域配套限 `source.go` 上述两出口及新增相邻 `content_source_test.go`，不重构旧测试。无新依赖、锁文件、迁移、metadata/五 POST/root 改动。Go/真实 PG/native/socket/新缓存仍由 root 按资源窗口另授，未执行不得称验收。

首领域纯测试原 FAIL 保留：测试夹具用 reflect.DeepEqual 比较含函数闭包的 LockKey，提前拒绝相等锁；已改正式 CompareLockKeys 配数量/顺序/Mode，Fault 也按原 cause/code/NotCommitted 检查而非被事务复制的指针。仅重跑受影响三 top 后通过。独审发现的 HEAD Problem 缺 headers、native 背压未断言原 Write Timeout 已分别补齐；前者已由实际 Schema 向量验证，后者已在 native01 观察原 Write 返回的 Timeout 并通过，原问题保留。

## 6. 默认根的有限读取组合

默认构造共用 Store、Account、Project Authority、Object 与 ProcessGuard；Knowledge metadata、树命令和正文 handlers 使用同一个 Knowledge.Service 与 AccountBoundary，SourceReads 沿正式 B02/D05。新必填 Confirmation keyring、固定 owner 路由与原事务内的 Maintenance owner 发现均保正式权限边界；本批不开放生产 Project 创建或完整生命周期。

原 `649e6ad3` 单 top 的业务及退出尾确曾完整 PASS（用例6.63s，总105.502s，Go179551/driver177729/outer177706 均0）。后续规范独审确认默认 initializer 未满足 D08 §7、D10 §§1、14 的完整 participant/guard 启用门；该接入接受已撤回，不能以内部调用成功或未开放 HTTP 豁免。原执行事实保留于[固定历史记录](https://github.com/LunaDeerTech/agenteam/blob/f3904ab7/.agent-state/owner-root-composition/root-first-pass.json)，不代表当前合规创建能力。

`0747b02c` 撤回生产 initializer 后，新单 top 先通过正式 Login，证明默认 Create 精确 `DEPENDENCY_UNBOUND`/`not_committed` 且13项相关事实均零；再用显式 test-only Project 服务沿原 Store、Audit、Skill/Object 真实端口建立既有数据，实际 Stop/Drain/Joined 后才运行默认读链。测试未替换默认 Project 服务或用 SQL 植入 ready。真实 Skill 包原 Close、Knowledge正文GET/HEAD字节及长度、Skills目录、Avatar PUT/GET/DELETE和原SIGTERM/ProcessGuard退出均在本限定范围。

root-02 用例7.39s；Go243371、driver241594、outer241447/session7494 实际终态均0，总111.577s，七资源/private/runtime/desc/TCP双尾及inputs一致齐。此结果只接受默认既有数据读取与创建unbound边界；不声明 Project Create HTTP、SPA、生产就绪、全 Project/Lease/Transfer/Execution 根入口或 Object Runtime 全局 join 已完成。原正文 PG01 FAIL 与原 root-01 接入规范拒绝分别保留。
