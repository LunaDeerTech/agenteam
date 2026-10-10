# D12 Human Owner Knowledge metadata 与文档树 HTTP

状态：五类Human Owner metadata/文档树只读 HTTP adapter 已实现，作者 pure/标准Schema、真实PG四top十四子与native三top六子均按限定组合通过，独立源码和安全投影/I/O风险控制接受，作为独立结果正式交付。开发基线main `29dd429881783595e5a1304b86af5bfb56ec80b2`，本结果基于正式main `ce65714a` 装配，仅新增本卡限定文件。不接生产root，不修改B02/SQL，不包含正文、下载URL、D13、UI或Project lifecycle/Runtime停止项。依据[B02服务](d12-b02-knowledge-service.md)、[D12规格](d12-knowledge-documents-design.md) §3/6与既有Owner HTTP/Account boundary。

## API 与真实依赖

前缀为 `/api/v1/projects/{project_id}/knowledge/documents`；全部只允许 GET/HEAD，通过浏览器/身份前置后其他方法405及 `Allow: GET, HEAD`（未认证或跨Origin仍先401/403），HEAD执行相同授权、查询和完整投影但不写body。

| 路径后缀 | 查询 | 服务 |
| --- | --- | --- |
| 空 | title_query/source_kind/media_type/indexing_status/limit/cursor | ListDocuments |
| `/{document_id}` | 无 | GetDocument |
| `/children` | 必需 parent_document_id；另同列表 | ListChildren |
| `/{document_id}/ancestors` | 无 | ReadAncestors |
| `/search-titles` | title_query/limit/cursor | SearchTitles |

children 的 parent_document_id 必须是字面 `null` 或规范 UUIDv7；不把缺席或空串默认为根。非根由 Service 核同 Project active 父节点。title_query 缺席表示原空筛选，出现则不得空；其它出现参数也不得空。拒绝重复decoded key、未知key、分号、无等号、非法转义/UTF8/控制字符、裸问号及非规范limit。raw query最多32KiB、cursor最多8192B；limit默认50、范围1..200。路径UUID沿foundation精确解析；不接受尾斜线/多余段或静态资源别名。

排序与cursor完全沿B02：title C/ID升序，字面子串，绑定stable user/Project/kind/parent/filter/order，不绑定limit；每页重验当前Session/Owner。不增加generation或快照承诺；并发rename/move后客户端按ID合并并刷新。祖先完整root→parent，不含自身；搜索返回各命中文档的完整祖先，不截断关系。

公开构造只接真实 `*knowledge.Service` 和 `*account.HTTPBoundary`；私有窄接口仅供包内测试。Account负责浏览器来源/身份前置，Service在同Store活Tx的User/Project/tree SH下重验当前Session、Owner、初始化与Read gate。管理员无跨Owner豁免，归档可读，未初始化/Deleting沿原ProjectNotActive。没有生产stub或HTTP自行查表授权。构造不启动恢复/I/O，Service与Account的生命周期归装配者，当前不改默认App root。

## 输出、错误与资源责任

专用Document DTO显式输出id/project_id/parent_document_id/title/content_version/source_kind/media_type/status/indexing_status/created_by/created_at/updated_at，共12字段，绝不直接序列化DocumentRef或包含object_id。creator沿Human或AgentRun原闭集；版本用foundation十进制字符串。详情严格二选一 `{active:...}` 或 `{deleted:{id,project_id,content_version,deleted_at}}`。列表为 `{items:[],next_cursor?:string}`；ancestors为 `{items:[]}`；搜索item为 `{document,ancestors:[]}`。空数组非null，不输出正文/私有来源/上传/lease/对象key/下载token。

先验证全部返回值（含原隐藏ObjectID、Project/目标ID、active、过滤、顺序/重复、完整祖先链），再安全投影和完整编码。表示最多5MiB，超额返回安全DependencyUnavailable，不发布部分前缀或静默截树。服务错误原Fault/CommitState/Cause保持；读COMMIT Unknown不发布候选，后继GET不是原事务确认。统一Account Problem不输出原查询/参数key/错误链/SQL。跨Owner与不存在沿原404，Get的合法最小tombstone为200；非法输入400、身份401、阶段409、未绑定503等沿现闭集。

从浏览器检查前开始原2s总预算，继承更早deadline；包括空请求体最多探测1B、认证、SQL/实际Rows与Tx尾、编码、写入/Flush、body Close和取消callback实际join。设置原生read/write deadline；不支持deadline的writer安全abort，不在失败后改发成功或无界Problem。正常join后清deadline才可复用keepalive。GET/HEAD没有Activity/Audit/Event/receipt写入。

本adapter不调用ReadDocument/OpenCanonical。正文后继负责有界文本UTF8/offset及实际reader Close；PDF/DOCX当前parser未绑定，raw stream不是下载授权。另已报告正式D12 §6的deleted正文410与现B02 source.go返回404差异，本结果不改或重新解释该行为。

## 验收与有限结论

作者纯控制覆盖严格路由/query、安全DTO、隐藏字段canary、坏尾项、原I/O取消/短写/Close与实际callback join；本地标准Schema验证20正反例。首pure失败只因新测试误期待公开cause_id，保留原失败并定向修正测试。独立Runner有限源码审查通过，HEAD错误Schema声明body的must-fix经原红控制转绿；两个独立实际pure风险控制验证AgentRun安全投影/跨Project拒绝/坏末项零候选，以及Body.Close后仍等取消callback实际退出，无剩余已知must-fix。

PG `TestKnowledgeOwnerReadHTTP{Metadata,CurrentAuthority,Transactions,CommitUnknown}` 四top十四子在75649/8181d0完整通过：真实Account正式登录/注销、B02同Store五GET/HEAD、非空树/字面搜索/分页、tombstone、安全Schema/无读事实、每页当前Owner/Session、两个原User SH/EX锁序、原SELECT取消与Tx实际退出、两种真实complete-frame COMMIT Unknown零候选。Project初始化/Owner变化/合法Deleting为明确上游SQL fixture，不冒Project.Create/BeginDelete/Skills。原Go6m、每top含fixture尾120s不变；Go/driver/outer实际Wait0、七ID/三private/runtime/desc/TCP双尾与542inputsame齐，supervisor129.994s。

native `TestKnowledgeHTTPNative{Deadlines,KeepAliveAndClose,BackpressureAndDisconnect}` 三top六子在35786/517ab4完整通过：自然2s与更早parent deadline、同连接keepalive清deadline、实际Body.Close错误、真实写背压/断连与原callback实际join。测试使用明确的局部authority/domain控制，仅证明传输；正式权限由PG矩阵证明。原Go90s/driver105s/supervisor123+共享3s/TCP75不变，三父六子各恰一次RUN/PASS、test/driver/outer实际Wait0、private/runtime/desc/TCP/input全尾齐，supervisor67.322s。两个真实窗口均已释放，不再追加矩阵或重复已通过场景。

Schema测试运行时必须显式设置 `AGENTEAM_KNOWLEDGE_HTTP_SCHEMA_PYTHON` 为已安装 `jsonschema`（Draft202012Validator）与 `referencing` 的本地Python；helper只读本地两个Schema，不做网络引用。纯Schema测试在未配置时跳过，PG的实际响应Schema验证在缺少解释器时明确失败；不以跳过计验收。native须显式 `AGENTEAM_KNOWLEDGE_HTTP_NATIVE=1`，默认跳过。验收原精确命令/工具与失败记录留任务current/分支，不是生产运行依赖。

## 最小正式文件范围

共15个新增技术文件和本卡/台账D12行（17路径），无共享生产源patch：

| 范围 | 精确文件 |
| --- | --- |
| 新HTTP包（10） | `internal/central/knowledge/http/handler.go`、`query.go`、`wire.go`、`io.go`、`handler_test.go`、`query_test.go`、`wire_test.go`、`io_test.go`、`schema_test.go`、`native_test.go` |
| 专用Schema（1） | `api/openapi/knowledge-owner.json` |
| 真实PG矩阵（3） | `tests/knowledge/owner_read_http_fixture_test.go`、`owner_read_http_test.go`、`owner_read_http_transactions_test.go` |
| 测试动态依赖（1） | `.agent-state/knowledge-owner-read/schema-controls.py`；它引用上述专用Schema及main已有的 `api/openapi/common.json`，不得漏交 |
| 正式说明（2） | 本卡；`docs/development/agent-team/tasks.md` 仅D12单行，保留main其他行 |

不携带整套WIP supervisor/native driver/selector controls覆盖main；编译产物、MinIO、日志、缓存不入正式结果。root构造仍未绑定，后继负责Service/Account生命周期与实际App装配；本结果不恢复Object Runtime join或增加下载、正文、写命令/创建/上传能力。完整D12仍未完成。
