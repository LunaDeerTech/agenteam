# D12 Human Owner Knowledge metadata 与文档树 HTTP

状态：实施中，基线正式 main `29dd429881783595e5a1304b86af5bfb56ec80b2`。只交付可独立构造的五类只读 HTTP adapter，不接生产 root，不修改 B02/SQL，不包含正文、下载 URL、D13、UI 或 Project lifecycle/Runtime 停止项。依据 [B02 服务](d12-b02-knowledge-service.md)、[D12 规格](d12-knowledge-documents-design.md) §3/6 与既有 Owner HTTP/Account boundary。

## API 与真实依赖

前缀为 `/api/v1/projects/{project_id}/knowledge/documents`；全部只允许 GET/HEAD，其他方法405及 `Allow: GET, HEAD`，HEAD执行相同授权、查询和完整投影但不写body。

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

## 写域与验收

唯一写域：新 `internal/central/knowledge/http/{handler,query,wire,io}.go` 及相邻pure/native tests，新 `api/openapi/knowledge-owner.json`，新 `tests/knowledge/owner_read_http*`，本卡/current及必要任务资产。复用既有有界HTTP I/O模式，不改共享框架/全局schema/原B02产品/SQL/生产root。Schema覆盖实际DTO与五路参数/错误，不声明其他Knowledge能力。

有限矩阵：① pure严格路由/query/DTO/隐藏字段canary/坏尾项/安全Fault与schema、原I/O取消/短写/Close/实际callback；② native socket慢body/慢写/断连/HEAD/keepalive/原2s自然期限与实际尾；③真实同Store Account正式登录、当前Project Owner、B02真实服务的五API/非空分页树/删除tombstone、每页撤权与两种锁序、取消/真实读COMMIT Unknown零候选。公共内容变更用于产生业务事实，不复制B02全量发布矩阵。PG/native均另需root独占窗口；pure/编译不冒动态通过。未参与实现者独立核安全投影/权限与有价值负控后才交付该有限adapter。
